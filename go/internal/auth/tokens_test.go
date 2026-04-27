package auth

import (
	"testing"
	"time"
)

func TestAccessTokenTTLDefaultsTo30Minutes(t *testing.T) {
	// Clean up any existing env var
	t.Setenv("TERMIX_ACCESS_TOKEN_TTL", "")
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
