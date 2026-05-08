package session

import (
	"context"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
	openapi "github.com/termix/termix/go/gen/openapi"
	daemonv1 "github.com/termix/termix/go/gen/proto/daemonv1"
	"github.com/termix/termix/go/internal/buildinfo"
	"github.com/termix/termix/go/internal/credentials"
)

type ControlClient interface {
	CreateHostSession(ctx context.Context, accessToken string, req openapi.CreateSessionRequest) (*openapi.CreateSessionResponse, error)
	UpdateHostSession(ctx context.Context, accessToken string, sessionID string, req openapi.UpdateSessionRequest) (*openapi.Session, error)
	HeartbeatHostSession(ctx context.Context, accessToken string, sessionID string, status string) (*openapi.Session, error)
}

type TmuxRunner interface {
	EnsureAvailable(ctx context.Context) error
	StartSession(ctx context.Context, spec StartSpec) error
	StartOutputPipe(ctx context.Context, sessionName, fifoPath string) error
	StopOutputPipe(ctx context.Context, sessionName string) error
	HasSession(ctx context.Context, sessionName string) bool
	ResizeWindow(ctx context.Context, sessionName string, cols, rows uint32) error
	KillSession(ctx context.Context, sessionName string) error
	PanePID(ctx context.Context, sessionName string) (int, error)
}

// MakeFifoFunc creates a named pipe at path with the given mode. Returns nil
// if the pipe already exists with the same mode. Used by the session manager
// to set up per-session output FIFOs; injected for testability.
type MakeFifoFunc func(path string, mode uint32) error

