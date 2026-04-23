package tests

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/termix/termix/go/internal/relay"
	"github.com/termix/termix/go/internal/relayproto"
)

type authCall struct {
	accessToken string
	sessionID   string
}

type fakeSessionAuthorizer struct {
	mu    sync.Mutex
	calls []authCall
}

func (f *fakeSessionAuthorizer) AuthorizeWatch(_ context.Context, accessToken string, sessionID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, authCall{accessToken: accessToken, sessionID: sessionID})
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
