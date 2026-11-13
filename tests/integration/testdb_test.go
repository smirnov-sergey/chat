//go:build integration

package integration_test

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"chat/internal/config"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/joho/godotenv"
)

func testPostgresConfig(t *testing.T) config.PostgresConfig {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve test configuration path")
	}
	cfg, err := loadConfig(t, filepath.Join(filepath.Dir(source), "..", "..", ".env"))
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func loadConfig(t *testing.T, envPath string) (config.PostgresConfig, error) {
	t.Helper()
	values, err := godotenv.Read(envPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return config.PostgresConfig{}, fmt.Errorf("read test .env: %w", err)
	}
	for key, value := range values {
		if _, exists := os.LookupEnv(key); !exists {
			t.Setenv(key, value)
		}
	}
	cfg, err := config.Load()
	if err != nil {
		return config.PostgresConfig{}, err
	}
	database := os.Getenv("POSTGRES_TEST_DB")
	if database == "" {
		return config.PostgresConfig{}, errors.New("POSTGRES_TEST_DB is required; see migrations/README.md")
	}
	if !strings.HasSuffix(database, "_test") {
		return config.PostgresConfig{}, errors.New("POSTGRES_TEST_DB must end in _test")
	}
	if database == cfg.Postgres.Database {
		return config.PostgresConfig{}, errors.New("POSTGRES_TEST_DB must differ from POSTGRES_DB")
	}
	cfg.Postgres.Database = database
	return cfg.Postgres, nil
}

func integrationDB(t *testing.T) *sql.DB {
	t.Helper()
	settings := testPostgresConfig(t)
	cfg, err := pgx.ParseConfig(settings.ConnectionString())
	if err != nil {
		t.Fatal("invalid test database configuration")
	}
	admin := stdlib.OpenDB(*cfg)
	t.Cleanup(func() { admin.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	schema := pgx.Identifier{"migration_" + strings.ToLower(rand.Text())}.Sanitize()
	if _, err := admin.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := admin.ExecContext(ctx, "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Errorf("clean migration test schema: %v", err)
		}
	})
	schemaConfig := cfg.Copy()
	schemaConfig.RuntimeParams["search_path"] = schema
	db := stdlib.OpenDB(*schemaConfig)
	t.Cleanup(func() { db.Close() })
	return db
}
