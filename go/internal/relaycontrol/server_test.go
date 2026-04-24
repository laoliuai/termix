package relaycontrol

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5"
	relaycontrolv1 "github.com/termix/termix/go/gen/proto/relaycontrolv1"
	"github.com/termix/termix/go/internal/auth"
	"github.com/termix/termix/go/internal/control"
	"github.com/termix/termix/go/internal/persistence"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestParseAccessTokenForRelayControl(t *testing.T) {
	token, err := auth.IssueAccessToken("signing-key", "user-1", "device-1", 15*time.Minute)
	if err != nil {
		t.Fatalf("IssueAccessToken returned error: %v", err)
	}

	claims, err := auth.ParseAccessToken("signing-key", token)
	if err != nil {
		t.Fatalf("ParseAccessToken returned error: %v", err)
	}
	if claims.UserID != "user-1" {
		t.Fatalf("expected user-1, got %q", claims.UserID)
	}
	if claims.DeviceID != "device-1" {
		t.Fatalf("expected device-1, got %q", claims.DeviceID)
	}
}

func TestParseAccessTokenForRelayControlFailures(t *testing.T) {
	signingKey := "signing-key"
	now := time.Now().UTC()
	issuedAt := jwt.NewNumericDate(now.Add(-2 * time.Minute))
	expiresAt := jwt.NewNumericDate(now.Add(15 * time.Minute))

	tests := []struct {
		name           string
		tokenString    string
		expectedSubstr string
	}{
		{
			name:        "wrong signing key",
			tokenString: mustIssueToken(t, "different-key", jwt.SigningMethodHS256, "user-1", "device-1", issuedAt, expiresAt),
		},
		{
			name:        "invalid token string",
			tokenString: "not-a-jwt",
		},
		{
			name:           "expired token",
			tokenString:    mustIssueToken(t, signingKey, jwt.SigningMethodHS256, "user-1", "device-1", issuedAt, jwt.NewNumericDate(now.Add(-1*time.Minute))),
			expectedSubstr: "expired",
		},
		{
			name:           "empty required claims",
			tokenString:    mustIssueToken(t, signingKey, jwt.SigningMethodHS256, "", "device-1", issuedAt, expiresAt),
			expectedSubstr: "missing bearer claims",
		},
		{
			name:           "wrong signing method",
			tokenString:    mustIssueToken(t, signingKey, jwt.SigningMethodHS384, "user-1", "device-1", issuedAt, expiresAt),
			expectedSubstr: "unexpected signing method",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			_, err := auth.ParseAccessToken(signingKey, tc.tokenString)
			if err == nil {
				t.Fatal("expected ParseAccessToken to fail")
			}
			if tc.expectedSubstr != "" && !strings.Contains(err.Error(), tc.expectedSubstr) {
				t.Fatalf("expected error containing %q, got %q", tc.expectedSubstr, err.Error())
			}
		})
	}
}

func TestGRPCErrorMappingMatrix(t *testing.T) {
	tests := []struct {
		name           string
		err            error
		expectedReason string
		expectedCode   codes.Code
	}{
		{
			name:           "unauthorized",
			err:            control.ErrUnauthorized,
			expectedReason: "unauthorized",
			expectedCode:   codes.Unauthenticated,
		},
		{
			name:           "not found",
			err:            control.ErrNotFound,
			expectedReason: "not_found",
			expectedCode:   codes.NotFound,
		},
		{
			name:           "session not controllable",
			err:            control.ErrSessionNotControllable,
			expectedReason: "session_not_controllable",
			expectedCode:   codes.FailedPrecondition,
		},
		{
			name:           "already controlled",
			err:            control.ErrAlreadyControlled,
			expectedReason: "already_controlled",
			expectedCode:   codes.FailedPrecondition,
		},
		{
			name:           "stale lease",
			err:            control.ErrStaleLease,
			expectedReason: "stale_lease",
			expectedCode:   codes.FailedPrecondition,
		},
		{
			name:           "internal fallback",
			err:            errors.New("database unavailable"),
			expectedReason: "internal",
			expectedCode:   codes.Internal,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			reason, code := reasonAndCode(tc.err)
			if reason != tc.expectedReason {
				t.Fatalf("expected reason %q, got %q", tc.expectedReason, reason)
			}
			if code != tc.expectedCode {
				t.Fatalf("expected code %s, got %s", tc.expectedCode, code)
			}

			st, ok := status.FromError(grpcError(tc.err))
			if !ok {
				t.Fatalf("expected gRPC status error, got %T", tc.err)
			}
			if st.Code() != tc.expectedCode {
				t.Fatalf("expected status code %s, got %s", tc.expectedCode, st.Code())
			}
			if st.Message() != tc.expectedReason {
				t.Fatalf("expected status message %q, got %q", tc.expectedReason, st.Message())
			}
		})
	}
}

