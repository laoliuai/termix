package config

import "errors"

type HostConfig struct {
	ServerBaseURL            string `json:"server_base_url"`
	ControlAPIURL            string `json:"control_api_url"`
	RelayWSURL               string `json:"relay_ws_url"`
	LogLevel                 string `json:"log_level"`
	PreviewMaxBytes          int    `json:"preview_max_bytes"`
	HeartbeatIntervalSeconds int    `json:"heartbeat_interval_seconds"`
}

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
