package tests

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/termix/termix/go/internal/relay"
	"github.com/termix/termix/go/internal/relayclient"
	"github.com/termix/termix/go/internal/relayproto"
	"github.com/termix/termix/go/internal/session"
)

type authCall struct {
	accessToken string
	sessionID   string
}

type acquireCall struct {
	accessToken string
	sessionID   string
}

type releaseCall struct {
	accessToken  string
	sessionID    string
	leaseVersion int64
}

type fakeSessionAuthorizer struct {
	mu         sync.Mutex
	calls      []authCall
	acquires   []acquireCall
	releases   []releaseCall
	denyNext   bool
	acquireErr error
	renewErr   error
	releaseErr error
}

func (f *fakeSessionAuthorizer) AuthorizeWatch(_ context.Context, accessToken string, sessionID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, authCall{accessToken: accessToken, sessionID: sessionID})
	return nil
}

func (f *fakeSessionAuthorizer) AcquireControl(_ context.Context, accessToken string, sessionID string) (relay.ControlGrant, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.acquires = append(f.acquires, acquireCall{accessToken: accessToken, sessionID: sessionID})
	if f.acquireErr != nil {
		return relay.ControlGrant{}, f.acquireErr
	}
	if f.denyNext {
		f.denyNext = false
		return relay.ControlGrant{}, relay.ErrControlDenied{
			Reason:  "already_controlled",
			Message: "control lease is held",
		}
	}
	return relay.ControlGrant{
		SessionID:         sessionID,
		LeaseVersion:      1,
		ExpiresAt:         time.Now().Add(30 * time.Second),
		RenewAfterSeconds: 15,
	}, nil
}

func (f *fakeSessionAuthorizer) RenewControl(_ context.Context, accessToken string, sessionID string, leaseVersion int64) (relay.ControlGrant, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.acquires = append(f.acquires, acquireCall{accessToken: accessToken, sessionID: sessionID})
	if f.renewErr != nil {
		return relay.ControlGrant{}, f.renewErr
	}
	return relay.ControlGrant{
		SessionID:         sessionID,
		LeaseVersion:      leaseVersion + 1,
		ExpiresAt:         time.Now().Add(30 * time.Second),
		RenewAfterSeconds: 15,
	}, nil
}

func (f *fakeSessionAuthorizer) ReleaseControl(_ context.Context, accessToken string, sessionID string, leaseVersion int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.releaseErr != nil {
		return f.releaseErr
	}
	f.releases = append(f.releases, releaseCall{
		accessToken:  accessToken,
		sessionID:    sessionID,
		leaseVersion: leaseVersion,
	})
	return nil
}

func (f *fakeSessionAuthorizer) hasCall(accessToken string, sessionID string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, call := range f.calls {
		if call.accessToken == accessToken && call.sessionID == sessionID {
			return true
		}
	}
	return false
}

func TestRelayWatchHandshakeRequestsSnapshotAndFansOutFrames(t *testing.T) {
	authorizer := &fakeSessionAuthorizer{}
	server := relay.NewServer(authorizer)
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	daemonConn, _, err := websocket.Dial(ctx, "ws"+httpServer.URL[len("http"):]+"/ws", nil)
	if err != nil {
		t.Fatalf("dial daemon: %v", err)
	}
	defer daemonConn.Close(websocket.StatusNormalClosure, "done")

	writeEnvelope(t, ctx, daemonConn, relayproto.Envelope{
		Type:    relayproto.TypeHelloDaemon,
		Payload: map[string]any{"device_id": "device-1"},
	})
	writeEnvelope(t, ctx, daemonConn, relayproto.Envelope{
		Type:    relayproto.TypeSessionOnline,
		Payload: map[string]any{"session_id": "session-1"},
	})

	viewerOne := watchViewer(t, ctx, httpServer.URL, "viewer-token-1")
	defer viewerOne.Close(websocket.StatusNormalClosure, "done")
	readEnvelope(t, ctx, daemonConn, relayproto.TypeSessionSnapshotReq)

	viewerTwo := watchViewer(t, ctx, httpServer.URL, "viewer-token-2")
	defer viewerTwo.Close(websocket.StatusNormalClosure, "done")
	readEnvelope(t, ctx, daemonConn, relayproto.TypeSessionSnapshotReq)

	if !authorizer.hasCall("viewer-token-1", "session-1") {
		t.Fatal("expected viewer one to be authorized")
	}
	if !authorizer.hasCall("viewer-token-2", "session-1") {
		t.Fatal("expected viewer two to be authorized")
	}

	writeEnvelope(t, ctx, daemonConn, relayproto.Envelope{
		Type:    relayproto.TypeSessionSnapshotReady,
		Payload: map[string]any{"session_id": "session-1"},
	})
	writeBinaryFrame(t, ctx, daemonConn, relayproto.BinaryFrame{
		FrameType: relayproto.FrameTypeSnapshotChunk,
		Header: map[string]any{
			"session_id": "session-1",
			"seq":        1,
			"is_last":    true,
		},
		Payload: []byte("snapshot"),
	})
	writeBinaryFrame(t, ctx, daemonConn, relayproto.BinaryFrame{
		FrameType: relayproto.FrameTypeTerminalOutput,
		Header: map[string]any{
			"session_id": "session-1",
			"seq":        2,
		},
		Payload: []byte("live-output"),
	})

	assertViewerFrames(t, ctx, viewerOne)
	assertViewerFrames(t, ctx, viewerTwo)
}

