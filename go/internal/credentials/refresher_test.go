package credentials

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeRefreshClient counts calls and lets each test wire up its own behaviour.
type fakeRefreshClient struct {
	mu    sync.Mutex
	calls int32
	fn    func(ctx context.Context, refreshToken string) (*RefreshResult, error)
}

func (f *fakeRefreshClient) RefreshAccessToken(ctx context.Context, refreshToken string) (*RefreshResult, error) {
	atomic.AddInt32(&f.calls, 1)
	f.mu.Lock()
	fn := f.fn
	f.mu.Unlock()
	if fn == nil {
		return &RefreshResult{AccessToken: "new-token", ExpiresInSeconds: 1800}, nil
	}
	return fn(ctx, refreshToken)
}

func writeCreds(t *testing.T, dir string, creds StoredCredentials) string {
	t.Helper()
	path := filepath.Join(dir, "credentials.json")
	if err := Save(path, creds); err != nil {
		t.Fatalf("Save: %v", err)
	}
	return path
}

func TestEnsureFreshSkipsRefreshWhenTokenIsFresh(t *testing.T) {
	now := time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC)
	path := writeCreds(t, t.TempDir(), StoredCredentials{
		AccessToken:  "current",
		RefreshToken: "refresh",
		ExpiresAt:    now.Add(20 * time.Minute).Format(time.RFC3339),
	})
	client := &fakeRefreshClient{}

	r := NewRefresher(path, client, func() time.Time { return now })
	got, err := r.EnsureFresh(context.Background())
	if err != nil {
		t.Fatalf("EnsureFresh: %v", err)
	}
	if got.AccessToken != "current" {
		t.Fatalf("expected token to stay 'current', got %q", got.AccessToken)
	}
	if c := atomic.LoadInt32(&client.calls); c != 0 {
		t.Fatalf("expected no refresh calls, got %d", c)
	}
}

func TestEnsureFreshRefreshesWhenTokenIsExpired(t *testing.T) {
	now := time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC)
	path := writeCreds(t, t.TempDir(), StoredCredentials{
		AccessToken:  "stale",
		RefreshToken: "refresh-xyz",
		ExpiresAt:    now.Add(-5 * time.Minute).Format(time.RFC3339),
	})
	var sawRefreshToken string
	client := &fakeRefreshClient{
		fn: func(_ context.Context, rt string) (*RefreshResult, error) {
			sawRefreshToken = rt
			return &RefreshResult{AccessToken: "fresh", ExpiresInSeconds: 1800}, nil
		},
	}

	r := NewRefresher(path, client, func() time.Time { return now })
	got, err := r.EnsureFresh(context.Background())
	if err != nil {
		t.Fatalf("EnsureFresh: %v", err)
	}
	if got.AccessToken != "fresh" {
		t.Fatalf("expected access token to be 'fresh', got %q", got.AccessToken)
	}
	if got.RefreshToken != "refresh-xyz" {
		t.Fatalf("refresh token must be preserved (V1 has no rotation), got %q", got.RefreshToken)
	}
	if sawRefreshToken != "refresh-xyz" {
		t.Fatalf("RefreshAccessToken got refresh_token %q, want %q", sawRefreshToken, "refresh-xyz")
	}
	expectedExpiry := now.Add(1800 * time.Second).Format(time.RFC3339)
	if got.ExpiresAt != expectedExpiry {
		t.Fatalf("ExpiresAt = %q, want %q", got.ExpiresAt, expectedExpiry)
	}

	// File on disk reflects the refresh.
	persisted, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if persisted.AccessToken != "fresh" {
		t.Fatalf("disk access token = %q, want 'fresh'", persisted.AccessToken)
	}
	if persisted.ExpiresAt != expectedExpiry {
		t.Fatalf("disk expires_at = %q, want %q", persisted.ExpiresAt, expectedExpiry)
	}
}

func TestEnsureFreshRefreshesWhenTokenWithinSkew(t *testing.T) {
	now := time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC)
	// Expires in 30s — within the 60s proactiveSkew, must refresh.
	path := writeCreds(t, t.TempDir(), StoredCredentials{
		AccessToken:  "almost-stale",
		RefreshToken: "refresh",
		ExpiresAt:    now.Add(30 * time.Second).Format(time.RFC3339),
	})
	client := &fakeRefreshClient{}

	r := NewRefresher(path, client, func() time.Time { return now })
	got, err := r.EnsureFresh(context.Background())
	if err != nil {
		t.Fatalf("EnsureFresh: %v", err)
	}
	if got.AccessToken != "new-token" {
		t.Fatalf("expected refresh, got token %q", got.AccessToken)
	}
}

