package relayclient

import (
	"context"
	"errors"
	"testing"

	"github.com/termix/termix/go/internal/credentials"
)

func TestSupervisorInitialStateIsConnecting(t *testing.T) {
	sup := NewSupervisor(SupervisorOptions{
		Factory:   func(context.Context, credentials.StoredCredentials) (*Client, error) { return nil, errors.New("unused") },
		Refresher: stubRefresher{},
		Clock:     RealClock(),
		Rand:      func() float64 { return 0.5 },
	})
	got := sup.State()
	if got.Phase != PhaseConnecting {
		t.Fatalf("Phase=%q want %q", got.Phase, PhaseConnecting)
	}
	if got.Attempt != 0 {
		t.Fatalf("Attempt=%d want 0", got.Attempt)
	}
}

func TestSupervisorPublishOutputReturnsErrNotConnectedWhenIdle(t *testing.T) {
	sup := NewSupervisor(SupervisorOptions{
		Factory:   func(context.Context, credentials.StoredCredentials) (*Client, error) { return nil, errors.New("unused") },
		Refresher: stubRefresher{},
		Clock:     RealClock(),
		Rand:      func() float64 { return 0.5 },
	})
	err := sup.PublishOutput(context.Background(), "sess", []byte("hi"))
	if !errors.Is(err, ErrNotConnected) {
		t.Fatalf("err=%v want %v", err, ErrNotConnected)
	}
}

type stubRefresher struct{}

func (stubRefresher) EnsureFresh(context.Context) (credentials.StoredCredentials, error) {
	return credentials.StoredCredentials{}, nil
}

func (stubRefresher) RefreshNow(context.Context) (credentials.StoredCredentials, error) {
	return credentials.StoredCredentials{}, nil
}
