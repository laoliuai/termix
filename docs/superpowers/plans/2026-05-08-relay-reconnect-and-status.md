# Relay reconnect + SPA disconnect UX + `termix status` Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the daemon's relay WSS recover automatically from any transient disconnect; give the SPA explicit reconnect UI (banner + persistent modal); ship a `termix status` command surfacing user, daemon, relay, sessions, and proxy state.

**Architecture:** New `relayclient.Supervisor` wraps the existing dumb `relayclient.Client` with a state machine, backoff loop, atomic.Pointer client slot, and re-announce callback. The SPA gets the same state machine in `web/app/src/bridge/inbound.ts` plus a `DisconnectModal` and `ReconnectBanner`. A new `Status` daemon RPC + `runStatus` CLI subcommand expose the supervisor's state alongside local config and proxy fingerprint.

**Tech Stack:** Go 1.25 (daemon, CLI), gorilla/coder websocket client, protobuf, TypeScript + Preact + @preact/signals (SPA), vitest (web tests), real Postgres + tmux (Go integration tests).

---

## Prerequisites

Before Task 1, set up an isolated worktree per `CLAUDE.md`:

```bash
git -C /media/liujia/data/workspace/xunfei/termix worktree add .worktrees/relay-reconnect -b relay-reconnect
cd /media/liujia/data/workspace/xunfei/termix/.worktrees/relay-reconnect
```

All file paths below are relative to the worktree root unless otherwise noted. After setup, log the slice in `docs/PROGRESS.md` In-Progress section before starting Task 1.

---

## Task 1: Add `Done()` and `Close()` to `relayclient.Client`

**Why:** The supervisor needs a way to detect that the current client's read loop has died. Today `readLoop` returns silently on read error — no signal escapes. Add a `done` channel that closes (with the error stored) when the read loop exits, plus a `Close()` method for graceful shutdown.

**Files:**
- Modify: `go/internal/relayclient/client.go`
- Modify: `go/internal/relayclient/client_test.go`

- [ ] **Step 1: Write the failing test**

Append to `go/internal/relayclient/client_test.go`:

```go
func TestClientDoneClosesWhenReadLoopExits(t *testing.T) {
	server := newRelayTestServer(t, func(conn *websocket.Conn, _ context.Context) {
		_ = conn.Close(websocket.StatusNormalClosure, "bye")
	})
	defer server.Close()

	c := relayclient.New(server.URL, "tok", "dev")
	if err := c.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	select {
	case err := <-c.Done():
		if err == nil {
			t.Fatalf("expected non-nil error from Done after server close")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Done() did not close within 2s of server-side close")
	}
}

func TestClientCloseTerminatesReadLoop(t *testing.T) {
	server := newRelayTestServer(t, func(conn *websocket.Conn, ctx context.Context) {
		<-ctx.Done() // hold open until the test cancels
	})
	defer server.Close()

	c := relayclient.New(server.URL, "tok", "dev")
	if err := c.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	select {
	case <-c.Done():
		// ok
	case <-time.After(2 * time.Second):
		t.Fatal("Done() did not close within 2s of explicit Close()")
	}
}
```

If `newRelayTestServer` or the imports don't yet exist, scan the existing `client_test.go` and reuse whatever helper it uses to spin up a fake relay (the file already has WebSocket-based tests).

- [ ] **Step 2: Run the tests and confirm they fail**

```bash
cd go && go test ./internal/relayclient/ -run 'TestClientDoneClosesWhenReadLoopExits|TestClientCloseTerminatesReadLoop' -count=1
```

Expected: FAIL with "c.Done undefined" / "c.Close undefined".

- [ ] **Step 3: Add the `done` channel and `Close()` method to `client.go`**

In `go/internal/relayclient/client.go`, modify the struct and `Connect`:

```go
type Client struct {
	url             string
	accessToken     string
	deviceID        string
	conn            *websocket.Conn
	mu              sync.Mutex
	snapshotHandler func(context.Context, string) ([]byte, error)
	inputHandler    func(context.Context, string, []byte) error
	resizeHandler   func(context.Context, string, uint32, uint32) error

	done     chan error
	closeOnce sync.Once
}

func New(url string, accessToken string, deviceID string) *Client {
	return &Client{
		url:         url,
		accessToken: accessToken,
		deviceID:    deviceID,
		done:        make(chan error, 1),
	}
}
```

Modify `readLoop` to push the exit error to `done` and close it (under sync.Once) on exit:

```go
func (c *Client) readLoop(ctx context.Context) {
	var loopErr error
	defer func() {
		c.closeOnce.Do(func() {
			if loopErr != nil {
				c.done <- loopErr
			}
			close(c.done)
		})
	}()

	for {
		msgType, data, err := c.conn.Read(ctx)
		if err != nil {
			loopErr = err
			return
		}
		// ... existing body unchanged
		if msgType == websocket.MessageBinary {
			c.handleInputFrame(ctx, data)
			continue
		}
		if msgType != websocket.MessageText {
			continue
		}

		env, err := relayproto.DecodeEnvelope(data)
		if err != nil {
			loopErr = err
			return
		}
		switch env.Type {
		case relayproto.TypeSessionSnapshotReq:
			c.handleSnapshotRequest(ctx, env)
		case relayproto.TypeClientResize:
			c.handleResizeRequest(ctx, env)
		}
	}
}
```

Add public methods at the bottom of `client.go`:

```go
// Done returns a channel that is closed when the client's read loop exits.
// A non-nil error is delivered before the close if the exit was caused by
// a read or decode error; a nil delivery indicates a normal close (e.g.
// after Close() was called).
func (c *Client) Done() <-chan error {
	return c.done
}

// Close gracefully terminates the WSS connection and the read loop.
// Idempotent; the second call returns nil without error.
func (c *Client) Close() error {
	if c.conn == nil {
		return nil
	}
	return c.conn.Close(websocket.StatusNormalClosure, "client closing")
}
```

- [ ] **Step 4: Run the tests and confirm they pass**

```bash
go test ./internal/relayclient/ -run 'TestClientDoneClosesWhenReadLoopExits|TestClientCloseTerminatesReadLoop' -count=1
```

Expected: PASS.

- [ ] **Step 5: Run the full relayclient suite**

```bash
go test ./internal/relayclient/ -count=1
```

Expected: all existing tests still pass plus the two new ones.

- [ ] **Step 6: Commit**

```bash
git add go/internal/relayclient/client.go go/internal/relayclient/client_test.go
git commit -m "relayclient: add Done() and Close() so callers can detect read-loop exit"
```

---

## Task 2: Define `RelayState`, `Phase`, and `Clock` interface

**Why:** The supervisor's state machine and reconnect loop both need typed states and an injectable clock for deterministic testing.

**Files:**
- Create: `go/internal/relayclient/state.go`
- Create: `go/internal/relayclient/clock.go`
- Create: `go/internal/relayclient/state_test.go`

- [ ] **Step 1: Write the failing test**

Create `go/internal/relayclient/state_test.go`:

```go
package relayclient

import (
	"testing"
	"time"
)

func TestPhaseConstantsAreNonEmptyAndDistinct(t *testing.T) {
	all := []Phase{PhaseConnecting, PhaseConnected, PhaseReconnecting, PhaseClosed}
	seen := map[Phase]bool{}
	for _, p := range all {
		if p == "" {
			t.Errorf("phase %q is empty", p)
		}
		if seen[p] {
			t.Errorf("phase %q is duplicated", p)
		}
		seen[p] = true
	}
}

func TestRelayStateZeroValueIsValid(t *testing.T) {
	var s RelayState
	if s.Phase != "" || s.Attempt != 0 || !s.LastConnectedAt.IsZero() {
		t.Errorf("zero value not zero: %+v", s)
	}
	_ = s.LastError
	_ = s.NextRetryAt
	_ = s.AuthFailures
}

func TestRealClockSleepReturnsOnContextCancel(t *testing.T) {
	c := realClock{}
	ctx, cancel := contextWithCancel(t)
	go func() { time.Sleep(50 * time.Millisecond); cancel() }()
	start := time.Now()
	if err := c.Sleep(ctx, 5*time.Second); err == nil {
		t.Fatalf("expected cancellation error, got nil")
	}
	if time.Since(start) > time.Second {
		t.Fatalf("Sleep did not honor cancel within 1s")
	}
}

func contextWithCancel(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithCancel(context.Background())
}
```

Add `import "context"` at the top.

- [ ] **Step 2: Run and confirm FAIL**

```bash
go test ./internal/relayclient/ -run 'TestPhase|TestRelayState|TestRealClock' -count=1
```

Expected: FAIL with "Phase undefined" / "RelayState undefined" / "realClock undefined".

- [ ] **Step 3: Create `state.go`**

```go
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
```

- [ ] **Step 4: Create `clock.go`**

```go
package relayclient

import (
	"context"
	"time"
)

// Clock abstracts the supervisor's only time-related operations so tests
// can drive the reconnect loop deterministically without real sleeps.
type Clock interface {
	Now() time.Time
	// Sleep blocks for d or until ctx is canceled. Returns ctx.Err() on
	// cancellation, nil otherwise.
	Sleep(ctx context.Context, d time.Duration) error
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

func (realClock) Sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// RealClock returns the production clock implementation.
func RealClock() Clock { return realClock{} }
```

- [ ] **Step 5: Run and confirm PASS**

```bash
go test ./internal/relayclient/ -run 'TestPhase|TestRelayState|TestRealClock' -count=1
```

Expected: PASS (3 tests).

- [ ] **Step 6: Commit**

```bash
git add go/internal/relayclient/state.go go/internal/relayclient/clock.go go/internal/relayclient/state_test.go
git commit -m "relayclient: add Phase, RelayState, and injectable Clock for supervisor"
```

---

## Task 3: Backoff schedule + jitter

**Why:** Pure function, easy to unit test in isolation. Doing this before the supervisor lets us nail the schedule constants once, regardless of supervisor wiring.

