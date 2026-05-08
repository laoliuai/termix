package relayclient

import (
	"context"
	"time"
)

// Clock abstracts the supervisor's only time-related operations so tests
// can drive the reconnect loop deterministically without real sleeps.
type Clock interface {
	Now() time.Time
	// Sleep blocks for d or until ctx is canceled. Returns ctx.Err() on
	// cancellation, nil otherwise.
	Sleep(ctx context.Context, d time.Duration) error
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

func (realClock) Sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// RealClock returns the production clock implementation.
func RealClock() Clock { return realClock{} }
