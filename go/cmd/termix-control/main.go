package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	relaycontrolv1 "github.com/termix/termix/go/gen/proto/relaycontrolv1"
	"github.com/termix/termix/go/internal/auth"
	"github.com/termix/termix/go/internal/controlapi"
	"github.com/termix/termix/go/internal/persistence"
	relaycontrol "github.com/termix/termix/go/internal/relaycontrol"
	"google.golang.org/grpc"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "create-user" {
		runCreateUser(os.Args[2:])
		return
	}
	runServer()
}

func mustPool(ctx context.Context) *pgxpool.Pool {
	dsn := os.Getenv("TERMIX_POSTGRES_DSN")
	if dsn == "" {
		log.Fatal("TERMIX_POSTGRES_DSN is required")
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		log.Fatal(err)
	}
	return pool
}

func runServer() {
	ctx := context.Background()

	signingKey := os.Getenv("TERMIX_JWT_SIGNING_KEY")
	if signingKey == "" {
		log.Fatal("TERMIX_JWT_SIGNING_KEY is required")
	}

	pool := mustPool(ctx)
	defer pool.Close()

	store := persistence.New(pool)

	if email, password := os.Getenv("TERMIX_ADMIN_EMAIL"), os.Getenv("TERMIX_ADMIN_PASSWORD"); email != "" && password != "" {
		seedAdmin(ctx, store, email, password)
	}

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

func seedAdmin(ctx context.Context, store *persistence.Store, email, password string) {
	if _, err := store.GetUserByEmail(ctx, email); err == nil {
		return // already exists
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		log.Fatalf("seed admin: hash password: %v", err)
	}
	name := email[:strings.IndexByte(email, '@')]
	if _, err := store.CreateUser(ctx, email, name, hash, "admin"); err != nil {
		log.Fatalf("seed admin: create user: %v", err)
	}
	log.Printf("created admin user: %s", email)
}

func runCreateUser(args []string) {
	fs := flag.NewFlagSet("create-user", flag.ExitOnError)
	email := fs.String("email", "", "user email (required)")
	password := fs.String("password", "", "plaintext password (required)")
	role := fs.String("role", "user", "role: admin or user")
	name := fs.String("name", "", "display name (defaults to local part of email)")
	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}
	if *email == "" || *password == "" {
		fmt.Fprintln(os.Stderr, "usage: termix-control create-user --email EMAIL --password PASSWORD [--role user|admin] [--name NAME]")
		os.Exit(1)
	}
	if *role != "admin" && *role != "user" {
		fmt.Fprintf(os.Stderr, "invalid role %q: must be admin or user\n", *role)
		os.Exit(1)
	}
	displayName := *name
	if displayName == "" {
		displayName = (*email)[:strings.IndexByte(*email, '@')]
	}

	ctx := context.Background()
	pool := mustPool(ctx)
	defer pool.Close()
	store := persistence.New(pool)

	hash, err := auth.HashPassword(*password)
	if err != nil {
		log.Fatalf("hash password: %v", err)
	}
	user, err := store.CreateUser(ctx, *email, displayName, hash, *role)
	if err != nil {
		log.Fatalf("create user: %v", err)
	}
	fmt.Printf("created user: id=%s email=%s role=%s\n", user.ID, user.Email, user.Role)
}
