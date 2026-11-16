package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"chat/internal/config"
	"chat/internal/handlers"
	"chat/internal/password"
	"chat/internal/postgres"
	"chat/internal/repository"
	"chat/internal/router"
	"chat/internal/service"

	"github.com/joho/godotenv"
)

func main() {
	if err := run(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

func run() error {
	if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("load .env: %w", err)
	}
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	defer listener.Close()

	connectCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	db, err := postgres.Open(connectCtx, cfg.Postgres.ConnectionString())
	cancel()
	if err != nil {
		return err
	}
	defer db.Close()

	userRegistrationService, err := service.NewUserRegistrationService(repository.NewUserRegistrationStore(db), password.Argon2id{}, 7*24*time.Hour)
	if err != nil {
		return fmt.Errorf("create user registration service: %w", err)
	}

	server.Handler = router.New(handlers.NewUserRegistrationHandler(userRegistrationService, cfg.SessionCookieSecure))

	log.Print("PostgreSQL connected")

	log.Printf("HTTP server listening on %s", listener.Addr())

	if err := serve(ctx, server, listener, 10*time.Second); err != nil {
		return err
	}

	log.Print("HTTP server stopped")
	return nil
}
