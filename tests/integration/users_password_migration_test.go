//go:build integration

package integration_test

import (
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"testing/fstest"
	"time"

	"chat/internal/migrate"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestUsersPasswordMigration(t *testing.T) {
	db := integrationDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve migration directory")
	}
	files := os.DirFS(filepath.Join(filepath.Dir(source), "..", "..", "migrations"))
	const firstMigration = "00001_create_users.sql"
	data, err := fs.ReadFile(files, firstMigration)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrate.Up(ctx, db, fstest.MapFS{firstMigration: {Data: data}}); err != nil {
		t.Fatal(err)
	}

	// Upgrade a database that already contains a profile without a password.
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	var userID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO users (username, name, created_at, updated_at)
		VALUES ('ivan', 'Иван', $1, $1) RETURNING id`, now).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if count, err := migrate.Up(ctx, db, files); err != nil || count == 0 {
		t.Fatalf("apply migrations = (%d, %v), want at least one new migration", count, err)
	}
	var username, name string
	var createdAt, updatedAt time.Time
	var hash sql.NullString
	if err := db.QueryRowContext(ctx, `
		SELECT username, name, created_at, updated_at, password_hash FROM users WHERE id = $1`, userID).
		Scan(&username, &name, &createdAt, &updatedAt, &hash); err != nil {
		t.Fatal(err)
	}
	if username != "ivan" || name != "Иван" || !createdAt.Equal(now) || !updatedAt.Equal(now) {
		t.Fatal("migration must preserve existing user data")
	}
	if hash.Valid {
		t.Fatal("migration must not invent a password hash for an existing profile")
	}

	// The database stores the hash as opaque text; its format is checked by password code.
	const passwordHash = "test-password-hash"
	if _, err := db.ExecContext(ctx, "UPDATE users SET password_hash = $1 WHERE id = $2", passwordHash, userID); err != nil {
		t.Fatal(err)
	}
	t.Run("empty hash is rejected", func(t *testing.T) {
		_, err := db.ExecContext(ctx, "UPDATE users SET password_hash = '' WHERE id = $1", userID)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
			t.Fatalf("update error = %v, want PostgreSQL code 23514", err)
		}
	})
	t.Run("new profile without password remains supported", func(t *testing.T) {
		var hash sql.NullString
		if err := db.QueryRowContext(ctx, `
			INSERT INTO users (username, name, created_at, updated_at)
			VALUES ('petr', 'Пётр', $1, $1) RETURNING password_hash`, now).Scan(&hash); err != nil {
			t.Fatal(err)
		}
		if hash.Valid {
			t.Fatal("a profile without a password must have a NULL hash")
		}
	})
	t.Run("insert with hash", func(t *testing.T) {
		var storedHash string
		if err := db.QueryRowContext(ctx, `
			INSERT INTO users (username, name, created_at, updated_at, password_hash)
			VALUES ('anna', 'Анна', $1, $1, $2) RETURNING password_hash`, now, passwordHash).Scan(&storedHash); err != nil {
			t.Fatal(err)
		}
		if storedHash != passwordHash {
			t.Fatal("insert must preserve the password hash")
		}
	})
	if count, err := migrate.Up(ctx, db, files); err != nil || count != 0 {
		t.Fatalf("repeat migrations = (%d, %v), want (0, nil)", count, err)
	}
	var storedHash string
	if err := db.QueryRowContext(ctx,
		"SELECT password_hash FROM users WHERE id = $1", userID).Scan(&storedHash); err != nil {
		t.Fatal(err)
	}
	if storedHash != passwordHash {
		t.Fatal("failed update and repeat migration must preserve the password hash")
	}
}
