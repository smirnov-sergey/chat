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
	"strconv"
	"strings"
	"testing"
	"time"

	"chat/internal/dto"
	"chat/internal/handlers"
	"chat/internal/migrate"
	"chat/internal/model/user"
	"chat/internal/password"
	"chat/internal/repository"
	"chat/internal/router"
	"chat/internal/service"
)

func TestUserProfileHTTP(t *testing.T) {
	db := integrationDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve migration directory")
	}
	if _, err := migrate.Up(ctx, db, os.DirFS(filepath.Join(filepath.Dir(source), "..", "..", "migrations"))); err != nil {
		t.Fatal(err)
	}
	const lifetime = 7 * 24 * time.Hour
	sessions, err := service.NewSessionService(repository.NewSessionRepository(db), lifetime)
	if err != nil {
		t.Fatal(err)
	}
	repo := repository.NewUserRepository(db)
	users, err := service.NewUserService(repo, repository.NewUserRegistrationStore(db), password.Argon2id{}, sessions, nil, nil, lifetime)
	if err != nil {
		t.Fatal(err)
	}
	userHandler := handlers.NewUserHandler(users, false, 5*1024*1024)
	appRouter := router.New(userHandler)
	crossOriginProtectionMiddleware := handlers.CrossOriginProtectionMiddleware()
	authRateLimitMiddleware := handlers.AuthRateLimitMiddleware()
	sessionMiddleware := handlers.SessionMiddleware(sessions)
	h := crossOriginProtectionMiddleware(authRateLimitMiddleware(sessionMiddleware(appRouter)))
	requestProfile := func(id int64, token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/users/"+strconv.FormatInt(id, 10)+"?user_id=999", nil).WithContext(ctx)
		req.Header.Set("X-User-ID", "999")
		if token != "" {
			req.AddCookie(&http.Cookie{Name: "chat_session", Value: token})
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	now := time.Now().UTC()
	var tokens []string
	var ids []int64
	for _, username := range []string{"ivan", "anna"} {
		u := user.User{Username: username, Name: username, PasswordHash: "private-stored-hash", CreatedAt: now, UpdatedAt: now}
		if err := repo.Create(ctx, &u); err != nil {
			t.Fatal(err)
		}
		token, err := sessions.Create(ctx, u.ID)
		if err != nil {
			t.Fatal(err)
		}
		tokens = append(tokens, token.Value)
		ids = append(ids, u.ID)
		before, err := sessions.Authenticate(ctx, token.Value)
		if err != nil {
			t.Fatal(err)
		}
		rec := requestProfile(u.ID, token.Value)
		if rec.Code != http.StatusOK {
			t.Fatalf("profile status = %d, want 200; body: %s", rec.Code, rec.Body.String())
		}
		var profile dto.UserResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &profile); err != nil {
			t.Fatal(err)
		}
		if profile.ID != u.ID || profile.Username != username || profile.Name != u.Name {
			t.Fatal("profile must belong to the cookie's session regardless of client-supplied user IDs")
		}
		if strings.Contains(rec.Body.String(), u.PasswordHash) || strings.Contains(rec.Body.String(), token.Value) {
			t.Fatal("profile must not expose the password hash or session token")
		}
		if rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("Set-Cookie") != "" {
			t.Fatal("profile response must disable caching without issuing cookies")
		}
		after, err := sessions.Authenticate(ctx, token.Value)
		if err != nil {
			t.Fatal(err)
		}
		if !after.CreatedAt.Equal(before.CreatedAt) || !after.ExpiresAt.Equal(before.ExpiresAt) {
			t.Fatal("reading a profile must not extend its session")
		}
	}
	for i, token := range tokens {
		foreignID := ids[1-i]
		rec := requestProfile(foreignID, token)
		if rec.Code != http.StatusForbidden || rec.Body.String() != "{\"error\":\"access denied\"}\n" {
			t.Fatalf("foreign profile status = %d, body = %q; want 403", rec.Code, rec.Body.String())
		}
	}
	if err := sessions.Logout(ctx, tokens[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "UPDATE sessions SET expires_at = created_at + INTERVAL '1 microsecond' WHERE user_id = $1", ids[1]); err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{"", "malformed", tokens[0], tokens[1]} {
		rec := requestProfile(ids[0], token)
		if rec.Code != http.StatusUnauthorized || rec.Body.String() != "{\"error\":\"invalid or expired session\"}\n" {
			t.Fatalf("invalid session status = %d, body = %q; want 401 with the public session error", rec.Code, rec.Body.String())
		}
	}
}
