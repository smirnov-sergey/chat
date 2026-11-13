package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"chat/internal/config"
	"chat/internal/migrate"
	"chat/internal/postgres"

	"github.com/joho/godotenv"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) != 1 || args[0] != "up" {
		return errors.New("usage: go run ./cmd/migrate up")
	}

	if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("load .env: %w", err)
	}

	cfg := config.Load()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()

	connectCtx, cancelConnect := context.WithTimeout(ctx, 5*time.Second)
	db, err := postgres.Open(connectCtx, cfg.Postgres.ConnectionString())
	cancelConnect()
	if err != nil {
		return err
	}
	defer db.Close()

	applied, err := migrate.Up(ctx, db, os.DirFS("migrations"))
	if err != nil {
		return err
	}

	log.Printf("migrations complete: %d applied", applied)

	return nil
}
