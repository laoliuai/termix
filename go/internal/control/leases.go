package control

import (
	"context"
	"errors"
	"time"

	"github.com/termix/termix/go/internal/persistence"
)

var (
	ErrUnauthorized           = errors.New("unauthorized")
	ErrNotFound               = errors.New("not found")
	ErrSessionNotControllable = errors.New("session not controllable")
	ErrAlreadyControlled      = errors.New("session already controlled")
	ErrStaleLease             = errors.New("stale lease")
)

type ControlActor struct {
	UserID   string
	DeviceID string
}

type LeaseRepository interface {
	GetSessionForUser(ctx context.Context, sessionID, userID string) (persistence.Session, error)
	GetDeviceForUser(ctx context.Context, deviceID, userID string) (persistence.Device, error)
	GetActiveControlLease(ctx context.Context, sessionID string, now time.Time) (persistence.ControlLease, bool, error)
	UpsertControlLease(ctx context.Context, params persistence.UpsertControlLeaseParams) (persistence.ControlLease, error)
	RenewControlLease(ctx context.Context, params persistence.RenewControlLeaseParams) (persistence.ControlLease, error)
	ReleaseControlLease(ctx context.Context, params persistence.ReleaseControlLeaseParams) (persistence.ControlLease, error)
}

type LeaseServiceConfig struct {
	TTL time.Duration
	Now func() time.Time
}

type LeaseService struct {
	repo LeaseRepository
	ttl  time.Duration
	now  func() time.Time
}

func NewLeaseService(repo LeaseRepository, cfg LeaseServiceConfig) *LeaseService {
	ttl := cfg.TTL
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}

	return &LeaseService{
		repo: repo,
		ttl:  ttl,
		now:  now,
	}
}

func (s *LeaseService) Acquire(ctx context.Context, actor ControlActor, sessionID string) (persistence.ControlLease, error) {
	if err := validateActor(actor); err != nil {
		return persistence.ControlLease{}, err
	}
	if sessionID == "" {
		return persistence.ControlLease{}, ErrNotFound
	}
	if err := s.validateSessionAndDevice(ctx, actor, sessionID); err != nil {
		return persistence.ControlLease{}, err
	}

	now := s.now().UTC()
	activeLease, active, err := s.repo.GetActiveControlLease(ctx, sessionID, now)
	if err != nil {
		return persistence.ControlLease{}, err
	}
	if active && activeLease.ControllerDeviceID != actor.DeviceID {
		return persistence.ControlLease{}, ErrAlreadyControlled
	}

	lease, err := s.repo.UpsertControlLease(ctx, persistence.UpsertControlLeaseParams{
		SessionID:          sessionID,
		ControllerDeviceID: actor.DeviceID,
		Now:                now,
		ExpiresAt:          now.Add(s.ttl),
	})
	if err != nil {
		if persistence.IsNotFound(err) {
			return persistence.ControlLease{}, ErrAlreadyControlled
		}
		return persistence.ControlLease{}, err
	}
	return lease, nil
}

func (s *LeaseService) Renew(ctx context.Context, actor ControlActor, sessionID string, leaseVersion int64) (persistence.ControlLease, error) {
	if err := validateActor(actor); err != nil {
		return persistence.ControlLease{}, err
	}
	if sessionID == "" {
		return persistence.ControlLease{}, ErrNotFound
	}
	if leaseVersion <= 0 {
		return persistence.ControlLease{}, ErrStaleLease
	}
	if err := s.validateSessionAndDevice(ctx, actor, sessionID); err != nil {
		return persistence.ControlLease{}, err
	}

	now := s.now().UTC()
	lease, err := s.repo.RenewControlLease(ctx, persistence.RenewControlLeaseParams{
		SessionID:          sessionID,
		ControllerDeviceID: actor.DeviceID,
		LeaseVersion:       leaseVersion,
		Now:                now,
		ExpiresAt:          now.Add(s.ttl),
	})
	if err != nil {
		if persistence.IsNotFound(err) {
			return persistence.ControlLease{}, ErrStaleLease
		}
		return persistence.ControlLease{}, err
	}
	return lease, nil
}

func (s *LeaseService) Release(ctx context.Context, actor ControlActor, sessionID string, leaseVersion int64) (persistence.ControlLease, error) {
	if err := validateActor(actor); err != nil {
		return persistence.ControlLease{}, err
	}
	if sessionID == "" {
		return persistence.ControlLease{}, ErrNotFound
	}
	if leaseVersion <= 0 {
		return persistence.ControlLease{}, ErrStaleLease
	}
	if err := s.validateSessionAndDevice(ctx, actor, sessionID); err != nil {
		return persistence.ControlLease{}, err
	}

	lease, err := s.repo.ReleaseControlLease(ctx, persistence.ReleaseControlLeaseParams{
		SessionID:          sessionID,
		ControllerDeviceID: actor.DeviceID,
		LeaseVersion:       leaseVersion,
	})
	if err != nil {
		if persistence.IsNotFound(err) {
			return persistence.ControlLease{}, ErrStaleLease
		}
		return persistence.ControlLease{}, err
	}
	return lease, nil
}

func (s *LeaseService) GetActive(ctx context.Context, sessionID string) (persistence.ControlLease, bool, error) {
	if sessionID == "" {
		return persistence.ControlLease{}, false, ErrNotFound
	}
	return s.repo.GetActiveControlLease(ctx, sessionID, s.now().UTC())
}

func RenewAfterSeconds(ttl time.Duration) int {
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	seconds := int((ttl / 2) / time.Second)
	if seconds < 1 {
		return 1
	}
	return seconds
}

func validateActor(actor ControlActor) error {
	if actor.UserID == "" || actor.DeviceID == "" {
		return ErrUnauthorized
	}
	return nil
}

func (s *LeaseService) validateSessionAndDevice(ctx context.Context, actor ControlActor, sessionID string) error {
	session, err := s.repo.GetSessionForUser(ctx, sessionID, actor.UserID)
	if err != nil {
		if persistence.IsNotFound(err) {
			return ErrNotFound
		}
		return err
	}
	if !isControllableStatus(session.Status) {
		return ErrSessionNotControllable
	}

	_, err = s.repo.GetDeviceForUser(ctx, actor.DeviceID, actor.UserID)
	if err != nil {
		if persistence.IsNotFound(err) {
			return ErrUnauthorized
		}
		return err
	}
	return nil
}

func isControllableStatus(status string) bool {
	return status == "running" || status == "idle"
}