type ManagerOptions struct {
	Store           *Store
	LoadCredentials func() (credentials.StoredCredentials, error)
	// RefreshCredentials, when set together with IsAuthError, lets the manager
	// transparently swap out an expired access token via the long-lived refresh
	// token: on an auth error from a control call, the manager calls
	// RefreshCredentials and retries the same call once with the fresh token.
	// Both hooks must be set for retry to fire — otherwise the auth error is
	// propagated unchanged.
	RefreshCredentials func(context.Context) (credentials.StoredCredentials, error)
	IsAuthError        func(error) bool
	Control            ControlClient
	NewControl         func(credentials.StoredCredentials) (ControlClient, error)
	Tmux               TmuxRunner
	Relay              RelayClient
	Snapshot           SnapshotFunc
	Input              InputFunc
	Now                func() time.Time
	Hostname           func() (string, error)
	DoctorChecks       func(context.Context) ([]string, error)

	// OutputFifoDir is where per-session output FIFOs are created. Required
	// to enable live-output streaming; if empty, output streaming is disabled
	// (only the initial snapshot is forwarded).
	OutputFifoDir string
	// LogDir is the host-side log directory. When set, the manager writes a
	// per-session stderr file under <LogDir>/sessions/<name>.err so a tool
	// that exits immediately leaves a readable tail.
	LogDir string
	// MakeFifo defaults to syscall.Mkfifo when nil.
	MakeFifo MakeFifoFunc

	// Version is the build version reported by Health. Forwarded into the
	// daemon's identity tuple; left empty when not set, in which case the
	// CLI handshake treats this daemon as outdated and replaces it.
	Version string

	// RequestShutdown, when set, is invoked by Manager.Shutdown after the
	// RPC response is acknowledged. hostdaemon.Run wires this to the
	// cancel func of the daemon's lifetime context so the gRPC server
	// teardown path runs as if SIGTERM had arrived.
	RequestShutdown func()

	// ProxyFingerprint is the daemon's effective proxy env fingerprint,
	// computed at boot after `proxyenv.Apply` enforced the host config's
	// `enable_proxy` policy. The CLI handshake compares its own freshly
	// computed fingerprint against this value and respawns the daemon
	// when they differ.
	ProxyFingerprint string

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

type Manager struct {
	daemonv1.UnimplementedDaemonServiceServer

	store              *Store
	loadCredentials    func() (credentials.StoredCredentials, error)
	refreshCredentials func(context.Context) (credentials.StoredCredentials, error)
	isAuthError        func(error) bool
	control            ControlClient
	newControl         func(credentials.StoredCredentials) (ControlClient, error)
	tmux               TmuxRunner
	relay              RelayClient
	snapshot           SnapshotFunc
	now                func() time.Time
	hostname           func() (string, error)
	doctorChecks       func(context.Context) ([]string, error)

	outputFifoDir string
	logDir        string
	makeFifo      MakeFifoFunc

	version          string
	requestShutdown  func()
	proxyFingerprint string
	relayStateSource func() RelayStateSnapshot
	startTime        time.Time
}

func NewManager(opts ManagerOptions) *Manager {
	now := opts.Now
	if now == nil {
		now = time.Now
	}

	startTime := opts.StartTime
	if startTime.IsZero() {
		startTime = now()
	}

	hostname := opts.Hostname
	if hostname == nil {
		hostname = os.Hostname
	}

	doctorChecks := opts.DoctorChecks
	if doctorChecks == nil {
		doctorChecks = func(context.Context) ([]string, error) {
			return nil, nil
		}
	}

	if opts.Relay != nil && opts.Snapshot != nil {
		opts.Relay.SetSnapshotHandler(func(ctx context.Context, sessionID string) ([]byte, error) {
			if opts.Store == nil {
				return nil, errors.New("session store is required")
			}
			localSession, err := opts.Store.Load(sessionID)
			if err != nil {
				return nil, err
			}
			return opts.Snapshot(ctx, localSession.TmuxSessionName)
		})
	}
	if opts.Relay != nil && opts.Input != nil {
		opts.Relay.SetInputHandler(func(ctx context.Context, sessionID string, payload []byte) error {
			if opts.Store == nil {
				return errors.New("session store is required")
			}
			localSession, err := opts.Store.Load(sessionID)
			if err != nil {
				return err
			}
			return opts.Input(ctx, localSession.TmuxSessionName, payload)
		})
	}

	makeFifo := opts.MakeFifo
	if makeFifo == nil {
		makeFifo = defaultMakeFifo
	}

	return &Manager{
		store:              opts.Store,
		loadCredentials:    opts.LoadCredentials,
		refreshCredentials: opts.RefreshCredentials,
		isAuthError:        opts.IsAuthError,
		control:            opts.Control,
		newControl:         opts.NewControl,
		tmux:               opts.Tmux,
		relay:              opts.Relay,
		snapshot:           opts.Snapshot,
		now:                now,
		hostname:           hostname,
		doctorChecks:       doctorChecks,
		outputFifoDir:      opts.OutputFifoDir,
		logDir:             opts.LogDir,
		makeFifo:           makeFifo,
		version:            opts.Version,
		requestShutdown:    opts.RequestShutdown,
		proxyFingerprint:   opts.ProxyFingerprint,
		relayStateSource:   opts.RelayStateSource,
		startTime:          startTime,
	}
}

func (m *Manager) Health(context.Context, *daemonv1.HealthRequest) (*daemonv1.HealthResponse, error) {
	id := buildinfo.Current(m.version)
	return &daemonv1.HealthResponse{
		Status:           "ok",
		Version:          id.Version,
		Revision:         id.Revision,
		Modified:         id.Modified,
		ProxyFingerprint: m.proxyFingerprint,
	}, nil
}

func (m *Manager) Shutdown(context.Context, *daemonv1.ShutdownRequest) (*daemonv1.ShutdownResponse, error) {
	if m.requestShutdown != nil {
		// Schedule cancellation after a short grace so the unary RPC
		// response flushes back to the caller before listener.Close +
		// server.Stop tears down the connection. The CLI does not depend
		// on the ack (it polls for the socket file disappearing) but
		// returning a clean response makes the contract obvious.
		go func() {
			time.Sleep(50 * time.Millisecond)
			m.requestShutdown()
		}()
	}
	return &daemonv1.ShutdownResponse{}, nil
}

func (m *Manager) StartSession(ctx context.Context, req *daemonv1.StartSessionRequest) (*daemonv1.StartSessionResponse, error) {
	if m.store == nil {
		return nil, errors.New("session store is required")
	}
	if m.loadCredentials == nil {
		return nil, errors.New("credentials loader is required")
	}
	if m.tmux == nil {
		return nil, errors.New("tmux runner is required")
	}

	creds, err := m.loadCredentials()
	if err != nil {
		return nil, err
	}
	if creds.DeviceID == "" {
		return nil, errors.New("stored credentials are missing device id")
	}
	if creds.AccessToken == "" {
		return nil, errors.New("stored credentials are missing access token")
	}

	controlClient, err := m.controlClient(creds)
	if err != nil {
		return nil, err
	}
	caller := &controlCaller{
		client:      controlClient,
		creds:       creds,
		refresh:     m.refreshCredentials,
		isAuthError: m.isAuthError,
	}

	deviceID, err := parseUUID(creds.DeviceID)
	if err != nil {
		return nil, err
	}

	host, err := m.hostname()
	if err != nil {
		host = "termix-host"
	}

	var name *string
	if req.Name != "" {
		name = &req.Name
	}

	createResp, err := caller.createHostSession(ctx, openapi.CreateSessionRequest{
		DeviceId:      deviceID,
		Tool:          openapi.CreateSessionRequestTool(req.Tool),
		Name:          name,
		LaunchCommand: req.Tool,
		Cwd:           req.Cwd,
		CwdLabel:      filepath.Base(req.Cwd),
		Hostname:      host,
	})
	if err != nil {
		return nil, err
	}

	startSpec := StartSpec{
		SessionName:         createResp.TmuxSessionName,
		WorkingDir:          req.Cwd,
		Shell:               req.Shell,
		Env:                 req.Env,
		ToolCommand:         req.Tool,
		ErrLogPath:          m.sessionErrLogPath(createResp.TmuxSessionName),
		DetectImmediateExit: true,
		Cols:                int(req.GetCols()),
		Rows:                int(req.GetRows()),
	}
	if err := m.tmux.EnsureAvailable(ctx); err != nil {
		m.markFailed(ctx, caller, createResp.SessionId.String(), err)
		return nil, err
	}
	if err := m.tmux.StartSession(ctx, startSpec); err != nil {
		m.markFailed(ctx, caller, createResp.SessionId.String(), err)
		return nil, err
	}

	// Live-output streaming: create a FIFO, start a goroutine that reads
	// from it and forwards bytes to the relay, then ask tmux to pipe the
	// pane's stdout into the FIFO. Failure here is logged but does not
	// abort the session — the user can still see snapshots.
	if m.outputFifoDir != "" && m.relay != nil {
		if err := m.startOutputPipe(createResp.SessionId.String(), createResp.TmuxSessionName); err != nil {
			// Best-effort: clean up partial state and continue.
			_ = m.tmux.StopOutputPipe(context.Background(), createResp.TmuxSessionName)
		}
	}

	if _, err := caller.updateHostSession(ctx, createResp.SessionId.String(), openapi.UpdateSessionRequest{
		Status: openapi.UpdateSessionRequestStatus("running"),
	}); err != nil {
		return nil, err
	}

	localSession := LocalSession{
		SessionID:       createResp.SessionId.String(),
		Name:            req.Name,
		Tool:            req.Tool,
		Status:          "running",
		TmuxSessionName: createResp.TmuxSessionName,
		AttachCommand:   attachCommand(createResp.TmuxSessionName),
		Cwd:             req.Cwd,
		LaunchCommand:   req.Tool,
		StartedAt:       m.now().UTC(),
	}
	if err := m.store.Save(localSession); err != nil {
		return nil, err
	}
	if m.relay != nil {
		if err := m.relay.AnnounceSession(ctx, localSession); err != nil {
			return nil, err
		}
	}

	return &daemonv1.StartSessionResponse{
		SessionId:       localSession.SessionID,
		TmuxSessionName: localSession.TmuxSessionName,
		AttachCommand:   localSession.AttachCommand,
		Status:          localSession.Status,
	}, nil
}

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

func (m *Manager) ListSessions(ctx context.Context, _ *daemonv1.ListSessionsRequest) (*daemonv1.ListSessionsResponse, error) {
	if m.store == nil {
		return nil, errors.New("session store is required")
	}

	sessions, err := m.store.List()
	if err != nil {
		return nil, err
	}

	response := &daemonv1.ListSessionsResponse{
		Sessions: make([]*daemonv1.SessionSummary, 0, len(sessions)),
	}
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
		response.Sessions = append(response.Sessions, summary)
	}
	return response, nil
}

