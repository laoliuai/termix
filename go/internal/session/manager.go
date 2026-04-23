package session

import (
	"context"
	"errors"
	"os"
	"path/filepath"
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
}

type ManagerOptions struct {
	Store           *Store
	LoadCredentials func() (credentials.StoredCredentials, error)
	Control         ControlClient
	NewControl      func(credentials.StoredCredentials) (ControlClient, error)
	Tmux            TmuxRunner
	Now             func() time.Time
	Hostname        func() (string, error)
	DoctorChecks    func(context.Context) ([]string, error)
}

type Manager struct {
	daemonv1.UnimplementedDaemonServiceServer

	store           *Store
	loadCredentials func() (credentials.StoredCredentials, error)
	control         ControlClient
	newControl      func(credentials.StoredCredentials) (ControlClient, error)
	tmux            TmuxRunner
	now             func() time.Time
	hostname        func() (string, error)
	doctorChecks    func(context.Context) ([]string, error)
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

	return &Manager{
		store:           opts.Store,
		loadCredentials: opts.LoadCredentials,
		control:         opts.Control,
		newControl:      opts.NewControl,
		tmux:            opts.Tmux,
		now:             now,
		hostname:        hostname,
		doctorChecks:    doctorChecks,
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

	if _, err := controlClient.UpdateHostSession(ctx, creds.AccessToken, createResp.SessionId.String(), openapi.UpdateSessionRequest{
		Status: openapi.Running,
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
		Status:    openapi.Failed,
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
