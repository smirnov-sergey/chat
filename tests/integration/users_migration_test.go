//go:build integration

package integration_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"chat/internal/migrate"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestUsersMigration(t *testing.T) {
	db := integrationDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve migration directory")
	}
	files := os.DirFS(filepath.Join(filepath.Dir(source), "..", "..", "migrations"))
	if count, err := migrate.Up(ctx, db, files); err != nil || count == 0 {
		t.Fatalf("apply migrations = (%d, %v), want at least one applied migration", count, err)
	}

	const insert = `INSERT INTO users (username, name, created_at, updated_at)
		VALUES ($1, $2, $3, $4)`
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	var id int64
	if err := db.QueryRowContext(ctx, insert+" RETURNING id", "ivan", "Иван", now, now).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if id <= 0 {
		t.Fatalf("generated ID = %d, want a positive value", id)
	}
	// Names may repeat; IDs must be generated independently.
	var otherID int64
	if err := db.QueryRowContext(ctx, insert+" RETURNING id", strings.Repeat("a", 32), "Иван", now, now).Scan(&otherID); err != nil {
		t.Fatal(err)
	}
	if otherID <= 0 || otherID == id {
		t.Fatal("expected a distinct generated ID")
	}
	if _, err := db.ExecContext(ctx, insert, "abc", strings.Repeat("Я", 80), now, now); err != nil {
		t.Fatalf("valid lower username and upper Unicode name length boundaries: %v", err)
	}

	for _, tt := range []struct {
		name      string
		username  any
		profile   any
		createdAt any
		updatedAt any
		code      string
	}{
		{"duplicate username", "ivan", "Другой Иван", now, now, "23505"},
		{"missing username", nil, "Иван", now, now, "23502"},
		{"empty username", "", "Иван", now, now, "23514"},
		{"short username", "ab", "Иван", now, now, "23514"},
		{"long username", strings.Repeat("a", 33), "Иван", now, now, "23514"},
		{"uppercase username", "Ivan", "Иван", now, now, "23514"},
		{"non-Latin username", "иван", "Иван", now, now, "23514"},
		{"invalid username character", "iv-an", "Иван", now, now, "23514"},
		{"missing name", "other", nil, now, now, "23502"},
		{"empty name", "other", "", now, now, "23514"},
		{"long name", "other", strings.Repeat("Я", 81), now, now, "23514"},
		{"missing creation time", "other", "Имя", nil, now, "23502"},
		{"missing update time", "other", "Имя", now, nil, "23502"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := db.ExecContext(ctx, insert, tt.username, tt.profile, tt.createdAt, tt.updatedAt)
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != tt.code {
				t.Fatalf("insert error = %v, want PostgreSQL code %s", err, tt.code)
			}
		})
	}

	if count, err := migrate.Up(ctx, db, files); err != nil || count != 0 {
		t.Fatalf("repeat migrations = (%d, %v), want (0, nil)", count, err)
	}
	var username, name string
	var createdAt, updatedAt time.Time
	if err := db.QueryRowContext(ctx, `SELECT username, name, created_at, updated_at FROM users WHERE id = $1`, id).
		Scan(&username, &name, &createdAt, &updatedAt); err != nil {
		t.Fatal(err)
	}
	if username != "ivan" || name != "Иван" || !createdAt.Equal(now) || !updatedAt.Equal(now) {
		t.Fatal("repeat migration must preserve user data and timestamps")
	}
}