func TestServerAuthorizeWatchAndLeaseFlow(t *testing.T) {
	t.Parallel()

	const (
		signingKey = "relay-signing-key"
		userID     = "11111111-1111-1111-1111-111111111111"
		deviceID   = "22222222-2222-2222-2222-222222222222"
		sessionID  = "33333333-3333-3333-3333-333333333333"
	)

	now := time.Date(2026, 4, 24, 8, 0, 0, 0, time.UTC)
	repo := newFakeServerLeaseRepo()
	repo.addSession(sessionID, userID, "running")
	repo.addDevice(deviceID, userID)

	token, err := auth.IssueAccessToken(signingKey, userID, deviceID, 15*time.Minute)
	if err != nil {
		t.Fatalf("IssueAccessToken returned error: %v", err)
	}

	srv := NewServer(repo, signingKey, ServerConfig{
		LeaseTTL: 30 * time.Second,
		Now:      func() time.Time { return now },
	})

	watchResp, err := srv.AuthorizeSessionWatch(context.Background(), &relaycontrolv1.AuthorizeSessionWatchRequest{
		AccessToken: token,
		SessionId:   sessionID,
	})
	if err != nil {
		t.Fatalf("AuthorizeSessionWatch returned error: %v", err)
	}
	if watchResp.GetSessionId() != sessionID {
		t.Fatalf("expected session id %q, got %q", sessionID, watchResp.GetSessionId())
	}
	if watchResp.GetUserId() != userID {
		t.Fatalf("expected user id %q, got %q", userID, watchResp.GetUserId())
	}

	acquireResp, err := srv.AcquireControlLease(context.Background(), &relaycontrolv1.AcquireControlLeaseRequest{
		AccessToken: token,
		SessionId:   sessionID,
	})
	if err != nil {
		t.Fatalf("AcquireControlLease returned error: %v", err)
	}
	if acquireResp.GetLeaseVersion() != 1 {
		t.Fatalf("expected lease version 1, got %d", acquireResp.GetLeaseVersion())
	}
	if acquireResp.GetRenewAfterSeconds() != 15 {
		t.Fatalf("expected renew_after_seconds 15, got %d", acquireResp.GetRenewAfterSeconds())
	}

	renewResp, err := srv.RenewControlLease(context.Background(), &relaycontrolv1.RenewControlLeaseRequest{
		AccessToken:  token,
		SessionId:    sessionID,
		LeaseVersion: acquireResp.GetLeaseVersion(),
	})
	if err != nil {
		t.Fatalf("RenewControlLease returned error: %v", err)
	}
	if renewResp.GetLeaseVersion() != 2 {
		t.Fatalf("expected lease version 2, got %d", renewResp.GetLeaseVersion())
	}

	releaseResp, err := srv.ReleaseControlLease(context.Background(), &relaycontrolv1.ReleaseControlLeaseRequest{
		AccessToken:  token,
		SessionId:    sessionID,
		LeaseVersion: renewResp.GetLeaseVersion(),
	})
	if err != nil {
		t.Fatalf("ReleaseControlLease returned error: %v", err)
	}
	if !releaseResp.GetReleased() {
		t.Fatal("expected released=true")
	}
}

func TestServerDenialsAndDeferredMethods(t *testing.T) {
	t.Parallel()

	const (
		signingKey = "relay-signing-key"
		userID     = "11111111-1111-1111-1111-111111111111"
		deviceID   = "22222222-2222-2222-2222-222222222222"
		sessionID  = "33333333-3333-3333-3333-333333333333"
	)

	now := time.Date(2026, 4, 24, 8, 30, 0, 0, time.UTC)
	repo := newFakeServerLeaseRepo()
	repo.addSession(sessionID, userID, "running")
	repo.addDevice(deviceID, userID)
	srv := NewServer(repo, signingKey, ServerConfig{
		LeaseTTL: 30 * time.Second,
		Now:      func() time.Time { return now },
	})

	_, err := srv.AuthorizeSessionWatch(context.Background(), &relaycontrolv1.AuthorizeSessionWatchRequest{
		AccessToken: "not-a-jwt",
		SessionId:   sessionID,
	})
	assertStatusCode(t, err, codes.Unauthenticated)

	token, err := auth.IssueAccessToken(signingKey, userID, deviceID, 15*time.Minute)
	if err != nil {
		t.Fatalf("IssueAccessToken returned error: %v", err)
	}
	acquireResp, err := srv.AcquireControlLease(context.Background(), &relaycontrolv1.AcquireControlLeaseRequest{
		AccessToken: token,
		SessionId:   sessionID,
	})
	if err != nil {
		t.Fatalf("AcquireControlLease returned error: %v", err)
	}

	_, err = srv.RenewControlLease(context.Background(), &relaycontrolv1.RenewControlLeaseRequest{
		AccessToken:  token,
		SessionId:    sessionID,
		LeaseVersion: acquireResp.GetLeaseVersion() - 1,
	})
	assertStatusCode(t, err, codes.FailedPrecondition)

	_, err = srv.ValidateAccessToken(context.Background(), &relaycontrolv1.ValidateAccessTokenRequest{AccessToken: token})
	assertStatusCode(t, err, codes.Unimplemented)

	_, err = srv.MarkConnectionOpened(context.Background(), &relaycontrolv1.MarkConnectionOpenedRequest{
		AccessToken:  token,
		ConnectionId: "conn-1",
		Role:         "viewer",
		SessionId:    sessionID,
	})
	assertStatusCode(t, err, codes.Unimplemented)

	_, err = srv.MarkConnectionClosed(context.Background(), &relaycontrolv1.MarkConnectionClosedRequest{
		AccessToken:  token,
		ConnectionId: "conn-1",
	})
	assertStatusCode(t, err, codes.Unimplemented)
}