func (m *Manager) AttachInfo(_ context.Context, req *daemonv1.AttachInfoRequest) (*daemonv1.AttachInfoResponse, error) {
	if m.store == nil {
		return nil, errors.New("session store is required")
	}

	session, err := m.store.Load(req.GetSessionId())
	if err != nil {
		return nil, err
	}

	return &daemonv1.AttachInfoResponse{
		TmuxSessionName: session.TmuxSessionName,
		AttachCommand:   session.AttachCommand,
	}, nil
}

// Reap walks the local-state store and reconciles with tmux: any session
// whose tmux pane is gone (claude exited cleanly, user ran tmux kill-session,
// termixd crashed and tmux died too, etc.) is PATCHed to status=exited in
// the control DB and removed from local state. Sessions whose tmux pane is
// still alive send a heartbeat so the control-plane running list only shows
// sessions the host has recently confirmed.
//
// Designed to be called both at termixd startup (after the relay handshake)
// and on a periodic ticker. Errors from individual sessions are logged and
// otherwise swallowed; the loop continues so a single bad session can't
// block reaping the rest.
func (m *Manager) Reap(ctx context.Context) error {
	if m.store == nil {
		return errors.New("session store is required")
	}
	if m.loadCredentials == nil {
		return errors.New("credentials loader is required")
	}
	if m.tmux == nil {
		return errors.New("tmux runner is required")
	}

	sessions, err := m.store.List()
	if err != nil {
		return err
	}

	var caller *controlCaller
	for _, s := range sessions {
		if s.Status != "running" && s.Status != "starting" && s.Status != "idle" {
			continue
		}
		if m.tmux.HasSession(ctx, s.TmuxSessionName) {
			if caller == nil {
				var err error
				caller, err = m.reapControlCaller()
				if err != nil {
					return err
				}
			}
			if _, err := caller.heartbeatHostSession(ctx, s.SessionID, s.Status); err != nil {
				if isSessionNotFoundError(err) {
					log.Printf("reap: dropping stale local session %s because control has no matching session", s.SessionID)
					if err := m.store.Delete(s.SessionID); err != nil {
						log.Printf("reap: store.Delete %s failed: %v", s.SessionID, err)
					}
					continue
				}
				log.Printf("reap: heartbeat %s failed: %v", s.SessionID, err)
			}
			continue
		}

		// Lazily build the caller; don't pay the load+dial cost when nothing
		// needs reaping.
		if caller == nil {
			var err error
			caller, err = m.reapControlCaller()
			if err != nil {
				return err
			}
		}

		if _, err := caller.updateHostSession(ctx, s.SessionID, openapi.UpdateSessionRequest{
			Status: openapi.UpdateSessionRequestStatus("exited"),
		}); err != nil {
			if isSessionNotFoundError(err) {
				log.Printf("reap: dropping stale local session %s because control has no matching session", s.SessionID)
				if err := m.store.Delete(s.SessionID); err != nil {
					log.Printf("reap: store.Delete %s failed: %v", s.SessionID, err)
				}
				continue
			}
			log.Printf("reap: PATCH %s -> exited failed: %v", s.SessionID, err)
			continue // try again on next tick
		}
		if err := m.store.Delete(s.SessionID); err != nil {
			log.Printf("reap: store.Delete %s failed: %v", s.SessionID, err)
		}
	}
	return nil
}

