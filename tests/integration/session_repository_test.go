//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"chat/internal/migrate"
	"chat/internal/model/session"
	"chat/internal/repository"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestSessionRepository(t *testing.T) {
	db := integrationDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve migration directory")
	}
	files := os.DirFS(filepath.Join(filepath.Dir(source), "..", "..", "migrations"))
	if _, err := migrate.Up(ctx, db, files); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 15, 12, 0, 0, 123456000, time.UTC)
	var userID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO users (username, name, created_at, updated_at, password_hash)
		VALUES ('ivan', 'Иван', $1, $1, 'test-password-hash') RETURNING id`, now).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	repo := repository.NewSessionRepository(db)
	value := session.Session{
		TokenHash: bytes.Repeat([]byte{1}, 32),
		UserID:    userID,
		CreatedAt: now,
		ExpiresAt: now.Add(time.Hour),
	}
	if err := repo.Create(ctx, &value); err != nil {
		t.Fatal(err)
	}
	assertStoredSession := func(t *testing.T, want session.Session) {
		t.Helper()
		got, err := repo.GetByTokenHash(ctx, want.TokenHash)
		if err != nil {
			t.Fatal(err)
		}
		if got == nil || !bytes.Equal(got.TokenHash, want.TokenHash) || got.UserID != want.UserID ||
			!got.CreatedAt.Equal(want.CreatedAt) || !got.ExpiresAt.Equal(want.ExpiresAt) {
			t.Fatalf("stored session = %+v, want %+v", got, want)
		}
	}

	t.Run("read persisted session", func(t *testing.T) {
		assertStoredSession(t, value)
	})
	t.Run("missing session", func(t *testing.T) {
		got, err := repo.GetByTokenHash(ctx, bytes.Repeat([]byte{9}, 32))
		if got != nil || !errors.Is(err, session.ErrNotFound) {
			t.Fatalf("GetByTokenHash = (%+v, %v), want (nil, ErrNotFound)", got, err)
		}
	})
	t.Run("duplicate does not overwrite session", func(t *testing.T) {
		duplicate := value
		duplicate.ExpiresAt = now.Add(2 * time.Hour)
		err := repo.Create(ctx, &duplicate)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
			t.Fatalf("Create error = %v, want a unique constraint violation", err)
		}
		assertStoredSession(t, value)
	})
	t.Run("constraint errors remain available", func(t *testing.T) {
		invalid := value
		invalid.TokenHash = make([]byte, 31)
		err := repo.Create(ctx, &invalid)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
			t.Fatalf("Create error = %v, want a check constraint violation", err)
		}
	})
	t.Run("expired rows are returned for the service to check", func(t *testing.T) {
		expired := value
		expired.TokenHash = bytes.Repeat([]byte{2}, 32)
		expired.CreatedAt = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
		expired.ExpiresAt = expired.CreatedAt.Add(time.Hour)
		if err := repo.Create(ctx, &expired); err != nil {
			t.Fatal(err)
		}
		assertStoredSession(t, expired)
	})
	t.Run("canceled context", func(t *testing.T) {
		canceled, stop := context.WithCancel(ctx)
		stop()
		if err := repo.Create(canceled, &value); !errors.Is(err, context.Canceled) {
			t.Fatalf("Create error = %v, want context.Canceled", err)
		}
		if _, err := repo.GetByTokenHash(canceled, value.TokenHash); !errors.Is(err, context.Canceled) {
			t.Fatalf("GetByTokenHash error = %v, want context.Canceled", err)
		}
		if err := repo.DeleteByTokenHash(canceled, value.TokenHash); !errors.Is(err, context.Canceled) {
			t.Fatalf("DeleteByTokenHash error = %v, want context.Canceled", err)
		}
		assertStoredSession(t, value)
	})
	t.Run("delete is specific and idempotent", func(t *testing.T) {
		other := value
		other.TokenHash = bytes.Repeat([]byte{3}, 32)
		if err := repo.Create(ctx, &other); err != nil {
			t.Fatal(err)
		}
		if err := repo.DeleteByTokenHash(ctx, value.TokenHash); err != nil {
			t.Fatal(err)
		}
		got, err := repo.GetByTokenHash(ctx, value.TokenHash)
		if got != nil || !errors.Is(err, session.ErrNotFound) {
			t.Fatalf("deleted session = (%+v, %v), want (nil, ErrNotFound)", got, err)
		}
		if err := repo.DeleteByTokenHash(ctx, value.TokenHash); err != nil {
			t.Fatalf("repeat deletion: %v", err)
		}
		assertStoredSession(t, other)
	})
}
