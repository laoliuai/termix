package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// OriginAllowlist returns a Gin middleware that rejects non-GET requests
// whose Origin header is set and does not equal `allowed`.
// Requests without an Origin header (CLI, Compose, curl) pass through.
func OriginAllowlist(allowed string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method != http.MethodGet && c.Request.Header.Get("Origin") != "" {
			if c.Request.Header.Get("Origin") != allowed {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "forbidden origin"})
				return
			}
		}
		c.Next()
	}
}