**Files:**
- Create: `go/internal/relayclient/backoff.go`
- Create: `go/internal/relayclient/backoff_test.go`

- [ ] **Step 1: Write the failing test**

Create `go/internal/relayclient/backoff_test.go`:

```go
package relayclient

import (
	"testing"
	"time"
)

func TestComputeBackoffSchedule(t *testing.T) {
	// When jitter is 0.0 (rand returns 0.5 → factor 1.0) the schedule is
	// the canonical one.
	rng := func() float64 { return 0.5 }
	cases := []struct {
		attempt int
		want    time.Duration
	}{
		{0, 1 * time.Second},
		{1, 2 * time.Second},
		{2, 5 * time.Second},
		{3, 10 * time.Second},
		{4, 30 * time.Second},
		{5, 30 * time.Second},  // cap
		{50, 30 * time.Second}, // far past cap
	}
	for _, tc := range cases {
		got := computeBackoff(tc.attempt, rng)
		if got != tc.want {
			t.Errorf("attempt=%d got %v want %v", tc.attempt, got, tc.want)
		}
	}
}

func TestComputeBackoffJitterBounds(t *testing.T) {
	// rng=0.0 → 0.8x; rng=1.0 → 1.2x.
	min := computeBackoff(2, func() float64 { return 0.0 }) // 5s × 0.8 = 4s
	max := computeBackoff(2, func() float64 { return 1.0 }) // 5s × 1.2 = 6s
	if min != 4*time.Second {
		t.Errorf("min jitter at attempt=2 got %v want 4s", min)
	}
	if max != 6*time.Second {
		t.Errorf("max jitter at attempt=2 got %v want 6s", max)
	}
}
```

- [ ] **Step 2: Run and confirm FAIL**

```bash
go test ./internal/relayclient/ -run 'TestComputeBackoff' -count=1
```

Expected: FAIL with "computeBackoff undefined".

- [ ] **Step 3: Create `backoff.go`**

```go
package relayclient

import "time"

// backoffSchedule is the canonical reconnect delay sequence. Past the
// last entry the supervisor stays at the cap (last entry).
var backoffSchedule = []time.Duration{
	1 * time.Second,
	2 * time.Second,
	5 * time.Second,
	10 * time.Second,
	30 * time.Second,
}

// computeBackoff returns the delay before the supervisor's next reconnect
// attempt. Applies ±20% jitter using the supplied rng (which must return
// a value in [0, 1]) to spread reconnects from many daemons recovering
// after a relay restart.
func computeBackoff(attempt int, rng func() float64) time.Duration {
	var base time.Duration
	if attempt < len(backoffSchedule) {
		base = backoffSchedule[attempt]
	} else {
		base = backoffSchedule[len(backoffSchedule)-1]
	}
	// rng in [0,1] → factor in [0.8, 1.2]
	factor := 0.8 + 0.4*rng()
	return time.Duration(float64(base) * factor)
}
```

- [ ] **Step 4: Run and confirm PASS**

```bash
go test ./internal/relayclient/ -run 'TestComputeBackoff' -count=1
```

Expected: PASS (2 tests).

- [ ] **Step 5: Commit**

```bash
git add go/internal/relayclient/backoff.go go/internal/relayclient/backoff_test.go
git commit -m "relayclient: backoff schedule with bounded jitter"
```

---

## Task 4: `Supervisor` skeleton + state accessor + atomic client slot

**Why:** The supervisor's data structure is the foundation for every subsequent task. Defining it with a thread-safe State() reader and an atomic.Pointer client slot lets every following step add behavior without restructuring.

**Files:**
- Create: `go/internal/relayclient/supervisor.go`
- Create: `go/internal/relayclient/supervisor_test.go`

- [ ] **Step 1: Write the failing test**

Create `go/internal/relayclient/supervisor_test.go`:

```go
package relayclient

import (
	"context"
	"errors"
	"testing"
	"time"

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
```

- [ ] **Step 2: Run and confirm FAIL**

```bash
go test ./internal/relayclient/ -run 'TestSupervisorInitialState|TestSupervisorPublishOutputReturnsErrNotConnectedWhenIdle' -count=1
```

Expected: FAIL with "NewSupervisor undefined", "ErrNotConnected undefined".

- [ ] **Step 3: Create `supervisor.go` with the skeleton**

```go
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
```

- [ ] **Step 4: Run and confirm PASS**

```bash
go test ./internal/relayclient/ -run 'TestSupervisorInitialState|TestSupervisorPublishOutputReturnsErrNotConnectedWhenIdle' -count=1
```

Expected: PASS (2 tests).

- [ ] **Step 5: Build the package to confirm no other regressions**

```bash
go build ./internal/relayclient/...
```

Expected: success.

- [ ] **Step 6: Commit**

```bash
git add go/internal/relayclient/supervisor.go go/internal/relayclient/supervisor_test.go
git commit -m "relayclient: introduce Supervisor with State() and ErrNotConnected wrappers"
```

---

## Task 5: Supervisor reconnect loop with state transitions

**Why:** The core behavior. Drives the connect → connected → reconnect cycle, exposes state changes through `State()`, calls the reconnect callback after each successful connection.

**Files:**
- Modify: `go/internal/relayclient/supervisor.go`
- Modify: `go/internal/relayclient/supervisor_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `supervisor_test.go`:

```go
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

// fakeClient is a minimal stand-in for *relayclient.Client that the
// supervisor goroutine treats as live until KillRead is called.
type fakeClient struct {
	done chan error
}

func newFakeClient() *fakeClient {
	return &fakeClient{done: make(chan error, 1)}
}

func (f *fakeClient) Done() <-chan error { return f.done }
func (f *fakeClient) KillRead(err error) {
	select {
	case f.done <- err:
	default:
	}
	close(f.done)
}

// We can't yet plug fakeClient into Supervisor — it expects *Client.
// Instead, this test uses the Connect path of the real Client against a
// stub WS server. See TestSupervisorRunReachesConnectedAndReannounces.

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
```

Add `import "sync/atomic"` and `"github.com/coder/websocket"` to the test file's imports.

- [ ] **Step 2: Run and confirm FAIL**

```bash
go test ./internal/relayclient/ -run 'TestSupervisorRun' -count=1
```

Expected: FAIL with "Run undefined".

- [ ] **Step 3: Add `Run` to `supervisor.go`**

Append to `supervisor.go`:

```go
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
```

Add `"strings"` and `"time"` to the import list at the top of `supervisor.go`.

- [ ] **Step 4: Run the full relayclient suite**

```bash
go test ./internal/relayclient/ -count=1
```

Expected: all existing tests still pass, plus the two new TestSupervisorRun tests.

If any test hangs, kill it (Ctrl-C) and inspect: most likely cause is a missing `clk.gate <- struct{}{}` or an unconfigured factory.

- [ ] **Step 5: Vet**

```bash
go vet ./internal/relayclient/...
```

Expected: no issues.

- [ ] **Step 6: Commit**

```bash
git add go/internal/relayclient/supervisor.go go/internal/relayclient/supervisor_test.go
git commit -m "relayclient: Supervisor.Run main loop with reconnect + auth-failure terminal"
```

---

## Task 6: `Manager.ReannounceAllSessions` + supervisor wiring helper

**Why:** The supervisor's reconnect callback needs to call back into the Manager to walk the local store and re-announce. Adding the method to Manager keeps the session/store knowledge in the right layer.

**Files:**
- Modify: `go/internal/session/manager.go`
- Create: `go/internal/session/manager_reannounce_test.go`

- [ ] **Step 1: Write the failing test**

Create `go/internal/session/manager_reannounce_test.go`:

```go
package session

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	openapi "github.com/termix/termix/go/gen/openapi"
	"github.com/termix/termix/go/internal/credentials"
)

type reannounceFakeRelay struct {
	announced []string
	snapshots []string
}

func (r *reannounceFakeRelay) AnnounceSession(_ context.Context, s LocalSession) error {
	r.announced = append(r.announced, s.SessionID)
	return nil
}
func (r *reannounceFakeRelay) PublishSnapshot(_ context.Context, sessionID string, _ []byte) error {
	r.snapshots = append(r.snapshots, sessionID)
	return nil
}
func (r *reannounceFakeRelay) PublishOutput(context.Context, string, []byte) error { return nil }
func (r *reannounceFakeRelay) SetSnapshotHandler(func(context.Context, string) ([]byte, error)) {}
func (r *reannounceFakeRelay) SetInputHandler(func(context.Context, string, []byte) error)     {}

