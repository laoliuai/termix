// Package proxyenv enforces termix's "ignore system HTTP/HTTPS/SOCKS proxy
// by default" policy across both the CLI and the daemon process.
//
// Background: mihomo / clash / v2ray TUN-mode users typically still have
// `http_proxy=127.0.0.1:7890` (or similar) lingering in their shell env even
// though TUN is the actual path the kernel takes. Go's stdlib
// `http.DefaultTransport` calls `ProxyFromEnvironment` at request time, so
// those env vars route the long-lived relay WSS through the proxy port, and
// the proxy's idle timeout cuts it — surfacing as `broken pipe` spam in
// `~/.local/state/termix/logs/termixd.log` with no actionable hint.
//
// This package centralizes the policy:
//   - Apply(enable=false) unsets every proxy-related env var in the current
//     process so that all downstream clients (controlapi, relayclient, gRPC,
//     anything reaching for `http.DefaultTransport`) silently bypass the
//     proxy. Apply is idempotent.
//   - Apply(enable=true) leaves the env untouched.
//   - EffectivePolicy resolves "is proxy enabled" given a HostConfig and the
//     `TERMIX_ENABLE_PROXY` env override. Precedence: env override (=1/true
//     forces on, =0/false forces off) > config.EnableProxy > default false.
//   - Fingerprint returns a short hex digest of the currently-effective
//     proxy env state (called *after* Apply). The CLI/daemon handshake uses
//     this to detect "user changed proxy preference between daemon boot
//     and current CLI invocation" and triggers an automatic respawn,
//     mirroring the existing version-handshake flow.
package proxyenv

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strings"
)

// Vars is the canonical set of proxy-related environment variables that
// termix manages. Both upper- and lower-case forms are listed because
// Go's `httpproxy.FromEnvironment` and other consumers honor either.
var Vars = []string{
	"HTTP_PROXY", "http_proxy",
	"HTTPS_PROXY", "https_proxy",
	"ALL_PROXY", "all_proxy",
	"NO_PROXY", "no_proxy",
}

// EnvOverride is the name of the per-process escape hatch users can set
// to force proxy on or off without touching `host.json`. Primary use case:
// corporate-proxy users running their first `termix login` (no host.json
// yet, but they need the proxy to reach the control plane). After login
// the CLI persists the resulting choice into host.json so the env var is
// no longer needed.
const EnvOverride = "TERMIX_ENABLE_PROXY"

// EnableProxyConfig is the subset of HostConfig that Policy needs. We
// take this as an interface so the proxyenv package does not import
// the config package (avoids an import cycle since config has no business
// importing proxyenv either).
type EnableProxyConfig interface {
	GetEnableProxy() bool
}

// EffectivePolicy returns true iff the proxy should be honored in the
// current process. Precedence: TERMIX_ENABLE_PROXY env override >
// cfg.EnableProxy > default false. cfg may be nil; an env override of
// `1`/`true`/`yes` is treated as on, `0`/`false`/`no` as off, anything
// else (including empty/unset) defers to the config.
func EffectivePolicy(cfg EnableProxyConfig, getenv func(string) string) bool {
	if getenv == nil {
		getenv = os.Getenv
	}
	switch strings.ToLower(strings.TrimSpace(getenv(EnvOverride))) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	if cfg == nil {
		return false
	}
	return cfg.GetEnableProxy()
}

// Apply enforces the policy on the current process. When enable is false,
// every variable in Vars is unsetenv'd so subsequent calls to
// `http.DefaultTransport` (and therefore `httpproxy.FromEnvironment`) see
// an empty proxy config and route requests directly. When enable is true,
// Apply is a no-op and the user's env is honored as-is.
//
// Apply is idempotent: calling it twice with the same argument has the
// same effect as calling it once.
func Apply(enable bool) {
	if enable {
		return
	}
	for _, v := range Vars {
		_ = os.Unsetenv(v)
	}
}

// Fingerprint returns the first 12 hex chars of sha256 over the current
// values of Vars (joined by NUL bytes). After Apply(false) has run, the
// fingerprint of "all empty" is a stable known constant, so daemons that
// agree on the policy produce identical fingerprints regardless of what
// their parent shell originally exported.
//
// The handshake uses this to detect drift: CLI computes its own
// fingerprint after Apply, daemon's HealthResponse carries the fingerprint
// it computed at boot — a mismatch means the user's effective policy has
// changed since daemon launch and the daemon needs a respawn so it
// rebuilds its HTTP transports under the new policy.
func Fingerprint() string {
	h := sha256.New()
	for i, v := range Vars {
		if i > 0 {
			_, _ = h.Write([]byte{0})
		}
		_, _ = h.Write([]byte(os.Getenv(v)))
	}
	sum := h.Sum(nil)
	return hex.EncodeToString(sum)[:12]
}
