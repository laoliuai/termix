package tests

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/termix/termix/go/internal/relay"
	"github.com/termix/termix/go/internal/relayproto"
)

func TestEnvelopeRoundTrip(t *testing.T) {
	data, err := relayproto.EncodeEnvelope(relayproto.Envelope{
		Type:      relayproto.TypeSessionWatch,
		RequestID: "req-1",
		Payload:   map[string]any{"session_id": "session-1"},
	})
	if err != nil {
		t.Fatalf("EncodeEnvelope returned error: %v", err)
	}

	env, err := relayproto.DecodeEnvelope(data)
	if err != nil {
		t.Fatalf("DecodeEnvelope returned error: %v", err)
	}
	if env.Type != relayproto.TypeSessionWatch {
		t.Fatalf("expected session.watch, got %q", env.Type)
	}
	if env.RequestID != "req-1" {
		t.Fatalf("expected request id req-1, got %q", env.RequestID)
	}
}

func TestBinaryFrameRoundTrip(t *testing.T) {
	frame, err := relayproto.EncodeBinaryFrame(relayproto.BinaryFrame{
		FrameType: relayproto.FrameTypeSnapshotChunk,
		Header: map[string]any{
			"session_id": "session-1",
			"seq":        1,
			"is_last":    true,
		},
		Payload: []byte("snapshot-data"),
	})
	if err != nil {
		t.Fatalf("EncodeBinaryFrame returned error: %v", err)
	}

	decoded, err := relayproto.DecodeBinaryFrame(frame)
	if err != nil {
		t.Fatalf("DecodeBinaryFrame returned error: %v", err)
	}
	if decoded.FrameType != relayproto.FrameTypeSnapshotChunk {
		t.Fatalf("unexpected frame type: %d", decoded.FrameType)
	}
	if !bytes.Equal(decoded.Payload, []byte("snapshot-data")) {
		t.Fatalf("unexpected payload: %q", decoded.Payload)
	}
}

func TestControlLeaseEnvelopeRoundTrip(t *testing.T) {
	data, err := relayproto.EncodeEnvelope(relayproto.Envelope{
		Type:      relayproto.TypeControlAcquire,
		RequestID: "request-1",
		Payload:   map[string]any{"session_id": "session-1"},
	})
	if err != nil {
		t.Fatalf("EncodeEnvelope returned error: %v", err)
	}

	env, err := relayproto.DecodeEnvelope(data)
	if err != nil {
		t.Fatalf("DecodeEnvelope returned error: %v", err)
	}
	if env.Type != relayproto.TypeControlAcquire {
		t.Fatalf("expected control.acquire, got %q", env.Type)
	}
	if env.RequestID != "request-1" {
		t.Fatalf("expected request id request-1, got %q", env.RequestID)
	}
}

func TestTerminalInputBinaryFrameRoundTrip(t *testing.T) {
	encoded, err := relayproto.EncodeBinaryFrame(relayproto.BinaryFrame{
		FrameType: relayproto.FrameTypeTerminalInput,
		Header: map[string]any{
			"session_id":    "session-1",
			"seq":           12,
			"encoding":      "raw",
			"lease_version": 3,
		},
		Payload: []byte("ls\n"),
	})
	if err != nil {
		t.Fatalf("EncodeBinaryFrame returned error: %v", err)
	}

	decoded, err := relayproto.DecodeBinaryFrame(encoded)
	if err != nil {
		t.Fatalf("DecodeBinaryFrame returned error: %v", err)
	}
	if decoded.FrameType != relayproto.FrameTypeTerminalInput {
		t.Fatalf("unexpected frame type: %d", decoded.FrameType)
	}
	if relayproto.HeaderString(decoded.Header, "encoding") != "raw" {
		t.Fatalf("unexpected encoding: %q", relayproto.HeaderString(decoded.Header, "encoding"))
	}
	if relayproto.HeaderInt64(decoded.Header, "lease_version") != 3 {
		t.Fatalf("unexpected lease version: %d", relayproto.HeaderInt64(decoded.Header, "lease_version"))
	}
	if !bytes.Equal(decoded.Payload, []byte("ls\n")) {
		t.Fatalf("unexpected payload: %q", decoded.Payload)
	}
}