func TestReannounceAllSessionsSkipsNonRunningRows(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewStore(tmpDir)
	idA := uuid.NewString()
	idB := uuid.NewString()
	idC := uuid.NewString()
	if err := store.Save(LocalSession{SessionID: idA, Status: "running", TmuxSessionName: "termix_a"}); err != nil {
		t.Fatalf("save A: %v", err)
	}
	if err := store.Save(LocalSession{SessionID: idB, Status: "exited", TmuxSessionName: "termix_b"}); err != nil {
		t.Fatalf("save B: %v", err)
	}
	if err := store.Save(LocalSession{SessionID: idC, Status: "idle", TmuxSessionName: "termix_c"}); err != nil {
		t.Fatalf("save C: %v", err)
	}

	relay := &reannounceFakeRelay{}
	m := NewManager(ManagerOptions{
		Store: store,
		Relay: relay,
		LoadCredentials: func() (credentials.StoredCredentials, error) {
			return credentials.StoredCredentials{}, nil
		},
		Snapshot: func(_ context.Context, name string) ([]byte, error) {
			if name == "" {
				return nil, errors.New("empty")
			}
			return []byte("snap-" + name), nil
		},
	})

	m.ReannounceAllSessions(context.Background())

	if len(relay.announced) != 2 {
		t.Fatalf("announced=%v want 2 entries (A and C only)", relay.announced)
	}
	gotSet := map[string]bool{relay.announced[0]: true, relay.announced[1]: true}
	if !gotSet[idA] || !gotSet[idC] {
		t.Fatalf("expected to announce A and C, got %v", relay.announced)
	}
	if len(relay.snapshots) != 2 {
		t.Fatalf("snapshots=%v want 2 (one per running/idle session)", relay.snapshots)
	}
	_ = openapi.UpdateSessionRequestStatusRunning // silence import in case not used elsewhere
}
```

- [ ] **Step 2: Run and confirm FAIL**

```bash
go test ./internal/session/ -run 'TestReannounceAllSessionsSkipsNonRunningRows' -count=1
```

Expected: FAIL with "ReannounceAllSessions undefined".

- [ ] **Step 3: Add a `snapshot` field to Manager**

Today `opts.Snapshot` is captured only in a closure passed to `relay.SetSnapshotHandler`; the function is not stored as a field. ReannounceAllSessions needs direct access to it. In `go/internal/session/manager.go`:

a) Add a field to the `Manager` struct (alongside the other function fields like `loadCredentials`):

```go
snapshot SnapshotFunc
```

b) In `NewManager`, store `opts.Snapshot` into the new field at the same place where the relay handler closure is set up. The existing closure can read the field rather than `opts.Snapshot` directly to keep a single source of truth, but changing that closure is optional — leaving it intact is fine.

Append this assignment in the `NewManager` returned struct literal (matching the surrounding indentation):

```go
return &Manager{
    // ... existing fields ...
    snapshot: opts.Snapshot,
}
```

- [ ] **Step 4: Implement `ReannounceAllSessions`**

In `go/internal/session/manager.go`, add a new method (place after the existing `Reap` method block for cohesion):

```go
// ReannounceAllSessions iterates the local-state store and re-announces
// every running/idle session to the relay, then publishes a fresh
// snapshot for each. Intended to be plugged into the relay supervisor's
// SetReconnectCallback so that immediately after a fresh WSS handshake
// every existing viewer is reconciled with current pane state.
//
// Per-session failures are logged and the loop continues — one bad
// session must not stall re-announcement of the rest.
func (m *Manager) ReannounceAllSessions(ctx context.Context) {
	if m.store == nil || m.relay == nil {
		return
	}
	sessions, err := m.store.List()
	if err != nil {
		log.Printf("re-announce: store.List failed: %v", err)
		return
	}
	for _, s := range sessions {
		if s.Status != "running" && s.Status != "idle" {
			continue
		}
		if err := m.relay.AnnounceSession(ctx, s); err != nil {
			log.Printf("re-announce: AnnounceSession %s failed: %v", s.SessionID, err)
			continue
		}
		if m.snapshot == nil {
			continue
		}
		data, err := m.snapshot(ctx, s.TmuxSessionName)
		if err != nil {
			log.Printf("re-announce: snapshot %s failed: %v", s.SessionID, err)
			continue
		}
		if err := m.relay.PublishSnapshot(ctx, s.SessionID, data); err != nil {
			log.Printf("re-announce: PublishSnapshot %s failed: %v", s.SessionID, err)
		}
	}
}
```

- [ ] **Step 5: Run and confirm PASS**

```bash
go test ./internal/session/ -run 'TestReannounceAllSessionsSkipsNonRunningRows' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add go/internal/session/manager.go go/internal/session/manager_reannounce_test.go
git commit -m "session: add Manager.ReannounceAllSessions for relay supervisor reconnect callback"
```

---

## Task 7: Wire `relayclient.Supervisor` into `hostdaemon.Run`

**Why:** Replace the one-shot `relayclient.New(...).Connect(ctx)` with a Supervisor that runs in its own goroutine. Manager keeps consuming the same `RelayClient` interface — the supervisor satisfies it.

**Files:**
- Modify: `go/internal/hostdaemon/daemon.go`

- [ ] **Step 1: Replace the relay client construction**

Open `go/internal/hostdaemon/daemon.go` and find the existing block (lines 90–93 in v0.3.0):

```go
relayClient := relayclient.New(cfg.RelayWSURL, freshCreds.AccessToken, freshCreds.DeviceID)
if err := relayClient.Connect(ctx); err != nil {
    return fmt.Errorf("connect relay: %w", err)
}
```

Replace with:

```go
supervisor := relayclient.NewSupervisor(relayclient.SupervisorOptions{
    Factory: func(ctx context.Context, creds credentials.StoredCredentials) (*relayclient.Client, error) {
        c := relayclient.New(cfg.RelayWSURL, creds.AccessToken, creds.DeviceID)
        if err := c.Connect(ctx); err != nil {
            return nil, err
        }
        return c, nil
    },
    Refresher:       refresher,
    Clock:           relayclient.RealClock(),
    Rand:            rand.Float64,
    RequestShutdown: cancelRun,
})
```

Add `"math/rand"` to the imports.

Then change the Manager construction's `Relay` field to use `supervisor` and start the supervisor goroutine. After `manager := session.NewManager(...)`, add:

```go
supervisor.SetReconnectCallback(manager.ReannounceAllSessions)
go func() {
    if err := supervisor.Run(ctx); err != nil {
        log.Printf("relay supervisor exited: %v", err)
    }
}()
```

If the existing Manager wiring uses `relayClient` as the `Relay` field, replace it with `supervisor`. The supervisor satisfies the `session.RelayClient` interface (AnnounceSession / Publish* / SetSnapshotHandler / SetInputHandler).

Also remove the eager `relayClient.Connect(ctx)` failure mode — the supervisor handles connect failures via its retry loop. The daemon should boot even if the relay is temporarily unreachable.

- [ ] **Step 2: Build to confirm types resolve**

```bash
cd go && go build ./...
```

Expected: success. If `*relayclient.Supervisor` doesn't satisfy `session.RelayClient` because of missing methods, look at `session/types.go` for the interface signature and ensure Task 4's wrappers match exactly. Required methods: `AnnounceSession`, `PublishSnapshot`, `PublishOutput`, `SetSnapshotHandler`, `SetInputHandler`.

- [ ] **Step 3: Run the full Go test suite**

```bash
go test ./... -count=1
```

Expected: all tests pass. Tests that previously asserted on `relayClient.Connect` errors at boot may need updating if they exist; with the supervisor, those errors no longer surface from `hostdaemon.Run`.

If any test fails because a fake relay client doesn't satisfy `session.RelayClient` anymore (extra methods needed), update the fake in that test file with no-op implementations.

- [ ] **Step 4: Commit**

```bash
git add go/internal/hostdaemon/daemon.go
git commit -m "hostdaemon: switch to relayclient.Supervisor for auto-reconnect"
```

---

## Task 8: Add `Status` RPC + `RelayState` proto messages

**Why:** Establish the wire contract before wiring up the Manager-side implementation.

**Files:**
- Modify: `proto/daemon.proto`
- Modify: `go/gen/proto/daemonv1/daemon.pb.go` (regenerated)
- Modify: `go/gen/proto/daemonv1/daemon_grpc.pb.go` (regenerated)

- [ ] **Step 1: Add `Status` RPC to the service**

In `proto/daemon.proto`, locate the `service DaemonService` block and add `rpc Status` next to the existing RPCs:

```proto
service DaemonService {
  rpc Health(HealthRequest) returns (HealthResponse);
  rpc Shutdown(ShutdownRequest) returns (ShutdownResponse);
  rpc StartSession(StartSessionRequest) returns (StartSessionResponse);
  rpc ListSessions(ListSessionsRequest) returns (ListSessionsResponse);
  rpc AttachInfo(AttachInfoRequest) returns (AttachInfoResponse);
  rpc EndSession(EndSessionRequest) returns (EndSessionResponse);
  rpc Doctor(DoctorRequest) returns (DoctorResponse);
  rpc Status(StatusRequest) returns (StatusResponse);
}
```

- [ ] **Step 2: Define the messages**

Append to the same file (after the existing `DoctorResponse` block):

```proto
message StatusRequest {}

message StatusResponse {
  string version = 1;
  string revision = 2;
  bool   modified = 3;
  int64  uptime_seconds = 4;
  RelayState relay = 5;
  repeated SessionSummary sessions = 6;
  string proxy_fingerprint = 7;
}

message RelayState {
  string phase = 1;             // "connecting" / "connected" / "reconnecting" / "closed"
  int32  attempt = 2;
  int64  last_connected_at = 3; // unix seconds; 0 = never
  string last_error = 4;
  int64  next_retry_at = 5;
  int32  auth_failures = 6;
}
```

- [ ] **Step 3: Regenerate the Go stubs**

From the worktree root:

```bash
protoc --go_out=go --go_opt=module=github.com/termix/termix/go \
       --go-grpc_out=go --go-grpc_opt=module=github.com/termix/termix/go \
       -I proto proto/daemon.proto
```

- [ ] **Step 4: Build to confirm regen**

```bash
cd go && go build ./...
```

Expected: success.

- [ ] **Step 5: Commit**

```bash
git add proto/daemon.proto go/gen/proto/daemonv1/
git commit -m "proto: add Status RPC + RelayState message"
```

---

## Task 9: `Manager.Status` implementation

**Why:** Bridge the supervisor's `State()` and the local store into a populated `StatusResponse`. Manager already has version + uptime + sessions + proxy fingerprint pieces handy.

**Files:**
- Modify: `go/internal/session/manager.go`
- Create: `go/internal/session/manager_status_test.go`

- [ ] **Step 1: Add `RelayStateSource` to `ManagerOptions`**

In `go/internal/session/manager.go`, extend `ManagerOptions`:

```go
type ManagerOptions struct {
	// ... existing fields ...

	// RelayStateSource returns the relay supervisor's current state for
	// the Status RPC. nil means "no relay state visibility" — Status
	// reports phase="" in that case.
	RelayStateSource func() RelayStateSnapshot

	// StartTime is recorded by the daemon and used to compute
	// uptime_seconds in Status responses. Defaults to time.Now() at
	// Manager construction when not supplied.
	StartTime time.Time
}

