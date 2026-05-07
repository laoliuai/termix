package config

import "errors"

type HostConfig struct {
	ServerBaseURL            string `json:"server_base_url"`
	ControlAPIURL            string `json:"control_api_url"`
	RelayWSURL               string `json:"relay_ws_url"`
	LogLevel                 string `json:"log_level"`
	PreviewMaxBytes          int    `json:"preview_max_bytes"`
	HeartbeatIntervalSeconds int    `json:"heartbeat_interval_seconds"`
	// EnableProxy controls whether the daemon and CLI honor the system's
	// HTTP/HTTPS/SOCKS proxy environment variables. False (the default and
	// the value seen for older host.json files that lack the key) makes
	// both processes unsetenv these vars at boot so the long-lived relay
	// WSS does not route through stale mihomo/clash/v2ray HTTP CONNECT
	// ports. Users who actually need a proxy to reach the control plane
	// set this to true (or run `TERMIX_ENABLE_PROXY=1 termix login` once
	// to bootstrap; the CLI persists the env override into this field on
	// first login).
	EnableProxy bool `json:"enable_proxy"`
}

// GetEnableProxy implements proxyenv.EnableProxyConfig so the proxyenv
// package can read this field without importing config (which would cause
// an import cycle).
func (c HostConfig) GetEnableProxy() bool { return c.EnableProxy }

func (c HostConfig) Validate() error {
	switch {
	case c.ServerBaseURL == "":
		return errors.New("server_base_url is required")
	case c.ControlAPIURL == "":
		return errors.New("control_api_url is required")
	case c.RelayWSURL == "":
		return errors.New("relay_ws_url is required")
	case c.PreviewMaxBytes <= 0:
		return errors.New("preview_max_bytes must be positive")
	case c.HeartbeatIntervalSeconds <= 0:
		return errors.New("heartbeat_interval_seconds must be positive")
	default:
		return nil
	}
}
