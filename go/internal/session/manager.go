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
	// MakeFifo defaults to syscall.Mkfifo when nil.
	MakeFifo MakeFifoFunc
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
	now                func() time.Time
	hostname           func() (string, error)
	doctorChecks       func(context.Context) ([]string, error)

	outputFifoDir string
	makeFifo      MakeFifoFunc
}

func NewManager(opts ManagerOptions) *Manager {
	now := opts.Now
	if now == nil {
		now = time.Now
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
		now:                now,
		hostname:           hostname,
		doctorChecks:       doctorChecks,
		outputFifoDir:      opts.OutputFifoDir,
		makeFifo:           makeFifo,
	}
}

func (m *Manager) Health(context.Context, *daemonv1.HealthRequest) (*daemonv1.HealthResponse, error) {
	return &daemonv1.HealthResponse{Status: "ok"}, nil
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
		SessionName: createResp.TmuxSessionName,
		WorkingDir:  req.Cwd,
		Shell:       req.Shell,
		Env:         req.Env,
		ToolCommand: req.Tool,
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

func (m *Manager) ListSessions(context.Context, *daemonv1.ListSessionsRequest) (*daemonv1.ListSessionsResponse, error) {
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
		response.Sessions = append(response.Sessions, &daemonv1.SessionSummary{
			SessionId:       item.SessionID,
			Name:            item.Name,
			Tool:            item.Tool,
			Status:          item.Status,
			TmuxSessionName: item.TmuxSessionName,
		})
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