// RelayStateSnapshot mirrors the supervisor's RelayState without
// importing relayclient (which would create a layering violation).
// hostdaemon.Run constructs the source closure that bridges the two.
type RelayStateSnapshot struct {
	Phase           string
	Attempt         int
	LastConnectedAt time.Time
	LastError       string
	NextRetryAt     time.Time
	AuthFailures    int
}
```

Extend the `Manager` struct with:

```go
relayStateSource func() RelayStateSnapshot
startTime        time.Time
```

Update `NewManager` to copy the new options into the struct and default `startTime` to `time.Now()` when unset.

- [ ] **Step 2: Write the failing test**

Create `go/internal/session/manager_status_test.go`:

```go
package session

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	daemonv1 "github.com/termix/termix/go/gen/proto/daemonv1"
	"github.com/termix/termix/go/internal/credentials"
)

func TestManagerStatusReportsConnectedRelayWithSessions(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewStore(tmpDir)
	id := uuid.NewString()
	if err := store.Save(LocalSession{
		SessionID:       id,
		Tool:            "claude",
		Status:          "running",
		TmuxSessionName: "termix_test",
	}); err != nil {
		t.Fatalf("save: %v", err)
	}

	start := time.Date(2026, 5, 8, 9, 0, 0, 0, time.UTC)
	connectedAt := time.Date(2026, 5, 8, 9, 5, 30, 0, time.UTC)
	now := time.Date(2026, 5, 8, 9, 10, 0, 0, time.UTC)

	m := NewManager(ManagerOptions{
		Store:     store,
		StartTime: start,
		Now:       func() time.Time { return now },
		LoadCredentials: func() (credentials.StoredCredentials, error) {
			return credentials.StoredCredentials{}, nil
		},
		RelayStateSource: func() RelayStateSnapshot {
			return RelayStateSnapshot{
				Phase:           "connected",
				Attempt:         0,
				LastConnectedAt: connectedAt,
			}
		},
		ProxyFingerprint: "fp123",
	})

	resp, err := m.Status(context.Background(), &daemonv1.StatusRequest{})
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if resp.GetUptimeSeconds() != 600 { // 9:10 - 9:00
		t.Errorf("UptimeSeconds=%d want 600", resp.GetUptimeSeconds())
	}
	if resp.GetRelay().GetPhase() != "connected" {
		t.Errorf("Phase=%q want connected", resp.GetRelay().GetPhase())
	}
	if resp.GetRelay().GetLastConnectedAt() != connectedAt.Unix() {
		t.Errorf("LastConnectedAt=%d want %d", resp.GetRelay().GetLastConnectedAt(), connectedAt.Unix())
	}
	if len(resp.GetSessions()) != 1 || resp.GetSessions()[0].GetSessionId() != id {
		t.Errorf("Sessions=%v want one entry with id=%s", resp.GetSessions(), id)
	}
	if resp.GetProxyFingerprint() != "fp123" {
		t.Errorf("ProxyFingerprint=%q want fp123", resp.GetProxyFingerprint())
	}
}

func TestManagerStatusReportsReconnectingPhase(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewStore(tmpDir)
	now := time.Date(2026, 5, 8, 9, 10, 0, 0, time.UTC)
	nextRetry := now.Add(8 * time.Second)
	m := NewManager(ManagerOptions{
		Store:     store,
		StartTime: now.Add(-time.Hour),
		Now:       func() time.Time { return now },
		LoadCredentials: func() (credentials.StoredCredentials, error) {
			return credentials.StoredCredentials{}, nil
		},
		RelayStateSource: func() RelayStateSnapshot {
			return RelayStateSnapshot{
				Phase:        "reconnecting",
				Attempt:      4,
				LastError:    "write tcp ... broken pipe",
				NextRetryAt:  nextRetry,
				AuthFailures: 0,
			}
		},
	})
	resp, err := m.Status(context.Background(), &daemonv1.StatusRequest{})
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if resp.GetRelay().GetPhase() != "reconnecting" {
		t.Errorf("Phase=%q want reconnecting", resp.GetRelay().GetPhase())
	}
	if resp.GetRelay().GetAttempt() != 4 {
		t.Errorf("Attempt=%d want 4", resp.GetRelay().GetAttempt())
	}
	if resp.GetRelay().GetLastError() != "write tcp ... broken pipe" {
		t.Errorf("LastError=%q", resp.GetRelay().GetLastError())
	}
	if resp.GetRelay().GetNextRetryAt() != nextRetry.Unix() {
		t.Errorf("NextRetryAt=%d want %d", resp.GetRelay().GetNextRetryAt(), nextRetry.Unix())
	}
}
```

- [ ] **Step 3: Run and confirm FAIL**

```bash
go test ./internal/session/ -run 'TestManagerStatus' -count=1
```

Expected: FAIL with "Status undefined" or "RelayStateSource undefined".

- [ ] **Step 4: Implement `Status`**

In `go/internal/session/manager.go`, add the method (place near `ListSessions`):

```go
// Status reports the daemon's current health, relay supervisor state,
// active sessions, and proxy fingerprint for the `termix status` CLI.
func (m *Manager) Status(ctx context.Context, _ *daemonv1.StatusRequest) (*daemonv1.StatusResponse, error) {
	id := buildinfo.Current(m.version)
	resp := &daemonv1.StatusResponse{
		Version:          id.Version,
		Revision:         id.Revision,
		Modified:         id.Modified,
		UptimeSeconds:    int64(m.now().Sub(m.startTime).Seconds()),
		ProxyFingerprint: m.proxyFingerprint,
	}

	if m.relayStateSource != nil {
		st := m.relayStateSource()
		resp.Relay = &daemonv1.RelayState{
			Phase:        st.Phase,
			Attempt:      int32(st.Attempt),
			LastError:    st.LastError,
			AuthFailures: int32(st.AuthFailures),
		}
		if !st.LastConnectedAt.IsZero() {
			resp.Relay.LastConnectedAt = st.LastConnectedAt.Unix()
		}
		if !st.NextRetryAt.IsZero() {
			resp.Relay.NextRetryAt = st.NextRetryAt.Unix()
		}
	} else {
		resp.Relay = &daemonv1.RelayState{}
	}

	if m.store != nil {
		sessions, err := m.store.List()
		if err == nil {
			for _, item := range sessions {
				summary := &daemonv1.SessionSummary{
					SessionId:       item.SessionID,
					Name:            item.Name,
					Tool:            item.Tool,
					Status:          item.Status,
					TmuxSessionName: item.TmuxSessionName,
					Cwd:             item.Cwd,
				}
				if !item.StartedAt.IsZero() {
					summary.StartedAt = item.StartedAt.UTC().Format(time.RFC3339)
				}
				if m.tmux != nil && item.TmuxSessionName != "" {
					if m.tmux.HasSession(ctx, item.TmuxSessionName) {
						summary.LiveInTmux = true
						if pid, err := m.tmux.PanePID(ctx, item.TmuxSessionName); err == nil && pid > 0 {
							summary.PanePid = int32(pid)
						}
					}
				}
				resp.Sessions = append(resp.Sessions, summary)
			}
		}
	}
	return resp, nil
}
```

- [ ] **Step 5: Run and confirm PASS**

```bash
go test ./internal/session/ -run 'TestManagerStatus' -count=1
```

Expected: PASS (2 tests).

- [ ] **Step 6: Update `hostdaemon.Run` to populate `RelayStateSource` and `StartTime`**

In `go/internal/hostdaemon/daemon.go`, extend the `session.NewManager(...)` call's `ManagerOptions` literal to include:

```go
RelayStateSource: func() session.RelayStateSnapshot {
    st := supervisor.State()
    return session.RelayStateSnapshot{
        Phase:           string(st.Phase),
        Attempt:         st.Attempt,
        LastConnectedAt: st.LastConnectedAt,
        LastError:       st.LastError,
        NextRetryAt:     st.NextRetryAt,
        AuthFailures:    st.AuthFailures,
    }
},
StartTime: time.Now(),
```

- [ ] **Step 7: Build and full test**

```bash
go build ./...
go test ./... -count=1
```

Expected: all green.

- [ ] **Step 8: Commit**

```bash
git add go/internal/session/manager.go go/internal/session/manager_status_test.go go/internal/hostdaemon/daemon.go
git commit -m "session: implement Manager.Status with relay state and uptime"
```

---

## Task 10: `runStatus` CLI command

**Why:** User-facing entry point. Combines the daemon RPC response with locally-readable data (credentials.email, host.json.enable_proxy, current proxy env) into the section-block output format.

**Files:**
- Modify: `go/cmd/termix/main.go`
- Modify: `go/cmd/termix/main_test.go`

- [ ] **Step 1: Add the dispatch**

In `go/cmd/termix/main.go`, in the `run()` function's switch, add a case for `"status"`:

```go
case "status":
    err = runStatus(ctx, deps)
