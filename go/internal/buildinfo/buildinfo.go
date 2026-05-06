// Package buildinfo exposes a build identity (version + VCS revision +
// dirty flag) shared by the termix CLI and daemon. It is consumed by the
// version handshake in cmd/termix to detect daemons running stale code.
package buildinfo

import (
	"runtime/debug"
)

// Identity is the build-identity tuple compared during the daemon
// handshake. Two identities match when all three fields are equal and
// both have a non-empty Version. Modified participates in the equality
// (clean never matches dirty) so a clean rebuild over a dirty daemon
// still triggers a respawn, while two binaries built from the same
// dirty tree are treated as the same identity.
type Identity struct {
	Version  string
	Revision string // short VCS revision, at most 12 hex chars; empty when unavailable
	Modified bool   // true when the working tree had uncommitted changes at build time
}

// Current returns the running binary's identity. The version string comes
// from the caller (typically cmd/termix's `var version`), so this package
// stays free of import cycles. Revision and Modified are read from
// runtime/debug.ReadBuildInfo; both are zero-valued when build info is
// unavailable (e.g. `go run`, stripped binary).
func Current(version string) Identity {
	id := Identity{Version: version}
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return id
	}
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			id.Revision = truncRevision(s.Value)
		case "vcs.modified":
			id.Modified = s.Value == "true"
		}
	}
	return id
}

// Matches reports whether two identities describe the same build.
// Both must have a non-empty Version (an empty Version indicates an
// unknown/legacy identity and never matches anything, so an old daemon
// returning empty handshake fields always forces a respawn). All three
// fields — Version, Revision, and Modified — must be equal. Note: when
// both sides are dirty at the same commit, Revision alone cannot prove
// the source is identical (Go's vcs.revision is the last commit hash,
// not a hash of the working tree), so a dev-loop rebuild that only
// touches uncommitted code will still be reused. Callers that need a
// stronger guarantee should kill the daemon explicitly.
func (a Identity) Matches(b Identity) bool {
	if a.Version == "" || b.Version == "" {
		return false
	}
	return a.Version == b.Version && a.Revision == b.Revision && a.Modified == b.Modified
}

// String returns a short human-readable form for log lines:
//
//	"v1.2.3@abc123def456"          (clean)
//	"v1.2.3@abc123def456-dirty"    (modified)
//	"dev@-"                        (no VCS info)
func (a Identity) String() string {
	rev := a.Revision
	if rev == "" {
		rev = "-"
	}
	s := a.Version + "@" + rev
	if a.Modified {
		s += "-dirty"
	}
	return s
}

func truncRevision(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}
