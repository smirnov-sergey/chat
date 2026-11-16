package app

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"time"

	"chat/internal/config"
	"chat/internal/handlers"
	"chat/internal/password"
	"chat/internal/postgres"
	"chat/internal/repository"
	"chat/internal/router"
	"chat/internal/service"
)

const sessionLifetime = 7 * 24 * time.Hour

func Run(ctx context.Context, cfg config.Config) error {
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

	userRepository := repository.NewUserRepository(db)
	userRegistrationStore := repository.NewUserRegistrationStore(db)
	sessionService, err := service.NewSessionService(repository.NewSessionRepository(db), sessionLifetime)
	if err != nil {
		return fmt.Errorf("create session service: %w", err)
	}

	userService, err := service.NewUserService(userRepository, userRegistrationStore, password.Argon2id{}, sessionService, sessionLifetime)
	if err != nil {
		return fmt.Errorf("create user service: %w", err)
	}

	userRegistrationHandler := handlers.NewUserRegistrationHandler(userService, cfg.SessionCookieSecure)
	server.Handler = router.New(userRegistrationHandler)

	log.Print("PostgreSQL connected")
	log.Printf("HTTP server listening on %s", listener.Addr())

	if err := serve(ctx, server, listener, 10*time.Second); err != nil {
		return err
	}

	log.Print("HTTP server stopped")

	return nil
}
