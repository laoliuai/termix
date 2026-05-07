package proxyenv

import (
	"os"
	"testing"
)

type stubConfig struct{ enable bool }

func (s stubConfig) GetEnableProxy() bool { return s.enable }

// TestApplyDisabledClearsAllProxyVars locks in the policy enforcement: when
// enable=false, every variable in Vars is cleared from the process env so
// `httpproxy.FromEnvironment` returns the empty config and HTTP clients
// dial directly.
func TestApplyDisabledClearsAllProxyVars(t *testing.T) {
	for _, v := range Vars {
		t.Setenv(v, "http://127.0.0.1:7897")
	}
	Apply(false)
	for _, v := range Vars {
		if got := os.Getenv(v); got != "" {
			t.Errorf("%s=%q after Apply(false), want empty", v, got)
		}
	}
}

// TestApplyEnabledLeavesEnvUntouched verifies the opt-in path does not
// disturb proxy vars. Important for corporate-proxy users.
func TestApplyEnabledLeavesEnvUntouched(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://corp.example.com:8080")
	t.Setenv("https_proxy", "http://corp.example.com:8080")
	Apply(true)
	if got := os.Getenv("HTTPS_PROXY"); got != "http://corp.example.com:8080" {
		t.Errorf("HTTPS_PROXY=%q after Apply(true), want unchanged", got)
	}
	if got := os.Getenv("https_proxy"); got != "http://corp.example.com:8080" {
		t.Errorf("https_proxy=%q after Apply(true), want unchanged", got)
	}
}

// TestEffectivePolicyPrecedence locks in: env override > config > default
// false. Each precedence rung is exercised with both "yes" and "no"
// values to make sure the override is bidirectional.
func TestEffectivePolicyPrecedence(t *testing.T) {
	cases := []struct {
		name     string
		envVal   string
		cfg      EnableProxyConfig
		expected bool
	}{
		{name: "default zero when nothing set", envVal: "", cfg: nil, expected: false},
		{name: "config true wins when no env override", envVal: "", cfg: stubConfig{true}, expected: true},
		{name: "config false (and unset) stays false", envVal: "", cfg: stubConfig{false}, expected: false},
		{name: "env=1 forces on regardless of config", envVal: "1", cfg: stubConfig{false}, expected: true},
		{name: "env=true forces on regardless of config", envVal: "true", cfg: stubConfig{false}, expected: true},
		{name: "env=0 forces off regardless of config", envVal: "0", cfg: stubConfig{true}, expected: false},
		{name: "env=false forces off regardless of config", envVal: "false", cfg: stubConfig{true}, expected: false},
		{name: "env=garbage falls through to config", envVal: "maybe", cfg: stubConfig{true}, expected: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			getenv := func(name string) string {
				if name == EnvOverride {
					return tc.envVal
				}
				return ""
			}
			got := EffectivePolicy(tc.cfg, getenv)
			if got != tc.expected {
				t.Fatalf("EffectivePolicy(env=%q,cfg=%v)=%t want %t",
					tc.envVal, tc.cfg, got, tc.expected)
			}
		})
	}
}

// TestFingerprintStableForIdenticalEnv verifies the digest is deterministic
// for a given proxy env state — the daemon and CLI both compute it
// independently, so they must agree when their effective env matches.
func TestFingerprintStableForIdenticalEnv(t *testing.T) {
	for _, v := range Vars {
		t.Setenv(v, "")
	}
	first := Fingerprint()
	second := Fingerprint()
	if first != second {
		t.Fatalf("Fingerprint not stable: %q != %q", first, second)
	}
	t.Setenv("HTTPS_PROXY", "http://example.com")
	mutated := Fingerprint()
	if mutated == first {
		t.Fatalf("Fingerprint did not change after HTTPS_PROXY was set: still %q", first)
	}
	if len(mutated) != 12 {
		t.Fatalf("Fingerprint length=%d want 12", len(mutated))
	}
}

// TestFingerprintAllEmptyConstant asserts that the post-Apply(false)
// fingerprint is well-defined and identical across processes that all
// agree the proxy is off — the key invariant the CLI/daemon handshake
// relies on. We capture the value here as a regression guard so any
// future change to Vars or the hashing scheme deliberately bumps it.
func TestFingerprintAllEmptyConstant(t *testing.T) {
	for _, v := range Vars {
		t.Setenv(v, "")
	}
	got := Fingerprint()
	// sha256 of seven NUL bytes (8 empty Vars joined by NUL separators);
	// truncated to 12 hex chars. Update this constant alongside any change
	// to Vars or the join scheme — the test failure is the prompt to
	// audit that the bump is intentional.
	const want = "837885c8f809"
	if got != want {
		t.Fatalf("all-empty fingerprint=%q want %q (Vars or hashing changed?)", got, want)
	}
}
