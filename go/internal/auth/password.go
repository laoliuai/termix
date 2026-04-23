package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	passwordHashVersion        = "v1"
	argonTime           uint32 = 1
	argonMemory         uint32 = 64 * 1024
	argonThreads        uint8  = 4
	argonKeyLen         uint32 = 32
	argonSaltLen               = 16
)

func HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	hash := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return strings.Join([]string{
		passwordHashVersion,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash),
	}, "."), nil
}

func ComparePassword(encoded string, password string) error {
	parts := strings.Split(encoded, ".")
	if len(parts) != 3 || parts[0] != passwordHashVersion {
		return errors.New("invalid password hash")
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[1])
	if err != nil {
		return err
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil {
		return err
	}
	actual := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	if subtle.ConstantTimeCompare(expected, actual) != 1 {
		return errors.New("password mismatch")
	}
	return nil
}
