package auth

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type AccessClaims struct {
	UserID   string `json:"user_id"`
	DeviceID string `json:"device_id"`
	jwt.RegisteredClaims
}

func IssueAccessToken(signingKey, userID, deviceID string, ttl time.Duration) (string, error) {
	switch {
	case signingKey == "":
		return "", errors.New("signing key is required")
	case userID == "":
		return "", errors.New("user id is required")
	case deviceID == "":
		return "", errors.New("device id is required")
	case ttl <= 0:
		return "", errors.New("ttl must be positive")
	}

	now := time.Now().UTC()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, AccessClaims{
		UserID:   userID,
		DeviceID: deviceID,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
	})
	return token.SignedString([]byte(signingKey))
}
