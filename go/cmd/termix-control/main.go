package main

import (
	"context"
	"log"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/termix/termix/go/internal/controlapi"
	"github.com/termix/termix/go/internal/persistence"
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
	router := controlapi.NewRouter(store, signingKey)
	if err := router.Run(":8080"); err != nil {
		log.Fatal(err)
	}
}
