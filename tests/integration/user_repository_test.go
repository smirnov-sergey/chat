//go:build integration

package integration_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"chat/internal/migrate"
	"chat/internal/model/user"
	"chat/internal/repository"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestUserRepository(t *testing.T) {
	db := integrationDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve migration directory")
	}
	files := os.DirFS(filepath.Join(filepath.Dir(source), "..", "..", "migrations"))
	if _, err := migrate.Up(ctx, db, files); err != nil {
		t.Fatal(err)
	}
	repo := repository.NewUserRepository(db)
	now := time.Date(2026, 11, 13, 12, 0, 0, 123456000, time.UTC)
	input := user.User{Username: "ivan", Name: "Иван", CreatedAt: now, UpdatedAt: now}
	const passwordHash = "test-password-hash"
	stored := input
	if err := repo.Create(ctx, &stored, passwordHash); err != nil {
		t.Fatal(err)
	}
	if stored.ID <= 0 {
		t.Fatalf("generated ID = %d, want a positive value", stored.ID)
	}
	assertStoredUser := func(t *testing.T, got *user.User) {
		t.Helper()
		if got == nil {
			t.Fatal("expected a user, got nil")
		}
		if got.ID != stored.ID || got.Username != input.Username || got.Name != input.Name ||
			!got.CreatedAt.Equal(now) || !got.UpdatedAt.Equal(now) {
			t.Fatalf("stored user = %+v, want ID %d and data %+v", got, stored.ID, input)
		}
	}
	assertStoredUser(t, &stored)

	t.Run("password hash is stored", func(t *testing.T) {
		var hash string
		if err := db.QueryRowContext(ctx, "SELECT password_hash FROM users WHERE id = $1", stored.ID).Scan(&hash); err != nil {
			t.Fatal(err)
		}
		if hash != passwordHash {
			t.Fatal("Create must preserve the supplied password hash")
		}
	})

	t.Run("create with password hash", func(t *testing.T) {
		value := input
		value.Username = "anna"
		const hash = "test-password-hash"
		if err := repo.Create(ctx, &value, hash); err != nil {
			t.Fatal(err)
		}
		if value.ID <= 0 || value.ID == stored.ID {
			t.Fatal("expected a distinct generated ID")
		}
		got, err := repo.GetByUsername(ctx, value.Username)
		if err != nil {
			t.Fatal(err)
		}
		if got.ID != value.ID || got.Name != value.Name ||
			!got.CreatedAt.Equal(now) || !got.UpdatedAt.Equal(now) {
			t.Fatalf("stored profile = %+v, want %+v", got, value)
		}

		duplicate := value
		duplicate.ID = 0
		duplicate.Name = "Другое имя"
		before := duplicate
		if err := repo.Create(ctx, &duplicate, "different-hash"); !errors.Is(err, user.ErrUsernameTaken) {
			t.Fatalf("Create error = %v, want ErrUsernameTaken", err)
		}
		if duplicate != before {
			t.Fatal("failed creation must not modify the supplied user")
		}
		var savedHash, savedName string
		if err := db.QueryRowContext(ctx, "SELECT password_hash, name FROM users WHERE id = $1", value.ID).
			Scan(&savedHash, &savedName); err != nil {
			t.Fatal(err)
		}
		if savedHash != hash || savedName != value.Name {
			t.Fatal("creation must preserve the supplied hash; a duplicate must not overwrite the existing account")
		}
	})

	t.Run("empty hash does not create a profile", func(t *testing.T) {
		value := input
		value.Username = "no_password"
		before := value
		err := repo.Create(ctx, &value, "")
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
			t.Fatalf("Create error = %v, want a check constraint violation", err)
		}
		if value != before {
			t.Fatal("failed creation must not modify the supplied user")
		}
		got, err := repo.GetByUsername(ctx, value.Username)
		if got != nil || !errors.Is(err, user.ErrNotFound) {
			t.Fatalf("GetByUsername = (%+v, %v), want (nil, ErrNotFound)", got, err)
		}
	})

	t.Run("read persisted user", func(t *testing.T) {
		got, err := repo.GetByUsername(ctx, input.Username)
		if err != nil {
			t.Fatal(err)
		}
		assertStoredUser(t, got)
	})

	t.Run("duplicate does not overwrite user", func(t *testing.T) {
		duplicate := input
		duplicate.Name = "Другое имя"
		duplicate.UpdatedAt = now.Add(time.Hour)
		before := duplicate
		if err := repo.Create(ctx, &duplicate, passwordHash); !errors.Is(err, user.ErrUsernameTaken) {
			t.Fatalf("Create error = %v, want ErrUsernameTaken", err)
		}
		if duplicate != before {
			t.Fatal("failed creation must not modify the supplied user")
		}
		got, err := repo.GetByUsername(ctx, input.Username)
		if err != nil {
			t.Fatal(err)
		}
		assertStoredUser(t, got)
	})

	t.Run("names may repeat", func(t *testing.T) {
		other := input
		other.Username = "petr"
		if err := repo.Create(ctx, &other, passwordHash); err != nil {
			t.Fatal(err)
		}
		if other.ID <= 0 || other.ID == stored.ID || other.Name != input.Name {
			t.Fatalf("second user = %+v, want distinct ID and same name", other)
		}
	})

	t.Run("lookup is exact", func(t *testing.T) {
		for _, username := range []string{"missing", "Ivan", " ivan "} {
			got, err := repo.GetByUsername(ctx, username)
			if !errors.Is(err, user.ErrNotFound) || got != nil {
				t.Fatalf("GetByUsername(%q) = (%+v, %v), want (nil, ErrNotFound)", username, got, err)
			}
		}
	})

	t.Run("constraint errors remain available", func(t *testing.T) {
		invalid := input
		invalid.Username = "invalid-name"
		before := invalid
		err := repo.Create(ctx, &invalid, passwordHash)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
			t.Fatalf("Create error = %v, want a wrapped check constraint violation", err)
		}
		if invalid != before {
			t.Fatal("failed creation must not modify the supplied user")
		}
	})

	t.Run("canceled context", func(t *testing.T) {
		canceled, stop := context.WithCancel(ctx)
		stop()
		value := input
		if err := repo.Create(canceled, &value, passwordHash); !errors.Is(err, context.Canceled) {
			t.Fatalf("Create error = %v, want context.Canceled", err)
		}
		if value != input {
			t.Fatal("canceled creation must not modify the supplied user")
		}
		if _, err := repo.GetByUsername(canceled, input.Username); !errors.Is(err, context.Canceled) {
			t.Fatalf("GetByUsername error = %v, want context.Canceled", err)
		}
	})
}
