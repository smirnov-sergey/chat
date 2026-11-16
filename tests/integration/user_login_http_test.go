//go:build integration

package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"chat/internal/dto"
	"chat/internal/handlers"
	"chat/internal/migrate"
	"chat/internal/password"
	"chat/internal/repository"
	"chat/internal/router"
	"chat/internal/service"
)

func TestUserLoginHTTP(t *testing.T) {
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
	const sessionLifetime = 7 * 24 * time.Hour
	sessions, err := service.NewSessionService(repository.NewSessionRepository(db), sessionLifetime)
	if err != nil {
		t.Fatal(err)
	}
	users, err := service.NewUserService(repository.NewUserRepository(db), repository.NewUserRegistrationStore(db), password.Argon2id{}, sessions, sessionLifetime)
	if err != nil {
		t.Fatal(err)
	}
	const originalPassword = "  long personal password  "
	registered, err := users.Register(ctx, "ivan", "Иван Петров", originalPassword)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO users (username, name, created_at, updated_at)
		VALUES ('legacy', 'Без пароля', $1, $1)`, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	h := router.New(handlers.NewUserHandler(users, false))
	login := func(t *testing.T, username, password string) *httptest.ResponseRecorder {
		t.Helper()
		data, err := json.Marshal(dto.UserLoginRequest{Username: username, Password: password})
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(string(data))).WithContext(ctx)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	assertSessionCount := func(t *testing.T, want int) {
		t.Helper()
		var count int
		if err := db.QueryRowContext(ctx, "SELECT count(*) FROM sessions").Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != want {
			t.Fatalf("session count = %d, want %d", count, want)
		}
	}

	t.Run("invalid credentials create no session", func(t *testing.T) {
		for _, input := range []struct{ username, password string }{
			{"ivan", "wrong personal password"},
			{"missing", originalPassword},
			{"legacy", originalPassword},
			{"ivan", strings.TrimSpace(originalPassword)},
			{"ivan", ""},
		} {
			rec := login(t, input.username, input.password)
			if rec.Code != http.StatusUnauthorized || rec.Header().Get("Set-Cookie") != "" || rec.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("login for %q: status = %d; want 401 without a cookie and with caching disabled", input.username, rec.Code)
			}
			if rec.Body.String() != "{\"error\":\"invalid username or password\"}\n" {
				t.Fatalf("unexpected error body: %q", rec.Body.String())
			}
			assertSessionCount(t, 1)
		}
	})

	t.Run("each login creates an independent session", func(t *testing.T) {
		seen := map[string]bool{registered.Token.Value: true}
		for i, username := range []string{" \tIVAN\n", "ivan"} {
			rec := login(t, username, originalPassword)
			if rec.Code != http.StatusOK {
				t.Fatalf("login status = %d, want 200; body: %s", rec.Code, rec.Body.String())
			}
			var profile dto.UserResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &profile); err != nil {
				t.Fatal(err)
			}
			if profile.ID != registered.User.ID || profile.Username != "ivan" || profile.Name != "Иван Петров" {
				t.Fatal("login must return the existing user's profile")
			}
			response := rec.Result()
			cookies := response.Cookies()
			response.Body.Close()
			if len(cookies) != 1 {
				t.Fatalf("cookie count = %d, want 1", len(cookies))
			}
			cookie := cookies[0]
			if cookie.Name != "chat_session" || cookie.Path != "/" || !cookie.HttpOnly || cookie.Secure || cookie.SameSite != http.SameSiteLaxMode || seen[cookie.Value] {
				t.Fatal("login must issue a new HttpOnly session cookie for local HTTP")
			}
			seen[cookie.Value] = true
			stored, err := sessions.Authenticate(ctx, cookie.Value)
			if err != nil {
				t.Fatalf("new cookie must authenticate: %v", err)
			}
			if stored.UserID != profile.ID || stored.ExpiresAt.Sub(stored.CreatedAt) != sessionLifetime || cookie.Expires.Sub(stored.ExpiresAt).Abs() > time.Second {
				t.Fatal("stored session must belong to the user and expire with the cookie after seven days")
			}
			if strings.Contains(rec.Body.String(), cookie.Value) || strings.Contains(rec.Body.String(), "personal password") || strings.Contains(rec.Body.String(), registered.User.PasswordHash) {
				t.Fatal("login JSON must not contain the token, password or password hash")
			}
			assertSessionCount(t, i+2)
		}
		for token := range seen {
			if _, err := sessions.Authenticate(ctx, token); err != nil {
				t.Fatalf("previous sessions must remain valid after another login: %v", err)
			}
		}
	})
}
