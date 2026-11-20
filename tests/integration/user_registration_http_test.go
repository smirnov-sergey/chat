//go:build integration

package integration_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
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

func TestUserRegistrationHTTP(t *testing.T) {
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
	hasher := password.Argon2id{}
	const sessionLifetime = 7 * 24 * time.Hour
	sessions, err := service.NewSessionService(repository.NewSessionRepository(db), sessionLifetime)
	if err != nil {
		t.Fatal(err)
	}
	users, err := service.NewUserService(repository.NewUserRepository(db), repository.NewUserRegistrationStore(db), hasher, sessions, nil, nil, sessionLifetime)
	if err != nil {
		t.Fatal(err)
	}
	userHandler := handlers.NewUserHandler(users, false, 5*1024*1024)
	appRouter := router.New(userHandler, handlers.NewConversationHandler(nil))
	crossOriginProtectionMiddleware := handlers.CrossOriginProtectionMiddleware()
	authRateLimitMiddleware := handlers.AuthRateLimitMiddleware()
	sessionMiddleware := handlers.SessionMiddleware(sessions)
	h := crossOriginProtectionMiddleware(authRateLimitMiddleware(sessionMiddleware(appRouter)))
	const body = `{"username":" IVAN ","name":" Иван Петров ","password":"long personal password"}`
	register := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/auth/register", strings.NewReader(body)).WithContext(ctx)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	rec := register()
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body: %s", rec.Code, rec.Body.String())
	}
	var profile dto.UserResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &profile); err != nil {
		t.Fatal(err)
	}
	if profile.ID <= 0 || profile.Username != "ivan" || profile.Name != "Иван Петров" {
		t.Fatalf("unexpected registered user: %+v", profile)
	}
	response := rec.Result()
	defer response.Body.Close()
	cookies := response.Cookies()
	if len(cookies) != 1 || cookies[0].Name != "chat_session" || !cookies[0].HttpOnly || cookies[0].Secure {
		t.Fatal("local HTTP registration must issue one HttpOnly session cookie without Secure")
	}
	rawToken, err := base64.RawURLEncoding.Strict().DecodeString(cookies[0].Value)
	if err != nil || len(rawToken) != 32 {
		t.Fatal("expected a session token containing 32 random bytes")
	}
	tokenHash := sha256.Sum256(rawToken)
	var storedPasswordHash string
	var createdAt, expiresAt time.Time
	if err := db.QueryRowContext(ctx, `
		SELECT users.password_hash, sessions.created_at, sessions.expires_at
		FROM users JOIN sessions ON sessions.user_id = users.id
		WHERE users.id = $1 AND sessions.token_hash = $2`, profile.ID, tokenHash[:]).
		Scan(&storedPasswordHash, &createdAt, &expiresAt); err != nil {
		t.Fatal(err)
	}
	if matches, err := hasher.Verify("long personal password", storedPasswordHash); err != nil || !matches {
		t.Fatalf("stored password verification = (%v, %v), want (true, nil)", matches, err)
	}
	if expiresAt.Sub(createdAt) != 7*24*time.Hour || cookies[0].Expires.Sub(expiresAt).Abs() > time.Second {
		t.Fatal("stored session and cookie must expire seven days after creation")
	}
	if strings.Contains(rec.Body.String(), cookies[0].Value) || strings.Contains(rec.Body.String(), "long personal password") {
		t.Fatal("JSON must not contain the session token or password")
	}
	duplicate := register()
	if duplicate.Code != http.StatusConflict || duplicate.Header().Get("Set-Cookie") != "" {
		t.Fatalf("duplicate registration status = %d, want 409 without a new cookie", duplicate.Code)
	}
	var sessionCount int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM sessions WHERE user_id = $1", profile.ID).Scan(&sessionCount); err != nil {
		t.Fatal(err)
	}
	if sessionCount != 1 {
		t.Fatalf("session count after duplicate registration = %d, want 1", sessionCount)
	}
}
