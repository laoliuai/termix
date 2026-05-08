package relayclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/termix/termix/go/internal/relayproto"
)

// TestNoProxyHTTPClientHasExplicitNilProxy locks in the v0.4.3 design:
// the dedicated relay http.Client must have `Proxy: nil` on its
// Transport so the long-lived WSS dial never honors HTTPS_PROXY etc.
// regardless of the user's shell env.
func TestNoProxyHTTPClientHasExplicitNilProxy(t *testing.T) {
	transport, ok := noProxyHTTPClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("noProxyHTTPClient.Transport is %T, want *http.Transport", noProxyHTTPClient.Transport)
	}
	if transport.Proxy != nil {
		t.Fatalf("noProxyHTTPClient.Transport.Proxy must be nil so HTTPS_PROXY is bypassed, got %p", transport.Proxy)
	}
}

// TestConnectIgnoresHTTPSProxyEnv verifies the end-to-end behavior: even
// with HTTPS_PROXY pointed at a black-hole address, Connect still
// successfully dials the real relay because the dedicated http.Client
// bypasses ProxyFromEnvironment.
func TestConnectIgnoresHTTPSProxyEnv(t *testing.T) {
	// Black-hole proxy: connecting to this address would hang/fail. If
	// the client honored HTTPS_PROXY, the dial would never reach the
	// real test server.
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1") // port 1 is reserved/unused
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("https_proxy", "http://127.0.0.1:1")
	t.Setenv("http_proxy", "http://127.0.0.1:1")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "done")
		// Drain hello.daemon then close.
		for i := 0; i < 2; i++ {
			if _, _, err := conn.Read(r.Context()); err != nil {
				return
			}
		}
	}))
	defer server.Close()
	wsURL := "ws" + server.URL[len("http"):] + "/ws"

	client := New(wsURL, "access-token", "device-1")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect failed despite explicit no-proxy client: %v", err)
	}
	defer client.Close()

	// Sanity: send a noop envelope so the read loop has something to
	// chew on; the assertion is just that Connect succeeded.
	if err := client.writeEnvelope(ctx, relayproto.Envelope{
		Type:    relayproto.TypeSessionWatch,
		Payload: map[string]any{"session_id": "probe"},
	}); err != nil {
		t.Fatalf("writeEnvelope after Connect: %v", err)
	}
}
