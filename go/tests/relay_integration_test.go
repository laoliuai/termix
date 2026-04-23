package tests

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/termix/termix/go/internal/relay"
	"github.com/termix/termix/go/internal/relayproto"
)

type fakeSessionAuthorizer struct {
	accessToken string
	sessionID   string
}

func (f *fakeSessionAuthorizer) AuthorizeWatch(_ context.Context, accessToken string, sessionID string) error {
	f.accessToken = accessToken
	f.sessionID = sessionID
	return nil
}

func TestRelayWatchHandshakeRequestsSnapshot(t *testing.T) {
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

	viewerConn, _, err := websocket.Dial(ctx, "ws"+httpServer.URL[len("http"):]+"/ws", &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": []string{"Bearer viewer-token"}},
	})
	if err != nil {
		t.Fatalf("dial viewer: %v", err)
	}
	defer viewerConn.Close(websocket.StatusNormalClosure, "done")

	daemonHello, err := relayproto.EncodeEnvelope(relayproto.Envelope{
		Type:    relayproto.TypeHelloDaemon,
		Payload: map[string]any{"device_id": "device-1"},
	})
	if err != nil {
		t.Fatalf("encode daemon hello: %v", err)
	}
	if err := daemonConn.Write(ctx, websocket.MessageText, daemonHello); err != nil {
		t.Fatalf("daemon hello: %v", err)
	}

	online, err := relayproto.EncodeEnvelope(relayproto.Envelope{
		Type:    relayproto.TypeSessionOnline,
		Payload: map[string]any{"session_id": "session-1"},
	})
	if err != nil {
		t.Fatalf("encode session online: %v", err)
	}
	if err := daemonConn.Write(ctx, websocket.MessageText, online); err != nil {
		t.Fatalf("session.online: %v", err)
	}

	viewerHello, err := relayproto.EncodeEnvelope(relayproto.Envelope{
		Type:    relayproto.TypeHelloViewer,
		Payload: map[string]any{},
	})
	if err != nil {
		t.Fatalf("encode viewer hello: %v", err)
	}
	if err := viewerConn.Write(ctx, websocket.MessageText, viewerHello); err != nil {
		t.Fatalf("viewer hello: %v", err)
	}

	watch, err := relayproto.EncodeEnvelope(relayproto.Envelope{
		Type:    relayproto.TypeSessionWatch,
		Payload: map[string]any{"session_id": "session-1"},
	})
	if err != nil {
		t.Fatalf("encode session watch: %v", err)
	}
	if err := viewerConn.Write(ctx, websocket.MessageText, watch); err != nil {
		t.Fatalf("session.watch: %v", err)
	}

	_, data, err := viewerConn.Read(ctx)
	if err != nil {
		t.Fatalf("viewer read joined: %v", err)
	}
	env, err := relayproto.DecodeEnvelope(data)
	if err != nil {
		t.Fatalf("decode joined: %v", err)
	}
	if env.Type != relayproto.TypeSessionJoined {
		t.Fatalf("expected session joined, got %q", env.Type)
	}

	_, data, err = daemonConn.Read(ctx)
	if err != nil {
		t.Fatalf("daemon read snapshot request: %v", err)
	}
	env, err = relayproto.DecodeEnvelope(data)
	if err != nil {
		t.Fatalf("decode snapshot request: %v", err)
	}
	if env.Type != relayproto.TypeSessionSnapshotReq {
		t.Fatalf("expected snapshot request, got %q", env.Type)
	}
	if authorizer.accessToken != "viewer-token" {
		t.Fatalf("expected viewer token, got %q", authorizer.accessToken)
	}
	if authorizer.sessionID != "session-1" {
		t.Fatalf("expected authorizer session-1, got %q", authorizer.sessionID)
	}
}
