package hostdaemon

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	daemonv1 "github.com/termix/termix/go/gen/proto/daemonv1"
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

func TestRunStopsDaemonIPCWithActiveBlockingRPC(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen returned error: %v", err)
	}
	server := grpc.NewServer()

	rpcStarted := make(chan struct{})
	releaseRPC := make(chan struct{})
	daemonv1.RegisterDaemonServiceServer(server, &blockingDaemonService{
		rpcStarted: rpcStarted,
		releaseRPC: releaseRPC,
	})

	errCh := make(chan error, 1)
	go func() {
		errCh <- serveDaemonIPC(ctx, server, listener)
	}()

	conn, err := grpc.DialContext(context.Background(), listener.Addr().String(), grpc.WithInsecure(), grpc.WithBlock())
	if err != nil {
		t.Fatalf("DialContext returned error: %v", err)
	}
	defer conn.Close()

	rpcErrCh := make(chan error, 1)
	go func() {
		_, err := daemonv1.NewDaemonServiceClient(conn).Health(context.Background(), &daemonv1.HealthRequest{})
		rpcErrCh <- err
	}()

	<-rpcStarted
	cancel()

	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
	case <-time.After(time.Second):
		close(releaseRPC)
		t.Fatal("serveDaemonIPC did not return while an RPC was active")
	}
	close(releaseRPC)
}

func TestRunReaperStopsWhenContextIsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	reapStarted := make(chan struct{})
	releaseReap := make(chan struct{})
	var calls atomic.Int32

	done := runReaper(ctx, time.Millisecond, time.Second, func(context.Context) error {
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

func TestRunReaperCancelsInFlightReapContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	reapStarted := make(chan struct{})

	done := runReaper(ctx, time.Hour, time.Hour, func(ctx context.Context) error {
		select {
		case reapStarted <- struct{}{}:
		default:
		}
		<-ctx.Done()
		return ctx.Err()
	})

	<-reapStarted
	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("runReaper did not stop after canceling an in-flight reap context")
	}
}

type blockingDaemonService struct {
	daemonv1.UnimplementedDaemonServiceServer
	rpcStarted chan<- struct{}
	releaseRPC <-chan struct{}
}

func (s *blockingDaemonService) Health(context.Context, *daemonv1.HealthRequest) (*daemonv1.HealthResponse, error) {
	select {
	case s.rpcStarted <- struct{}{}:
	default:
	}
	<-s.releaseRPC
	return &daemonv1.HealthResponse{Status: "ok"}, nil
}

func TestShutdownRPCCancelsServeDaemonIPC(t *testing.T) {
	// Mirror Run's wiring: a parent ctx, a cancel func passed to a fake
	// DaemonServiceServer's Shutdown handler, and serveDaemonIPC running
	// the gRPC server. When the client calls Shutdown, the handler
	// triggers cancel; serveDaemonIPC must return context.Canceled.

	parent, cancelParent := context.WithCancel(context.Background())
	defer cancelParent()
	runCtx, cancelRun := context.WithCancel(parent)
	defer cancelRun()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	server := grpc.NewServer()
	daemonv1.RegisterDaemonServiceServer(server, &shutdownTriggerService{
		shutdown: func() {
			// Match Manager.Shutdown's behaviour: a brief grace period
			// before cancelling so the unary response can flush.
			go func() {
				time.Sleep(10 * time.Millisecond)
				cancelRun()
			}()
		},
	})

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- serveDaemonIPC(runCtx, server, listener)
	}()

	conn, err := grpc.DialContext(context.Background(), listener.Addr().String(),
		grpc.WithInsecure(), grpc.WithBlock())
	if err != nil {
		t.Fatalf("DialContext: %v", err)
	}
	defer conn.Close()

	if _, err := daemonv1.NewDaemonServiceClient(conn).Shutdown(
		context.Background(), &daemonv1.ShutdownRequest{}); err != nil {
		// The conn may close mid-flight after the cancel fires; that is
		// acceptable. Only fail on hard transport errors that indicate
		// the RPC never reached the handler.
		t.Logf("Shutdown RPC returned err (acceptable if conn closed): %v", err)
	}

	select {
	case err := <-serveErr:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("serveDaemonIPC err=%v want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("serveDaemonIPC did not return after Shutdown RPC")
	}
}

type shutdownTriggerService struct {
	daemonv1.UnimplementedDaemonServiceServer
	shutdown func()
}

func (s *shutdownTriggerService) Shutdown(context.Context, *daemonv1.ShutdownRequest) (*daemonv1.ShutdownResponse, error) {
	if s.shutdown != nil {
		s.shutdown()
	}
	return &daemonv1.ShutdownResponse{}, nil
}
