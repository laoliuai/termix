package relayclient

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/termix/termix/go/internal/credentials"
)

func TestSupervisorInitialStateIsConnecting(t *testing.T) {
	sup := NewSupervisor(SupervisorOptions{
		Factory:   func(context.Context, credentials.StoredCredentials) (*Client, error) { return nil, errors.New("unused") },
		Refresher: stubRefresher{},
		Clock:     RealClock(),
		Rand:      func() float64 { return 0.5 },
	})
	got := sup.State()
	if got.Phase != PhaseConnecting {
		t.Fatalf("Phase=%q want %q", got.Phase, PhaseConnecting)
	}
	if got.Attempt != 0 {
		t.Fatalf("Attempt=%d want 0", got.Attempt)
	}
}

func TestSupervisorPublishOutputReturnsErrNotConnectedWhenIdle(t *testing.T) {
	sup := NewSupervisor(SupervisorOptions{
		Factory:   func(context.Context, credentials.StoredCredentials) (*Client, error) { return nil, errors.New("unused") },
		Refresher: stubRefresher{},
		Clock:     RealClock(),
		Rand:      func() float64 { return 0.5 },
	})
	err := sup.PublishOutput(context.Background(), "sess", []byte("hi"))
	if !errors.Is(err, ErrNotConnected) {
		t.Fatalf("err=%v want %v", err, ErrNotConnected)
	}
}

type stubRefresher struct{}

func (stubRefresher) EnsureFresh(context.Context) (credentials.StoredCredentials, error) {
	return credentials.StoredCredentials{}, nil
}

func (stubRefresher) RefreshNow(context.Context) (credentials.StoredCredentials, error) {
	return credentials.StoredCredentials{}, nil
}

type fakeClock struct {
	now    time.Time
	sleeps []time.Duration
	gate   chan struct{} // unblocks each Sleep call when the test sends on it
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC), gate: make(chan struct{}, 16)}
}

func (f *fakeClock) Now() time.Time { return f.now }

func (f *fakeClock) Sleep(ctx context.Context, d time.Duration) error {
	f.sleeps = append(f.sleeps, d)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-f.gate:
		f.now = f.now.Add(d)
		return nil
	}
}

func TestSupervisorRunReachesConnectedAndReannounces(t *testing.T) {
	server := newRelayTestServer(t, func(conn *websocket.Conn, ctx context.Context) {
		<-ctx.Done()
	})
	defer server.Close()

	clk := newFakeClock()
	reannounced := make(chan struct{}, 1)

	sup := NewSupervisor(SupervisorOptions{
		Factory: func(ctx context.Context, _ credentials.StoredCredentials) (*Client, error) {
			c := New(server.URL, "tok", "dev")
			if err := c.Connect(ctx); err != nil {
				return nil, err
			}
			return c, nil
		},
		Refresher: stubRefresher{},
		Clock:     clk,
		Rand:      func() float64 { return 0.5 },
	})
	sup.SetReconnectCallback(func(context.Context) {
		select {
		case reannounced <- struct{}{}:
		default:
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go func() { _ = sup.Run(ctx) }()

	// wait for connected
	deadline := time.After(2 * time.Second)
	for {
		st := sup.State()
		if st.Phase == PhaseConnected {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("never reached PhaseConnected: state=%+v", st)
		case <-time.After(20 * time.Millisecond):
		}
	}

	select {
	case <-reannounced:
	case <-time.After(time.Second):
		t.Fatal("reconnect callback was not invoked")
	}
}

func TestSupervisorRunRetriesAfterClientDeath(t *testing.T) {
	var connectCalls int32
	dialOnce := make(chan struct{})

	server := newRelayTestServer(t, func(conn *websocket.Conn, ctx context.Context) {
		// first call: close immediately to trigger reconnect
		// subsequent calls: stay open
		if atomic.AddInt32(&connectCalls, 1) == 1 {
			_ = conn.Close(websocket.StatusNormalClosure, "die")
			return
		}
		close(dialOnce)
		<-ctx.Done()
	})
	defer server.Close()

	clk := newFakeClock()
	sup := NewSupervisor(SupervisorOptions{
		Factory: func(ctx context.Context, _ credentials.StoredCredentials) (*Client, error) {
			c := New(server.URL, "tok", "dev")
			if err := c.Connect(ctx); err != nil {
				return nil, err
			}
			return c, nil
		},
		Refresher: stubRefresher{},
		Clock:     clk,
		Rand:      func() float64 { return 0.5 },
	})

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	go func() { _ = sup.Run(ctx) }()

	// allow the supervisor to advance through its first backoff sleep
	select {
	case clk.gate <- struct{}{}:
	case <-time.After(2 * time.Second):
		t.Fatal("supervisor never entered backoff sleep")
	}

	select {
	case <-dialOnce:
	case <-time.After(2 * time.Second):
		t.Fatal("supervisor did not redial after first client died")
	}

	if got := sup.State().Attempt; got == 0 {
		t.Fatalf("Attempt counter not incremented after retry: %+v", sup.State())
	}
}

// TestSupervisorAuthFailuresResetOnNonAuthError verifies that a non-auth
// transient error resets the consecutive AuthFailures counter to zero,
// so that an auth_fail → network_err sequence does not keep accumulating
// toward the persistent-auth-failure limit.
func TestSupervisorAuthFailuresResetOnNonAuthError(t *testing.T) {
	sup := NewSupervisor(SupervisorOptions{
		Factory:   func(context.Context, credentials.StoredCredentials) (*Client, error) { return nil, errors.New("unused") },
		Refresher: stubRefresher{},
		Clock:     RealClock(),
		Rand:      func() float64 { return 0.5 },
	})

	// Simulate two consecutive auth failures.
	sup.bumpAuthFailures(100)
	sup.bumpAuthFailures(100)
	if got := sup.State().AuthFailures; got != 2 {
		t.Fatalf("AuthFailures=%d want 2 after two bumps", got)
	}

	// A non-auth error should break the streak.
	sup.recordError(errors.New("connection refused"))

	if got := sup.State().AuthFailures; got != 0 {
		t.Fatalf("AuthFailures=%d want 0 after non-auth recordError", got)
	}
}
