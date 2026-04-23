package persistence

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
	sqlcgen "github.com/termix/termix/go/gen/sqlc"
)

type CreateSessionParams struct {
	UserID          string
	HostDeviceID    string
	Name            string
	Tool            string
	LaunchCommand   string
	Cwd             string
	CwdLabel        string
	TmuxSessionName string
	Status          string
}

type Session struct {
	ID              string
	UserID          string
	HostDeviceID    string
	Name            *string
	Tool            string
	LaunchCommand   string
	Cwd             string
	CwdLabel        string
	TmuxSessionName string
	Status          string
}

func (s *Store) CreateSession(ctx context.Context, params CreateSessionParams) (Session, error) {
	userID, err := parseUUID(params.UserID)
	if err != nil {
		return Session{}, err
	}
	hostDeviceID, err := parseUUID(params.HostDeviceID)
	if err != nil {
		return Session{}, err
	}

	row, err := sqlcgen.New(s.Pool).CreateSession(ctx, sqlcgen.CreateSessionParams{
		UserID:          userID,
		HostDeviceID:    hostDeviceID,
		Name:            nullableText(params.Name),
		Tool:            params.Tool,
		LaunchCommand:   params.LaunchCommand,
		Cwd:             params.Cwd,
		CwdLabel:        params.CwdLabel,
		TmuxSessionName: params.TmuxSessionName,
		Status:          params.Status,
	})
	if err != nil {
		return Session{}, err
	}
	return sessionFromRow(row), nil
}

func (s *Store) UpdateSessionStatus(ctx context.Context, sessionID string, status string, lastError *string, lastExitCode *int) (Session, error) {
	id, err := parseUUID(sessionID)
	if err != nil {
		return Session{}, err
	}

	lastExitCodeValue := pgtype.Int4{}
	if lastExitCode != nil {
		lastExitCodeValue = pgtype.Int4{
			Int32: int32(*lastExitCode),
			Valid: true,
		}
	}

	lastErrorValue := pgtype.Text{}
	if lastError != nil {
		lastErrorValue = pgtype.Text{
			String: *lastError,
			Valid:  true,
		}
	}

	row, err := sqlcgen.New(s.Pool).UpdateSessionStatus(ctx, sqlcgen.UpdateSessionStatusParams{
		ID:           id,
		Status:       status,
		LastError:    lastErrorValue,
		LastExitCode: lastExitCodeValue,
	})
	if err != nil {
		return Session{}, err
	}
	return sessionFromRow(row), nil
}

func (s *Store) GetSessionForUser(ctx context.Context, sessionID string, userID string) (Session, error) {
	id, err := parseUUID(sessionID)
	if err != nil {
		return Session{}, err
	}
	uid, err := parseUUID(userID)
	if err != nil {
		return Session{}, err
	}

	row, err := sqlcgen.New(s.Pool).GetSessionForUser(ctx, sqlcgen.GetSessionForUserParams{
		ID:     id,
		UserID: uid,
	})
	if err != nil {
		return Session{}, err
	}
	return sessionFromRow(row), nil
}

func sessionFromRow(row sqlcgen.Session) Session {
	return Session{
		ID:              row.ID.String(),
		UserID:          row.UserID.String(),
		HostDeviceID:    row.HostDeviceID.String(),
		Name:            textPtr(row.Name),
		Tool:            row.Tool,
		LaunchCommand:   row.LaunchCommand,
		Cwd:             row.Cwd,
		CwdLabel:        row.CwdLabel,
		TmuxSessionName: row.TmuxSessionName,
		Status:          row.Status,
	}
}

func textPtr(value pgtype.Text) *string {
	if !value.Valid {
		return nil
	}
	result := value.String
	return &result
}
