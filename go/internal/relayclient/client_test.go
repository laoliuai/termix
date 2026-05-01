package relayclient_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/termix/termix/go/internal/relayclient"
	"github.com/termix/termix/go/internal/relayproto"
)

func TestClientAnswersSnapshotRequest(t *testing.T) {
	done := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer access-token" {
			t.Fatalf("unexpected authorization header: %q", got)
		}

		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Fatalf("Accept returned error: %v", err)
		}
		defer conn.Close(websocket.StatusNormalClosure, "done")

		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		msgType, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("read hello: %v", err)
		}
		if msgType != websocket.MessageText {
			t.Fatalf("expected hello text frame, got %v", msgType)
		}
		env, err := relayproto.DecodeEnvelope(data)
		if err != nil {
			t.Fatalf("decode hello: %v", err)
		}
		if env.Type != relayproto.TypeHelloDaemon {
			t.Fatalf("expected hello.daemon, got %q", env.Type)
		}

		request, err := relayproto.EncodeEnvelope(relayproto.Envelope{
			Type:    relayproto.TypeSessionSnapshotReq,
			Payload: map[string]any{"session_id": "session-1"},
		})
		if err != nil {
			t.Fatalf("encode request: %v", err)
		}
		if err := conn.Write(ctx, websocket.MessageText, request); err != nil {
			t.Fatalf("write snapshot request: %v", err)
		}

		msgType, data, err = conn.Read(ctx)
		if err != nil {
			t.Fatalf("read snapshot ready: %v", err)
		}
		if msgType != websocket.MessageText {
			t.Fatalf("expected snapshot ready text frame, got %v", msgType)
		}
		env, err = relayproto.DecodeEnvelope(data)
		if err != nil {
			t.Fatalf("decode snapshot ready: %v", err)
		}
		if env.Type != relayproto.TypeSessionSnapshotReady {
			t.Fatalf("expected snapshot ready, got %q", env.Type)
		}

		msgType, data, err = conn.Read(ctx)
		if err != nil {
			t.Fatalf("read snapshot frame: %v", err)
		}
		if msgType != websocket.MessageBinary {
			t.Fatalf("expected binary snapshot frame, got %v", msgType)
		}
		frame, err := relayproto.DecodeBinaryFrame(data)
		if err != nil {
			t.Fatalf("decode snapshot frame: %v", err)
		}
		if string(frame.Payload) != "snapshot" {
			t.Fatalf("unexpected snapshot payload: %q", frame.Payload)
		}
		close(done)
	}))
	defer server.Close()

	client := relayclient.New("ws"+server.URL[len("http"):], "access-token", "device-1")
	client.SetSnapshotHandler(func(context.Context, string) ([]byte, error) {
		return []byte("snapshot"), nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect returned error: %v", err)
	}

	select {
	case <-done:
	case <-ctx.Done():
		t.Fatalf("timed out waiting for snapshot response: %v", ctx.Err())
	}
}

func TestClientHandlesTerminalInputFrame(t *testing.T) {
	done := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer access-token" {
			t.Fatalf("unexpected authorization header: %q", got)
		}

		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Fatalf("Accept returned error: %v", err)
		}
		defer conn.Close(websocket.StatusNormalClosure, "done")

		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		msgType, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("read hello: %v", err)
		}
		if msgType != websocket.MessageText {
			t.Fatalf("expected hello text frame, got %v", msgType)
		}
		env, err := relayproto.DecodeEnvelope(data)
		if err != nil {
			t.Fatalf("decode hello: %v", err)
		}
		if env.Type != relayproto.TypeHelloDaemon {
			t.Fatalf("expected hello.daemon, got %q", env.Type)
		}

		frame, err := relayproto.EncodeBinaryFrame(relayproto.BinaryFrame{
			FrameType: relayproto.FrameTypeTerminalInput,
			Header: map[string]any{
				"session_id": "session-1",
				"encoding":   "raw",
			},
			Payload: []byte("pwd\n"),
		})
		if err != nil {
			t.Fatalf("encode input frame: %v", err)
		}
		if err := conn.Write(ctx, websocket.MessageBinary, frame); err != nil {
			t.Fatalf("write input frame: %v", err)
		}

		select {
		case <-done:
		case <-ctx.Done():
			t.Fatalf("timed out waiting for input handler: %v", ctx.Err())
		}
	}))
	defer server.Close()

	var gotSessionID string
	var gotPayload []byte

	client := relayclient.New("ws"+server.URL[len("http"):], "access-token", "device-1")
	client.SetInputHandler(func(_ context.Context, sessionID string, payload []byte) error {
		gotSessionID = sessionID
		gotPayload = append([]byte(nil), payload...)
		close(done)
		return nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect returned error: %v", err)
	}

	select {
	case <-done:
	case <-ctx.Done():
		t.Fatalf("timed out waiting for input frame handling: %v", ctx.Err())
	}
	if gotSessionID != "session-1" {
		t.Fatalf("unexpected session id: %q", gotSessionID)
	}
	if string(gotPayload) != "pwd\n" {
		t.Fatalf("unexpected payload: %q", gotPayload)
	}
}

