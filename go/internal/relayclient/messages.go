package relayclient

import "github.com/termix/termix/go/internal/relayproto"

type OnlinePayload struct {
	SessionID string `json:"session_id"`
}

type SnapshotRequestPayload struct {
	SessionID string `json:"session_id"`
	Cols      uint32 `json:"cols,omitempty"`
	Rows      uint32 `json:"rows,omitempty"`
}

type ResizeRequestPayload struct {
	SessionID string `json:"session_id"`
	Cols      uint32 `json:"cols"`
	Rows      uint32 `json:"rows"`
	// Debug carries optional client-observed viewport geometry, present only
	// when the SPA is in DEBUG mode (localStorage `termix_debug`). The daemon
	// logs it for correlation; it never affects resize behaviour. Absent (nil)
	// on normal resizes.
	Debug map[string]any `json:"debug,omitempty"`
}

func HelloDaemonEnvelope(deviceID string) relayproto.Envelope {
	return relayproto.Envelope{
		Type:    relayproto.TypeHelloDaemon,
		Payload: map[string]any{"device_id": deviceID},
	}
}
