package main

import (
	"log"
	"net/http"
	"os"

	"github.com/termix/termix/go/internal/controlapi"
	"github.com/termix/termix/go/internal/relay"
)

func main() {
	controlURL := os.Getenv("TERMIX_CONTROL_API_URL")
	if controlURL == "" {
		log.Fatal("TERMIX_CONTROL_API_URL is required")
	}

	controlClient, err := controlapi.New(controlURL, http.DefaultTransport)
	if err != nil {
		log.Fatal(err)
	}

	addr := os.Getenv("TERMIX_RELAY_LISTEN_ADDR")
	if addr == "" {
		addr = ":8090"
	}
	server := relay.NewServer(relay.NewControlAuthorizer(controlClient))
	log.Fatal(http.ListenAndServe(addr, server.Handler()))
}
