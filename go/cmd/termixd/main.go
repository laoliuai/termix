package main

import (
	"context"
	"log"

	"github.com/termix/termix/go/internal/config"
	"github.com/termix/termix/go/internal/hostdaemon"
)

func main() {
	if err := hostdaemon.Run(context.Background(), config.DefaultHostPaths()); err != nil {
		log.Fatal(err)
	}
}