func TestRelayControlLeaseAllowsOnlyControllerInput(t *testing.T) {
	authorizer := &fakeSessionAuthorizer{}
	server := relay.NewServer(authorizer)
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	daemonConn, _, err := websocket.Dial(ctx, "ws"+httpServer.URL[len("http"):]+"/ws", nil)
	if err != nil {
		t.Fatalf("dial daemon: %v", err)
	}
	defer daemonConn.Close(websocket.StatusNormalClosure, "done")

	writeEnvelope(t, ctx, daemonConn, relayproto.Envelope{
		Type:    relayproto.TypeHelloDaemon,
		Payload: map[string]any{"device_id": "device-1"},
	})
	writeEnvelope(t, ctx, daemonConn, relayproto.Envelope{
		Type:    relayproto.TypeSessionOnline,
		Payload: map[string]any{"session_id": "session-1"},
	})

	controller := watchViewer(t, ctx, httpServer.URL, "controller-token")
	defer controller.Close(websocket.StatusNormalClosure, "done")
	readEnvelope(t, ctx, daemonConn, relayproto.TypeSessionSnapshotReq)

	watcher := watchViewer(t, ctx, httpServer.URL, "watcher-token")
	defer watcher.Close(websocket.StatusNormalClosure, "done")
	readEnvelope(t, ctx, daemonConn, relayproto.TypeSessionSnapshotReq)

	writeEnvelope(t, ctx, controller, relayproto.Envelope{
		Type:      relayproto.TypeControlAcquire,
		RequestID: "acquire-1",
		Payload:   map[string]any{"session_id": "session-1"},
	})
	granted := readEnvelope(t, ctx, controller, relayproto.TypeControlGranted)
	if granted.RequestID != "acquire-1" {
		t.Fatalf("expected acquire request id acquire-1, got %q", granted.RequestID)
	}

	writeBinaryFrame(t, ctx, watcher, relayproto.BinaryFrame{
		FrameType: relayproto.FrameTypeTerminalInput,
		Header: map[string]any{
			"session_id":    "session-1",
			"encoding":      "raw",
			"lease_version": int64(1),
		},
		Payload: []byte("blocked\n"),
	})
	readEnvelope(t, ctx, watcher, relayproto.TypeError)

	writeBinaryFrame(t, ctx, controller, relayproto.BinaryFrame{
		FrameType: relayproto.FrameTypeTerminalInput,
		Header: map[string]any{
			"session_id":    "session-1",
			"encoding":      "raw",
			"lease_version": int64(1),
		},
		Payload: []byte("allowed\n"),
	})
	input := readBinaryFrame(t, ctx, daemonConn, relayproto.FrameTypeTerminalInput)
	if string(input.Payload) != "allowed\n" {
		t.Fatalf("expected daemon input payload %q, got %q", "allowed\n", input.Payload)
	}

	writeEnvelope(t, ctx, controller, relayproto.Envelope{
		Type:      relayproto.TypeControlRelease,
		RequestID: "release-1",
		Payload: map[string]any{
			"session_id":    "session-1",
			"lease_version": int64(1),
		},
	})
	revoked := readEnvelope(t, ctx, controller, relayproto.TypeControlRevoked)
	if revoked.RequestID != "release-1" {
		t.Fatalf("expected release request id release-1, got %q", revoked.RequestID)
	}

	writeBinaryFrame(t, ctx, controller, relayproto.BinaryFrame{
		FrameType: relayproto.FrameTypeTerminalInput,
		Header: map[string]any{
			"session_id":    "session-1",
			"encoding":      "raw",
			"lease_version": int64(1),
		},
		Payload: []byte("after-release\n"),
	})
	readEnvelope(t, ctx, controller, relayproto.TypeError)
}

