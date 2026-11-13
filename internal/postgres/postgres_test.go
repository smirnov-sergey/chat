package postgres_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"chat/internal/config"
	"chat/internal/postgres"

	"github.com/joho/godotenv"
)

func TestOpenRejectsInvalidConnectionString(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	db, err := postgres.Open(ctx, "postgres://localhost:invalid/chat")
	if db != nil {
		db.Close()
		t.Fatal("expected nil pool on failure")
	}
	if err == nil {
		t.Fatal("expected connection configuration error")
	}
}

func TestOpenHonorsContext(t *testing.T) {
	tests := []struct {
		name string
		ctx  func() (context.Context, context.CancelFunc)
		want error
	}{
		{
			name: "canceled",
			ctx: func() (context.Context, context.CancelFunc) {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx, cancel
			},
			want: context.Canceled,
		},
		{
			name: "deadline exceeded",
			ctx: func() (context.Context, context.CancelFunc) {
				return context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
			},
			want: context.DeadlineExceeded,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := tt.ctx()
			defer cancel()
			db, err := postgres.Open(ctx, "postgres://chat:password@127.0.0.1:5432/chat?sslmode=disable")
			if db != nil {
				db.Close()
				t.Fatal("expected nil pool on failure")
			}
			if !errors.Is(err, tt.want) {
				t.Errorf("error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestOpenIntegration(t *testing.T) {
	// Resolve .env from the source file so GoLand's working directory does not matter.
	_, testFile, _, _ := runtime.Caller(0)
	envPath := filepath.Join(filepath.Dir(testFile), "..", "..", ".env")
	values, err := godotenv.Read(envPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	for key, value := range values {
		if _, exists := os.LookupEnv(key); !exists {
			t.Setenv(key, value)
		}
	}
	cfg := config.Load()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	db, err := postgres.Open(ctx, cfg.Postgres.ConnectionString())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	var result int
	if err := db.QueryRowContext(ctx, "SELECT 1").Scan(&result); err != nil {
		t.Fatal(err)
	}
	if result != 1 {
		t.Errorf("SELECT 1 = %d, want 1", result)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := db.PingContext(ctx); err == nil {
		t.Error("expected error after closing pool")
	}
}