```

Update the usage line:

```go
fmt.Fprintln(deps.stderr, "usage: termix <login|start|sessions|status|doctor|version>")
```

- [ ] **Step 2: Add `daemonClient` interface support for Status**

In `go/cmd/termix/main.go`, find the `daemonClient` interface (around line 651) and add:

```go
Status(ctx context.Context, in *daemonv1.StatusRequest, opts ...grpc.CallOption) (*daemonv1.StatusResponse, error)
```

- [ ] **Step 3: Write the failing test**

Append to `go/cmd/termix/main_test.go`:

```go
func TestRunStatusPrintsConnectedSection(t *testing.T) {
	paths := testPaths(t)
	writeLoggedInHostFiles(t, paths)
	deps := testDeps(paths)
	var stdout bytes.Buffer
	deps.stdout = &stdout

	client := &fakeDaemonClient{
		healthResponses: []*daemonv1.HealthResponse{healthResponseMatching()},
		statusResponse: &daemonv1.StatusResponse{
			Version:       "v0.4.0",
			Revision:      "abc123",
			UptimeSeconds: 600,
			Relay: &daemonv1.RelayState{
				Phase:           "connected",
				Attempt:         2,
				LastConnectedAt: time.Date(2026, 5, 8, 9, 5, 0, 0, time.UTC).Unix(),
			},
			Sessions: []*daemonv1.SessionSummary{
				{
					SessionId: "11111111-1111-1111-1111-111111111111",
					Tool:      "claude",
					Name:      "main",
					Status:    "running",
					LiveInTmux: true,
					PanePid:    4242,
				},
			},
			ProxyFingerprint: "fp123",
		},
	}
	deps.dialDaemon = func(context.Context, string) (daemonv1.DaemonServiceClient, io.Closer, error) {
		return client, nopCloser{}, nil
	}

	code := run(context.Background(), []string{"termix", "status"}, deps)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d (stdout=%q)", code, stdout.String())
	}
	out := stdout.String()
	for _, want := range []string{
		"USER", "DAEMON", "RELAY", "SESSIONS", "PROXY",
		"v0.4.0",
		"connected",
		"11111111-1111-1111-1111-111111111111",
		"claude",
		"fp123",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\noutput:\n%s", want, out)
		}
	}
}

func TestRunStatusPrintsReconnectingDetails(t *testing.T) {
	paths := testPaths(t)
	writeLoggedInHostFiles(t, paths)
	deps := testDeps(paths)
	var stdout bytes.Buffer
	deps.stdout = &stdout

	client := &fakeDaemonClient{
		healthResponses: []*daemonv1.HealthResponse{healthResponseMatching()},
		statusResponse: &daemonv1.StatusResponse{
			Version: "v0.4.0",
			Relay: &daemonv1.RelayState{
				Phase:       "reconnecting",
				Attempt:     4,
				NextRetryAt: time.Date(2026, 5, 8, 9, 5, 8, 0, time.UTC).Unix(),
				LastError:   "write tcp ... broken pipe",
			},
		},
	}
	deps.dialDaemon = func(context.Context, string) (daemonv1.DaemonServiceClient, io.Closer, error) {
		return client, nopCloser{}, nil
	}

	code := run(context.Background(), []string{"termix", "status"}, deps)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	out := stdout.String()
	if !strings.Contains(out, "reconnecting") {
		t.Errorf("missing reconnecting; got:\n%s", out)
	}
	if !strings.Contains(out, "attempt 4") {
		t.Errorf("missing attempt 4; got:\n%s", out)
	}
	if !strings.Contains(out, "broken pipe") {
		t.Errorf("missing last error; got:\n%s", out)
	}
}
```

Update `fakeDaemonClient` (in main_test.go) to add `statusResponse *daemonv1.StatusResponse` and a `Status` method that returns it:

```go
type fakeDaemonClient struct {
	// ... existing fields ...
	statusResponse *daemonv1.StatusResponse
}

func (f *fakeDaemonClient) Status(context.Context, *daemonv1.StatusRequest, ...grpc.CallOption) (*daemonv1.StatusResponse, error) {
	if f.statusResponse == nil {
		return &daemonv1.StatusResponse{}, nil
	}
	return f.statusResponse, nil
}
```

- [ ] **Step 4: Run and confirm FAIL**

```bash
go test ./cmd/termix/ -run 'TestRunStatus' -count=1
```

Expected: FAIL with "runStatus undefined".

- [ ] **Step 5: Implement `runStatus`**

Append to `go/cmd/termix/main.go`:

```go
// runStatus prints a section-block summary covering logged-in user,
// daemon health, relay connection state, active sessions, and proxy
// policy. The output mirrors the spec's worked example so it stays
// stable as a paste-into-a-bug-report artifact.
func runStatus(ctx context.Context, deps cliDeps) error {
	creds, _ := credentials.Load(deps.paths.CredentialsFile)
	cfg, _ := config.LoadHostConfig(deps.paths.HostConfigFile)

	if err := ensureDaemon(ctx, deps); err != nil {
		return err
	}
	client, conn, err := deps.dialDaemon(ctx, daemonipc.SocketPath(deps.paths))
	if err != nil {
		return err
	}
	defer conn.Close()

	resp, err := client.Status(ctx, &daemonv1.StatusRequest{})
	if err != nil {
		return err
	}

	w := deps.stdout

	fmt.Fprintln(w, "USER")
	if creds.UserID != "" {
		// Email isn't stored in StoredCredentials; surface the user id
		// instead. Future work: persist email at login.
		fmt.Fprintf(w, "  user_id %s\n", creds.UserID)
	} else {
		fmt.Fprintln(w, "  (not logged in)")
	}
	if creds.ServerBaseURL != "" {
		fmt.Fprintf(w, "  %s\n", creds.ServerBaseURL)
	}
	fmt.Fprintln(w)

	fmt.Fprintln(w, "DAEMON")
	identity := fmt.Sprintf("%s (rev %s)", resp.GetVersion(), resp.GetRevision())
	if resp.GetModified() {
		identity += " dirty"
	}
	uptime := time.Duration(resp.GetUptimeSeconds()) * time.Second
	fmt.Fprintf(w, "  up %s, version %s\n", uptime, identity)
	fmt.Fprintf(w, "  socket %s\n", daemonipc.SocketPath(deps.paths))
	fmt.Fprintln(w)

	fmt.Fprintln(w, "RELAY")
	rs := resp.GetRelay()
	switch rs.GetPhase() {
	case "connected":
		fmt.Fprintf(w, "  connected (since %s UTC)\n", time.Unix(rs.GetLastConnectedAt(), 0).UTC().Format(time.RFC3339))
		if rs.GetAttempt() > 0 {
			fmt.Fprintf(w, "  reconnects this session: %d\n", rs.GetAttempt())
		}
	case "reconnecting":
		nextRetry := time.Unix(rs.GetNextRetryAt(), 0).UTC()
		lastConnected := "never"
		if rs.GetLastConnectedAt() > 0 {
			lastConnected = time.Unix(rs.GetLastConnectedAt(), 0).UTC().Format(time.RFC3339)
		}
		fmt.Fprintf(w, "  reconnecting (attempt %d, next try at %s, last connected %s)\n",
			rs.GetAttempt(), nextRetry.Format(time.RFC3339), lastConnected)
		if rs.GetLastError() != "" {
			fmt.Fprintf(w, "  last error: %s\n", rs.GetLastError())
		}
	case "closed":
		fmt.Fprintf(w, "  closed: %s\n", rs.GetLastError())
	default:
		fmt.Fprintf(w, "  %s\n", rs.GetPhase())
	}
	fmt.Fprintln(w)

	fmt.Fprintf(w, "SESSIONS  (%d active)\n", len(resp.GetSessions()))
	for _, s := range resp.GetSessions() {
		state := "orphan"
		if s.GetLiveInTmux() {
			state = "live"
		}
		name := s.GetName()
		if name == "" {
			name = "-"
		}
		fmt.Fprintf(w, "  %s  %s  %s  %s  pid %d\n",
			s.GetSessionId(), s.GetTool(), name, state, s.GetPanePid())
	}
	fmt.Fprintln(w)

	fmt.Fprintln(w, "PROXY")
	fmt.Fprintf(w, "  enable_proxy: %t\n", cfg.EnableProxy)
	envVals := []string{}
	for _, name := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY"} {
		if v := deps.getenv(name); v != "" {
			envVals = append(envVals, name+"="+v)
		} else if v := deps.getenv(strings.ToLower(name)); v != "" {
			envVals = append(envVals, strings.ToLower(name)+"="+v)
		}
	}
	if len(envVals) == 0 {
		fmt.Fprintln(w, "  effective:    bypassed (HTTP_PROXY / HTTPS_PROXY / ALL_PROXY / NO_PROXY all unset)")
	} else {
		fmt.Fprintf(w, "  effective:    %s\n", strings.Join(envVals, ", "))
	}
	fmt.Fprintf(w, "  fingerprint:  %s\n", resp.GetProxyFingerprint())
	return nil
}
```

- [ ] **Step 6: Run and confirm PASS**

```bash
go test ./cmd/termix/ -run 'TestRunStatus' -count=1
```

Expected: PASS (2 tests).

- [ ] **Step 7: Run the full Go suite**

```bash
go test ./... -count=1
```

Expected: all green.

- [ ] **Step 8: Commit**

```bash
git add go/cmd/termix/main.go go/cmd/termix/main_test.go
git commit -m "termix: add `status` command surfacing daemon, relay, sessions, proxy"
```

---

## Task 11: SPA reconnect supervisor in `bridge/inbound.ts`

**Why:** Add the same state machine semantics on the SPA side: detect close/error events, run a backoff loop, refresh the access token before each attempt, expose a state observable for the UI.

**Files:**
- Modify: `web/app/src/bridge/inbound.ts`
- Create: `web/app/src/bridge/reconnect.ts`
- Create: `web/app/src/bridge/reconnect.test.ts`

- [ ] **Step 1: Write the failing test**

Create `web/app/src/bridge/reconnect.test.ts`:

```typescript
import { describe, it, expect, vi, beforeEach } from "vitest";
import { signal } from "@preact/signals";
import { createReconnectSupervisor } from "./reconnect";

const sleep = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));

