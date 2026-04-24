package relaycontrol

import (
	"context"
	"time"

	relaycontrolv1 "github.com/termix/termix/go/gen/proto/relaycontrolv1"
	"github.com/termix/termix/go/internal/auth"
	"github.com/termix/termix/go/internal/control"
	"github.com/termix/termix/go/internal/persistence"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const defaultLeaseTTL = 30 * time.Second

type ServerConfig struct {
	LeaseTTL time.Duration
	Now      func() time.Time
}

type Server struct {
	relaycontrolv1.UnimplementedRelayControlServiceServer

	repo         control.LeaseRepository
	signingKey   string
	ttl          time.Duration
	leaseService *control.LeaseService
}

func NewServer(repo control.LeaseRepository, signingKey string, cfg ServerConfig) *Server {
	ttl := cfg.LeaseTTL
	if ttl <= 0 {
		ttl = defaultLeaseTTL
	}

	return &Server{
		repo:       repo,
		signingKey: signingKey,
		ttl:        ttl,
		leaseService: control.NewLeaseService(repo, control.LeaseServiceConfig{
			TTL: ttl,
			Now: cfg.Now,
		}),
	}
}

func (s *Server) ValidateAccessToken(context.Context, *relaycontrolv1.ValidateAccessTokenRequest) (*relaycontrolv1.ValidateAccessTokenResponse, error) {
	return nil, status.Error(codes.Unimplemented, "validate access token is deferred")
}

func (s *Server) AuthorizeSessionWatch(ctx context.Context, req *relaycontrolv1.AuthorizeSessionWatchRequest) (*relaycontrolv1.AuthorizeSessionWatchResponse, error) {
	actor, err := s.actorFromToken(req.GetAccessToken())
	if err != nil {
		return nil, err
	}

	session, err := s.repo.GetSessionForUser(ctx, req.GetSessionId(), actor.UserID)
	if err != nil {
		if persistence.IsNotFound(err) {
			return nil, grpcError(control.ErrNotFound)
		}
		return nil, grpcError(err)
	}

	return &relaycontrolv1.AuthorizeSessionWatchResponse{
		SessionId: session.ID,
		UserId:    session.UserID,
	}, nil
}

func (s *Server) AcquireControlLease(ctx context.Context, req *relaycontrolv1.AcquireControlLeaseRequest) (*relaycontrolv1.ControlLeaseResponse, error) {
	actor, err := s.actorFromToken(req.GetAccessToken())
	if err != nil {
		return nil, err
	}

	lease, err := s.leaseService.Acquire(ctx, actor, req.GetSessionId())
	if err != nil {
		return nil, grpcError(err)
	}
	return s.controlLeaseResponse(lease), nil
}

func (s *Server) RenewControlLease(ctx context.Context, req *relaycontrolv1.RenewControlLeaseRequest) (*relaycontrolv1.ControlLeaseResponse, error) {
	actor, err := s.actorFromToken(req.GetAccessToken())
	if err != nil {
		return nil, err
	}

	lease, err := s.leaseService.Renew(ctx, actor, req.GetSessionId(), req.GetLeaseVersion())
	if err != nil {
		return nil, grpcError(err)
	}
	return s.controlLeaseResponse(lease), nil
}

func (s *Server) ReleaseControlLease(ctx context.Context, req *relaycontrolv1.ReleaseControlLeaseRequest) (*relaycontrolv1.ReleaseControlLeaseResponse, error) {
	actor, err := s.actorFromToken(req.GetAccessToken())
	if err != nil {
		return nil, err
	}

	lease, err := s.leaseService.Release(ctx, actor, req.GetSessionId(), req.GetLeaseVersion())
	if err != nil {
		return nil, grpcError(err)
	}
	return &relaycontrolv1.ReleaseControlLeaseResponse{
		SessionId:    lease.SessionID,
		LeaseVersion: lease.LeaseVersion,
		Released:     true,
	}, nil
}

func (s *Server) MarkConnectionOpened(context.Context, *relaycontrolv1.MarkConnectionOpenedRequest) (*relaycontrolv1.MarkConnectionOpenedResponse, error) {
	return nil, status.Error(codes.Unimplemented, "mark connection opened is deferred")
}

func (s *Server) MarkConnectionClosed(context.Context, *relaycontrolv1.MarkConnectionClosedRequest) (*relaycontrolv1.MarkConnectionClosedResponse, error) {
	return nil, status.Error(codes.Unimplemented, "mark connection closed is deferred")
}

func (s *Server) actorFromToken(accessToken string) (control.ControlActor, error) {
	claims, err := auth.ParseAccessToken(s.signingKey, accessToken)
	if err != nil {
		return control.ControlActor{}, grpcError(control.ErrUnauthorized)
	}
	return control.ControlActor{
		UserID:   claims.UserID,
		DeviceID: claims.DeviceID,
	}, nil
}

func (s *Server) controlLeaseResponse(lease persistence.ControlLease) *relaycontrolv1.ControlLeaseResponse {
	return &relaycontrolv1.ControlLeaseResponse{
		SessionId:          lease.SessionID,
		ControllerDeviceId: lease.ControllerDeviceID,
		LeaseVersion:       lease.LeaseVersion,
		GrantedAt:          lease.GrantedAt.UTC().Format(time.RFC3339),
		ExpiresAt:          lease.ExpiresAt.UTC().Format(time.RFC3339),
		RenewAfterSeconds:  int32(control.RenewAfterSeconds(s.ttl)),
	}
}