func TestRelayControlInputBackendLoop(t *testing.T) {
	authorizer := &fakeSessionAuthorizer{}
	server := relay.NewServer(authorizer)
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	type inputCall struct {
		sessionID string
		payload   []byte
	}
	inputCalls := make(chan inputCall, 1)

	daemonClient := relayclient.New("ws"+httpServer.URL[len("http"):]+"/ws", "daemon-token", "device-1")
	daemonClient.SetInputHandler(func(_ context.Context, sessionID string, payload []byte) error {
		inputCalls <- inputCall{
			sessionID: sessionID,
			payload:   append([]byte(nil), payload...),
		}
		return nil
	})
	if err := daemonClient.Connect(ctx); err != nil {
		t.Fatalf("connect daemon relay client: %v", err)
	}
	if err := daemonClient.AnnounceSession(ctx, session.LocalSession{SessionID: "session-1"}); err != nil {
		t.Fatalf("announce session: %v", err)
	}

	viewer := watchViewer(t, ctx, httpServer.URL, "viewer-token")
	defer viewer.Close(websocket.StatusNormalClosure, "done")

	writeEnvelope(t, ctx, viewer, relayproto.Envelope{
		Type:      relayproto.TypeControlAcquire,
		RequestID: "acquire-loop",
		Payload:   map[string]any{"session_id": "session-1"},
	})
	granted := readEnvelope(t, ctx, viewer, relayproto.TypeControlGranted)
	if granted.RequestID != "acquire-loop" {
		t.Fatalf("expected acquire request id acquire-loop, got %q", granted.RequestID)
	}

	writeBinaryFrame(t, ctx, viewer, relayproto.BinaryFrame{
		FrameType: relayproto.FrameTypeTerminalInput,
		Header: map[string]any{
			"session_id":    "session-1",
			"encoding":      "raw",
			"lease_version": int64(1),
		},
		Payload: []byte("whoami\n"),
	})

	select {
	case got := <-inputCalls:
		if got.sessionID != "session-1" {
			t.Fatalf("expected session_id session-1, got %q", got.sessionID)
		}
		if string(got.payload) != "whoami\n" {
			t.Fatalf("expected payload %q, got %q", "whoami\n", got.payload)
		}
	case <-ctx.Done():
		t.Fatalf("timed out waiting for daemon input handler: %v", ctx.Err())
	}
}

func TestRelayControlAcquireDenied(t *testing.T) {
	authorizer := &fakeSessionAuthorizer{denyNext: true}
	server := relay.NewServer(authorizer)
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	daemonConn, _, err := websocket.Dial(ctx, "ws"+httpServer.URL[len("http"):]+"/ws", nil)
	if err != nil {
		t.Fatalf("dial daemon: %v", err)
	}
	defer daemonConn.Close(websocket.StatusNormalClosure, "done")

	writeEnvelope(t, ctx, daemonConn, relayproto.Envelope{
		Type:    relayproto.TypeHelloDaemon,
		Payload: map[string]any{"device_id": "device-1"},
	})
	writeEnvelope(t, ctx, daemonConn, relayproto.Envelope{
		Type:    relayproto.TypeSessionOnline,
		Payload: map[string]any{"session_id": "session-1"},
	})

	viewer := watchViewer(t, ctx, httpServer.URL, "viewer-token")
	defer viewer.Close(websocket.StatusNormalClosure, "done")
	readEnvelope(t, ctx, daemonConn, relayproto.TypeSessionSnapshotReq)

	writeEnvelope(t, ctx, viewer, relayproto.Envelope{
		Type:      relayproto.TypeControlAcquire,
		RequestID: "acquire-denied",
		Payload: map[string]any{
			"session_id": "session-1",
		},
	})
	denied := readEnvelope(t, ctx, viewer, relayproto.TypeControlDenied)
	if denied.RequestID != "acquire-denied" {
		t.Fatalf("expected request id acquire-denied, got %q", denied.RequestID)
	}
	reason, _ := denied.Payload["reason"].(string)
	if reason != "already_controlled" {
		t.Fatalf("expected reason already_controlled, got %q", reason)
	}
}

