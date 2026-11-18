//go:build integration

package integration_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"chat/internal/handlers"
	"chat/internal/migrate"
	"chat/internal/model/session"
	"chat/internal/password"
	"chat/internal/repository"
	"chat/internal/router"
	"chat/internal/service"
)

func TestUserLogoutHTTP(t *testing.T) {
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
	const lifetime = 7 * 24 * time.Hour
	sessions, err := service.NewSessionService(repository.NewSessionRepository(db), lifetime)
	if err != nil {
		t.Fatal(err)
	}
	users, err := service.NewUserService(repository.NewUserRepository(db), repository.NewUserRegistrationStore(db), password.Argon2id{}, sessions, nil, nil, lifetime)
	if err != nil {
		t.Fatal(err)
	}
	registered, err := users.Register(ctx, "ivan", "Иван", "long personal password")
	if err != nil {
		t.Fatal(err)
	}
	other, err := sessions.Create(ctx, registered.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	appRouter := router.New(handlers.NewUserHandler(users, false, 5*1024*1024))
	crossOriginProtectionMiddleware := handlers.CrossOriginProtectionMiddleware()
	authRateLimitMiddleware := handlers.AuthRateLimitMiddleware()
	sessionMiddleware := handlers.SessionMiddleware(sessions)
	h := crossOriginProtectionMiddleware(authRateLimitMiddleware(sessionMiddleware(appRouter)))
	request := func(method, path, token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil).WithContext(ctx)
		if token != "" {
			req.AddCookie(&http.Cookie{Name: "chat_session", Value: token})
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	assertLogout := func(token string) {
		t.Helper()
		rec := request(http.MethodPost, "/auth/logout", token)
		if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 || rec.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("logout status = %d, body = %q; want 204 with no body and caching disabled", rec.Code, rec.Body.String())
		}
		response := rec.Result()
		defer response.Body.Close()
		cookies := response.Cookies()
		if len(cookies) != 1 || cookies[0].Name != "chat_session" || cookies[0].Value != "" || cookies[0].MaxAge != -1 || cookies[0].Path != "/" {
			t.Fatal("successful logout must delete the browser's session cookie")
		}
	}
	assertLogout(registered.Token.Value)
	if _, err := sessions.Authenticate(ctx, registered.Token.Value); !errors.Is(err, session.ErrInvalid) {
		t.Fatalf("logged-out token authentication error = %v, want ErrInvalid", err)
	}
	profilePath := "/users/" + strconv.FormatInt(registered.User.ID, 10)
	if rec := request(http.MethodGet, profilePath, registered.Token.Value); rec.Code != http.StatusUnauthorized {
		t.Fatalf("logged-out token profile status = %d, want 401", rec.Code)
	}
	if rec := request(http.MethodGet, profilePath, other.Value); rec.Code != http.StatusOK {
		t.Fatalf("other session profile status = %d, want 200", rec.Code)
	}
	for _, token := range []string{registered.Token.Value, "malformed", strings.Repeat("A", 43), ""} {
		assertLogout(token)
	}
	expired, err := sessions.Create(ctx, registered.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := sessions.Authenticate(ctx, expired.Value)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "UPDATE sessions SET expires_at = created_at + INTERVAL '1 microsecond' WHERE token_hash = $1", stored.TokenHash); err != nil {
		t.Fatal(err)
	}
	assertLogout(expired.Value)
	var count int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM sessions WHERE user_id = $1", registered.User.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("remaining sessions = %d, want only the other session", count)
	}
}
