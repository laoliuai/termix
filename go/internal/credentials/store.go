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

// Save atomically writes credentials to path: data is written to a sibling
// temp file, fsynced (best-effort), then renamed over path. Concurrent readers
// observe either the old or new contents — never a partial write.
func Save(path string, creds StoredCredentials) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
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
