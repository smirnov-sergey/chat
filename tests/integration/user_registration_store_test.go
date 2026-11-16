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
	"chat/internal/model/user"
	"chat/internal/repository"
	"chat/internal/service"

	"github.com/jackc/pgx/v5/pgconn"
)

var _ service.UserRegistrationStore = (*repository.UserRegistrationStore)(nil)

func TestUserRegistrationStore(t *testing.T) {
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
	repo := repository.NewUserRegistrationStore(db)
	now := time.Date(2026, 11, 16, 12, 0, 0, 123456000, time.UTC)
	u := user.User{Username: "ivan", Name: "Иван", CreatedAt: now, UpdatedAt: now}
	s := session.Session{TokenHash: bytes.Repeat([]byte{1}, 32), CreatedAt: now, ExpiresAt: now.Add(7 * 24 * time.Hour)}
	const passwordHash = "test-password-hash"
	if err := repo.Create(ctx, &u, passwordHash, &s); err != nil {
		t.Fatal(err)
	}
	if u.ID <= 0 || s.UserID != u.ID {
		t.Fatal("successful registration must assign the generated user ID to both models")
	}
	assertOriginalRegistration := func(t *testing.T) {
		t.Helper()
		var name, storedHash string
		var userID int64
		var createdAt, expiresAt time.Time
		if err := db.QueryRowContext(ctx, `
			SELECT users.name, users.password_hash, sessions.user_id, sessions.created_at, sessions.expires_at
			FROM users JOIN sessions ON sessions.user_id = users.id
			WHERE users.id = $1 AND sessions.token_hash = $2`, u.ID, s.TokenHash).
			Scan(&name, &storedHash, &userID, &createdAt, &expiresAt); err != nil {
			t.Fatal(err)
		}
		if name != u.Name || storedHash != passwordHash || userID != u.ID ||
			!createdAt.Equal(s.CreatedAt) || !expiresAt.Equal(s.ExpiresAt) {
			t.Fatal("stored user, password hash and session must match the successful registration")
		}
	}
	assertOriginalRegistration(t)

	t.Run("duplicate username creates no session", func(t *testing.T) {
		duplicate := u
		duplicate.ID = 0
		duplicate.Name = "Другой Иван"
		before := duplicate
		newSession := s
		newSession.TokenHash = bytes.Repeat([]byte{2}, 32)
		newSession.UserID = 0
		if err := repo.Create(ctx, &duplicate, "different-hash", &newSession); !errors.Is(err, user.ErrUsernameTaken) {
			t.Fatalf("Create error = %v, want ErrUsernameTaken", err)
		}
		if duplicate != before || newSession.UserID != 0 {
			t.Fatal("failed registration must not change model IDs or user data")
		}
		var count int
		if err := db.QueryRowContext(ctx, "SELECT count(*) FROM sessions WHERE token_hash = $1", newSession.TokenHash).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatal("a rejected username must not create a session")
		}
		assertOriginalRegistration(t)
	})

	for _, tt := range []struct {
		name      string
		username  string
		tokenHash []byte
		expiresAt time.Time
		code      string
	}{
		{"duplicate token", "duplicate_token", s.TokenHash, s.ExpiresAt, "23505"},
		{"invalid token hash", "invalid_token", make([]byte, 31), s.ExpiresAt, "23514"},
		{"invalid expiration", "invalid_expiry", bytes.Repeat([]byte{3}, 32), now, "23514"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			newUser := user.User{Username: tt.username, Name: "Новый пользователь", CreatedAt: now, UpdatedAt: now}
			before := newUser
			newSession := session.Session{TokenHash: tt.tokenHash, CreatedAt: now, ExpiresAt: tt.expiresAt}
			err := repo.Create(ctx, &newUser, passwordHash, &newSession)
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != tt.code {
				t.Fatalf("Create error = %v, want PostgreSQL code %s", err, tt.code)
			}
			if newUser != before || newSession.UserID != 0 {
				t.Fatal("rolled-back registration must not change the supplied model IDs or user data")
			}
			var count int
			if err := db.QueryRowContext(ctx, "SELECT count(*) FROM users WHERE username = $1", tt.username).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count != 0 {
				t.Fatal("a session insert failure must roll back the new user")
			}
			assertOriginalRegistration(t)
		})
	}

	t.Run("canceled context creates neither record", func(t *testing.T) {
		canceled, stop := context.WithCancel(ctx)
		stop()
		newUser := user.User{Username: "canceled", Name: "Иван", CreatedAt: now, UpdatedAt: now}
		newSession := session.Session{TokenHash: bytes.Repeat([]byte{4}, 32), CreatedAt: now, ExpiresAt: s.ExpiresAt}
		if err := repo.Create(canceled, &newUser, passwordHash, &newSession); !errors.Is(err, context.Canceled) {
			t.Fatalf("Create error = %v, want context.Canceled", err)
		}
		if newUser.ID != 0 || newSession.UserID != 0 {
			t.Fatal("canceled registration must not change model IDs")
		}
		var count int
		if err := db.QueryRowContext(ctx, "SELECT count(*) FROM users WHERE username = 'canceled'").Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatal("canceled registration must not create a user")
		}
	})
}
