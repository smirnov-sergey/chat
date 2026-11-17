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

func TestUsersPhotoMigration(t *testing.T) {
	db := integrationDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve migration directory")
	}
	files := os.DirFS(filepath.Join(filepath.Dir(source), "..", "..", "migrations"))
	previous := fstest.MapFS{}
	for _, name := range []string{"00001_create_users.sql", "00002_add_users_password_hash.sql", "00003_create_sessions.sql"} {
		data, err := fs.ReadFile(files, name)
		if err != nil {
			t.Fatal(err)
		}
		previous[name] = &fstest.MapFile{Data: data}
	}
	if _, err := migrate.Up(ctx, db, previous); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 17, 12, 0, 0, 123456000, time.UTC)
	var userID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO users (username, name, password_hash, created_at, updated_at)
		VALUES ('ivan', 'Иван', 'existing-password-hash', $1, $1) RETURNING id`, now).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if count, err := migrate.Up(ctx, db, files); err != nil || count == 0 {
		t.Fatalf("apply migrations = (%d, %v), want new migrations", count, err)
	}
	var username, name, passwordHash string
	var createdAt, updatedAt time.Time
	var photo sql.NullString
	if err := db.QueryRowContext(ctx, `
		SELECT username, name, password_hash, created_at, updated_at, photo
		FROM users WHERE id = $1`, userID).
		Scan(&username, &name, &passwordHash, &createdAt, &updatedAt, &photo); err != nil {
		t.Fatal(err)
	}
	if username != "ivan" || name != "Иван" || passwordHash != "existing-password-hash" ||
		!createdAt.Equal(now) || !updatedAt.Equal(now) || photo.Valid {
		t.Fatal("migration must preserve the existing profile and leave its photo NULL")
	}
	assertPhoto := func(want sql.NullString) {
		t.Helper()
		var got sql.NullString
		if err := db.QueryRowContext(ctx, "SELECT photo FROM users WHERE id = $1", userID).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("photo = %+v, want %+v", got, want)
		}
	}
	for _, filename := range []string{"first-photo.png", "replacement-photo.png"} {
		if _, err := db.ExecContext(ctx, "UPDATE users SET photo = $1 WHERE id = $2", filename, userID); err != nil {
			t.Fatal(err)
		}
		assertPhoto(sql.NullString{String: filename, Valid: true})
	}
	_, err := db.ExecContext(ctx, "UPDATE users SET photo = '' WHERE id = $1", userID)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23514" || pgErr.ConstraintName != "users_photo_not_empty" {
		t.Fatalf("empty filename error = %v, want users_photo_not_empty violation", err)
	}
	if count, err := migrate.Up(ctx, db, files); err != nil || count != 0 {
		t.Fatalf("repeat migrations = (%d, %v), want (0, nil)", count, err)
	}
	assertPhoto(sql.NullString{String: "replacement-photo.png", Valid: true})
	if _, err := db.ExecContext(ctx, "UPDATE users SET photo = NULL WHERE id = $1", userID); err != nil {
		t.Fatal(err)
	}
	assertPhoto(sql.NullString{})

	var newPhoto sql.NullString
	if err := db.QueryRowContext(ctx, `
		INSERT INTO users (username, name, password_hash, created_at, updated_at)
		VALUES ('anna', 'Анна', 'new-password-hash', $1, $1)
		RETURNING photo`, now).Scan(&newPhoto); err != nil {
		t.Fatal(err)
	}
	if newPhoto.Valid {
		t.Fatal("registration without a photo must leave photo NULL")
	}
}
