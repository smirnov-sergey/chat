package app_test

import (
	"context"
	"errors"
	"net"
	"testing"

	"chat/internal/app"
	"chat/internal/config"
)

func TestRunRejectsOccupiedAddress(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	cfg := config.Default()
	cfg.HTTPAddr = listener.Addr().String()
	err = app.Run(t.Context(), cfg)
	var networkError *net.OpError
	if !errors.As(err, &networkError) || networkError.Op != "listen" {
		t.Fatalf("app.Run error = %v, want a wrapped listen error", err)
	}
}

func TestRunReleasesListenerAfterCanceledConnection(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.HTTPAddr = address
	cfg.Postgres = config.PostgresConfig{
		Host: "127.0.0.1", Port: "5432", Database: "unused",
		User: "unused", Password: "unused", SSLMode: "disable",
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := app.Run(ctx, cfg); !errors.Is(err, context.Canceled) {
		t.Fatalf("app.Run error = %v, want context.Canceled", err)
	}
	// A failed database connection must release the listener opened by app.Run.
	reopened, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatalf("HTTP address is still occupied after failed startup: %v", err)
	}
	reopened.Close()
}
