package auth

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

func BearerMiddleware(signingKey string) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		tokenString := strings.TrimPrefix(header, "Bearer ")
		claims, err := ParseAccessToken(signingKey, tokenString)
		if err != nil {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		c.Set("user_id", claims.UserID)
		c.Set("device_id", claims.DeviceID)
		c.Next()
	}
}
