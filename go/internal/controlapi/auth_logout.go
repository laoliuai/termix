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
	if rawToken != "" {
		hash := auth.HashRefreshToken(rawToken)
		// Idempotent: RevokeRefreshToken only updates rows where revoked_at IS NULL,
		// so a second logout (or an already-expired token) is a no-op with no error.
		// We intentionally ignore the error here — even a DB hiccup should not prevent
		// the cookie from being cleared, and we still respond 204 to the client.
		_ = s.store.RevokeRefreshToken(c.Request.Context(), hash)
	}
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     "termix_refresh",
		Value:    "",
		Path:     "/api/v1/auth",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	})
	if rawToken == "" {
		c.Status(http.StatusBadRequest)
		return
	}
	c.Status(http.StatusNoContent)
}
