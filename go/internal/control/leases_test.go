package control

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/termix/termix/go/internal/persistence"
)

func TestLeaseService(t *testing.T) {
	t.Parallel()

	const (
		sessionID = "session-1"
		userID    = "user-1"
		deviceID  = "device-1"
	)

	now := time.Date(2026, 4, 24, 10, 0, 0, 0, time.UTC)
	actor := ControlActor{UserID: userID, DeviceID: deviceID}

	newService := func(repo *fakeLeaseRepo) *LeaseService {
		return NewLeaseService(repo, LeaseServiceConfig{Now: func() time.Time { return now }})
	}

	t.Run("Acquire creates lease with version 1 and default expiry", func(t *testing.T) {
		t.Parallel()

		repo := newFakeLeaseRepo()
		repo.addSession(sessionID, userID, "running")
		repo.addDevice(deviceID, userID)
		svc := newService(repo)

		lease, err := svc.Acquire(context.Background(), actor, sessionID)
		if err != nil {
			t.Fatalf("Acquire returned error: %v", err)
		}
		if lease.LeaseVersion != 1 {
			t.Fatalf("expected lease version 1, got %d", lease.LeaseVersion)
		}
		if !lease.GrantedAt.Equal(now) {
			t.Fatalf("expected granted_at %s, got %s", now, lease.GrantedAt)
		}
		expectedExpiry := now.Add(30 * time.Second)
		if !lease.ExpiresAt.Equal(expectedExpiry) {
			t.Fatalf("expected expires_at %s, got %s", expectedExpiry, lease.ExpiresAt)
		}
	})

	t.Run("Acquire denied when another device has an active lease", func(t *testing.T) {
		t.Parallel()

		repo := newFakeLeaseRepo()
		repo.addSession(sessionID, userID, "running")
		repo.addDevice(deviceID, userID)
		repo.leases[sessionID] = persistence.ControlLease{
			SessionID:          sessionID,
			ControllerDeviceID: "device-2",
			LeaseVersion:       4,
			GrantedAt:          now.Add(-5 * time.Second),
			ExpiresAt:          now.Add(20 * time.Second),
		}
		svc := newService(repo)

		_, err := svc.Acquire(context.Background(), actor, sessionID)
		if !errors.Is(err, ErrAlreadyControlled) {
			t.Fatalf("expected ErrAlreadyControlled, got %v", err)
		}
	})

	t.Run("Acquire replaces expired lease with a later version", func(t *testing.T) {
		t.Parallel()

		repo := newFakeLeaseRepo()
		repo.addSession(sessionID, userID, "running")
		repo.addDevice(deviceID, userID)
		repo.leases[sessionID] = persistence.ControlLease{
			SessionID:          sessionID,
			ControllerDeviceID: "device-2",
			LeaseVersion:       3,
			GrantedAt:          now.Add(-2 * time.Minute),
			ExpiresAt:          now.Add(-1 * time.Second),
		}
		svc := newService(repo)

		lease, err := svc.Acquire(context.Background(), actor, sessionID)
		if err != nil {
			t.Fatalf("Acquire returned error: %v", err)
		}
		if lease.LeaseVersion != 4 {
			t.Fatalf("expected lease version 4, got %d", lease.LeaseVersion)
		}
		if lease.ControllerDeviceID != deviceID {
			t.Fatalf("expected controller device %q, got %q", deviceID, lease.ControllerDeviceID)
		}
	})

	t.Run("Renew requires current device and version", func(t *testing.T) {
		t.Parallel()

		repo := newFakeLeaseRepo()
		repo.addSession(sessionID, userID, "running")
		repo.addDevice(deviceID, userID)
		repo.leases[sessionID] = persistence.ControlLease{
			SessionID:          sessionID,
			ControllerDeviceID: deviceID,
			LeaseVersion:       2,
			GrantedAt:          now.Add(-5 * time.Second),
			ExpiresAt:          now.Add(20 * time.Second),
		}
		svc := newService(repo)

		_, err := svc.Renew(context.Background(), actor, sessionID, 1)
		if !errors.Is(err, ErrStaleLease) {
			t.Fatalf("expected ErrStaleLease, got %v", err)
		}
	})

	t.Run("Renew maps repository not found to stale lease", func(t *testing.T) {
		t.Parallel()

		repo := newFakeLeaseRepo()
		repo.addSession(sessionID, userID, "running")
		repo.addDevice(deviceID, userID)
		repo.failRenewWithNotFound = true
		svc := newService(repo)

		_, err := svc.Renew(context.Background(), actor, sessionID, 1)
		if !errors.Is(err, ErrStaleLease) {
			t.Fatalf("expected ErrStaleLease, got %v", err)
		}
	})

	t.Run("Release requires current device and version", func(t *testing.T) {
		t.Parallel()

		repo := newFakeLeaseRepo()
		repo.addSession(sessionID, userID, "running")
		repo.addDevice(deviceID, userID)
		repo.leases[sessionID] = persistence.ControlLease{
			SessionID:          sessionID,
			ControllerDeviceID: deviceID,
			LeaseVersion:       5,
			GrantedAt:          now.Add(-5 * time.Second),
			ExpiresAt:          now.Add(20 * time.Second),
		}
		svc := newService(repo)

		_, err := svc.Release(context.Background(), actor, sessionID, 4)
		if !errors.Is(err, ErrStaleLease) {
			t.Fatalf("expected ErrStaleLease, got %v", err)
		}
	})

	t.Run("Release maps repository not found to stale lease", func(t *testing.T) {
		t.Parallel()

		repo := newFakeLeaseRepo()
		repo.addSession(sessionID, userID, "running")
		repo.addDevice(deviceID, userID)
		repo.failReleaseWithNotFound = true
		svc := newService(repo)

		_, err := svc.Release(context.Background(), actor, sessionID, 1)
		if !errors.Is(err, ErrStaleLease) {
			t.Fatalf("expected ErrStaleLease, got %v", err)
		}
	})

	t.Run("Missing actor claims are unauthorized", func(t *testing.T) {
		t.Parallel()

		repo := newFakeLeaseRepo()
		repo.addSession(sessionID, userID, "running")
		repo.addDevice(deviceID, userID)
		svc := newService(repo)

		_, err := svc.Acquire(context.Background(), ControlActor{UserID: userID}, sessionID)
		if !errors.Is(err, ErrUnauthorized) {
			t.Fatalf("expected ErrUnauthorized, got %v", err)
		}
	})

	t.Run("Non-controllable session status is rejected", func(t *testing.T) {
		t.Parallel()

		repo := newFakeLeaseRepo()
		repo.addSession(sessionID, userID, "failed")
		repo.addDevice(deviceID, userID)
		svc := newService(repo)

		_, err := svc.Acquire(context.Background(), actor, sessionID)
		if !errors.Is(err, ErrSessionNotControllable) {
			t.Fatalf("expected ErrSessionNotControllable, got %v", err)
		}
	})

	t.Run("Session not found maps to not found", func(t *testing.T) {
		t.Parallel()

		repo := newFakeLeaseRepo()
		repo.addDevice(deviceID, userID)
		svc := newService(repo)

		_, err := svc.Acquire(context.Background(), actor, sessionID)
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got %v", err)
		}
	})
}

