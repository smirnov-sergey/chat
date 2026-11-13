package migrate_test

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

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/joho/godotenv"
)

func unopenedDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("pgx", "postgres://unused:unused@127.0.0.1:1/unused?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func integrationDB(t *testing.T) *sql.DB {
	t.Helper()
	// Resolve the root .env independently of the IDE's working directory.
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve migration test source path")
	}
	dsn, err := testDatabaseURL(filepath.Join(filepath.Dir(sourceFile), "..", "..", ".env"))
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal("invalid TEST_DATABASE_URL")
	}
	if !strings.HasSuffix(cfg.Database, "_test") {
		t.Fatal("TEST_DATABASE_URL must select a dedicated database with a name ending in _test")
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
	cfg.RuntimeParams["search_path"] = schema
	db := stdlib.OpenDB(*cfg)
	t.Cleanup(func() { db.Close() })
	return db
}

func testDatabaseURL(envPath string) (string, error) {
	dsn, exists := os.LookupEnv("TEST_DATABASE_URL")
	if !exists {
		values, err := godotenv.Read(envPath)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("read test .env: %w", err)
		}
		dsn = values["TEST_DATABASE_URL"]
	}
	if dsn == "" {
		return "", errors.New("set TEST_DATABASE_URL in the environment or root .env to a dedicated PostgreSQL database with a name ending in _test; see migrations/README.md")
	}
	return dsn, nil
}

func TestDatabaseURLSources(t *testing.T) {
	tests := []struct {
		name    string
		file    string
		setEnv  bool
		env     string
		want    string
		wantErr bool
	}{
		{name: "root dotenv", file: "TEST_DATABASE_URL=from-file\n", want: "from-file"},
		{name: "environment overrides file", file: "TEST_DATABASE_URL=from-file\n", setEnv: true, env: "from-env", want: "from-env"},
		{name: "environment without file", setEnv: true, env: "from-env", want: "from-env"},
		{name: "explicit empty environment overrides file", file: "TEST_DATABASE_URL=from-file\n", setEnv: true, wantErr: true},
		{name: "missing configuration", wantErr: true},
		{name: "empty file value", file: "TEST_DATABASE_URL=\n", wantErr: true},
		{name: "malformed dotenv", file: "TEST_DATABASE_URL=\"unterminated\n", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("TEST_DATABASE_URL", tt.env)
			if !tt.setEnv {
				if err := os.Unsetenv("TEST_DATABASE_URL"); err != nil {
					t.Fatal(err)
				}
			}
			envPath := filepath.Join(t.TempDir(), ".env")
			if tt.file != "" {
				if err := os.WriteFile(envPath, []byte(tt.file), 0600); err != nil {
					t.Fatal(err)
				}
			}
			// Loading the explicitly located file must not depend on cwd.
			t.Chdir(t.TempDir())
			got, err := testDatabaseURL(envPath)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr = %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Error("unexpected test database configuration")
			}
		})
	}
}
