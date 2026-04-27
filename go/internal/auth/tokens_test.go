package auth

import (
	"os"
	"testing"
	"time"
)

func TestAccessTokenTTLDefaultsTo30Minutes(t *testing.T) {
	t.Helper()
	// Save+restore explicitly (t.Setenv requires a value; use os.Unsetenv)
	orig, hadOrig := os.LookupEnv("TERMIX_ACCESS_TOKEN_TTL")
	os.Unsetenv("TERMIX_ACCESS_TOKEN_TTL")
	t.Cleanup(func() {
		if hadOrig {
			os.Setenv("TERMIX_ACCESS_TOKEN_TTL", orig)
		} else {
			os.Unsetenv("TERMIX_ACCESS_TOKEN_TTL")
		}
	})
	if got := AccessTokenTTL(); got != 30*time.Minute {
		t.Fatalf("default TTL: want 30m, got %v", got)
	}
}

func TestAccessTokenTTLEnvOverride(t *testing.T) {
	t.Setenv("TERMIX_ACCESS_TOKEN_TTL", "5m")
	if got := AccessTokenTTL(); got != 5*time.Minute {
		t.Fatalf("override 5m: got %v", got)
	}
}

func TestAccessTokenTTLEnvBadValueFallsBackToDefault(t *testing.T) {
	t.Setenv("TERMIX_ACCESS_TOKEN_TTL", "not-a-duration")
	if got := AccessTokenTTL(); got != 30*time.Minute {
		t.Fatalf("bad override fallback: got %v", got)
	}
}

func TestAccessTokenTTLEnvEmptyStringFallsBackToDefault(t *testing.T) {
	t.Setenv("TERMIX_ACCESS_TOKEN_TTL", "")
	if got := AccessTokenTTL(); got != 30*time.Minute {
		t.Fatalf("empty override fallback: got %v", got)
	}
}

func TestAccessTokenTTLZeroDurationFallsBackToDefault(t *testing.T) {
	t.Setenv("TERMIX_ACCESS_TOKEN_TTL", "0s")
	if got := AccessTokenTTL(); got != 30*time.Minute {
		t.Fatalf("zero duration fallback: want 30m, got %v", got)
	}
}

func TestAccessTokenTTLNegativeDurationFallsBackToDefault(t *testing.T) {
	t.Setenv("TERMIX_ACCESS_TOKEN_TTL", "-5m")
	if got := AccessTokenTTL(); got != 30*time.Minute {
		t.Fatalf("negative duration fallback: want 30m, got %v", got)
	}
}
