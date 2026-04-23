package tests

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/termix/termix/go/internal/config"
	"github.com/termix/termix/go/internal/credentials"
)

func TestHostConfigValidate(t *testing.T) {
	cfg := config.HostConfig{
		ServerBaseURL:            "https://termix.example.com",
		ControlAPIURL:            "https://termix.example.com/api",
		RelayWSURL:               "wss://termix.example.com/relay",
		LogLevel:                 "info",
		PreviewMaxBytes:          4096,
		HeartbeatIntervalSeconds: 15,
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected valid config, got %v", err)
	}
}

func TestDefaultHostPaths(t *testing.T) {
	paths := config.DefaultHostPaths()

	if paths.ConfigDir == "" || !filepath.IsAbs(paths.ConfigDir) {
		t.Fatalf("expected absolute config dir, got %q", paths.ConfigDir)
	}
	if paths.StateDir == "" || !filepath.IsAbs(paths.StateDir) {
		t.Fatalf("expected absolute state dir, got %q", paths.StateDir)
	}
	if paths.CredentialsFile != filepath.Join(paths.ConfigDir, "credentials.json") {
		t.Fatalf("unexpected credentials file path: %q", paths.CredentialsFile)
	}
}

func TestCredentialsSaveEnforces0600(t *testing.T) {
	credPath := filepath.Join(t.TempDir(), "nested", "credentials.json")
	creds := credentials.StoredCredentials{
		ServerBaseURL: "https://termix.example.com",
		UserID:        "user-1",
		DeviceID:      "device-1",
		AccessToken:   "access-token",
		RefreshToken:  "refresh-token",
		ExpiresAt:     "2030-01-01T00:00:00Z",
	}

	if err := credentials.Save(credPath, creds); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}
	if err := os.Chmod(credPath, 0o644); err != nil {
		t.Fatalf("failed to mutate mode for overwrite check: %v", err)
	}
	if err := credentials.Save(credPath, creds); err != nil {
		t.Fatalf("Save overwrite returned error: %v", err)
	}

	info, err := os.Stat(credPath)
	if err != nil {
		t.Fatalf("Stat returned error: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("expected mode 0600, got %04o", info.Mode().Perm())
	}
}
