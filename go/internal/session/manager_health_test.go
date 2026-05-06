package session

import (
	"context"
	"testing"
	"time"

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

func TestManagerShutdownTriggersRequestShutdown(t *testing.T) {
	called := make(chan struct{}, 1)
	m := NewManager(ManagerOptions{
		Version: "v1",
		RequestShutdown: func() {
			called <- struct{}{}
		},
	})

	resp, err := m.Shutdown(context.Background(), &daemonv1.ShutdownRequest{})
	if err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if resp == nil {
		t.Fatalf("Shutdown returned nil response")
	}

	select {
	case <-called:
	case <-time.After(time.Second):
		t.Fatalf("RequestShutdown was not invoked within 1s of Shutdown returning")
	}
}

func TestManagerShutdownIsSafeWhenRequestShutdownNil(t *testing.T) {
	m := NewManager(ManagerOptions{Version: "v1"}) // RequestShutdown nil
	if _, err := m.Shutdown(context.Background(), &daemonv1.ShutdownRequest{}); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
}
