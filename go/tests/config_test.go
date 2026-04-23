package tests

import (
	"os"
	"path/filepath"
	"strings"
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

func TestCredentialsLoadRoundTrip(t *testing.T) {
	credPath := filepath.Join(t.TempDir(), "credentials.json")
	want := credentials.StoredCredentials{
		ServerBaseURL: "https://termix.example.com",
		UserID:        "user-1",
		DeviceID:      "device-1",
		AccessToken:   "access-token",
		RefreshToken:  "refresh-token",
		ExpiresAt:     "2030-01-01T00:00:00Z",
	}

	if err := credentials.Save(credPath, want); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}

	got, err := credentials.Load(credPath)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if got.AccessToken != want.AccessToken {
		t.Fatalf("expected access token %q, got %q", want.AccessToken, got.AccessToken)
	}
	if got.DeviceID != want.DeviceID {
		t.Fatalf("expected device id %q, got %q", want.DeviceID, got.DeviceID)
	}
}

func TestHostConfigSaveAndLoadRoundTrip(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "host.json")
	cfg := config.HostConfig{
		ServerBaseURL:            "https://termix.example.com",
		ControlAPIURL:            "https://termix.example.com",
		RelayWSURL:               "wss://termix.example.com/ws",
		LogLevel:                 "info",
		PreviewMaxBytes:          8192,
		HeartbeatIntervalSeconds: 15,
	}

	if err := config.SaveHostConfig(cfgPath, cfg); err != nil {
		t.Fatalf("SaveHostConfig returned error: %v", err)
	}

	got, err := config.LoadHostConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadHostConfig returned error: %v", err)
	}
	if got.RelayWSURL != cfg.RelayWSURL {
		t.Fatalf("expected relay url %q, got %q", cfg.RelayWSURL, got.RelayWSURL)
	}

	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if !strings.Contains(string(data), `"relay_ws_url"`) {
		t.Fatalf("expected host config to use stable snake_case keys, got %s", string(data))
	}
}

func TestDeriveHostConfig(t *testing.T) {
	cfg, err := config.DeriveHostConfig("https://termix.example.com")
	if err != nil {
		t.Fatalf("DeriveHostConfig returned error: %v", err)
	}
	if cfg.ControlAPIURL != "https://termix.example.com" {
		t.Fatalf("expected control api base url to stay at server root, got %q", cfg.ControlAPIURL)
	}
	if cfg.RelayWSURL != "wss://termix.example.com/ws" {
		t.Fatalf("expected relay ws url, got %q", cfg.RelayWSURL)
	}
}
