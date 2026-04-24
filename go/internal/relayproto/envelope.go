package relayproto

import "encoding/json"

const (
	TypeHelloDaemon          = "hello.daemon"
	TypeHelloViewer          = "hello.viewer"
	TypeSessionWatch         = "session.watch"
	TypeSessionUnwatch       = "session.unwatch"
	TypeSessionJoined        = "session.joined"
	TypeSessionLeft          = "session.left"
	TypeSessionSnapshotReq   = "session.snapshot.request"
	TypeSessionSnapshotReady = "session.snapshot.ready"
	TypeSessionOnline        = "session.online"
	TypeSessionOffline       = "session.offline"
	TypeControlAcquire       = "control.acquire"
	TypeControlRenew         = "control.renew"
	TypeControlRelease       = "control.release"
	TypeControlGranted       = "control.granted"
	TypeControlDenied        = "control.denied"
	TypeControlRevoked       = "control.revoked"
	TypeHeartbeat            = "heartbeat"
	TypeError                = "error"
)

type Envelope struct {
	Type      string         `json:"type"`
	RequestID string         `json:"request_id,omitempty"`
	Payload   map[string]any `json:"payload"`
}

func EncodeEnvelope(env Envelope) ([]byte, error) {
	return json.Marshal(env)
}

func DecodeEnvelope(data []byte) (Envelope, error) {
	var env Envelope
	err := json.Unmarshal(data, &env)
	return env, err
}
