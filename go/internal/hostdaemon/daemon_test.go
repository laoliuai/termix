package hostdaemon

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"
)

func TestRunStopsDaemonIPCWhenContextIsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen returned error: %v", err)
	}
	server := grpc.NewServer()

	errCh := make(chan error, 1)
	go func() {
		errCh <- serveDaemonIPC(ctx, server, listener)
	}()

	cancel()

	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("serveDaemonIPC did not return after context cancellation")
	}
}

func TestRunReaperStopsWhenContextIsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	reapStarted := make(chan struct{})
	releaseReap := make(chan struct{})
	var calls atomic.Int32

	done := runReaper(ctx, time.Millisecond, func(context.Context) error {
		calls.Add(1)
		select {
		case reapStarted <- struct{}{}:
		default:
		}
		<-releaseReap
		return nil
	})

	<-reapStarted
	cancel()
	close(releaseReap)

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("runReaper did not stop after context cancellation")
	}
	if calls.Load() != 1 {
		t.Fatalf("expected no periodic reap after cancellation, got %d calls", calls.Load())
	}
}
