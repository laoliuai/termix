package middleware

import "github.com/gin-gonic/gin"

// SecurityHeaders returns a Gin middleware that injects standard
// security response headers. relayOrigin is the wss:// URL of the
// relay server, used in CSP connect-src.
func SecurityHeaders(relayOrigin string) gin.HandlerFunc {
	csp := "default-src 'self'; " +
		"connect-src 'self' " + relayOrigin + "; " +
		"style-src 'self' 'unsafe-inline'; " +
		"img-src 'self' data:; " +
		"manifest-src 'self'; " +
		"worker-src 'self';"
	return func(c *gin.Context) {
		h := c.Writer.Header()
		h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		h.Set("Content-Security-Policy", csp)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")
		c.Next()
	}
}
