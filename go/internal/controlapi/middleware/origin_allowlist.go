package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// OriginAllowlist returns a Gin middleware that rejects non-GET requests
// whose Origin header is set and does not equal `allowed`. Requests
// without an Origin header (CLI, Compose, curl) pass through.
//
// IMPORTANT: this middleware is designed for SAME-ORIGIN deployments
// (SPA and API served from the same hostname). It rejects all OPTIONS
// preflight requests with a non-matching Origin, which means it would
// break a cross-origin browser deployment. If you ever need to serve
// the SPA from a different origin than the API, remove this middleware
// and replace it with a proper CORS layer.
func OriginAllowlist(allowed string) gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.Request.Header.Get("Origin")
		if c.Request.Method != http.MethodGet && origin != "" && origin != allowed {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "forbidden origin"})
			return
		}
		c.Next()
	}
}
