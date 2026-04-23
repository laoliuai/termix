package daemonipc

import (
	"errors"
	"net"
	"os"
	"syscall"
	"time"

	daemonv1 "github.com/termix/termix/go/gen/proto/daemonv1"
	"google.golang.org/grpc"
)

func NewServer(impl daemonv1.DaemonServiceServer) *grpc.Server {
	server := grpc.NewServer()
	daemonv1.RegisterDaemonServiceServer(server, impl)
	return server
}

func Listen(socketPath string) (net.Listener, error) {
	info, err := os.Stat(socketPath)
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return nil, err
	case info.Mode()&os.ModeSocket == 0:
		return nil, errors.New("existing daemon socket path is not a Unix socket")
	default:
		conn, dialErr := net.DialTimeout("unix", socketPath, 200*time.Millisecond)
		switch {
		case dialErr == nil:
			_ = conn.Close()
			return nil, errors.New("daemon socket is already in use")
		case isStaleUnixSocket(dialErr):
		default:
			return nil, dialErr
		}

		if err := os.Remove(socketPath); err != nil {
			return nil, err
		}
	}

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(socketPath, 0o600); err != nil {
		_ = listener.Close()
		return nil, err
	}
	return listener, nil
}

func isStaleUnixSocket(err error) bool {
	return errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.ENOENT)
}
