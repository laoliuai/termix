package persistence

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	sqlcgen "github.com/termix/termix/go/gen/sqlc"
)

type Store struct {
	Pool    *pgxpool.Pool
	queries *sqlcgen.Queries
}

func New(pool *pgxpool.Pool) *Store {
	return &Store{
		Pool:    pool,
		queries: sqlcgen.New(pool),
	}
}

func NewTestStore(t *testing.T) (*Store, func()) {
	t.Helper()
	dsn := os.Getenv("TERMIX_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TERMIX_TEST_DATABASE_URL to run database integration tests")
	}

	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("pgxpool.New returned error: %v", err)
	}
	if err := ensureTestSchema(context.Background(), pool); err != nil {
		pool.Close()
		t.Fatalf("ensure test schema returned error: %v", err)
	}

	return New(pool), func() { pool.Close() }
}

func (s *Store) Ping(ctx context.Context) error {
	return s.Pool.Ping(ctx)
}

func IsNotFound(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}

func parseUUID(raw string) (pgtype.UUID, error) {
	var id pgtype.UUID
	if err := id.Scan(raw); err != nil {
		return pgtype.UUID{}, fmt.Errorf("parse uuid %q: %w", raw, err)
	}
	return id, nil
}

func nullableText(raw string) pgtype.Text {
	if raw == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{
		String: raw,
		Valid:  true,
	}
}

func ensureTestSchema(ctx context.Context, pool *pgxpool.Pool) error {
	const schemaStateQuery = `
select
  to_regclass('public.users') is not null,
  to_regclass('public.devices') is not null,
  to_regclass('public.refresh_tokens') is not null,
  to_regclass('public.sessions') is not null,
  to_regclass('public.control_leases') is not null
`

	var usersExists, devicesExists, refreshTokensExists, sessionsExists, controlLeasesExists bool
	if err := pool.QueryRow(ctx, schemaStateQuery).Scan(&usersExists, &devicesExists, &refreshTokensExists, &sessionsExists, &controlLeasesExists); err != nil {
		return fmt.Errorf("check schema presence: %w", err)
	}

	existingCount := 0
	if usersExists {
		existingCount++
	}
	if devicesExists {
		existingCount++
	}
	if refreshTokensExists {
		existingCount++
	}
	if sessionsExists {
		existingCount++
	}

	if existingCount == 4 {
		if controlLeasesExists {
			return nil
		}
		controlLeasesSQL, err := loadMigrationSQL("000002_control_leases.up.sql")
		if err != nil {
			return err
		}
		if _, err := pool.Exec(ctx, controlLeasesSQL); err != nil {
			return fmt.Errorf("apply control leases migration: %w", err)
		}
		return nil
	}
	if existingCount != 0 {
		return fmt.Errorf(
			"partial schema state detected: users=%t devices=%t refresh_tokens=%t sessions=%t control_leases=%t; expected all or none",
			usersExists,
			devicesExists,
			refreshTokensExists,
			sessionsExists,
			controlLeasesExists,
		)
	}
	if controlLeasesExists {
		return fmt.Errorf(
			"partial schema state detected: users=%t devices=%t refresh_tokens=%t sessions=%t control_leases=%t; expected all or none",
			usersExists,
			devicesExists,
			refreshTokensExists,
			sessionsExists,
			controlLeasesExists,
		)
	}

	migrationSQL, err := loadMigrationSQL("000001_init.up.sql")
	if err != nil {
		return err
	}
	if _, err := pool.Exec(ctx, migrationSQL); err != nil {
		return fmt.Errorf("apply init migration: %w", err)
	}

	controlLeasesSQL, err := loadMigrationSQL("000002_control_leases.up.sql")
	if err != nil {
		return err
	}
	if _, err := pool.Exec(ctx, controlLeasesSQL); err != nil {
		return fmt.Errorf("apply control leases migration: %w", err)
	}
	return nil
}

func loadMigrationSQL(filename string) (string, error) {
	_, src, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("resolve source location for migrations")
	}
	migrationPath := filepath.Clean(filepath.Join(filepath.Dir(src), "..", "..", "..", "db", "migrations", filename))
	sqlBytes, err := os.ReadFile(migrationPath)
	if err != nil {
		return "", fmt.Errorf("read migration file %q: %w", migrationPath, err)
	}
	return string(sqlBytes), nil
}