func assertStatusCode(t *testing.T, err error, expectedCode codes.Code) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected gRPC error with code %s", expectedCode)
	}
	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("expected status error, got %T", err)
	}
	if st.Code() != expectedCode {
		t.Fatalf("expected code %s, got %s", expectedCode, st.Code())
	}
}

type fakeServerLeaseRepo struct {
	sessions map[string]persistence.Session
	devices  map[string]persistence.Device
	leases   map[string]persistence.ControlLease
}

func newFakeServerLeaseRepo() *fakeServerLeaseRepo {
	return &fakeServerLeaseRepo{
		sessions: make(map[string]persistence.Session),
		devices:  make(map[string]persistence.Device),
		leases:   make(map[string]persistence.ControlLease),
	}
}

func (f *fakeServerLeaseRepo) addSession(id, userID, status string) {
	f.sessions[id] = persistence.Session{
		ID:           id,
		UserID:       userID,
		HostDeviceID: "44444444-4444-4444-4444-444444444444",
		Status:       status,
	}
}

func (f *fakeServerLeaseRepo) addDevice(id, userID string) {
	f.devices[id] = persistence.Device{
		ID:     id,
		UserID: userID,
	}
}

func (f *fakeServerLeaseRepo) GetSessionForUser(_ context.Context, sessionID, userID string) (persistence.Session, error) {
	session, ok := f.sessions[sessionID]
	if !ok || session.UserID != userID {
		return persistence.Session{}, pgx.ErrNoRows
	}
	return session, nil
}

func (f *fakeServerLeaseRepo) GetDeviceForUser(_ context.Context, deviceID, userID string) (persistence.Device, error) {
	device, ok := f.devices[deviceID]
	if !ok || device.UserID != userID {
		return persistence.Device{}, pgx.ErrNoRows
	}
	return device, nil
}

func (f *fakeServerLeaseRepo) GetActiveControlLease(_ context.Context, sessionID string, now time.Time) (persistence.ControlLease, bool, error) {
	lease, ok := f.leases[sessionID]
	if !ok {
		return persistence.ControlLease{}, false, nil
	}
	if !lease.ExpiresAt.After(now) {
		return persistence.ControlLease{}, false, nil
	}
	return lease, true, nil
}

func (f *fakeServerLeaseRepo) UpsertControlLease(_ context.Context, params persistence.UpsertControlLeaseParams) (persistence.ControlLease, error) {
	existing, ok := f.leases[params.SessionID]
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

func (f *fakeServerLeaseRepo) RenewControlLease(_ context.Context, params persistence.RenewControlLeaseParams) (persistence.ControlLease, error) {
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

func (f *fakeServerLeaseRepo) ReleaseControlLease(_ context.Context, params persistence.ReleaseControlLeaseParams) (persistence.ControlLease, error) {
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

func mustIssueToken(
	t *testing.T,
	signingKey string,
	signingMethod jwt.SigningMethod,
	userID string,
	deviceID string,
	issuedAt *jwt.NumericDate,
	expiresAt *jwt.NumericDate,
) string {
	t.Helper()
	token := jwt.NewWithClaims(signingMethod, auth.AccessClaims{
		UserID:   userID,
		DeviceID: deviceID,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			IssuedAt:  issuedAt,
			ExpiresAt: expiresAt,
		},
	})
	signed, err := token.SignedString([]byte(signingKey))
	if err != nil {
		t.Fatalf("SignedString returned error: %v", err)
	}
	return signed
}
