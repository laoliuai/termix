package persistence

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	sqlcgen "github.com/termix/termix/go/gen/sqlc"
)

const SessionHeartbeatStaleAfter = 90 * time.Second

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
	LastSeenAt      time.Time
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
		LastSeenAt:      row.LastSeenAt.Time,
	}
}

func (s *Store) ListUserSessions(ctx context.Context, userID, statusFilter string) ([]Session, error) {
	if statusFilter == "" {
		statusFilter = "all"
	}
	uid, err := parseUUID(userID)
	if err != nil {
		return nil, err
	}
	rows, err := sqlcgen.New(s.Pool).ListUserSessions(ctx, uid)
	if err != nil {
		return nil, err
	}
	cutoff := time.Now().Add(-SessionHeartbeatStaleAfter)
	out := make([]Session, 0, len(rows))
	for _, r := range rows {
		session := sessionFromRow(r)
		session.Status = effectiveSessionStatus(session, cutoff)
		if statusFilter != "all" && session.Status != statusFilter {
			continue
		}
		out = append(out, session)
	}
	return out, nil
}

func (s *Store) TouchSessionHeartbeat(ctx context.Context, sessionID, userID, hostDeviceID, status string) (Session, error) {
	id, err := parseUUID(sessionID)
	if err != nil {
		return Session{}, err
	}
	uid, err := parseUUID(userID)
	if err != nil {
		return Session{}, err
	}
	hid, err := parseUUID(hostDeviceID)
	if err != nil {
		return Session{}, err
	}

	row, err := sqlcgen.New(s.Pool).TouchSessionHeartbeat(ctx, sqlcgen.TouchSessionHeartbeatParams{
		ID:           id,
		UserID:       uid,
		HostDeviceID: hid,
		Status:       status,
	})
	if err != nil {
		return Session{}, err
	}
	return sessionFromRow(row), nil
}

func effectiveSessionStatus(session Session, cutoff time.Time) string {
	if session.Status != "starting" && session.Status != "running" && session.Status != "idle" {
		return session.Status
	}
	if session.LastSeenAt.IsZero() || !session.LastSeenAt.After(cutoff) {
		return "disconnected"
	}
	return session.Status
}

func textPtr(value pgtype.Text) *string {
	if !value.Valid {
		return nil
	}
	result := value.String
	return &result
}