func (m *Manager) reapControlCaller() (*controlCaller, error) {
	creds, err := m.loadCredentials()
	if err != nil {
		return nil, err
	}
	controlClient, err := m.controlClient(creds)
	if err != nil {
		return nil, err
	}
	return &controlCaller{
		client:      controlClient,
		creds:       creds,
		refresh:     m.refreshCredentials,
		isAuthError: m.isAuthError,
	}, nil
}

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

// ErrSessionNotFound is returned by manager methods when no local session
// matches the supplied id (typical cause: SPA holds a stale session_id, or
// the daemon was restarted and the local store was cleared).
var ErrSessionNotFound = errors.New("session_not_found")

// EndSession kills the tmux session at the source, then PATCHes the control
// row to status=exited and removes the local-store entry. Mirrors the
// tmux-gone branch of Reap so the manual shutdown path and the periodic
// reaper produce identical state transitions. Returns ErrSessionNotFound
// when the daemon does not have that session in local state — that is the
// signal to the CLI that the user must run shutdown on the right host.
//
// Order of operations:
//  1. Load local state (fail fast if missing).
//  2. tmux kill-session — idempotent; no-ops when the pane is already gone.
//  3. PATCH status=exited via the control plane.
//  4. Delete the local-store row.
//
// On PATCH failure we deliberately *keep* the local-store row so the
// periodic reaper retries on its next sweep. tmux is already dead at that
// point, so the row will be picked up by the reaper's tmux-gone branch and
// PATCHed again.
func (m *Manager) EndSession(ctx context.Context, req *daemonv1.EndSessionRequest) (*daemonv1.EndSessionResponse, error) {
	if m.store == nil {
		return nil, errors.New("session store is required")
	}
	if m.tmux == nil {
		return nil, errors.New("tmux runner is required")
	}
	if m.loadCredentials == nil {
		return nil, errors.New("credentials loader is required")
	}
	id := req.GetSessionId()
	if id == "" {
		return nil, errors.New("session_id is required")
	}

	local, err := m.store.Load(id)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrSessionNotFound
		}
		return nil, err
	}

	if err := m.tmux.KillSession(ctx, local.TmuxSessionName); err != nil {
		log.Printf("end-session: tmux kill-session %s failed: %v", local.TmuxSessionName, err)
	}

	caller, err := m.reapControlCaller()
	if err != nil {
		return nil, err
	}
	if _, err := caller.updateHostSession(ctx, id, openapi.UpdateSessionRequest{
		Status: openapi.UpdateSessionRequestStatus("exited"),
	}); err != nil {
		if isSessionNotFoundError(err) {
			if derr := m.store.Delete(id); derr != nil {
				log.Printf("end-session: store.Delete %s failed: %v", id, derr)
			}
			return &daemonv1.EndSessionResponse{}, nil
		}
		return nil, err
	}

	if err := m.store.Delete(id); err != nil {
		log.Printf("end-session: store.Delete %s failed: %v", id, err)
	}
	return &daemonv1.EndSessionResponse{}, nil
}

