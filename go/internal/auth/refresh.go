package auth

import (
	"crypto/sha256"
	"encoding/hex"
)

// HashRefreshToken produces a deterministic, fast SHA-256 hex digest of the
// supplied refresh token. Used for both insert (at login) and lookup (at
// refresh). Refresh tokens are random 256-bit strings — no salt needed.
func HashRefreshToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
