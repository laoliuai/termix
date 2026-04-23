package config

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

func SaveHostConfig(path string, cfg HostConfig) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

func LoadHostConfig(path string) (HostConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return HostConfig{}, err
	}

	var cfg HostConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return HostConfig{}, err
	}
	return cfg, cfg.Validate()
}

func DeriveHostConfig(serverBaseURL string) (HostConfig, error) {
	u, err := url.Parse(serverBaseURL)
	if err != nil {
		return HostConfig{}, err
	}

	wsScheme := "wss"
	if strings.EqualFold(u.Scheme, "http") {
		wsScheme = "ws"
	}

	relayURL := *u
	relayURL.Scheme = wsScheme
	relayURL.Path = "/ws"
	relayURL.RawPath = ""
	relayURL.RawQuery = ""
	relayURL.Fragment = ""

	cfg := HostConfig{
		ServerBaseURL:            serverBaseURL,
		ControlAPIURL:            serverBaseURL,
		RelayWSURL:               relayURL.String(),
		LogLevel:                 "info",
		PreviewMaxBytes:          8192,
		HeartbeatIntervalSeconds: 15,
	}
	return cfg, cfg.Validate()
}
