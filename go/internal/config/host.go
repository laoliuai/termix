package config

import "errors"

type HostConfig struct {
	ServerBaseURL            string
	ControlAPIURL            string
	RelayWSURL               string
	LogLevel                 string
	PreviewMaxBytes          int
	HeartbeatIntervalSeconds int
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
