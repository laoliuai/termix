package credentials

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type StoredCredentials struct {
	ServerBaseURL string `json:"server_base_url"`
	UserID        string `json:"user_id"`
	DeviceID      string `json:"device_id"`
	AccessToken   string `json:"access_token"`
	RefreshToken  string `json:"refresh_token"`
	ExpiresAt     string `json:"expires_at"`
}

func Save(path string, creds StoredCredentials) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

func Load(path string) (StoredCredentials, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return StoredCredentials{}, err
	}

	var creds StoredCredentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return StoredCredentials{}, err
	}
	return creds, nil
}
