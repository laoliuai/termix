package persistence

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	sqlcgen "github.com/termix/termix/go/gen/sqlc"
)

type Device struct {
	ID         string
	UserID     string
	DeviceType string
	Platform   string
	Label      string
}

func (s *Store) CreateDevice(ctx context.Context, userID, deviceType, platform, label, hostname string) (Device, error) {
	parsedUserID, err := parseUUID(userID)
	if err != nil {
		return Device{}, err
	}

	row, err := sqlcgen.New(s.Pool).CreateDevice(ctx, sqlcgen.CreateDeviceParams{
		UserID:     parsedUserID,
		DeviceType: deviceType,
		Platform:   platform,
		Label:      label,
		Hostname:   nullableText(hostname),
	})
	if err != nil {
		return Device{}, err
	}
	return deviceFromRow(row), nil
}

func (s *Store) CreateHostDevice(ctx context.Context, userID, platform, label, hostname string) (Device, error) {
	return s.CreateDevice(ctx, userID, "host", platform, label, hostname)
}

type TouchDeviceParams struct {
	ID         string
	AppVersion string
}

func (s *Store) TouchDevice(ctx context.Context, params TouchDeviceParams) error {
	id, err := parseUUID(params.ID)
	if err != nil {
		return err
	}

	return s.queries.TouchDevice(ctx, sqlcgen.TouchDeviceParams{
		ID:         id,
		AppVersion: nullableText(params.AppVersion),
	})
}

func (s *Store) GetDeviceForUser(ctx context.Context, deviceID string, userID string) (Device, error) {
	parsedDeviceID, err := parseUUID(deviceID)
	if err != nil {
		return Device{}, err
	}
	parsedUserID, err := parseUUID(userID)
	if err != nil {
		return Device{}, err
	}

	row, err := s.queries.GetDeviceForUser(ctx, sqlcgen.GetDeviceForUserParams{
		ID:     parsedDeviceID,
		UserID: parsedUserID,
	})
	if err != nil {
		return Device{}, err
	}
	return deviceFromRow(row), nil
}

func deviceFromRow(row sqlcgen.Device) Device {
	return Device{
		ID:         row.ID.String(),
		UserID:     row.UserID.String(),
		DeviceType: row.DeviceType,
		Platform:   row.Platform,
		Label:      row.Label,
	}
}

type RefreshToken struct {
	ID        string
	UserID    string
	DeviceID  string
	TokenHash string
	ExpiresAt string
	CreatedAt string
	RevokedAt string
}

func (s *Store) InsertRefreshToken(ctx context.Context, userID, deviceID, tokenHash string, expiresAt time.Time) (RefreshToken, error) {
	parsedUserID, err := parseUUID(userID)
	if err != nil {
		return RefreshToken{}, err
	}
	parsedDeviceID, err := parseUUID(deviceID)
	if err != nil {
		return RefreshToken{}, err
	}

	expiresAtTS := pgtype.Timestamptz{}
	if err := expiresAtTS.Scan(expiresAt); err != nil {
		return RefreshToken{}, err
	}

	row, err := sqlcgen.New(s.Pool).InsertRefreshToken(ctx, sqlcgen.InsertRefreshTokenParams{
		UserID:    parsedUserID,
		DeviceID:  parsedDeviceID,
		TokenHash: tokenHash,
		ExpiresAt: expiresAtTS,
	})
	if err != nil {
		return RefreshToken{}, err
	}
	return refreshTokenFromRow(row), nil
}

func (s *Store) GetActiveRefreshTokenByHash(ctx context.Context, tokenHash string) (RefreshToken, error) {
	row, err := sqlcgen.New(s.Pool).GetActiveRefreshTokenByHash(ctx, tokenHash)
	if err != nil {
		return RefreshToken{}, err
	}
	return refreshTokenFromRow(row), nil
}

func (s *Store) RevokeRefreshToken(ctx context.Context, tokenHash string) error {
	return sqlcgen.New(s.Pool).RevokeRefreshToken(ctx, tokenHash)
}

func refreshTokenFromRow(row sqlcgen.RefreshToken) RefreshToken {
	return RefreshToken{
		ID:        row.ID.String(),
		UserID:    row.UserID.String(),
		DeviceID:  row.DeviceID.String(),
		TokenHash: row.TokenHash,
		ExpiresAt: row.ExpiresAt.Time.Format("2006-01-02T15:04:05Z07:00"),
		CreatedAt: row.CreatedAt.Time.Format("2006-01-02T15:04:05Z07:00"),
		RevokedAt: row.RevokedAt.Time.Format("2006-01-02T15:04:05Z07:00"),
	}
}