func TestRelayForwardsWatchSizeHintIntoSnapshotRequest(t *testing.T) {
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

	viewerConn, _, err := websocket.Dial(ctx, "ws"+httpServer.URL[len("http"):]+"/ws", &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": []string{"Bearer viewer-token"}},
	})
	if err != nil {
		t.Fatalf("dial viewer: %v", err)
	}
	defer viewerConn.Close(websocket.StatusNormalClosure, "done")
	writeEnvelope(t, ctx, viewerConn, relayproto.Envelope{
		Type:    relayproto.TypeHelloViewer,
		Payload: map[string]any{},
	})
	writeEnvelope(t, ctx, viewerConn, relayproto.Envelope{
		Type: relayproto.TypeSessionWatch,
		Payload: map[string]any{
			"session_id": "session-1",
			"cols":       float64(117),
			"rows":       float64(38),
		},
	})

	snapshotReq := readEnvelope(t, ctx, daemonConn, relayproto.TypeSessionSnapshotReq)
	if sid, _ := snapshotReq.Payload["session_id"].(string); sid != "session-1" {
		t.Fatalf("expected session_id session-1, got %q", sid)
	}
	cols, _ := snapshotReq.Payload["cols"].(float64)
	if cols != 117 {
		t.Fatalf("expected cols 117 forwarded, got %v", snapshotReq.Payload["cols"])
	}
	rows, _ := snapshotReq.Payload["rows"].(float64)
	if rows != 38 {
		t.Fatalf("expected rows 38 forwarded, got %v", snapshotReq.Payload["rows"])
	}
}

func TestRelayOmitsSizeFromSnapshotRequestWhenWatchHasNoSize(t *testing.T) {
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

	viewerConn := watchViewer(t, ctx, httpServer.URL, "viewer-token")
	defer viewerConn.Close(websocket.StatusNormalClosure, "done")

	snapshotReq := readEnvelope(t, ctx, daemonConn, relayproto.TypeSessionSnapshotReq)
	if _, hasCols := snapshotReq.Payload["cols"]; hasCols {
		t.Fatalf("expected no cols when watch carried none, got %v", snapshotReq.Payload["cols"])
	}
	if _, hasRows := snapshotReq.Payload["rows"]; hasRows {
		t.Fatalf("expected no rows when watch carried none, got %v", snapshotReq.Payload["rows"])
	}
}

func TestRelayForwardsClientResizeFromWatcherToDaemon(t *testing.T) {
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

	viewer := watchViewer(t, ctx, httpServer.URL, "viewer-token")
	defer viewer.Close(websocket.StatusNormalClosure, "done")
	// Consume the snapshot request the relay sends to daemon when a watcher joins.
	readEnvelope(t, ctx, daemonConn, relayproto.TypeSessionSnapshotReq)

	writeEnvelope(t, ctx, viewer, relayproto.Envelope{
		Type: relayproto.TypeClientResize,
		Payload: map[string]any{
			"session_id": "session-1",
			"cols":       float64(80),
			"rows":       float64(24),
		},
	})

	resizeEnv := readEnvelope(t, ctx, daemonConn, relayproto.TypeClientResize)
	sessionID, _ := resizeEnv.Payload["session_id"].(string)
	if sessionID != "session-1" {
		t.Fatalf("expected session_id session-1, got %q", sessionID)
	}
	cols, _ := resizeEnv.Payload["cols"].(float64)
	if cols != 80 {
		t.Fatalf("expected cols 80, got %v", resizeEnv.Payload["cols"])
	}
	rows, _ := resizeEnv.Payload["rows"].(float64)
	if rows != 24 {
		t.Fatalf("expected rows 24, got %v", resizeEnv.Payload["rows"])
	}
}
