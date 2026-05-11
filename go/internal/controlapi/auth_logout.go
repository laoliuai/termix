package controlapi

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/termix/termix/go/internal/auth"
)

func (s *server) PostAuthLogout(c *gin.Context) {
	var rawToken string
	if ck, err := c.Request.Cookie("termix_refresh"); err == nil {
		rawToken = ck.Value
	}

	// Attempt the revoke FIRST, but always clear the cookie on the response
	// path regardless of outcome. Idempotent: RevokeRefreshToken only updates
	// rows where revoked_at IS NULL, so a second logout (or an already-expired
	// token) returns nil. A non-nil error is therefore a real DB failure that
	// must reach the client — otherwise the cookie would advertise "logged
	// out" while the token row remained live in the DB and a stolen copy of
	// it could still mint access tokens until natural expiry.
	var revokeErr error
	if rawToken != "" {
		revokeErr = s.store.RevokeRefreshToken(c.Request.Context(), auth.HashRefreshToken(rawToken))
	}

	http.SetCookie(c.Writer, &http.Cookie{
		Name:     "termix_refresh",
		Value:    "",
		Path:     "/api/v1/auth",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   shouldUseSecureRefreshCookie(c.Request),
		SameSite: http.SameSiteStrictMode,
	})

	switch {
	case rawToken == "":
		c.Status(http.StatusBadRequest)
	case revokeErr != nil:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "revoke_failed"})
	default:
		c.Status(http.StatusNoContent)
	}
}
