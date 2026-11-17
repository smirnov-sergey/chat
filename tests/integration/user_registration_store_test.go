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

func TestUserRegistrationStoreConcurrentUsername(t *testing.T) {
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
	now := time.Date(2026, 11, 17, 12, 0, 0, 123456000, time.UTC)
	users := [2]user.User{
		{Username: "ivan", Name: "Первый", PasswordHash: "first-hash", CreatedAt: now, UpdatedAt: now},
		{Username: "ivan", Name: "Второй", PasswordHash: "second-hash", CreatedAt: now, UpdatedAt: now},
	}
	sessions := [2]session.Session{
		{TokenHash: bytes.Repeat([]byte{1}, 32), CreatedAt: now, ExpiresAt: now.Add(7 * 24 * time.Hour)},
		{TokenHash: bytes.Repeat([]byte{2}, 32), CreatedAt: now, ExpiresAt: now.Add(7 * 24 * time.Hour)},
	}
	originalUsers := users
	type registrationResult struct {
		index int
		err   error
	}
	start := make(chan struct{})
	ready := make(chan struct{}, len(users))
	results := make(chan registrationResult, len(users))
	for i := range users {
		go func() {
			ready <- struct{}{}
			<-start
			results <- registrationResult{index: i, err: repo.Create(ctx, &users[i], &sessions[i])}
		}()
	}
	for range users {
		<-ready
	}
	close(start)

	winner, loser := -1, -1
	for range users {
		result := <-results
		switch {
		case result.err == nil:
			if winner != -1 {
				t.Error("only one registration may succeed")
			}
			winner = result.index
		case errors.Is(result.err, user.ErrUsernameTaken):
			if loser != -1 {
				t.Error("one registration must succeed rather than both being rejected")
			}
			loser = result.index
		default:
			t.Errorf("registration %d: unexpected error: %v", result.index, result.err)
		}
	}
	if winner == -1 || loser == -1 {
		t.Fatal("expected one successful registration and one ErrUsernameTaken")
	}
	if users[winner].ID <= 0 || sessions[winner].UserID != users[winner].ID {
		t.Fatal("successful registration must assign the same user ID to both models")
	}
	if users[loser] != originalUsers[loser] || sessions[loser].UserID != 0 {
		t.Fatal("rejected registration must not modify the user or assign a session user ID")
	}
	var userCount, sessionCount int
	if err := db.QueryRowContext(ctx, "SELECT (SELECT count(*) FROM users), (SELECT count(*) FROM sessions)").Scan(&userCount, &sessionCount); err != nil {
		t.Fatal(err)
	}
	if userCount != 1 || sessionCount != 1 {
		t.Fatalf("users=%d, sessions=%d; want one user and one session", userCount, sessionCount)
	}
	storedUser, err := repository.NewUserRepository(db).GetByUsername(ctx, "ivan")
	if err != nil {
		t.Fatal(err)
	}
	if storedUser.ID != users[winner].ID || storedUser.Username != users[winner].Username ||
		storedUser.Name != users[winner].Name || storedUser.PasswordHash != users[winner].PasswordHash ||
		!storedUser.CreatedAt.Equal(now) || !storedUser.UpdatedAt.Equal(now) {
		t.Fatal("stored user must contain the winner's data without changes from the rejected registration")
	}
	storedSession, err := repository.NewSessionRepository(db).GetByTokenHash(ctx, sessions[winner].TokenHash)
	if err != nil {
		t.Fatal(err)
	}
	if storedSession.UserID != users[winner].ID || !storedSession.CreatedAt.Equal(now) || !storedSession.ExpiresAt.Equal(sessions[winner].ExpiresAt) {
		t.Fatal("the only stored session must belong to the successful registration")
	}
}

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
	const passwordHash = "test-password-hash"
	u := user.User{Username: "ivan", Name: "Иван", PasswordHash: passwordHash, CreatedAt: now, UpdatedAt: now}
	s := session.Session{TokenHash: bytes.Repeat([]byte{1}, 32), CreatedAt: now, ExpiresAt: now.Add(7 * 24 * time.Hour)}
	if err := repo.Create(ctx, &u, &s); err != nil {
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
		duplicate.PasswordHash = "different-hash"
		before := duplicate
		newSession := s
		newSession.TokenHash = bytes.Repeat([]byte{2}, 32)
		newSession.UserID = 0
		if err := repo.Create(ctx, &duplicate, &newSession); !errors.Is(err, user.ErrUsernameTaken) {
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
			newUser := user.User{Username: tt.username, Name: "Новый пользователь", PasswordHash: passwordHash, CreatedAt: now, UpdatedAt: now}
			before := newUser
			newSession := session.Session{TokenHash: tt.tokenHash, CreatedAt: now, ExpiresAt: tt.expiresAt}
			err := repo.Create(ctx, &newUser, &newSession)
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
		newUser := user.User{Username: "canceled", Name: "Иван", PasswordHash: passwordHash, CreatedAt: now, UpdatedAt: now}
		newSession := session.Session{TokenHash: bytes.Repeat([]byte{4}, 32), CreatedAt: now, ExpiresAt: s.ExpiresAt}
		if err := repo.Create(canceled, &newUser, &newSession); !errors.Is(err, context.Canceled) {
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
