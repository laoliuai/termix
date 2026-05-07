package diagnostics

import (
	"strings"
	"testing"
)

// checkProxy reads HTTPS_PROXY / HTTP_PROXY / ALL_PROXY (both upper and
// lowercase) and emits a warning so that users diagnosing "browser shows
// disconnected" / "WS keeps dying" can see the proxy is in the path.
//
// Background: Go's net/http reads HTTPS_PROXY at process start, so a daemon
// spawned from a shell with HTTPS_PROXY set will route every relay WS
// connection through that proxy. HTTP-CONNECT-tunnelled WebSocket sessions
// often die with broken-pipe errors after the proxy's idle timeout — and
// when the user is already on a TUN/VPN, the env vars are pure overhead.
// checkProxy describes the proxy state effective for HTTP/gRPC/WSS dials
// in this process. After proxyenv.Apply has run with enable_proxy=false
// the env is empty and we report `proxy: ok`; with proxy env set we
// report `proxy: enabled` plus the names of the set vars.
func TestCheckProxyOkWhenUnset(t *testing.T) {
	// Clear both upper and lower case forms — net/http reads both, so do we.
	for _, name := range proxyEnvNames {
		t.Setenv(name, "")
		t.Setenv(strings.ToLower(name), "")
	}
	got := checkProxy()
	if !strings.HasPrefix(got, "proxy: ok") {
		t.Fatalf("got %q, want it to start with %q", got, "proxy: ok")
	}
}

func TestCheckProxyReportsEnabledWhenAnySet(t *testing.T) {
	cases := []struct {
		name string
		set  map[string]string
		want []string // substrings that must appear in the line
	}{
		{
			name: "HTTPS_PROXY uppercase",
			set:  map[string]string{"HTTPS_PROXY": "http://127.0.0.1:7897"},
			want: []string{"enabled", "HTTPS_PROXY"},
		},
		{
			name: "https_proxy lowercase counted under HTTPS_PROXY",
			set:  map[string]string{"https_proxy": "http://127.0.0.1:7897"},
			want: []string{"enabled", "HTTPS_PROXY"},
		},
		{
			name: "all three set",
			set: map[string]string{
				"HTTPS_PROXY": "http://127.0.0.1:7897",
				"HTTP_PROXY":  "http://127.0.0.1:7897",
				"ALL_PROXY":   "socks5://127.0.0.1:7897",
			},
			want: []string{"enabled", "HTTPS_PROXY", "HTTP_PROXY", "ALL_PROXY"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, name := range proxyEnvNames {
				t.Setenv(name, "")
				t.Setenv(strings.ToLower(name), "")
			}
			for k, v := range tc.set {
				t.Setenv(k, v)
			}
			got := checkProxy()
			for _, s := range tc.want {
				if !strings.Contains(got, s) {
					t.Errorf("got %q, want it to contain %q", got, s)
				}
			}
		})
	}
}

func TestProxyHintMentionsHostJSONRemediation(t *testing.T) {
	// When proxy is in effect we still want a hint pointing at the way to
	// turn it off so users know the policy is configurable.
	for _, name := range proxyEnvNames {
		t.Setenv(name, "")
		t.Setenv(strings.ToLower(name), "")
	}
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:7897")
	got := checkProxy()
	for _, s := range []string{"enable_proxy", "host.json"} {
		if !strings.Contains(got, s) {
			t.Errorf("got %q, want it to mention %q", got, s)
		}
	}
}
