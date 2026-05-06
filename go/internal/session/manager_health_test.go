package session

import (
	"context"
	"testing"

	daemonv1 "github.com/termix/termix/go/gen/proto/daemonv1"
)

func TestManagerHealthReturnsIdentity(t *testing.T) {
	m := NewManager(ManagerOptions{
		Version: "v1.2.3-test",
	})
	resp, err := m.Health(context.Background(), &daemonv1.HealthRequest{})
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	if resp.GetStatus() != "ok" {
		t.Fatalf("Status=%q want ok", resp.GetStatus())
	}
	if resp.GetVersion() != "v1.2.3-test" {
		t.Fatalf("Version=%q want v1.2.3-test", resp.GetVersion())
	}
	// Revision/Modified come from runtime/debug; we only assert Version
	// here because the test binary's VCS state is environment-dependent.
}
