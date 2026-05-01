package relayclient

import "github.com/termix/termix/go/internal/relayproto"

type OnlinePayload struct {
	SessionID string `json:"session_id"`
}

type SnapshotRequestPayload struct {
	SessionID string `json:"session_id"`
}

type ResizeRequestPayload struct {
	SessionID string `json:"session_id"`
	Cols      uint32 `json:"cols"`
	Rows      uint32 `json:"rows"`
}

func HelloDaemonEnvelope(deviceID string) relayproto.Envelope {
	return relayproto.Envelope{
		Type:    relayproto.TypeHelloDaemon,
		Payload: map[string]any{"device_id": deviceID},
	}
}