type fakeLeaseRepo struct {
	sessions map[string]persistence.Session
	devices  map[string]persistence.Device
	leases   map[string]persistence.ControlLease

	failUpsertWithNotFound  bool
	failRenewWithNotFound   bool
	failReleaseWithNotFound bool
}

func newFakeLeaseRepo() *fakeLeaseRepo {
	return &fakeLeaseRepo{
		sessions: make(map[string]persistence.Session),
		devices:  make(map[string]persistence.Device),
		leases:   make(map[string]persistence.ControlLease),
	}
}

func (f *fakeLeaseRepo) addSession(id, userID, status string) {
	f.sessions[id] = persistence.Session{ID: id, UserID: userID, Status: status}
}

func (f *fakeLeaseRepo) addDevice(id, userID string) {
	f.devices[id] = persistence.Device{ID: id, UserID: userID}
}

func (f *fakeLeaseRepo) GetSessionForUser(_ context.Context, sessionID, userID string) (persistence.Session, error) {
	session, ok := f.sessions[sessionID]
	if !ok || session.UserID != userID {
		return persistence.Session{}, pgx.ErrNoRows
	}
	return session, nil
}

func (f *fakeLeaseRepo) GetDeviceForUser(_ context.Context, deviceID, userID string) (persistence.Device, error) {
	device, ok := f.devices[deviceID]
	if !ok || device.UserID != userID {
		return persistence.Device{}, pgx.ErrNoRows
	}
	return device, nil
}

