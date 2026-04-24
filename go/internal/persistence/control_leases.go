package persistence

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	sqlcgen "github.com/termix/termix/go/gen/sqlc"
)

type ControlLease struct {
	SessionID          string
	ControllerDeviceID string
	LeaseVersion       int64
	GrantedAt          time.Time
	ExpiresAt          time.Time
}

type UpsertControlLeaseParams struct {
	SessionID          string
	ControllerDeviceID string
	Now                time.Time
	ExpiresAt          time.Time
}

func (s *Store) UpsertControlLease(ctx context.Context, params UpsertControlLeaseParams) (ControlLease, error) {
	sessionID, err := parseUUID(params.SessionID)
	if err != nil {
		return ControlLease{}, err
	}
	controllerDeviceID, err := parseUUID(params.ControllerDeviceID)
	if err != nil {
		return ControlLease{}, err
	}

	row, err := s.queries.UpsertControlLease(ctx, sqlcgen.UpsertControlLeaseParams{
		SessionID:          sessionID,
		ControllerDeviceID: controllerDeviceID,
		Now:                timestamptz(params.Now),
		ExpiresAt:          timestamptz(params.ExpiresAt),
	})
	if err != nil {
		return ControlLease{}, err
	}
	return leaseFromRow(row), nil
}

func (s *Store) GetActiveControlLease(ctx context.Context, sessionID string, now time.Time) (ControlLease, bool, error) {
	parsedSessionID, err := parseUUID(sessionID)
	if err != nil {
		return ControlLease{}, false, err
	}

	row, err := s.queries.GetActiveControlLease(ctx, sqlcgen.GetActiveControlLeaseParams{
		SessionID: parsedSessionID,
		Now:       timestamptz(now),
	})
	if err != nil {
		if IsNotFound(err) {
			return ControlLease{}, false, nil
		}
		return ControlLease{}, false, err
	}
	return leaseFromRow(row), true, nil
}

type RenewControlLeaseParams struct {
	SessionID          string
	ControllerDeviceID string
	LeaseVersion       int64
	Now                time.Time
	ExpiresAt          time.Time
}

func (s *Store) RenewControlLease(ctx context.Context, params RenewControlLeaseParams) (ControlLease, error) {
	sessionID, err := parseUUID(params.SessionID)
	if err != nil {
		return ControlLease{}, err
	}
	controllerDeviceID, err := parseUUID(params.ControllerDeviceID)
	if err != nil {
		return ControlLease{}, err
	}

	row, err := s.queries.RenewControlLease(ctx, sqlcgen.RenewControlLeaseParams{
		SessionID:          sessionID,
		ControllerDeviceID: controllerDeviceID,
		LeaseVersion:       params.LeaseVersion,
		Now:                timestamptz(params.Now),
		ExpiresAt:          timestamptz(params.ExpiresAt),
	})
	if err != nil {
		return ControlLease{}, err
	}
	return leaseFromRow(row), nil
}

type ReleaseControlLeaseParams struct {
	SessionID          string
	ControllerDeviceID string
	LeaseVersion       int64
}

func (s *Store) ReleaseControlLease(ctx context.Context, params ReleaseControlLeaseParams) (ControlLease, error) {
	sessionID, err := parseUUID(params.SessionID)
	if err != nil {
		return ControlLease{}, err
	}
	controllerDeviceID, err := parseUUID(params.ControllerDeviceID)
	if err != nil {
		return ControlLease{}, err
	}

	row, err := s.queries.ReleaseControlLease(ctx, sqlcgen.ReleaseControlLeaseParams{
		SessionID:          sessionID,
		ControllerDeviceID: controllerDeviceID,
		LeaseVersion:       params.LeaseVersion,
	})
	if err != nil {
		return ControlLease{}, err
	}
	return leaseFromRow(row), nil
}

func leaseFromRow(row sqlcgen.ControlLease) ControlLease {
	return ControlLease{
		SessionID:          row.SessionID.String(),
		ControllerDeviceID: row.ControllerDeviceID.String(),
		LeaseVersion:       row.LeaseVersion,
		GrantedAt:          row.GrantedAt.Time,
		ExpiresAt:          row.ExpiresAt.Time,
	}
}

func timestamptz(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{
		Time:  value.UTC(),
		Valid: true,
	}
}
