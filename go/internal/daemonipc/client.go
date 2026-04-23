package daemonipc

import (
	"context"
	"net"

	daemonv1 "github.com/termix/termix/go/gen/proto/daemonv1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func Dial(ctx context.Context, socketPath string) (daemonv1.DaemonServiceClient, *grpc.ClientConn, error) {
	conn, err := grpc.DialContext(
		ctx,
		socketPath,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", addr)
		}),
	)
	if err != nil {
		return nil, nil, err
	}
	return daemonv1.NewDaemonServiceClient(conn), conn, nil
}
