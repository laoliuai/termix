package tests

import (
	"testing"

	"github.com/termix/termix/go/internal/relay"
)

func TestScrubAccessTokenQuery(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"single param", "access_token=abc", "access_token=<redacted>"},
		{"with siblings", "x=1&access_token=abc&y=2", "x=1&access_token=<redacted>&y=2"},
		{"url-encoded value", "access_token=abc%2Bdef", "access_token=<redacted>"},
		{"empty value", "access_token=", "access_token=<redacted>"},
		{"no token", "foo=bar&baz=qux", "foo=bar&baz=qux"},
		{"empty string", "", ""},
		{"token at end", "x=1&access_token=eyJabc", "x=1&access_token=<redacted>"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := relay.ScrubAccessTokenQuery(c.in)
			if got != c.want {
				t.Errorf("ScrubAccessTokenQuery(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}
