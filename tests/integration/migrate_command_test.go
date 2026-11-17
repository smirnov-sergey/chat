//go:build integration

package integration_test

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestMigrateCommandApplyAndRepeat(t *testing.T) {
	db := integrationDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	var schema string
	if err := db.QueryRowContext(ctx, "SELECT current_schema()").Scan(&schema); err != nil {
		t.Fatal(err)
	}
	settings := testPostgresConfig(t)
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve project directory")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "..", ".."))
	migrations, err := filepath.Glob(filepath.Join(root, "migrations", "*.sql"))
	if err != nil || len(migrations) == 0 {
		t.Fatalf("cannot find SQL migrations: %v", err)
	}
	binary := filepath.Join(t.TempDir(), "migrate")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	build := exec.CommandContext(ctx, "go", "build", "-o", binary, "./cmd/migrate")
	build.Dir = root
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build migration command: %v\n%s", err, output)
	}

	for _, expectedApplied := range []int{len(migrations), 0} {
		command := exec.CommandContext(ctx, binary, "up")
		command.Dir = root
		// Explicit settings override the local .env and keep both runs in the test schema.
		command.Env = append(command.Environ(),
			"POSTGRES_HOST="+settings.Host,
			"POSTGRES_PORT="+settings.Port,
			"POSTGRES_DB="+settings.Database,
			"POSTGRES_USER="+settings.User,
			"POSTGRES_PASSWORD="+settings.Password,
			"POSTGRES_SSLMODE="+settings.SSLMode,
			"SESSION_COOKIE_SECURE=false",
			"STORAGE_DIR=storage/files",
			"MAX_PHOTO_SIZE=5242880",
			"PGOPTIONS=-c search_path="+schema,
		)
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("migration command: %v\n%s", err, output)
		}
		want := fmt.Sprintf("migrations complete: %d applied", expectedApplied)
		if !strings.Contains(string(output), want) {
			t.Fatalf("command output = %q, want %q", output, want)
		}
		var applied, users, sessions int
		if err := db.QueryRowContext(ctx, `
			SELECT
				(SELECT count(*) FROM goose_db_version WHERE version_id > 0 AND is_applied),
				(SELECT count(*) FROM users),
				(SELECT count(*) FROM sessions)
		`).Scan(&applied, &users, &sessions); err != nil {
			t.Fatal(err)
		}
		if applied != len(migrations) || users != 0 || sessions != 0 {
			t.Fatalf("applied=%d, users=%d, sessions=%d; want %d, 0, 0", applied, users, sessions, len(migrations))
		}
	}
}
