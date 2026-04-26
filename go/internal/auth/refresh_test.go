package auth

import "testing"

func TestHashRefreshTokenDeterministic(t *testing.T) {
	h1 := HashRefreshToken("abc")
	h2 := HashRefreshToken("abc")
	if h1 != h2 {
		t.Fatalf("hash should be deterministic, got %q vs %q", h1, h2)
	}
	if HashRefreshToken("abc") == HashRefreshToken("abd") {
		t.Fatal("different inputs must produce different hashes")
	}
	if len(h1) != 64 {
		t.Fatalf("expected 64-char hex SHA-256, got %d", len(h1))
	}
}
