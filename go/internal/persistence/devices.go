package persistence

import (
	"context"

	sqlcgen "github.com/termix/termix/go/gen/sqlc"
)

type Device struct {
	ID         string
	UserID     string
	DeviceType string
	Platform   string
	Label      string
}

func (s *Store) CreateHostDevice(ctx context.Context, userID, platform, label, hostname string) (Device, error) {
	parsedUserID, err := parseUUID(userID)
	if err != nil {
		return Device{}, err
	}

	row, err := sqlcgen.New(s.Pool).CreateHostDevice(ctx, sqlcgen.CreateHostDeviceParams{
		UserID:   parsedUserID,
		Platform: platform,
		Label:    label,
		Hostname: nullableText(hostname),
	})
	if err != nil {
		return Device{}, err
	}
	return deviceFromRow(row), nil
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
