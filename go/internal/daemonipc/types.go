package daemonipc

import (
	"context"
	"path/filepath"

	daemonv1 "github.com/termix/termix/go/gen/proto/daemonv1"
	"github.com/termix/termix/go/internal/config"
)

type Service interface {
	Health(context.Context, *daemonv1.HealthRequest) (*daemonv1.HealthResponse, error)
	StartSession(context.Context, *daemonv1.StartSessionRequest) (*daemonv1.StartSessionResponse, error)
	ListSessions(context.Context, *daemonv1.ListSessionsRequest) (*daemonv1.ListSessionsResponse, error)
	AttachInfo(context.Context, *daemonv1.AttachInfoRequest) (*daemonv1.AttachInfoResponse, error)
	Doctor(context.Context, *daemonv1.DoctorRequest) (*daemonv1.DoctorResponse, error)
}

func SocketPath(paths config.HostPaths) string {
	return filepath.Join(paths.RunDir, "daemon.sock")
}
