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
}

type TmuxRunner interface {
	EnsureAvailable(ctx context.Context) error
	StartSession(ctx context.Context, spec StartSpec) error
	StartOutputPipe(ctx context.Context, sessionName, fifoPath string) error
	StopOutputPipe(ctx context.Context, sessionName string) error
}

// MakeFifoFunc creates a named pipe at path with the given mode. Returns nil
// if the pipe already exists with the same mode. Used by the session manager
// to set up per-session output FIFOs; injected for testability.
type MakeFifoFunc func(path string, mode uint32) error

type ManagerOptions struct {
	Store           *Store
	LoadCredentials func() (credentials.StoredCredentials, error)
	Control         ControlClient
	NewControl      func(credentials.StoredCredentials) (ControlClient, error)
	Tmux            TmuxRunner
	Relay           RelayClient
	Snapshot        SnapshotFunc
	Input           InputFunc
	Now             func() time.Time
	Hostname        func() (string, error)
	DoctorChecks    func(context.Context) ([]string, error)

	// OutputFifoDir is where per-session output FIFOs are created. Required
	// to enable live-output streaming; if empty, output streaming is disabled
	// (only the initial snapshot is forwarded).
	OutputFifoDir string
	// MakeFifo defaults to syscall.Mkfifo when nil.
	MakeFifo MakeFifoFunc
}

type Manager struct {
	daemonv1.UnimplementedDaemonServiceServer

	store           *Store
	loadCredentials func() (credentials.StoredCredentials, error)
	control         ControlClient
	newControl      func(credentials.StoredCredentials) (ControlClient, error)
	tmux            TmuxRunner
	relay           RelayClient
	now             func() time.Time
	hostname        func() (string, error)
	doctorChecks    func(context.Context) ([]string, error)

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
		store:           opts.Store,
		loadCredentials: opts.LoadCredentials,
		control:         opts.Control,
		newControl:      opts.NewControl,
		tmux:            opts.Tmux,
		relay:           opts.Relay,
		now:             now,
		hostname:        hostname,
		doctorChecks:    doctorChecks,
		outputFifoDir:   opts.OutputFifoDir,
		makeFifo:        makeFifo,
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

	createResp, err := controlClient.CreateHostSession(ctx, creds.AccessToken, openapi.CreateSessionRequest{
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
		m.markFailed(ctx, controlClient, creds.AccessToken, createResp.SessionId.String(), err)
		return nil, err
	}
	if err := m.tmux.StartSession(ctx, startSpec); err != nil {
		m.markFailed(ctx, controlClient, creds.AccessToken, createResp.SessionId.String(), err)
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

	if _, err := controlClient.UpdateHostSession(ctx, creds.AccessToken, createResp.SessionId.String(), openapi.UpdateSessionRequest{
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

func (m *Manager) markFailed(ctx context.Context, controlClient ControlClient, accessToken string, sessionID string, startErr error) {
	message := startErr.Error()
	if len(message) > 256 {
		message = message[:256]
	}
	_, _ = controlClient.UpdateHostSession(ctx, accessToken, sessionID, openapi.UpdateSessionRequest{
		Status:    openapi.UpdateSessionRequestStatus("failed"),
		LastError: &message,
	})
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
