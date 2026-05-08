package relayclient

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/termix/termix/go/internal/credentials"
	"github.com/termix/termix/go/internal/session"
)

// ErrNotConnected is returned by Publish*/Announce* when the supervisor is
// not in the connected phase. Callers should log and continue rather than
// blocking — the supervisor will reconnect on its own and a fresh
// snapshot will be pushed to viewers when the connection comes back.
var ErrNotConnected = errors.New("relay: not connected")

// ErrClosed is returned after the supervisor has terminally shut down
// (ctx canceled or persistent auth failure).
var ErrClosed = errors.New("relay: closed")

// Refresher is the credentials.Refresher subset the supervisor uses.
// Defining it here lets tests inject without pulling in credentials's
// full struct.
type Refresher interface {
	EnsureFresh(ctx context.Context) (credentials.StoredCredentials, error)
	RefreshNow(ctx context.Context) (credentials.StoredCredentials, error)
}

// ClientFactory builds a fresh, connected *Client from current credentials.
// The factory is responsible for calling Connect on the returned client.
// Tests inject a fake; production passes a closure that wraps New + Connect.
type ClientFactory func(ctx context.Context, creds credentials.StoredCredentials) (*Client, error)

type SupervisorOptions struct {
	Factory         ClientFactory
	Refresher       Refresher
	Clock           Clock
	Rand            func() float64
	RequestShutdown func() // called on persistent auth failure
}

type Supervisor struct {
	factory         ClientFactory
	refresher       Refresher
	clock           Clock
	rand            func() float64
	requestShutdown func()

	stateMu sync.RWMutex
	state   RelayState

	client      atomic.Pointer[Client]
	reconnectCb atomic.Pointer[func(context.Context)]

	// snapshotHandler / inputHandler are stored on the supervisor so that
	// each newly-built client gets them re-installed via Connect.
	handlersMu      sync.Mutex
	snapshotHandler func(context.Context, string) ([]byte, error)
	inputHandler    func(context.Context, string, []byte) error
}

func NewSupervisor(opts SupervisorOptions) *Supervisor {
	if opts.Clock == nil {
		opts.Clock = RealClock()
	}
	if opts.Rand == nil {
		opts.Rand = func() float64 { return 0.5 } // no jitter when not provided
	}
	return &Supervisor{
		factory:         opts.Factory,
		refresher:       opts.Refresher,
		clock:           opts.Clock,
		rand:            opts.Rand,
		requestShutdown: opts.RequestShutdown,
		state:           RelayState{Phase: PhaseConnecting},
	}
}

// State returns a snapshot of the supervisor's current state.
func (s *Supervisor) State() RelayState {
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()
	return s.state
}

func (s *Supervisor) setState(mut func(*RelayState)) {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	mut(&s.state)
}

// SetReconnectCallback registers a function that will be invoked
// (synchronously) after each successful reconnect. The callback should
// re-announce all sessions and push fresh snapshots.
func (s *Supervisor) SetReconnectCallback(fn func(context.Context)) {
	s.reconnectCb.Store(&fn)
}

func (s *Supervisor) SetSnapshotHandler(fn func(context.Context, string) ([]byte, error)) {
	s.handlersMu.Lock()
	s.snapshotHandler = fn
	s.handlersMu.Unlock()
	if c := s.client.Load(); c != nil {
		c.SetSnapshotHandler(fn)
	}
}

func (s *Supervisor) SetInputHandler(fn func(context.Context, string, []byte) error) {
	s.handlersMu.Lock()
	s.inputHandler = fn
	s.handlersMu.Unlock()
	if c := s.client.Load(); c != nil {
		c.SetInputHandler(fn)
	}
}

func (s *Supervisor) AnnounceSession(ctx context.Context, sess session.LocalSession) error {
	c := s.client.Load()
	if c == nil {
		return ErrNotConnected
	}
	return c.AnnounceSession(ctx, sess)
}

func (s *Supervisor) PublishSnapshot(ctx context.Context, sessionID string, data []byte) error {
	c := s.client.Load()
	if c == nil {
		return ErrNotConnected
	}
	return c.PublishSnapshot(ctx, sessionID, data)
}

func (s *Supervisor) PublishOutput(ctx context.Context, sessionID string, data []byte) error {
	c := s.client.Load()
	if c == nil {
		return ErrNotConnected
	}
	return c.PublishOutput(ctx, sessionID, data)
}
