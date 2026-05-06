package buildinfo

import (
	"strings"
	"testing"
)

func TestMatches(t *testing.T) {
	cases := []struct {
		name string
		a, b Identity
		want bool
	}{
		{"identical clean", Identity{"v1", "abc123def456", false}, Identity{"v1", "abc123def456", false}, true},
		{"identical but a dirty", Identity{"v1", "abc123def456", true}, Identity{"v1", "abc123def456", false}, false},
		{"identical but b dirty", Identity{"v1", "abc123def456", false}, Identity{"v1", "abc123def456", true}, false},
		{"both dirty same fields", Identity{"v1", "abc123def456", true}, Identity{"v1", "abc123def456", true}, true},
		{"both dirty version differs", Identity{"v1", "abc123def456", true}, Identity{"v2", "abc123def456", true}, false},
		{"both dirty revision differs", Identity{"v1", "abc123def456", true}, Identity{"v1", "999999999999", true}, false},
		{"version differs", Identity{"v1", "abc123def456", false}, Identity{"v2", "abc123def456", false}, false},
		{"revision differs", Identity{"v1", "abc123def456", false}, Identity{"v1", "999999999999", false}, false},
		{"both zero value", Identity{}, Identity{}, false},
		{"empty version non-empty revision", Identity{"", "abc123def456", false}, Identity{"", "abc123def456", false}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.a.Matches(tc.b); got != tc.want {
				t.Fatalf("Matches=%v want %v", got, tc.want)
			}
		})
	}
}

func TestCurrentReturnsCallerVersion(t *testing.T) {
	id := Current("v9.9.9-test")
	if id.Version != "v9.9.9-test" {
		t.Fatalf("Version=%q want v9.9.9-test", id.Version)
	}
}

func TestCurrentRevisionIsAtMostTwelveChars(t *testing.T) {
	id := Current("dev")
	if len(id.Revision) > 12 {
		t.Fatalf("Revision=%q too long", id.Revision)
	}
}

func TestStringIncludesVersionAndRevision(t *testing.T) {
	s := Identity{"v1.2.3", "abc123def456", false}.String()
	if !strings.Contains(s, "v1.2.3") || !strings.Contains(s, "abc123def456") {
		t.Fatalf("String=%q missing fields", s)
	}
	if strings.Contains(s, "dirty") {
		t.Fatalf("String=%q must not contain 'dirty' for clean build", s)
	}
	dirty := Identity{"v1.2.3", "abc123def456", true}.String()
	if !strings.Contains(dirty, "dirty") {
		t.Fatalf("String=%q must contain 'dirty' for modified build", dirty)
	}
}

func TestStringEmptyRevision(t *testing.T) {
	s := Identity{"dev", "", false}.String()
	if s == "" {
		t.Fatalf("String=%q must not be empty", s)
	}
}
