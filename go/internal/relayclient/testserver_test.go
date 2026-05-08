package relayclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/coder/websocket"
)

// relayTestServer wraps an httptest.Server upgraded to WebSocket. URL is
// already ws://-prefixed so callers can pass it straight to relayclient.New.
type relayTestServer struct {
	*httptest.Server
	URL string
}

// newRelayTestServer accepts a per-connection handler invoked on each
// successful upgrade. The supplied ctx is bound to the request and is
// canceled when the underlying http handler returns.
func newRelayTestServer(t *testing.T, handler func(conn *websocket.Conn, ctx context.Context)) *relayTestServer {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("Accept: %v", err)
			return
		}
		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()
		handler(conn, ctx)
		_ = conn.Close(websocket.StatusNormalClosure, "done")
	}))
	return &relayTestServer{
		Server: srv,
		URL:    "ws" + srv.URL[len("http"):],
	}
}
