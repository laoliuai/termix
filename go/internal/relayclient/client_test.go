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
