package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"

	"github.com/termix/termix/go/internal/controlapi"
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
	if grpcAddr := os.Getenv("TERMIX_RELAY_CONTROL_GRPC_ADDR"); grpcAddr != "" {
		conn, err := grpc.DialContext(context.Background(), grpcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			return nil, func() {}, err
		}
		return relaycontrol.NewClient(conn), func() { _ = conn.Close() }, nil
	}

	controlURL := os.Getenv("TERMIX_CONTROL_API_URL")
	if controlURL == "" {
		return nil, func() {}, errors.New("TERMIX_RELAY_CONTROL_GRPC_ADDR or TERMIX_CONTROL_API_URL is required")
	}
	controlClient, err := controlapi.New(controlURL, http.DefaultTransport)
	if err != nil {
		return nil, func() {}, err
	}
	return relay.NewControlAuthorizer(controlClient), func() {}, nil
}
