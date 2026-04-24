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

func ParseAccessToken(signingKey, tokenString string) (AccessClaims, error) {
	if signingKey == "" {
		return AccessClaims{}, errors.New("signing key is required")
	}
	if tokenString == "" {
		return AccessClaims{}, errors.New("access token is required")
	}

	claims := &AccessClaims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
		method, ok := token.Method.(*jwt.SigningMethodHMAC)
		if !ok || method.Alg() != jwt.SigningMethodHS256.Alg() {
			return nil, errors.New("unexpected signing method")
		}
		return []byte(signingKey), nil
	})
	if err != nil || token == nil || !token.Valid {
		if err != nil {
			return AccessClaims{}, err
		}
		return AccessClaims{}, errors.New("invalid access token")
	}
	if claims.UserID == "" || claims.DeviceID == "" {
		return AccessClaims{}, errors.New("missing bearer claims")
	}
	return *claims, nil
}