// ResizeSession drives the SPA's target (cols, rows) into tmux for the
// session referenced by sessionID. Returns ErrSessionNotFound if the
// daemon does not know that session anymore. Errors from the runner are
// surfaced verbatim so the caller (relayclient) can log them.
func (m *Manager) ResizeSession(ctx context.Context, sessionID string, cols, rows uint32) error {
	if m.store == nil {
		return errors.New("session store is required")
	}
	if m.tmux == nil {
		return errors.New("tmux runner is required")
	}
	if sessionID == "" {
		return errors.New("session_id is required")
	}
	local, err := m.store.Load(sessionID)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrSessionNotFound
		}
		return err
	}
	return m.tmux.ResizeWindow(ctx, local.TmuxSessionName, cols, rows)
}

func (m *Manager) Doctor(ctx context.Context, _ *daemonv1.DoctorRequest) (*daemonv1.DoctorResponse, error) {
	checks, err := m.doctorChecks(ctx)
	if err != nil {
		return nil, err
	}
	return &daemonv1.DoctorResponse{Checks: checks}, nil
}

func (m *Manager) controlClient(creds credentials.StoredCredentials) (ControlClient, error) {
	if m.control != nil {
		return m.control, nil
	}
	if m.newControl == nil {
		return nil, errors.New("control client is required")
	}
	return m.newControl(creds)
}

func (m *Manager) markFailed(ctx context.Context, caller *controlCaller, sessionID string, startErr error) {
	message := startErr.Error()
	if len(message) > 256 {
		message = message[:256]
	}
	_, _ = caller.updateHostSession(ctx, sessionID, openapi.UpdateSessionRequest{
		Status:    openapi.UpdateSessionRequestStatus("failed"),
		LastError: &message,
	})
}

// controlCaller wraps a ControlClient with one-shot retry-on-auth-error. The
// stored creds are updated in place after a successful refresh so subsequent
// calls in the same StartSession flow use the fresh access token.
type controlCaller struct {
	client      ControlClient
	creds       credentials.StoredCredentials
	refresh     func(context.Context) (credentials.StoredCredentials, error)
	isAuthError func(error) bool
}

func (c *controlCaller) createHostSession(ctx context.Context, req openapi.CreateSessionRequest) (*openapi.CreateSessionResponse, error) {
	resp, err := c.client.CreateHostSession(ctx, c.creds.AccessToken, req)
	if err == nil || !c.shouldRetry(err) {
		return resp, err
	}
	if rerr := c.refreshCreds(ctx); rerr != nil {
		return nil, rerr
	}
	return c.client.CreateHostSession(ctx, c.creds.AccessToken, req)
}

func (c *controlCaller) updateHostSession(ctx context.Context, sessionID string, req openapi.UpdateSessionRequest) (*openapi.Session, error) {
	resp, err := c.client.UpdateHostSession(ctx, c.creds.AccessToken, sessionID, req)
	if err == nil || !c.shouldRetry(err) {
		return resp, err
	}
	if rerr := c.refreshCreds(ctx); rerr != nil {
		return nil, rerr
	}
	return c.client.UpdateHostSession(ctx, c.creds.AccessToken, sessionID, req)
}