describe("createReconnectSupervisor", () => {
  beforeEach(() => {
    vi.useRealTimers();
  });

  it("starts in connecting and transitions to connected on success", async () => {
    const state = signal<{ phase: string; attempt: number }>({ phase: "", attempt: 0 });
    const sup = createReconnectSupervisor({
      connect: async () => ({ disconnect: () => {} }),
      refreshToken: async () => "tok",
      onStateChange: (s) => (state.value = s),
      now: () => new Date("2026-05-08T09:00:00Z"),
    });
    sup.start();
    await sleep(10);
    expect(state.value.phase).toBe("connected");
  });

  it("retries with backoff after disconnect and increments attempt", async () => {
    vi.useFakeTimers();
    let calls = 0;
    const seenPhases: string[] = [];
    const sup = createReconnectSupervisor({
      connect: async () => {
        calls++;
        return {
          disconnect: () => {},
          // first call: simulate immediate close
          onCloseTrigger: calls === 1 ? () => sup.signalClose(new Error("server EOF")) : undefined,
        };
      },
      refreshToken: async () => "tok",
      onStateChange: (s) => seenPhases.push(s.phase),
      now: () => new Date("2026-05-08T09:00:00Z"),
    });
    sup.start();
    await vi.runAllTimersAsync();
    expect(calls).toBeGreaterThanOrEqual(2);
    expect(seenPhases).toContain("reconnecting");
    expect(seenPhases).toContain("connected");
  });

  it("transitions to gave-up after 5 minutes of failed attempts", async () => {
    vi.useFakeTimers();
    let phase = "";
    const sup = createReconnectSupervisor({
      connect: async () => {
        throw new Error("ECONNREFUSED");
      },
      refreshToken: async () => "tok",
      onStateChange: (s) => (phase = s.phase),
      now: () => new Date("2026-05-08T09:00:00Z"),
    });
    sup.start();
    // Advance 5 minutes + 1 second
    await vi.advanceTimersByTimeAsync(5 * 60 * 1000 + 1000);
    expect(phase).toBe("gave-up");
  });

  it("retry() resets to reconnecting and tries again", async () => {
    vi.useFakeTimers();
    let calls = 0;
    let phase = "";
    const sup = createReconnectSupervisor({
      connect: async () => {
        calls++;
        if (calls < 3) throw new Error("nope");
        return { disconnect: () => {} };
      },
      refreshToken: async () => "tok",
      onStateChange: (s) => (phase = s.phase),
      now: () => new Date("2026-05-08T09:00:00Z"),
    });
    sup.start();
    await vi.advanceTimersByTimeAsync(5 * 60 * 1000 + 1000);
    expect(phase).toBe("gave-up");

    sup.retry();
    await vi.advanceTimersByTimeAsync(60_000);
    expect(phase).toBe("connected");
  });
});
```

- [ ] **Step 2: Run and confirm FAIL**

```bash
cd web/app && npm test -- --run src/bridge/reconnect.test.ts
```

Expected: FAIL with "Cannot find module './reconnect'".

- [ ] **Step 3: Create `web/app/src/bridge/reconnect.ts`**

```typescript
const BACKOFF_SCHEDULE_MS = [1000, 2000, 5000, 10000, 30000];
const GIVE_UP_AFTER_MS = 5 * 60 * 1000;

export interface SupervisorState {
  phase: "connecting" | "connected" | "reconnecting" | "gave-up" | "closed";
  attempt: number;
  lastConnectedAt: Date | null;
  lastError: string;
  attemptHistory: Array<{ at: Date; error: string }>;
}

export interface ConnectHandle {
  disconnect: () => void;
  onCloseTrigger?: () => void; // for tests; real connect attaches close handlers itself
}

export interface ReconnectOptions {
  connect: (token: string) => Promise<ConnectHandle>;
  refreshToken: () => Promise<string>;
  onStateChange: (state: SupervisorState) => void;
  now?: () => Date;
  rng?: () => number;
}

export interface ReconnectSupervisor {
  start: () => void;
  stop: () => void;
  retry: () => void;
  signalClose: (err: unknown) => void;
  state: () => SupervisorState;
}

function backoffMs(attempt: number, rng: () => number): number {
  const base =
    attempt < BACKOFF_SCHEDULE_MS.length
      ? BACKOFF_SCHEDULE_MS[attempt]
      : BACKOFF_SCHEDULE_MS[BACKOFF_SCHEDULE_MS.length - 1];
  const factor = 0.8 + 0.4 * rng();
  return Math.round(base * factor);
}

export function createReconnectSupervisor(opts: ReconnectOptions): ReconnectSupervisor {
  const now = opts.now ?? (() => new Date());
  const rng = opts.rng ?? Math.random;
  let state: SupervisorState = {
    phase: "connecting",
    attempt: 0,
    lastConnectedAt: null,
    lastError: "",
    attemptHistory: [],
  };
  let stopped = false;
  let pendingTimer: ReturnType<typeof setTimeout> | null = null;
  let giveUpAt: Date | null = null;
  let currentHandle: ConnectHandle | null = null;
  let closeSignal: { resolve: (err: unknown) => void } | null = null;

  const setState = (mut: (s: SupervisorState) => void) => {
    state = { ...state };
    state.attemptHistory = [...state.attemptHistory];
    mut(state);
    opts.onStateChange(state);
  };

  const waitClose = () =>
    new Promise<unknown>((resolve) => {
      closeSignal = { resolve };
    });

  const attemptOnce = async () => {
    try {
      const token = await opts.refreshToken();
      const handle = await opts.connect(token);
      currentHandle = handle;
      setState((s) => {
        s.phase = "connected";
        s.lastConnectedAt = now();
        s.lastError = "";
      });
      giveUpAt = null;
      // Allow tests to fire a close right after connect.
      if (handle.onCloseTrigger) handle.onCloseTrigger();
      const closeErr = await waitClose();
      currentHandle = null;
      const errMsg = closeErr instanceof Error ? closeErr.message : String(closeErr ?? "closed");
      setState((s) => {
        s.phase = "reconnecting";
        s.attempt += 1;
        s.lastError = errMsg;
        s.attemptHistory.push({ at: now(), error: errMsg });
        if (s.attemptHistory.length > 5) s.attemptHistory.shift();
      });
    } catch (err) {
      const errMsg = err instanceof Error ? err.message : String(err);
      setState((s) => {
        s.phase = "reconnecting";
        s.attempt += 1;
        s.lastError = errMsg;
        s.attemptHistory.push({ at: now(), error: errMsg });
        if (s.attemptHistory.length > 5) s.attemptHistory.shift();
      });
    }
  };

  const loop = async () => {
    while (!stopped) {
      if (giveUpAt && now().getTime() >= giveUpAt.getTime()) {
        setState((s) => {
          s.phase = "gave-up";
        });
        return;
      }
      if (state.phase !== "connecting" && state.attempt > 0) {
        const delay = backoffMs(state.attempt - 1, rng);
        if (giveUpAt === null) giveUpAt = new Date(now().getTime() + GIVE_UP_AFTER_MS);
        await new Promise<void>((resolve) => {
          pendingTimer = setTimeout(resolve, delay);
        });
        if (stopped) return;
      }
      await attemptOnce();
      if (stopped) return;
    }
  };

  return {
    start: () => {
      stopped = false;
      void loop();
    },
    stop: () => {
      stopped = true;
      if (pendingTimer) clearTimeout(pendingTimer);
      pendingTimer = null;
      if (currentHandle) currentHandle.disconnect();
      if (closeSignal) closeSignal.resolve(new Error("supervisor stopped"));
      setState((s) => {
        s.phase = "closed";
      });
    },
    retry: () => {
      giveUpAt = null;
      setState((s) => {
        s.phase = "reconnecting";
        s.attempt = 0;
      });
      void loop();
    },
    signalClose: (err) => {
      if (closeSignal) {
        closeSignal.resolve(err);
        closeSignal = null;
      }
    },
    state: () => state,
  };
}
```

- [ ] **Step 4: Run and confirm PASS**

```bash
cd web/app && npm test -- --run src/bridge/reconnect.test.ts
```

Expected: PASS (4 tests).

- [ ] **Step 5: Commit**

```bash
git add web/app/src/bridge/reconnect.ts web/app/src/bridge/reconnect.test.ts
git commit -m "bridge: add reconnect supervisor with backoff and 5-min give-up"
```

---

## Task 12: Wire `reconnect` into `inbound.ts`

**Why:** Replace the existing one-shot `setSession` lifecycle with a supervisor-driven one. Plumb the state change events into `outbound.onConnectionState` (extended).

**Files:**
- Modify: `web/app/src/bridge/inbound.ts`
- Modify: `web/app/src/bridge/outbound.ts`

- [ ] **Step 1: Extend `outbound.ts` to emit reconnect-aware states**

Open `web/app/src/bridge/outbound.ts` and locate `onConnectionState`. Extend the union of state values to include `"reconnecting"` and `"gave-up"`. Inspect the existing implementation to see where it relays to `window.onConnectionState`. Update its TypeScript types accordingly:

```typescript
export type ConnectionState =
  | { phase: "connecting" }
  | { phase: "connected" }
  | { phase: "reconnecting"; attempt: number; lastError: string }
  | { phase: "gave-up"; attemptCount: number; durationMs: number; lastError: string }
  | { phase: "disconnected" }
  | { phase: "error" };