func TestClientHandlesResizeEnvelope(t *testing.T) {
	done := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer access-token" {
			t.Fatalf("unexpected authorization header: %q", got)
		}

		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Fatalf("Accept returned error: %v", err)
		}
		defer conn.Close(websocket.StatusNormalClosure, "done")

		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		msgType, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("read hello: %v", err)
		}
		if msgType != websocket.MessageText {
			t.Fatalf("expected hello text frame, got %v", msgType)
		}
		env, err := relayproto.DecodeEnvelope(data)
		if err != nil {
			t.Fatalf("decode hello: %v", err)
		}
		if env.Type != relayproto.TypeHelloDaemon {
			t.Fatalf("expected hello.daemon, got %q", env.Type)
		}

		request, err := relayproto.EncodeEnvelope(relayproto.Envelope{
			Type: relayproto.TypeClientResize,
			Payload: map[string]any{
				"session_id": "sid-1",
				"cols":       float64(80),
				"rows":       float64(24),
			},
		})
		if err != nil {
			t.Fatalf("encode resize request: %v", err)
		}
		if err := conn.Write(ctx, websocket.MessageText, request); err != nil {
			t.Fatalf("write resize request: %v", err)
		}

		select {
		case <-done:
		case <-ctx.Done():
			t.Fatalf("timed out waiting for resize handler: %v", ctx.Err())
		}
	}))
	defer server.Close()

	var gotSessionID string
	var gotCols, gotRows uint32

	client := relayclient.New("ws"+server.URL[len("http"):], "access-token", "device-1")
	client.SetResizeHandler(func(_ context.Context, sessionID string, cols, rows uint32) error {
		gotSessionID = sessionID
		gotCols = cols
		gotRows = rows
		close(done)
		return nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect returned error: %v", err)
	}

	select {
	case <-done:
	case <-ctx.Done():
		t.Fatalf("timed out waiting for resize envelope handling: %v", ctx.Err())
	}
	if gotSessionID != "sid-1" {
		t.Fatalf("unexpected session id: %q", gotSessionID)
	}
	if gotCols != 80 {
		t.Fatalf("unexpected cols: %d", gotCols)
	}
	if gotRows != 24 {
		t.Fatalf("unexpected rows: %d", gotRows)
	}
}

func TestClientPublishOutputSendsTerminalOutputFrameWithStreamHeader(t *testing.T) {
	type captured struct {
		frame relayproto.BinaryFrame
	}
	got := make(chan captured, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Fatalf("Accept returned error: %v", err)
		}
		defer conn.Close(websocket.StatusNormalClosure, "done")

		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		// Drain the hello.daemon envelope.
		if _, _, err := conn.Read(ctx); err != nil {
			t.Fatalf("read hello: %v", err)
		}

		// Read the output binary frame.
		msgType, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("read output frame: %v", err)
		}
		if msgType != websocket.MessageBinary {
			t.Fatalf("expected binary output frame, got %v", msgType)
		}
		frame, err := relayproto.DecodeBinaryFrame(data)
		if err != nil {
			t.Fatalf("decode output frame: %v", err)
		}
		got <- captured{frame: frame}
	}))
	defer server.Close()

	client := relayclient.New("ws"+server.URL[len("http"):], "access-token", "device-1")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect returned error: %v", err)
	}

	if err := client.PublishOutput(ctx, "session-1", []byte("hi")); err != nil {
		t.Fatalf("PublishOutput returned error: %v", err)
	}

	var cap captured
	select {
	case cap = <-got:
	case <-ctx.Done():
		t.Fatalf("timed out waiting for output frame: %v", ctx.Err())
	}

	if cap.frame.FrameType != relayproto.FrameTypeTerminalOutput {
		t.Fatalf("expected FrameTypeTerminalOutput, got %d", cap.frame.FrameType)
	}
	if got, want := cap.frame.Header["session_id"], "session-1"; got != want {
		t.Fatalf("session_id mismatch: want %q, got %v", want, got)
	}
	if stream, _ := cap.frame.Header["stream"].(string); stream != "stdout" {
		t.Fatalf("stream header missing or wrong: want %q, got %v", "stdout", cap.frame.Header["stream"])
	}
	if string(cap.frame.Payload) != "hi" {
		t.Fatalf("payload mismatch: got %q", cap.frame.Payload)
	}
}
