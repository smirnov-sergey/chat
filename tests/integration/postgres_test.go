//go:build integration

package integration_test

import (
	"context"
	"testing"
	"time"

	"chat/internal/postgres"
)

func TestOpenIntegration(t *testing.T) {
	cfg := testPostgresConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	db, err := postgres.Open(ctx, cfg.ConnectionString())
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
