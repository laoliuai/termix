package relayclient

import (
	"context"
	"testing"
	"time"
)

func TestPhaseConstantsAreNonEmptyAndDistinct(t *testing.T) {
	all := []Phase{PhaseConnecting, PhaseConnected, PhaseReconnecting, PhaseClosed}
	seen := map[Phase]bool{}
	for _, p := range all {
		if p == "" {
			t.Errorf("phase %q is empty", p)
		}
		if seen[p] {
			t.Errorf("phase %q is duplicated", p)
		}
		seen[p] = true
	}
}

func TestRelayStateZeroValueIsValid(t *testing.T) {
	var s RelayState
	if s.Phase != "" || s.Attempt != 0 || !s.LastConnectedAt.IsZero() {
		t.Errorf("zero value not zero: %+v", s)
	}
	_ = s.LastError
	_ = s.NextRetryAt
	_ = s.AuthFailures
}

func TestRealClockSleepReturnsOnContextCancel(t *testing.T) {
	c := realClock{}
	ctx, cancel := contextWithCancel(t)
	go func() { time.Sleep(50 * time.Millisecond); cancel() }()
	start := time.Now()
	if err := c.Sleep(ctx, 5*time.Second); err == nil {
		t.Fatalf("expected cancellation error, got nil")
	}
	if time.Since(start) > time.Second {
		t.Fatalf("Sleep did not honor cancel within 1s")
	}
}

func contextWithCancel(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithCancel(context.Background())
}
