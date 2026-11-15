//go:build integration

package integration_test

import (
	"bytes"
	"context"
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

func TestSessionsMigration(t *testing.T) {
	db := integrationDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve migration directory")
	}
	files := os.DirFS(filepath.Join(filepath.Dir(source), "..", "..", "migrations"))
	previous := fstest.MapFS{}
	for _, name := range []string{"00001_create_users.sql", "00002_add_users_password_hash.sql"} {
		data, err := fs.ReadFile(files, name)
		if err != nil {
			t.Fatal(err)
		}
		previous[name] = &fstest.MapFile{Data: data}
	}
	if _, err := migrate.Up(ctx, db, previous); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	const passwordHash = "test-password-hash"
	var userID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO users (username, name, created_at, updated_at, password_hash)
		VALUES ('ivan', 'Иван', $1, $1, $2) RETURNING id`, now, passwordHash).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if count, err := migrate.Up(ctx, db, files); err != nil || count == 0 {
		t.Fatalf("apply migrations = (%d, %v), want at least one new migration", count, err)
	}
	var name, storedPasswordHash string
	if err := db.QueryRowContext(ctx, "SELECT name, password_hash FROM users WHERE id = $1", userID).
		Scan(&name, &storedPasswordHash); err != nil {
		t.Fatal(err)
	}
	if name != "Иван" || storedPasswordHash != passwordHash {
		t.Fatal("migration must preserve the existing user and password hash")
	}
	var count int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM sessions").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("migration must not create sessions for existing users")
	}

	const insert = "INSERT INTO sessions (token_hash, user_id, created_at, expires_at) VALUES ($1, $2, $3, $4)"
	tokenHash := bytes.Repeat([]byte{1}, 32)
	expiresAt := now.Add(time.Hour)
	if _, err := db.ExecContext(ctx, insert, tokenHash, userID, now, expiresAt); err != nil {
		t.Fatal(err)
	}
	otherHash := bytes.Repeat([]byte{2}, 32)
	for _, tt := range []struct {
		name      string
		hash      any
		userID    any
		createdAt any
		expiresAt any
		code      string
	}{
		{"duplicate token hash", tokenHash, userID, now, expiresAt, "23505"},
		{"missing token hash", nil, userID, now, expiresAt, "23502"},
		{"short token hash", make([]byte, 31), userID, now, expiresAt, "23514"},
		{"long token hash", make([]byte, 33), userID, now, expiresAt, "23514"},
		{"missing user", otherHash, nil, now, expiresAt, "23502"},
		{"unknown user", otherHash, userID + 1, now, expiresAt, "23503"},
		{"missing creation time", otherHash, userID, nil, expiresAt, "23502"},
		{"missing expiration time", otherHash, userID, now, nil, "23502"},
		{"expiration equals creation", otherHash, userID, now, now, "23514"},
		{"expiration before creation", otherHash, userID, now, now.Add(-time.Second), "23514"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := db.ExecContext(ctx, insert, tt.hash, tt.userID, tt.createdAt, tt.expiresAt)
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != tt.code {
				t.Fatalf("insert error = %v, want PostgreSQL code %s", err, tt.code)
			}
		})
	}
	t.Run("multiple sessions per user", func(t *testing.T) {
		if _, err := db.ExecContext(ctx, insert, otherHash, userID, now, expiresAt); err != nil {
			t.Fatal(err)
		}
		var count int
		if err := db.QueryRowContext(ctx, "SELECT count(*) FROM sessions WHERE user_id = $1", userID).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 2 {
			t.Fatalf("session count = %d, want 2", count)
		}
	})
	if count, err := migrate.Up(ctx, db, files); err != nil || count != 0 {
		t.Fatalf("repeat migrations = (%d, %v), want (0, nil)", count, err)
	}
	var storedUserID int64
	var createdAt, storedExpiresAt time.Time
	if err := db.QueryRowContext(ctx,
		"SELECT user_id, created_at, expires_at FROM sessions WHERE token_hash = $1", tokenHash).
		Scan(&storedUserID, &createdAt, &storedExpiresAt); err != nil {
		t.Fatal(err)
	}
	if storedUserID != userID || !createdAt.Equal(now) || !storedExpiresAt.Equal(expiresAt) {
		t.Fatal("repeat migration must preserve existing sessions")
	}
}