func (f *fakeLeaseRepo) GetActiveControlLease(_ context.Context, sessionID string, now time.Time) (persistence.ControlLease, bool, error) {
	lease, ok := f.leases[sessionID]
	if !ok {
		return persistence.ControlLease{}, false, nil
	}
	if !lease.ExpiresAt.After(now) {
		return persistence.ControlLease{}, false, nil
	}
	return lease, true, nil
}

func (f *fakeLeaseRepo) UpsertControlLease(_ context.Context, params persistence.UpsertControlLeaseParams) (persistence.ControlLease, error) {
	if f.failUpsertWithNotFound {
		return persistence.ControlLease{}, pgx.ErrNoRows
	}

	existing, ok := f.leases[params.SessionID]
	if ok && existing.ExpiresAt.After(params.Now) && existing.ControllerDeviceID != params.ControllerDeviceID {
		return persistence.ControlLease{}, pgx.ErrNoRows
	}

	version := int64(1)
	if ok {
		version = existing.LeaseVersion + 1
	}

	lease := persistence.ControlLease{
		SessionID:          params.SessionID,
		ControllerDeviceID: params.ControllerDeviceID,
		LeaseVersion:       version,
		GrantedAt:          params.Now,
		ExpiresAt:          params.ExpiresAt,
	}
	f.leases[params.SessionID] = lease
	return lease, nil
}

func (f *fakeLeaseRepo) RenewControlLease(_ context.Context, params persistence.RenewControlLeaseParams) (persistence.ControlLease, error) {
	if f.failRenewWithNotFound {
		return persistence.ControlLease{}, pgx.ErrNoRows
	}

	lease, ok := f.leases[params.SessionID]
	if !ok {
		return persistence.ControlLease{}, pgx.ErrNoRows
	}
	if lease.ControllerDeviceID != params.ControllerDeviceID || lease.LeaseVersion != params.LeaseVersion || !lease.ExpiresAt.After(params.Now) {
		return persistence.ControlLease{}, pgx.ErrNoRows
	}

	lease.LeaseVersion++
	lease.ExpiresAt = params.ExpiresAt
	f.leases[params.SessionID] = lease
	return lease, nil
}

func (f *fakeLeaseRepo) ReleaseControlLease(_ context.Context, params persistence.ReleaseControlLeaseParams) (persistence.ControlLease, error) {
	if f.failReleaseWithNotFound {
		return persistence.ControlLease{}, pgx.ErrNoRows
	}

	lease, ok := f.leases[params.SessionID]
	if !ok {
		return persistence.ControlLease{}, pgx.ErrNoRows
	}
	if lease.ControllerDeviceID != params.ControllerDeviceID || lease.LeaseVersion != params.LeaseVersion {
		return persistence.ControlLease{}, pgx.ErrNoRows
	}

	delete(f.leases, params.SessionID)
	return lease, nil
}
