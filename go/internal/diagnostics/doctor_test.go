package diagnostics

import (
	"strings"
	"testing"
)

// checkProxy is informational only after the v0.4.3 refactor: relay WSS
// uses its own dedicated http.Client with `Proxy: nil`, so the long-lived
// stream is never affected by HTTPS_PROXY etc. doctor surfaces the
// shell's proxy env so users can see what other endpoints (login,
// refresh, heartbeat) will use.

func TestCheckProxyOkWhenUnset(t *testing.T) {
	for _, name := range proxyEnvNames {
		t.Setenv(name, "")
		t.Setenv(strings.ToLower(name), "")
	}
	got := checkProxy()
	if !strings.HasPrefix(got, "proxy: ok") {
		t.Fatalf("got %q, want it to start with %q", got, "proxy: ok")
	}
	if !strings.Contains(got, "all dials direct") {
		t.Fatalf("got %q, want it to mention all-direct posture when env is unset", got)
	}
}

func TestCheckProxyMentionsBypassWhenAnySet(t *testing.T) {
	cases := []struct {
		name string
		set  map[string]string
		want []string // substrings that must appear in the line
	}{
		{
			name: "HTTPS_PROXY uppercase",
			set:  map[string]string{"HTTPS_PROXY": "http://127.0.0.1:7897"},
			want: []string{"proxy: ok", "HTTPS_PROXY", "relay WSS bypasses"},
		},
		{
			name: "https_proxy lowercase counted under HTTPS_PROXY",
			set:  map[string]string{"https_proxy": "http://127.0.0.1:7897"},
			want: []string{"proxy: ok", "HTTPS_PROXY", "relay WSS bypasses"},
		},
		{
			name: "all three set",
			set: map[string]string{
				"HTTPS_PROXY": "http://127.0.0.1:7897",
				"HTTP_PROXY":  "http://127.0.0.1:7897",
				"ALL_PROXY":   "socks5://127.0.0.1:7897",
			},
			want: []string{"proxy: ok", "HTTPS_PROXY", "HTTP_PROXY", "ALL_PROXY"},
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
