package relaycontrol

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/termix/termix/go/internal/auth"
	"github.com/termix/termix/go/internal/control"
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