func TestRefreshNowAlwaysRefreshes(t *testing.T) {
	now := time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC)
	path := writeCreds(t, t.TempDir(), StoredCredentials{
		AccessToken:  "current",
		RefreshToken: "refresh",
		ExpiresAt:    now.Add(time.Hour).Format(time.RFC3339), // far from expiry
	})
	client := &fakeRefreshClient{}

	r := NewRefresher(path, client, func() time.Time { return now })
	if _, err := r.RefreshNow(context.Background()); err != nil {
		t.Fatalf("RefreshNow: %v", err)
	}
	if c := atomic.LoadInt32(&client.calls); c != 1 {
		t.Fatalf("expected exactly 1 refresh call, got %d", c)
	}
}

func TestEnsureFreshSingleFlight(t *testing.T) {
	now := time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC)
	path := writeCreds(t, t.TempDir(), StoredCredentials{
		AccessToken:  "stale",
		RefreshToken: "refresh",
		ExpiresAt:    now.Add(-time.Minute).Format(time.RFC3339),
	})
	gate := make(chan struct{})
	client := &fakeRefreshClient{
		fn: func(_ context.Context, _ string) (*RefreshResult, error) {
			<-gate // hold the first refresh until the rest are queued
			return &RefreshResult{AccessToken: "fresh", ExpiresInSeconds: 1800}, nil
		},
	}

	r := NewRefresher(path, client, func() time.Time { return now })

	const N = 10
	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			if _, err := r.EnsureFresh(context.Background()); err != nil {
				t.Errorf("EnsureFresh: %v", err)
			}
		}()
	}
	// Give the goroutines time to all queue up on the mutex, then release.
	time.Sleep(50 * time.Millisecond)
	close(gate)
	wg.Wait()

	// The first call refreshes; once the on-disk token is fresh, subsequent
	// callers must skip the refresh entirely.
	calls := atomic.LoadInt32(&client.calls)
	if calls != 1 {
		t.Fatalf("expected exactly 1 refresh call across %d goroutines, got %d", N, calls)
	}
}

func TestRefreshReturnsErrReLoginRequiredWhenServer401(t *testing.T) {
	now := time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC)
	path := writeCreds(t, t.TempDir(), StoredCredentials{
		AccessToken:  "stale",
		RefreshToken: "revoked",
		ExpiresAt:    now.Add(-time.Minute).Format(time.RFC3339),
	})
	client := &fakeRefreshClient{
		fn: func(_ context.Context, _ string) (*RefreshResult, error) {
			return nil, ErrReLoginRequired
		},
	}

	r := NewRefresher(path, client, func() time.Time { return now })
	_, err := r.EnsureFresh(context.Background())
	if !errors.Is(err, ErrReLoginRequired) {
		t.Fatalf("expected ErrReLoginRequired, got %v", err)
	}
}

func TestRefreshReturnsErrReLoginRequiredWhenNoRefreshToken(t *testing.T) {
	now := time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC)
	path := writeCreds(t, t.TempDir(), StoredCredentials{
		AccessToken: "stale",
		// RefreshToken intentionally empty — credentials file pre-dates V1.
		ExpiresAt: now.Add(-time.Minute).Format(time.RFC3339),
	})
	client := &fakeRefreshClient{}

	r := NewRefresher(path, client, func() time.Time { return now })
	_, err := r.EnsureFresh(context.Background())
	if !errors.Is(err, ErrReLoginRequired) {
		t.Fatalf("expected ErrReLoginRequired, got %v", err)
	}
	if c := atomic.LoadInt32(&client.calls); c != 0 {
		t.Fatalf("expected no HTTP call when refresh_token absent, got %d", c)
	}
}

func TestExpiresSoonHandlesMissingOrMalformedExpiry(t *testing.T) {
	r := NewRefresher("", &fakeRefreshClient{}, func() time.Time { return time.Now() })
	if !r.expiresSoon(StoredCredentials{ExpiresAt: ""}) {
		t.Fatalf("missing expires_at should trigger refresh")
	}
	if !r.expiresSoon(StoredCredentials{ExpiresAt: "not-a-timestamp"}) {
		t.Fatalf("malformed expires_at should trigger refresh")
	}
}