func (c *controlCaller) heartbeatHostSession(ctx context.Context, sessionID string, status string) (*openapi.Session, error) {
	resp, err := c.client.HeartbeatHostSession(ctx, c.creds.AccessToken, sessionID, status)
	if err == nil || !c.shouldRetry(err) {
		return resp, err
	}
	if rerr := c.refreshCreds(ctx); rerr != nil {
		return nil, rerr
	}
	return c.client.HeartbeatHostSession(ctx, c.creds.AccessToken, sessionID, status)
}

func isSessionNotFoundError(err error) bool {
	type reasonedError interface {
		Reason() string
	}
	var reasoned reasonedError
	return errors.As(err, &reasoned) && reasoned.Reason() == "session_not_found"
}

func (c *controlCaller) shouldRetry(err error) bool {
	return c.refresh != nil && c.isAuthError != nil && c.isAuthError(err)
}

func (c *controlCaller) refreshCreds(ctx context.Context) error {
	creds, err := c.refresh(ctx)
	if err != nil {
		return err
	}
	c.creds = creds
	return nil
}

func parseUUID(raw string) (openapi_types.UUID, error) {
	value, err := uuid.Parse(raw)
	if err != nil {
		return openapi_types.UUID{}, err
	}
	return openapi_types.UUID(value), nil
}

func attachCommand(sessionName string) string {
	return "tmux attach-session -t " + sessionName
}

// sessionErrLogPath returns the per-session stderr log file under
// <LogDir>/sessions/<tmuxSessionName>.err so the tmux runner can mirror the
// pane's stderr there and surface a useful tail when a launch fails fast.
// Returns "" when LogDir is not configured (skips redirection in that case).
func (m *Manager) sessionErrLogPath(tmuxSessionName string) string {
	if m.logDir == "" || tmuxSessionName == "" {
		return ""
	}
	return filepath.Join(m.logDir, "sessions", tmuxSessionName+".err")
}

// startOutputPipe creates a per-session FIFO, asks tmux to redirect the pane's
// stdout into it via pipe-pane, and spawns a goroutine that reads bytes from
// the FIFO and forwards them to the relay as terminal-output frames. The
// goroutine exits naturally when the FIFO returns EOF (tmux pipe stopped or
// session closed) or when reads error out persistently.
func (m *Manager) startOutputPipe(sessionID, tmuxSessionName string) error {
	if err := os.MkdirAll(m.outputFifoDir, 0o700); err != nil {
		return err
	}
	fifoPath := filepath.Join(m.outputFifoDir, sessionID+".fifo")
	// Remove any leftover from a previous run; ignore not-found errors.
	if err := os.Remove(fifoPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := m.makeFifo(fifoPath, 0o600); err != nil {
		return err
	}
	// Open the FIFO RDWR so the read end never blocks waiting for a writer
	// (tmux's pipe-pane child opens it RDONLY-style via shell redirection).
	fifo, err := os.OpenFile(fifoPath, os.O_RDWR, 0)
	if err != nil {
		return err
	}

	// Tell tmux to start piping the pane's output into the FIFO.
	pipeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	pipeErr := m.tmux.StartOutputPipe(pipeCtx, tmuxSessionName, fifoPath)
	cancel()
	if pipeErr != nil {
		_ = fifo.Close()
		_ = os.Remove(fifoPath)
		return pipeErr
	}

	go m.streamOutput(sessionID, fifo, fifoPath)
	return nil
}

func (m *Manager) streamOutput(sessionID string, fifo io.ReadCloser, fifoPath string) {
	defer func() {
		_ = fifo.Close()
		_ = os.Remove(fifoPath)
	}()
	buf := make([]byte, 4096)
	for {
		n, err := fifo.Read(buf)
		if n > 0 {
			payload := make([]byte, n)
			copy(payload, buf[:n])
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			if pubErr := m.relay.PublishOutput(ctx, sessionID, payload); pubErr != nil {
				log.Printf("session %s: PublishOutput failed: %v", sessionID, pubErr)
			}
			cancel()
		}
		if err != nil {
			if err != io.EOF {
				log.Printf("session %s: output FIFO read error: %v", sessionID, err)
			}
			return
		}
	}
}

func defaultMakeFifo(path string, mode uint32) error {
	return syscall.Mkfifo(path, mode)
}
