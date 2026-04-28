package session

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
)

type Store struct {
	baseDir string
}

func NewStore(baseDir string) *Store {
	return &Store{baseDir: baseDir}
}

func (s *Store) Save(session LocalSession) error {
	if err := os.MkdirAll(s.sessionsDir(), 0o700); err != nil {
		return err
	}

	data, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(s.sessionPath(session.SessionID), data, 0o600)
}

func (s *Store) Load(sessionID string) (LocalSession, error) {
	data, err := os.ReadFile(s.sessionPath(sessionID))
	if err != nil {
		return LocalSession{}, err
	}

	var session LocalSession
	if err := json.Unmarshal(data, &session); err != nil {
		return LocalSession{}, err
	}
	return session, nil
}

func (s *Store) List() ([]LocalSession, error) {
	entries, err := filepath.Glob(filepath.Join(s.sessionsDir(), "*.json"))
	if err != nil {
		return nil, err
	}
	sort.Strings(entries)

	sessions := make([]LocalSession, 0, len(entries))
	for _, entry := range entries {
		data, err := os.ReadFile(entry)
		if err != nil {
			return nil, err
		}

		var session LocalSession
		if err := json.Unmarshal(data, &session); err != nil {
			return nil, err
		}
		sessions = append(sessions, session)
	}
	return sessions, nil
}

// Delete removes the local-state file for sessionID. Missing files are not
// an error — Delete is idempotent so the reaper can call it after a PATCH
// without worrying about partial cleanup from a previous run.
func (s *Store) Delete(sessionID string) error {
	if err := os.Remove(s.sessionPath(sessionID)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (s *Store) sessionsDir() string {
	return filepath.Join(s.baseDir, "sessions")
}

func (s *Store) sessionPath(sessionID string) string {
	return filepath.Join(s.sessionsDir(), sessionID+".json")
}