func TestRelayControlRenew(t *testing.T) {
	authorizer := &fakeSessionAuthorizer{}
	server := relay.NewServer(authorizer)
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	daemonConn, _, err := websocket.Dial(ctx, "ws"+httpServer.URL[len("http"):]+"/ws", nil)
	if err != nil {
		t.Fatalf("dial daemon: %v", err)
	}
	defer daemonConn.Close(websocket.StatusNormalClosure, "done")

	writeEnvelope(t, ctx, daemonConn, relayproto.Envelope{
		Type:    relayproto.TypeHelloDaemon,
		Payload: map[string]any{"device_id": "device-1"},
	})
	writeEnvelope(t, ctx, daemonConn, relayproto.Envelope{
		Type:    relayproto.TypeSessionOnline,
		Payload: map[string]any{"session_id": "session-1"},
	})

	controller := watchViewer(t, ctx, httpServer.URL, "controller-token")
	defer controller.Close(websocket.StatusNormalClosure, "done")
	readEnvelope(t, ctx, daemonConn, relayproto.TypeSessionSnapshotReq)

	writeEnvelope(t, ctx, controller, relayproto.Envelope{
		Type:      relayproto.TypeControlAcquire,
		RequestID: "acquire-renew-1",
		Payload:   map[string]any{"session_id": "session-1"},
	})
	readEnvelope(t, ctx, controller, relayproto.TypeControlGranted)

	writeEnvelope(t, ctx, controller, relayproto.Envelope{
		Type:      relayproto.TypeControlRenew,
		RequestID: "renew-invalid-float",
		Payload: map[string]any{
			"session_id":    "session-1",
			"lease_version": 1.5,
		},
	})
	invalidFloat := readEnvelope(t, ctx, controller, relayproto.TypeControlDenied)
	invalidFloatReason, _ := invalidFloat.Payload["reason"].(string)
	if invalidFloatReason != "invalid_request" {
		t.Fatalf("expected invalid_request for float lease_version, got %q", invalidFloatReason)
	}

	writeEnvelope(t, ctx, controller, relayproto.Envelope{
		Type:      relayproto.TypeControlRenew,
		RequestID: "renew-invalid-zero",
		Payload: map[string]any{
			"session_id":    "session-1",
			"lease_version": int64(0),
		},
	})
	invalidZero := readEnvelope(t, ctx, controller, relayproto.TypeControlDenied)
	invalidZeroReason, _ := invalidZero.Payload["reason"].(string)
	if invalidZeroReason != "invalid_request" {
		t.Fatalf("expected invalid_request for zero lease_version, got %q", invalidZeroReason)
	}

	writeEnvelope(t, ctx, controller, relayproto.Envelope{
		Type:      relayproto.TypeControlRenew,
		RequestID: "renew-1",
		Payload: map[string]any{
			"session_id":    "session-1",
			"lease_version": int64(1),
		},
	})
	renewed := readEnvelope(t, ctx, controller, relayproto.TypeControlGranted)
	leaseVersion, ok := renewed.Payload["lease_version"].(float64)
	if !ok || int64(leaseVersion) != 2 {
		t.Fatalf("expected renewed lease_version 2, got %#v", renewed.Payload["lease_version"])
	}

	writeBinaryFrame(t, ctx, controller, relayproto.BinaryFrame{
		FrameType: relayproto.FrameTypeTerminalInput,
		Header: map[string]any{
			"session_id":    "session-1",
			"encoding":      "raw",
			"lease_version": int64(1),
		},
		Payload: []byte("old-version\n"),
	})
	readEnvelope(t, ctx, controller, relayproto.TypeError)

	writeBinaryFrame(t, ctx, controller, relayproto.BinaryFrame{
		FrameType: relayproto.FrameTypeTerminalInput,
		Header: map[string]any{
			"session_id":    "session-1",
			"encoding":      "raw",
			"lease_version": int64(2),
		},
		Payload: []byte("new-version\n"),
	})
	frame := readBinaryFrame(t, ctx, daemonConn, relayproto.FrameTypeTerminalInput)
	if string(frame.Payload) != "new-version\n" {
		t.Fatalf("expected payload %q, got %q", "new-version\n", frame.Payload)
	}
}

