package credentials

import (
	"context"
	"errors"
	"sync"
	"time"
)

// ErrReLoginRequired is returned when the refresh token itself is rejected
// (expired or revoked). Callers should surface "session expired, run
// `termix login` again" rather than a raw 401.
var ErrReLoginRequired = errors.New("refresh token rejected; run `termix login` again")

// proactiveSkew: refresh if the access token expires within this window.
const proactiveSkew = 60 * time.Second

// RefreshResult is the minimum subset of /auth/refresh that the Refresher
// cares about. Implemented by an adapter over controlapi.Client so the
// credentials package stays free of HTTP / OpenAPI imports.
type RefreshResult struct {
	AccessToken      string
	ExpiresInSeconds int
}

// RefreshClient is the dependency the Refresher needs from the control API.
// Implementations must translate a control 401 (refresh token rejected) into
// ErrReLoginRequired so the Refresher can distinguish "transport failure,
// retry later" from "must re-login".
type RefreshClient interface {
	RefreshAccessToken(ctx context.Context, refreshToken string) (*RefreshResult, error)
}

// Refresher reads and writes credentials.json transparently, swapping out an
// expired access token for a fresh one via the long-lived refresh token. All
// methods are safe for concurrent use; concurrent callers coalesce to a
// single in-flight refresh.
type Refresher struct {
	path   string
	client RefreshClient
	now    func() time.Time
	mu     sync.Mutex
}

// NewRefresher returns a Refresher backed by the credentials file at path.
// now defaults to time.Now when nil.
func NewRefresher(path string, client RefreshClient, now func() time.Time) *Refresher {
	if now == nil {
		now = time.Now
	}
	return &Refresher{path: path, client: client, now: now}
}

// EnsureFresh returns credentials whose access token is valid for at least
// proactiveSkew. If the on-disk token is expired or expiring soon, it is
// refreshed and the file is rewritten before EnsureFresh returns.
func (r *Refresher) EnsureFresh(ctx context.Context) (StoredCredentials, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	creds, err := Load(r.path)
	if err != nil {
		return StoredCredentials{}, err
	}
	if !r.expiresSoon(creds) {
		return creds, nil
	}
	return r.refreshLocked(ctx, creds)
}

// RefreshNow forces a refresh regardless of the current expires_at. Use this
// when a downstream call returned 401 even though the token looked fresh
// (e.g. clock skew, server-side revocation).
func (r *Refresher) RefreshNow(ctx context.Context) (StoredCredentials, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	creds, err := Load(r.path)
	if err != nil {
		return StoredCredentials{}, err
	}
	return r.refreshLocked(ctx, creds)
}

func (r *Refresher) expiresSoon(creds StoredCredentials) bool {
	if creds.ExpiresAt == "" {
		return true
	}
	t, err := time.Parse(time.RFC3339, creds.ExpiresAt)
	if err != nil {
		return true
	}
	return t.Sub(r.now()) <= proactiveSkew
}

func (r *Refresher) refreshLocked(ctx context.Context, creds StoredCredentials) (StoredCredentials, error) {
	if creds.RefreshToken == "" {
		return StoredCredentials{}, ErrReLoginRequired
	}
	res, err := r.client.RefreshAccessToken(ctx, creds.RefreshToken)
	if err != nil {
		return StoredCredentials{}, err
	}
	creds.AccessToken = res.AccessToken
	creds.ExpiresAt = r.now().UTC().Add(time.Duration(res.ExpiresInSeconds) * time.Second).Format(time.RFC3339)
	if err := Save(r.path, creds); err != nil {
		return StoredCredentials{}, err
	}
	return creds, nil
}
