package tests

import (
	"testing"
	"time"

	"github.com/termix/termix/go/internal/auth"
)

func TestPasswordHashRoundTrip(t *testing.T) {
	hash, err := auth.HashPassword("secret-pass")
	if err != nil {
		t.Fatalf("HashPassword returned error: %v", err)
	}

	if err := auth.ComparePassword(hash, "secret-pass"); err != nil {
		t.Fatalf("ComparePassword returned error: %v", err)
	}
}

func TestIssueAccessToken(t *testing.T) {
	token, err := auth.IssueAccessToken("signing-key", "user-1", "device-1", 15*time.Minute)
	if err != nil {
		t.Fatalf("IssueAccessToken returned error: %v", err)
	}

	if token == "" {
		t.Fatal("expected non-empty token")
	}
}

func TestIssueAccessTokenRejectsInvalidInput(t *testing.T) {
	cases := []struct {
		name       string
		signingKey string
		userID     string
		deviceID   string
		ttl        time.Duration
	}{
		{name: "empty signing key", userID: "user-1", deviceID: "device-1", ttl: 15 * time.Minute},
		{name: "empty user id", signingKey: "signing-key", deviceID: "device-1", ttl: 15 * time.Minute},
		{name: "empty device id", signingKey: "signing-key", userID: "user-1", ttl: 15 * time.Minute},
		{name: "non-positive ttl", signingKey: "signing-key", userID: "user-1", deviceID: "device-1", ttl: 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := auth.IssueAccessToken(tc.signingKey, tc.userID, tc.deviceID, tc.ttl); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}