```

If the existing API is `onConnectionState(state: string)` (string-only), extend the signature to take this richer object and update callers accordingly.

- [ ] **Step 2: Refactor `inbound.ts::setSession` to use the supervisor**

Modify `web/app/src/bridge/inbound.ts`:

- Import `createReconnectSupervisor` and the relevant types from `./reconnect`.
- Inside `setSession`, instead of calling `openWSClient` once, build a connect closure that does the WSS dial and returns a `ConnectHandle`.
- Call `createReconnectSupervisor({...})` and pipe its state into `outbound.onConnectionState`.
- On the WSClient's `onClose` / `onError`, call `sup.signalClose(err)`.
- Replace the existing `closeActive()` body with `sup.stop()`.

Sketch (the implementor should adapt to whatever `WSClient` interface exposes):

```typescript
const setSession = (sessionId: string, relayUrl: string, accessToken: string, deviceId: string): void => {
  closeActive();
  if (!sessionId || !relayUrl) return;

  let activeWS: WSClient | null = null;

  const sup = createReconnectSupervisor({
    connect: async (token) => {
      const url = new URL(relayUrl);
      url.searchParams.set("access_token", token);
      url.searchParams.set("device_id", deviceId);
      url.searchParams.set("session_id", sessionId);

      return new Promise((resolve, reject) => {
        const ws = openWSClient(url.toString(), {
          onOpen: () => {
            ws.sendText(encodeEnvelope("hello.android", { device_id: deviceId }));
            ws.sendText(encodeEnvelope("session.watch", { session_id: sessionId }));
            // ... existing session.watch / client.resize / heartbeat setup ...
            activeWS = ws;
            resolve({
              disconnect: () => ws.close(),
            });
          },
          onText: /* ... existing ... */,
          onBinary: /* ... existing ... */,
          onClose: () => sup.signalClose(new Error("ws close")),
          onError: () => sup.signalClose(new Error("ws error")),
        }, cfg.factory);
      });
    },
    refreshToken: async () => {
      const res = await fetch("/api/v1/auth/refresh", { method: "POST" });
      if (res.status === 401) {
        // Refresh token unrecoverable — bail to login (the SPA's existing
        // navigation helper does this; for now throw a sentinel).
        window.location.href = "/login?next=" + encodeURIComponent(window.location.pathname);
        throw new Error("refresh failed; redirecting");
      }
      const body = await res.json();
      return body.access_token as string;
    },
    onStateChange: (s) => {
      // Map supervisor state to outbound API.
      if (s.phase === "connected") outbound.onConnectionState({ phase: "connected" });
      else if (s.phase === "connecting") outbound.onConnectionState({ phase: "connecting" });
      else if (s.phase === "reconnecting") {
        outbound.onConnectionState({
          phase: "reconnecting",
          attempt: s.attempt,
          lastError: s.lastError,
        });
      } else if (s.phase === "gave-up") {
        outbound.onConnectionState({
          phase: "gave-up",
          attemptCount: s.attempt,
          durationMs: 5 * 60 * 1000,
          lastError: s.lastError,
        });
      } else if (s.phase === "closed") outbound.onConnectionState({ phase: "disconnected" });
    },
  });

  active = {
    sessionId,
    sup,
    // ... other fields ...
  };
  sup.start();
};
```

The implementor should preserve the input/output handlers and the heartbeat from the existing implementation; the supervisor only owns the connection lifecycle.

- [ ] **Step 3: Run existing inbound tests + new reconnect tests**

```bash
cd web/app && npm test -- --run src/bridge/
```

Expected: all bridge tests pass. Some existing tests in `inbound.test.ts` may need updating because the connection state object shape changed; update assertions to the new union.

- [ ] **Step 4: Typecheck**

```bash
npm run typecheck
```

Expected: no errors.

- [ ] **Step 5: Commit**

```bash
git add web/app/src/bridge/inbound.ts web/app/src/bridge/outbound.ts web/app/src/bridge/inbound.test.ts
git commit -m "bridge: drive ws lifecycle through reconnect supervisor"
```

---

## Task 13: `ReconnectBanner` component

**Why:** Lightweight horizontal indicator visible during transient reconnects (3s–5min). Distinct from the modal so the visual hierarchy matches the severity.

**Files:**
- Create: `web/app/src/components/reconnect-banner.tsx`
- Create: `web/app/src/components/reconnect-banner.test.tsx`
- Modify: `web/app/src/i18n/messages.ts`
- Modify: `web/app/src/theme/styles.css`

- [ ] **Step 1: Add i18n keys**

In `web/app/src/i18n/messages.ts`, locate the messages object and add to both EN and ZH variants:

```typescript
"relay.banner.reconnecting": "Reconnecting… (attempt {attempt})",
"relay.banner.reconnectingZh" not used; for ZH variant add the same key in ZH section:
"relay.banner.reconnecting": "重新连接中…（第 {attempt} 次）",
```

(Use the existing pattern in this file for placeholders — typically `{name}` substitution.)

- [ ] **Step 2: Write the failing test**

Create `web/app/src/components/reconnect-banner.test.tsx`:

```typescript
import { describe, it, expect, beforeEach } from "vitest";
import { render, screen } from "@testing-library/preact";
import { ReconnectBanner } from "./reconnect-banner";
import { setLocale } from "@/i18n/store";

describe("ReconnectBanner", () => {
  beforeEach(() => {
    localStorage.clear();
    setLocale("en");
  });

  it("renders nothing when not reconnecting", () => {
    const { container } = render(<ReconnectBanner phase="connected" attempt={0} />);
    expect(container.textContent ?? "").toBe("");
  });

  it("shows the attempt counter when reconnecting", () => {
    render(<ReconnectBanner phase="reconnecting" attempt={3} />);
    expect(screen.getByText(/Reconnecting/)).toBeTruthy();
    expect(screen.getByText(/3/)).toBeTruthy();
  });

  it("renders Chinese copy when locale is zh", () => {
    setLocale("zh");
    render(<ReconnectBanner phase="reconnecting" attempt={2} />);
    expect(screen.getByText(/重新连接中/)).toBeTruthy();
  });
});
```

- [ ] **Step 3: Confirm FAIL**

```bash
cd web/app && npm test -- --run src/components/reconnect-banner.test.tsx
```

Expected: FAIL with "Cannot find module './reconnect-banner'".

- [ ] **Step 4: Create the component**

`web/app/src/components/reconnect-banner.tsx`:

```typescript
import { t } from "@/i18n/store";

interface Props {
  phase: string;
  attempt: number;
}

export function ReconnectBanner({ phase, attempt }: Props) {
  if (phase !== "reconnecting") return null;
  return (
    <div role="status" className="reconnect-banner">
      {t("relay.banner.reconnecting", { attempt: String(attempt) })}
    </div>
  );
}
```

Add styles in `web/app/src/theme/styles.css`:

```css
.reconnect-banner {
  background: #fff8c5;
  color: #735c0f;
  padding: 6px 12px;
  font-size: 13px;
  text-align: center;
  border-bottom: 1px solid #e6d27a;
}
```

- [ ] **Step 5: Run and confirm PASS**

```bash
npm test -- --run src/components/reconnect-banner.test.tsx
```

Expected: PASS (3 tests).

- [ ] **Step 6: Commit**

```bash
git add web/app/src/components/reconnect-banner.tsx web/app/src/components/reconnect-banner.test.tsx web/app/src/i18n/messages.ts web/app/src/theme/styles.css
git commit -m "components: ReconnectBanner for 3s-5min reconnect window"
```

---

## Task 14: `DisconnectModal` component

**Why:** The persistent failure UI. Cannot auto-dismiss; two clear actions; collapsible details for technical users.

**Files:**
- Create: `web/app/src/components/disconnect-modal.tsx`
- Create: `web/app/src/components/disconnect-modal.test.tsx`
- Modify: `web/app/src/i18n/messages.ts`
- Modify: `web/app/src/theme/styles.css`

- [ ] **Step 1: Add i18n keys**

In `web/app/src/i18n/messages.ts`, add to both locales:

```typescript
"relay.modal.title":   "Disconnected"   /* zh: "连接断开" */,
"relay.modal.body":    "Could not reconnect to {server}.\nAttempts: {attempts}, disconnected for {duration}." /* zh: "无法重新连接到 {server}\n已尝试 {attempts} 次，断开 {duration}。" */,
"relay.modal.reload":  "Reload page"     /* zh: "重新加载页面" */,
"relay.modal.retry":   "Retry connection" /* zh: "重试连接" */,
"relay.modal.details": "Show details"   /* zh: "显示详情" */,
"relay.modal.lastError": "Last error: {error}" /* zh: "最近错误：{error}" */,
```

Use the existing parameter substitution pattern.

- [ ] **Step 2: Write the failing test**

Create `web/app/src/components/disconnect-modal.test.tsx`:

```typescript
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent } from "@testing-library/preact";
import { DisconnectModal } from "./disconnect-modal";
import { setLocale } from "@/i18n/store";

const sampleProps = {
  open: true,
  serverUrl: "https://termix.cloud",
  attempts: 14,
  durationMs: 323_000,
  lastError: "broken pipe",
  onReload: vi.fn(),
  onRetry: vi.fn(),
};

describe("DisconnectModal", () => {
  beforeEach(() => {
    localStorage.clear();
    setLocale("en");
    sampleProps.onReload.mockClear();
    sampleProps.onRetry.mockClear();
  });

  it("renders nothing when open is false", () => {
    const { container } = render(<DisconnectModal {...sampleProps} open={false} />);
    expect(container.textContent ?? "").toBe("");
  });

  it("shows attempts, duration, and a reload button", () => {
    render(<DisconnectModal {...sampleProps} />);
    expect(screen.getByText(/14/)).toBeTruthy();
    expect(screen.getByRole("button", { name: /Reload page/ })).toBeTruthy();
  });

  it("calls onReload when reload is clicked", () => {
    render(<DisconnectModal {...sampleProps} />);
    fireEvent.click(screen.getByRole("button", { name: /Reload page/ }));
    expect(sampleProps.onReload).toHaveBeenCalledTimes(1);
  });

  it("calls onRetry when retry is clicked", () => {
    render(<DisconnectModal {...sampleProps} />);
    fireEvent.click(screen.getByRole("button", { name: /Retry connection/ }));
    expect(sampleProps.onRetry).toHaveBeenCalledTimes(1);
  });

  it("renders Chinese copy when locale is zh", () => {
    setLocale("zh");
    render(<DisconnectModal {...sampleProps} />);
    expect(screen.getByText(/连接断开/)).toBeTruthy();
    expect(screen.getByRole("button", { name: /重新加载/ })).toBeTruthy();
  });
});
```

- [ ] **Step 3: Confirm FAIL**

```bash
npm test -- --run src/components/disconnect-modal.test.tsx
```

Expected: FAIL.

- [ ] **Step 4: Create the component**

`web/app/src/components/disconnect-modal.tsx`:

```typescript
import { useState } from "preact/hooks";
import { t } from "@/i18n/store";