func TestRelayControlStaleDenialClearsControllerState(t *testing.T) {
	authorizer := &fakeSessionAuthorizer{
		renewErr: relay.ErrControlDenied{
			Reason:  "stale_lease",
			Message: "stale control lease",
		},
	}
	server := relay.NewServer(authorizer)
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	daemonConn, _, err := websocket.Dial(ctx, "ws"+httpServer.URL[len("http"):]+"/ws", nil)
	if err != nil {
		t.Fatalf("dial daemon: %v", err)
	}
	defer daemonConn.Close(websocket.StatusNormalClosure, "done")

	writeEnvelope(t, ctx, daemonConn, relayproto.Envelope{
		Type:    relayproto.TypeHelloDaemon,
		Payload: map[string]any{"device_id": "device-1"},
	})
	writeEnvelope(t, ctx, daemonConn, relayproto.Envelope{
		Type:    relayproto.TypeSessionOnline,
		Payload: map[string]any{"session_id": "session-1"},
	})

	controller := watchViewer(t, ctx, httpServer.URL, "controller-token")
	defer controller.Close(websocket.StatusNormalClosure, "done")
	readEnvelope(t, ctx, daemonConn, relayproto.TypeSessionSnapshotReq)

	writeEnvelope(t, ctx, controller, relayproto.Envelope{
		Type:      relayproto.TypeControlAcquire,
		RequestID: "acquire-stale-1",
		Payload:   map[string]any{"session_id": "session-1"},
	})
	readEnvelope(t, ctx, controller, relayproto.TypeControlGranted)

	writeEnvelope(t, ctx, controller, relayproto.Envelope{
		Type:      relayproto.TypeControlRenew,
		RequestID: "renew-stale-1",
		Payload: map[string]any{
			"session_id":    "session-1",
			"lease_version": int64(1),
		},
	})
	denied := readEnvelope(t, ctx, controller, relayproto.TypeControlDenied)
	reason, _ := denied.Payload["reason"].(string)
	if reason != "stale_lease" {
		t.Fatalf("expected stale_lease denial, got %q", reason)
	}

	writeBinaryFrame(t, ctx, controller, relayproto.BinaryFrame{
		FrameType: relayproto.FrameTypeTerminalInput,
		Header: map[string]any{
			"session_id":    "session-1",
			"encoding":      "raw",
			"lease_version": int64(1),
		},
		Payload: []byte("should-fail\n"),
	})
	readEnvelope(t, ctx, controller, relayproto.TypeError)
}

func TestRelayControlAcquireInternalFailureReturnsError(t *testing.T) {
	authorizer := &fakeSessionAuthorizer{
		acquireErr: errors.New("control backend unavailable"),
	}
	server := relay.NewServer(authorizer)
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	daemonConn, _, err := websocket.Dial(ctx, "ws"+httpServer.URL[len("http"):]+"/ws", nil)
	if err != nil {
		t.Fatalf("dial daemon: %v", err)
	}
	defer daemonConn.Close(websocket.StatusNormalClosure, "done")

	writeEnvelope(t, ctx, daemonConn, relayproto.Envelope{
		Type:    relayproto.TypeHelloDaemon,
		Payload: map[string]any{"device_id": "device-1"},
	})
	writeEnvelope(t, ctx, daemonConn, relayproto.Envelope{
		Type:    relayproto.TypeSessionOnline,
		Payload: map[string]any{"session_id": "session-1"},
	})

	viewer := watchViewer(t, ctx, httpServer.URL, "viewer-token")
	defer viewer.Close(websocket.StatusNormalClosure, "done")
	readEnvelope(t, ctx, daemonConn, relayproto.TypeSessionSnapshotReq)

	writeEnvelope(t, ctx, viewer, relayproto.Envelope{
		Type:      relayproto.TypeControlAcquire,
		RequestID: "acquire-internal-failure",
		Payload: map[string]any{
			"session_id": "session-1",
		},
	})
	readEnvelope(t, ctx, viewer, relayproto.TypeError)
}

