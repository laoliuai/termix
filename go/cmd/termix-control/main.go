package main

import (
	"context"
	"log"
	"net"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
	relaycontrolv1 "github.com/termix/termix/go/gen/proto/relaycontrolv1"
	"github.com/termix/termix/go/internal/controlapi"
	"github.com/termix/termix/go/internal/persistence"
	relaycontrol "github.com/termix/termix/go/internal/relaycontrol"
	"google.golang.org/grpc"
)

func main() {
	dsn := os.Getenv("TERMIX_POSTGRES_DSN")
	if dsn == "" {
		log.Fatal("TERMIX_POSTGRES_DSN is required")
	}

	signingKey := os.Getenv("TERMIX_JWT_SIGNING_KEY")
	if signingKey == "" {
		log.Fatal("TERMIX_JWT_SIGNING_KEY is required")
	}

	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()

	store := persistence.New(pool)
	if grpcAddr := os.Getenv("TERMIX_CONTROL_RELAY_GRPC_ADDR"); grpcAddr != "" {
		listener, err := net.Listen("tcp", grpcAddr)
		if err != nil {
			log.Fatal(err)
		}
		grpcServer := grpc.NewServer()
		relaycontrolv1.RegisterRelayControlServiceServer(grpcServer, relaycontrol.NewServer(store, signingKey, relaycontrol.ServerConfig{}))
		go func() {
			log.Printf("relay-control gRPC listening on %s", grpcAddr)
			if err := grpcServer.Serve(listener); err != nil {
				log.Fatal(err)
			}
		}()
		defer grpcServer.GracefulStop()
	}

	restAddr := os.Getenv("TERMIX_CONTROL_REST_ADDR")
	if restAddr == "" {
		restAddr = ":8080"
	}
	router := controlapi.NewRouter(store, signingKey)
	if err := router.Run(restAddr); err != nil {
		log.Fatal(err)
	}
}
