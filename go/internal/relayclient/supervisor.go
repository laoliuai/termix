package relayclient

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"time"

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

// Run is the supervisor's main loop. Owns the reconnect goroutine
// lifetime — caller invokes it once via `go sup.Run(ctx)`. Returns nil
// when ctx is canceled or after persistent auth failure triggers
// requestShutdown.
func (s *Supervisor) Run(ctx context.Context) error {
	const persistentAuthFailures = 3
	for {
		s.setState(func(st *RelayState) {
			st.Phase = PhaseConnecting
			st.NextRetryAt = time.Time{}
		})

		creds, err := s.refresher.EnsureFresh(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return s.terminalClosed()
			}
			if isAuthError(err) {
				if reachedAuthLimit := s.bumpAuthFailures(persistentAuthFailures); reachedAuthLimit {
					return s.terminalAuthFailure()
				}
			}
			s.recordError(err)
			if err := s.backoffSleep(ctx); err != nil {
				return s.terminalClosed()
			}
			continue
		}

		client, err := s.factory(ctx, creds)
		if err != nil {
			if ctx.Err() != nil {
				return s.terminalClosed()
			}
			if isAuthError(err) {
				if reachedAuthLimit := s.bumpAuthFailures(persistentAuthFailures); reachedAuthLimit {
					return s.terminalAuthFailure()
				}
			}
			s.recordError(err)
			if err := s.backoffSleep(ctx); err != nil {
				return s.terminalClosed()
			}
			continue
		}

		// Apply current handlers to the fresh client.
		s.handlersMu.Lock()
		if s.snapshotHandler != nil {
			client.SetSnapshotHandler(s.snapshotHandler)
		}
		if s.inputHandler != nil {
			client.SetInputHandler(s.inputHandler)
		}
		s.handlersMu.Unlock()

		s.client.Store(client)
		s.setState(func(st *RelayState) {
			st.Phase = PhaseConnected
			st.LastConnectedAt = s.clock.Now()
			st.LastError = ""
			st.AuthFailures = 0
			st.NextRetryAt = time.Time{}
			// Attempt counter is preserved so callers can see how much
			// flapping has happened during this supervisor's lifetime.
		})

		if cb := s.reconnectCb.Load(); cb != nil {
			(*cb)(ctx)
		}

		// Wait for ctx cancellation or client death.
		select {
		case <-ctx.Done():
			_ = client.Close()
			s.client.Store(nil)
			return s.terminalClosed()
		case err := <-client.Done():
			s.client.Store(nil)
			s.setState(func(st *RelayState) {
				st.Phase = PhaseReconnecting
				st.Attempt++
				if err != nil {
					st.LastError = err.Error()
				}
				st.NextRetryAt = s.clock.Now().Add(computeBackoff(st.Attempt-1, s.rand))
			})
			if err := s.backoffSleep(ctx); err != nil {
				return s.terminalClosed()
			}
		}
	}
}

func (s *Supervisor) backoffSleep(ctx context.Context) error {
	st := s.State()
	delay := computeBackoff(st.Attempt, s.rand)
	return s.clock.Sleep(ctx, delay)
}

func (s *Supervisor) recordError(err error) {
	s.setState(func(st *RelayState) {
		st.Phase = PhaseReconnecting
		st.Attempt++
		st.LastError = err.Error()
		st.NextRetryAt = s.clock.Now().Add(computeBackoff(st.Attempt-1, s.rand))
	})
}

func (s *Supervisor) bumpAuthFailures(limit int) bool {
	var reached bool
	s.setState(func(st *RelayState) {
		st.AuthFailures++
		reached = st.AuthFailures >= limit
	})
	return reached
}

func (s *Supervisor) terminalClosed() error {
	s.setState(func(st *RelayState) { st.Phase = PhaseClosed })
	return nil
}

func (s *Supervisor) terminalAuthFailure() error {
	s.setState(func(st *RelayState) {
		st.Phase = PhaseClosed
		st.LastError = "persistent auth failure"
	})
	if s.requestShutdown != nil {
		s.requestShutdown()
	}
	return nil
}

// isAuthError detects whether err looks like an HTTP 401 / unauthorized
// response from the relay or refresh endpoints. Errors carrying
// "401" or "unauthorized" (case-insensitive) qualify; anything else is
// treated as transient.
func isAuthError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "401") || strings.Contains(strings.ToLower(msg), "unauthorized")
}