func watchViewer(t *testing.T, ctx context.Context, serverURL string, accessToken string) *websocket.Conn {
	t.Helper()
	viewerConn, _, err := websocket.Dial(ctx, "ws"+serverURL[len("http"):]+"/ws", &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": []string{"Bearer " + accessToken}},
	})
	if err != nil {
		t.Fatalf("dial viewer: %v", err)
	}
	writeEnvelope(t, ctx, viewerConn, relayproto.Envelope{
		Type:    relayproto.TypeHelloViewer,
		Payload: map[string]any{},
	})
	writeEnvelope(t, ctx, viewerConn, relayproto.Envelope{
		Type:    relayproto.TypeSessionWatch,
		Payload: map[string]any{"session_id": "session-1"},
	})
	readEnvelope(t, ctx, viewerConn, relayproto.TypeSessionJoined)
	return viewerConn
}

func assertViewerFrames(t *testing.T, ctx context.Context, conn *websocket.Conn) {
	t.Helper()
	readEnvelope(t, ctx, conn, relayproto.TypeSessionSnapshotReady)
	snapshot := readBinaryFrame(t, ctx, conn, relayproto.FrameTypeSnapshotChunk)
	if string(snapshot.Payload) != "snapshot" {
		t.Fatalf("unexpected snapshot payload: %q", snapshot.Payload)
	}
	output := readBinaryFrame(t, ctx, conn, relayproto.FrameTypeTerminalOutput)
	if string(output.Payload) != "live-output" {
		t.Fatalf("unexpected output payload: %q", output.Payload)
	}
}

func writeEnvelope(t *testing.T, ctx context.Context, conn *websocket.Conn, env relayproto.Envelope) {
	t.Helper()
	data, err := relayproto.EncodeEnvelope(env)
	if err != nil {
		t.Fatalf("encode envelope: %v", err)
	}
	if err := conn.Write(ctx, websocket.MessageText, data); err != nil {
		t.Fatalf("write envelope: %v", err)
	}
}

func readEnvelope(t *testing.T, ctx context.Context, conn *websocket.Conn, wantType string) relayproto.Envelope {
	t.Helper()
	msgType, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read envelope: %v", err)
	}
	if msgType != websocket.MessageText {
		t.Fatalf("expected text frame, got %v", msgType)
	}
	env, err := relayproto.DecodeEnvelope(data)
	if err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if env.Type != wantType {
		t.Fatalf("expected %s, got %q", wantType, env.Type)
	}
	return env
}

func writeBinaryFrame(t *testing.T, ctx context.Context, conn *websocket.Conn, frame relayproto.BinaryFrame) {
	t.Helper()
	data, err := relayproto.EncodeBinaryFrame(frame)
	if err != nil {
		t.Fatalf("encode binary frame: %v", err)
	}
	if err := conn.Write(ctx, websocket.MessageBinary, data); err != nil {
		t.Fatalf("write binary frame: %v", err)
	}
}

func readBinaryFrame(t *testing.T, ctx context.Context, conn *websocket.Conn, wantType byte) relayproto.BinaryFrame {
	t.Helper()
	msgType, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read binary frame: %v", err)
	}
	if msgType != websocket.MessageBinary {
		t.Fatalf("expected binary frame, got %v", msgType)
	}
	frame, err := relayproto.DecodeBinaryFrame(data)
	if err != nil {
		t.Fatalf("decode binary frame: %v", err)
	}
	if frame.FrameType != wantType {
		t.Fatalf("expected frame type %d, got %d", wantType, frame.FrameType)
	}
	return frame
}
