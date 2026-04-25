package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"

	"github.com/termix/termix/go/internal/relay"
	relaycontrol "github.com/termix/termix/go/internal/relaycontrol"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	authorizer, cleanup, err := buildAuthorizer()
	if err != nil {
		log.Fatal(err)
	}
	defer cleanup()

	addr := os.Getenv("TERMIX_RELAY_LISTEN_ADDR")
	if addr == "" {
		addr = ":8090"
	}
	server := relay.NewServer(authorizer)
	log.Fatal(http.ListenAndServe(addr, server.Handler()))
}

func buildAuthorizer() (relay.SessionAuthorizer, func(), error) {
	grpcAddr := os.Getenv("TERMIX_RELAY_CONTROL_GRPC_ADDR")
	if grpcAddr == "" {
		return nil, func() {}, errors.New("TERMIX_RELAY_CONTROL_GRPC_ADDR is required")
	}

	conn, err := grpc.DialContext(context.Background(), grpcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, func() {}, err
	}
	return relaycontrol.NewClient(conn), func() { _ = conn.Close() }, nil
}
