package config

import "errors"

type CloudConfig struct {
	ListenAddr             string
	PublicBaseURL          string
	PostgresDSN            string
	JWTSigningKey          string
	AccessTokenTTLSeconds  int
	RefreshTokenTTLSeconds int
}

func (c CloudConfig) Validate() error {
	switch {
	case c.ListenAddr == "":
		return errors.New("listen_addr is required")
	case c.PublicBaseURL == "":
		return errors.New("public_base_url is required")
	case c.PostgresDSN == "":
		return errors.New("postgres_dsn is required")
	case c.JWTSigningKey == "":
		return errors.New("jwt_signing_key is required")
	default:
		return nil
	}
}
