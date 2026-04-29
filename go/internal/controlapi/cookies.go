package controlapi

import (
	"net/http"
	"strings"
)

func shouldUseSecureRefreshCookie(req *http.Request) bool {
	if req == nil {
		return true
	}
	if req.TLS != nil || req.URL.Scheme == "https" {
		return true
	}
	proto := req.Header.Get("X-Forwarded-Proto")
	if proto == "" {
		return false
	}
	firstProto := strings.TrimSpace(strings.Split(proto, ",")[0])
	return strings.EqualFold(firstProto, "https")
}
