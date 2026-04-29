package controlapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestShouldUseSecureRefreshCookie(t *testing.T) {
	tests := []struct {
		name string
		req  *http.Request
		want bool
	}{
		{
			name: "plain HTTP LAN dev",
			req:  httptest.NewRequest(http.MethodPost, "http://192.168.0.95/api/v1/auth/login", nil),
			want: false,
		},
		{
			name: "direct HTTPS",
			req:  httptest.NewRequest(http.MethodPost, "https://termix.example.com/api/v1/auth/login", nil),
			want: true,
		},
		{
			name: "HTTPS reverse proxy header",
			req: func() *http.Request {
				req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/api/v1/auth/login", nil)
				req.Header.Set("X-Forwarded-Proto", "https")
				return req
			}(),
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldUseSecureRefreshCookie(tt.req); got != tt.want {
				t.Fatalf("shouldUseSecureRefreshCookie() = %v, want %v", got, tt.want)
			}
		})
	}
}
