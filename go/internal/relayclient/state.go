package relayclient

import "time"

// Phase enumerates the high-level connection states tracked by the
// supervisor. Values are plain strings so they can be passed through to
// the Status RPC and the SPA without translation.
type Phase string

const (
	PhaseConnecting   Phase = "connecting"
	PhaseConnected    Phase = "connected"
	PhaseReconnecting Phase = "reconnecting"
	PhaseClosed       Phase = "closed"
)

// RelayState is the supervisor's externally-visible snapshot of its current
// connection state. Read-only from the outside; the supervisor mutates it
// under its own mutex.
type RelayState struct {
	Phase           Phase
	Attempt         int       // current reconnect attempt counter; 0 when connected
	LastConnectedAt time.Time
	LastError       string
	NextRetryAt     time.Time // valid only when Phase == PhaseReconnecting
	AuthFailures    int       // consecutive 401s during reconnect handshake
}
