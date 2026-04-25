package tests

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/coder/websocket"
	relaycontrolv1 "github.com/termix/termix/go/gen/proto/relaycontrolv1"
	"github.com/termix/termix/go/internal/auth"
	"github.com/termix/termix/go/internal/persistence"
	"github.com/termix/termix/go/internal/relay"
	"github.com/termix/termix/go/internal/relayclient"
	relaycontrol "github.com/termix/termix/go/internal/relaycontrol"
	"github.com/termix/termix/go/internal/relayproto"
	"github.com/termix/termix/go/internal/session"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func TestRelayWatchAndControlEndToEndOverGRPCWithRealControlServer(t *testing.T) {
	if os.Getenv("TERMIX_TEST_DATABASE_URL") == "" {
		t.Skip("set TERMIX_TEST_DATABASE_URL to run end-to-end gRPC integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	store, cleanupStore := persistence.NewTestStore(t)
	defer cleanupStore()

	seed := seedLeaseSession(t, ctx, store)

	const signingKey = "signing-key"
	accessToken, err := auth.IssueAccessToken(signingKey, seed.userID, seed.controllerDeviceID, 15*time.Minute)
	if err != nil {
		t.Fatalf("IssueAccessToken: %v", err)
	}

	grpcListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	grpcServer := grpc.NewServer()
	relaycontrolv1.RegisterRelayControlServiceServer(
		grpcServer,
		relaycontrol.NewServer(store, signingKey, relaycontrol.ServerConfig{}),
	)
	go func() { _ = grpcServer.Serve(grpcListener) }()
	defer grpcServer.Stop()

	grpcConn, err := grpc.Dial(grpcListener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc dial: %v", err)
	}
	defer grpcConn.Close()
	authorizer := relaycontrol.NewClient(grpcConn)

	relayServer := relay.NewServer(authorizer)
	httpServer := httptest.NewServer(relayServer.Handler())
	defer httpServer.Close()

	inputCalls := make(chan []byte, 1)
	daemonClient := relayclient.New("ws"+httpServer.URL[len("http"):]+"/ws", "daemon-token", "device-1")
	daemonClient.SetInputHandler(func(_ context.Context, _ string, payload []byte) error {
		inputCalls <- append([]byte(nil), payload...)
		return nil
	})
	if err := daemonClient.Connect(ctx); err != nil {
		t.Fatalf("daemon connect: %v", err)
	}
	if err := daemonClient.AnnounceSession(ctx, session.LocalSession{SessionID: seed.sessionID}); err != nil {
		t.Fatalf("announce session: %v", err)
	}

	viewer, _, err := websocket.Dial(ctx, "ws"+httpServer.URL[len("http"):]+"/ws", &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": []string{"Bearer " + accessToken}},
	})
	if err != nil {
		t.Fatalf("dial viewer: %v", err)
	}
	defer viewer.Close(websocket.StatusNormalClosure, "done")

	writeEnvelope(t, ctx, viewer, relayproto.Envelope{
		Type:    relayproto.TypeHelloViewer,
		Payload: map[string]any{},
	})
	writeEnvelope(t, ctx, viewer, relayproto.Envelope{
		Type:    relayproto.TypeSessionWatch,
		Payload: map[string]any{"session_id": seed.sessionID},
	})
	readEnvelope(t, ctx, viewer, relayproto.TypeSessionJoined)

	writeEnvelope(t, ctx, viewer, relayproto.Envelope{
		Type:      relayproto.TypeControlAcquire,
		RequestID: "acquire-e2e",
		Payload:   map[string]any{"session_id": seed.sessionID},
	})
	granted := readEnvelope(t, ctx, viewer, relayproto.TypeControlGranted)
	if v, _ := granted.Payload["lease_version"].(float64); int64(v) != 1 {
		t.Fatalf("expected acquire lease_version 1, got %#v", granted.Payload["lease_version"])
	}

	writeEnvelope(t, ctx, viewer, relayproto.Envelope{
		Type:      relayproto.TypeControlRenew,
		RequestID: "renew-e2e",
		Payload: map[string]any{
			"session_id":    seed.sessionID,
			"lease_version": int64(1),
		},
	})
	renewed := readEnvelope(t, ctx, viewer, relayproto.TypeControlGranted)
	if v, _ := renewed.Payload["lease_version"].(float64); int64(v) != 2 {
		t.Fatalf("expected renew lease_version 2, got %#v", renewed.Payload["lease_version"])
	}

	writeBinaryFrame(t, ctx, viewer, relayproto.BinaryFrame{
		FrameType: relayproto.FrameTypeTerminalInput,
		Header: map[string]any{
			"session_id":    seed.sessionID,
			"encoding":      "raw",
			"lease_version": int64(2),
		},
		Payload: []byte("e2e\n"),
	})
	select {
	case got := <-inputCalls:
		if string(got) != "e2e\n" {
			t.Fatalf("expected daemon payload %q, got %q", "e2e\n", got)
		}
	case <-ctx.Done():
		t.Fatalf("timed out waiting for daemon input handler: %v", ctx.Err())
	}

	writeEnvelope(t, ctx, viewer, relayproto.Envelope{
		Type:      relayproto.TypeControlRelease,
		RequestID: "release-e2e",
		Payload: map[string]any{
			"session_id":    seed.sessionID,
			"lease_version": int64(2),
		},
	})
	readEnvelope(t, ctx, viewer, relayproto.TypeControlRevoked)
}
