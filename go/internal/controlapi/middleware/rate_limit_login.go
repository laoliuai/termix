package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

type loginBucket struct {
	lim  *rate.Limiter
	last time.Time
}

type loginLimiter struct {
	mu      sync.Mutex
	buckets map[string]*loginBucket
	refill  rate.Limit
	burst   int
	ttl     time.Duration
}

// LoginRateLimit returns a Gin middleware that rate-limits POST /auth/login.
// perMinute is the number of requests allowed per minute per (IP, email) tuple.
// burst is the maximum burst size (tokens in the bucket at start).
func LoginRateLimit(perMinute, burst int) gin.HandlerFunc {
	l := &loginLimiter{
		buckets: map[string]*loginBucket{},
		refill:  rate.Every(time.Minute / time.Duration(perMinute)),
		burst:   burst,
		ttl:     5 * time.Minute,
	}
	return func(c *gin.Context) {
		// Peek email without consuming body.
		var email string
		if c.Request.Body != nil {
			buf, _ := io.ReadAll(c.Request.Body)
			c.Request.Body = io.NopCloser(bytes.NewReader(buf))
			var probe struct {
				Email string `json:"email"`
			}
			_ = json.Unmarshal(buf, &probe)
			email = probe.Email
		}
		ip, _, _ := net.SplitHostPort(c.Request.RemoteAddr)
		if ip == "" {
			ip = c.Request.RemoteAddr
		}
		key := ip + "|" + email

		l.mu.Lock()
		// Sweep stale buckets.
		now := time.Now()
		for k, bb := range l.buckets {
			if now.Sub(bb.last) > l.ttl {
				delete(l.buckets, k)
			}
		}
		b, ok := l.buckets[key]
		if !ok {
			b = &loginBucket{lim: rate.NewLimiter(l.refill, l.burst)}
			l.buckets[key] = b
		}
		b.last = now
		allowed := b.lim.Allow()
		l.mu.Unlock()

		if !allowed {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": "too many login attempts"})
			return
		}
		c.Next()
	}
}