interface Props {
  open: boolean;
  serverUrl: string;
  attempts: number;
  durationMs: number;
  lastError: string;
  onReload: () => void;
  onRetry: () => void;
}

function formatDuration(ms: number): string {
  const total = Math.floor(ms / 1000);
  const m = Math.floor(total / 60);
  const s = total % 60;
  return `${m}m ${s}s`;
}

export function DisconnectModal(props: Props) {
  const [showDetails, setShowDetails] = useState(false);
  if (!props.open) return null;
  return (
    <div className="modal-backdrop" role="dialog" aria-modal="true">
      <div className="modal-card">
        <h2 className="modal-title">{t("relay.modal.title")}</h2>
        <p className="modal-body">
          {t("relay.modal.body", {
            server: props.serverUrl,
            attempts: String(props.attempts),
            duration: formatDuration(props.durationMs),
          })}
        </p>
        <button className="modal-details-toggle" onClick={() => setShowDetails((v) => !v)}>
          {t("relay.modal.details")}
        </button>
        {showDetails && (
          <pre className="modal-details">
            {t("relay.modal.lastError", { error: props.lastError })}
          </pre>
        )}
        <div className="modal-actions">
          <button className="btn btn-primary" onClick={props.onReload}>
            {t("relay.modal.reload")}
          </button>
          <button className="btn btn-secondary" onClick={props.onRetry}>
            {t("relay.modal.retry")}
          </button>
        </div>
      </div>
    </div>
  );
}
```

Add styles in `web/app/src/theme/styles.css`:

```css
.modal-backdrop {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.5);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 1000;
}
.modal-card {
  background: var(--surface, #ffffff);
  border-radius: 8px;
  padding: 24px 28px;
  max-width: 420px;
  width: calc(100% - 32px);
  box-shadow: 0 10px 30px rgba(0, 0, 0, 0.2);
}
.modal-title { margin: 0 0 12px; font-size: 18px; }
.modal-body { white-space: pre-line; margin: 0 0 12px; color: #444; }
.modal-details-toggle {
  background: none;
  border: none;
  color: #1f6feb;
  cursor: pointer;
  padding: 4px 0;
  font-size: 13px;
}
.modal-details {
  background: #f6f8fa;
  padding: 8px 10px;
  border-radius: 4px;
  font-size: 12px;
  margin: 8px 0 12px;
  white-space: pre-wrap;
}
.modal-actions { display: flex; gap: 8px; justify-content: flex-end; margin-top: 12px; }
.btn { border-radius: 4px; padding: 8px 14px; font-size: 14px; cursor: pointer; border: 1px solid transparent; }
.btn-primary   { background: #1f6feb; color: #fff; }
.btn-secondary { background: #fff; color: #1f6feb; border-color: #1f6feb; }
```

- [ ] **Step 5: Run and confirm PASS**

```bash
npm test -- --run src/components/disconnect-modal.test.tsx
```

Expected: PASS (5 tests).

- [ ] **Step 6: Commit**

```bash
git add web/app/src/components/disconnect-modal.tsx web/app/src/components/disconnect-modal.test.tsx web/app/src/i18n/messages.ts web/app/src/theme/styles.css
git commit -m "components: DisconnectModal for >5min reconnect failures"
```

---

## Task 15: Wire banner + modal into the terminal page

**Why:** Connect the supervisor's state changes to the UI surfaces.

**Files:**
- Modify: `web/app/src/pages/terminal.tsx`
- Modify: `web/app/src/pages/terminal.test.tsx` (if relevant tests need updating)

- [ ] **Step 1: Subscribe to connection state in terminal page**

In `web/app/src/pages/terminal.tsx`, locate where the page already wires the bridge (look for `installInboundBridge` or a connection-state callback). Add state to track the supervisor's reported phase, then render `<ReconnectBanner>` and `<DisconnectModal>` based on it.

If `outbound.onConnectionState(state)` is already called from the bridge, extend the page-level handler to track:

```typescript
import { signal } from "@preact/signals";
import { ReconnectBanner } from "@/components/reconnect-banner";
import { DisconnectModal } from "@/components/disconnect-modal";

const connState = signal<{
  phase: string;
  attempt: number;
  durationMs: number;
  lastError: string;
}>({ phase: "connecting", attempt: 0, durationMs: 0, lastError: "" });

// in setup:
installInboundBridge({
  ui,
  onConnectionState: (s) => {
    connState.value = {
      phase: s.phase,
      attempt: "attempt" in s ? s.attempt : 0,
      durationMs: "durationMs" in s ? s.durationMs : 0,
      lastError: "lastError" in s ? s.lastError : "",
    };
  },
});
```

In the JSX:

```tsx
<>
  <ReconnectBanner phase={connState.value.phase} attempt={connState.value.attempt} />
  <DisconnectModal
    open={connState.value.phase === "gave-up"}
    serverUrl={location.host}
    attempts={connState.value.attempt}
    durationMs={connState.value.durationMs}
    lastError={connState.value.lastError}
    onReload={() => window.location.reload()}
    onRetry={() => (window as any).retryRelay?.()}
  />
  {/* existing terminal JSX */}
</>
```

For the retry path, expose `sup.retry` on the window via the bridge layer:

```typescript
(window as { retryRelay?: () => void }).retryRelay = () => sup.retry();
```

- [ ] **Step 2: Typecheck and run all web tests**

```bash
cd web/app && npm run typecheck && npm test -- --run
```

Expected: no type errors; all tests pass. Some existing terminal tests may need updates if their `onConnectionState` mock receives a richer object — adjust assertions accordingly.

- [ ] **Step 3: Build the bundle and embed**

```bash
make build-web
make check-web-dist
```

Expected: success. The new components and styles are included in the bundle and rsynced into `go/internal/controlapi/web_dist/`.

- [ ] **Step 4: Commit**

```bash
git add web/app/src/pages/terminal.tsx web/app/src/pages/terminal.test.tsx go/internal/controlapi/web_dist/
git commit -m "terminal: render ReconnectBanner and DisconnectModal on supervisor state"
```

---

## Task 16: Final verification + manual smoke

**Why:** Catch any cross-component regressions before merging. Validate end-to-end that the user-reported scenario (overnight WSS death) is actually fixed.

- [ ] **Step 1: Full Go test suite + integration**

```bash
cd go && go vet ./...
cd go && go build ./...
cd go && go test ./... -count=1
TERMIX_TEST_DATABASE_URL='postgres://postgres:test@127.0.0.1:55432/termix_test?sslmode=disable' \
  TERMIX_TMUX_INTEGRATION=1 \
  go test ./... -count=1
```

Expected: all green. Numbers: ~253 base, ~287 with integration (v0.3.0 was 235 / 269; the new tests add ~18).

- [ ] **Step 2: Full web test suite**

```bash
cd web/app && npm run typecheck && npm test -- --run
```

Expected: ~186 pass (v0.3.0 was 174; +12).

- [ ] **Step 3: Build a fresh CLI binary**

```bash
cd /media/liujia/data/workspace/xunfei/termix/.worktrees/relay-reconnect
mkdir -p bin && (cd go && go build -o ../bin/termix ./cmd/termix)
```

- [ ] **Step 4: Manual daemon-side smoke**

Stop any current daemon, then exercise the reconnect path:

```bash
pkill -fx '(^|.*/)termix __daemon' || true
.worktrees/relay-reconnect/bin/termix status
# Expected: USER / DAEMON / RELAY (connected) / SESSIONS / PROXY block.

# Simulate disconnect
sudo iptables -A OUTPUT -p tcp -d 43.156.83.27 --dport 443 -j DROP
sleep 5
.worktrees/relay-reconnect/bin/termix status
# Expected: RELAY shows reconnecting (attempt N, next try in ...s).

sudo iptables -D OUTPUT -p tcp -d 43.156.83.27 --dport 443 -j DROP
sleep 10
.worktrees/relay-reconnect/bin/termix status
# Expected: RELAY back to connected.
```

- [ ] **Step 5: Manual SPA-side smoke**

Open `https://termix.cloud/sessions/<id>` in Chrome with DevTools. In Network panel, set Offline. Wait 5 seconds → banner appears with attempt counter. Wait 5 minutes (or use a temporary supervisor option to shorten the give-up timer for the test) → modal appears. Click Retry while still offline → banner reappears, attempts reset. Click Reload → page reloads, fresh session.

- [ ] **Step 6: Update PROGRESS.md**

In `docs/PROGRESS.md`, move the slice's In-Progress entry into Completed with:
- summary of shipped components,
- final test counts (Go base / Go integration / Web),
- the manual smoke results.

- [ ] **Step 7: Commit + merge to main**

```bash
git add docs/PROGRESS.md
git commit -m "PROGRESS: log relay reconnect + status slice as completed"

# From the main worktree:
cd /media/liujia/data/workspace/xunfei/termix
git merge --ff-only relay-reconnect
git worktree remove .worktrees/relay-reconnect
git branch -d relay-reconnect
```

- [ ] **Step 8: Push to origin**

```bash
git push origin main
```

This task does NOT include the v0.4.0 release (tag + GH Actions + deploy). The user explicitly handles release commands; once the merge lands, surface the option to bump the version and tag.

---

## Out of scope for this plan

- v0.4.0 version bump and `termix.cloud` deployment (the user runs these explicitly).
- JSON output for `termix status`.
- In-band token refresh on a live WSS.
- SPA's `setLocale` integration tests beyond what each new component covers.
- Application-layer ping/heartbeat. Existing `startHeartbeat` on the SPA's WSS sends `heartbeat` envelopes every 20s; we keep that as-is and rely on Go TCP keepalive + write-error detection on the daemon side.
