package handlers_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"chat/internal/handlers"
	"chat/internal/model/session"
	"chat/internal/model/user"
	"chat/internal/service"
)

type fakeUserLoginService func(context.Context, string, string) (*service.UserSession, error)

func (fake fakeUserLoginService) Login(ctx context.Context, username, password string) (*service.UserSession, error) {
	return fake(ctx, username, password)
}

var (
	_ handlers.UserLoginService = fakeUserLoginService(nil)
	_ handlers.UserLoginService = (*service.UserService)(nil)
)

const loginBody = `{"username":" IVAN ","password":"  LongPassword123!  "}`

func TestUserLoginHandler(t *testing.T) {
	for _, secure := range []bool{false, true} {
		t.Run(fmt.Sprintf("secure_%v", secure), func(t *testing.T) {
			now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
			localTime := now.In(time.FixedZone("UTC+3", 3*60*60))
			const token = "test-session-token"
			req := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(loginBody))
			req.Header.Set("Content-Type", "application/json; charset=utf-8")
			calls := 0
			login := fakeUserLoginService(func(ctx context.Context, username, password string) (*service.UserSession, error) {
				calls++
				if ctx != req.Context() || username != " IVAN " || password != "  LongPassword123!  " {
					t.Fatal("service must receive the request context and unchanged input fields")
				}
				return &service.UserSession{
					User:  &user.User{ID: 42, Username: "ivan", Name: "Иван", PasswordHash: "private-password-hash", CreatedAt: localTime, UpdatedAt: localTime},
					Token: &session.Token{Value: token, ExpiresAt: now.Add(7 * 24 * time.Hour)},
				}, nil
			})
			rec := httptest.NewRecorder()
			handlers.NewUserLoginHandler(login, secure).Login(rec, req)
			if calls != 1 || rec.Code != http.StatusOK {
				t.Fatalf("calls = %d, status = %d; want 1, 200", calls, rec.Code)
			}
			if rec.Header().Get("Content-Type") != "application/json" || rec.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("expected a JSON response with caching disabled")
			}
			var body map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if len(body) != 5 || body["id"] != float64(42) || body["username"] != "ivan" || body["name"] != "Иван" ||
				body["created_at"] != now.Format(time.RFC3339) || body["updated_at"] != now.Format(time.RFC3339) {
				t.Fatalf("unexpected user response: %v", body)
			}
			if strings.Contains(rec.Body.String(), token) || strings.Contains(rec.Body.String(), "LongPassword") || strings.Contains(rec.Body.String(), "private-password-hash") {
				t.Fatal("JSON must not contain the session token, password or password hash")
			}
			response := rec.Result()
			defer response.Body.Close()
			cookies := response.Cookies()
			if len(cookies) != 1 {
				t.Fatalf("cookie count = %d, want 1", len(cookies))
			}
			cookie := cookies[0]
			if cookie.Name != "chat_session" || cookie.Value != token || cookie.Path != "/" || cookie.Domain != "" ||
				!cookie.HttpOnly || cookie.Secure != secure || cookie.SameSite != http.SameSiteLaxMode ||
				!cookie.Expires.Equal(now.Add(7*24*time.Hour)) {
				t.Fatalf("unexpected session cookie: %+v", cookie)
			}
		})
	}
}

func TestUserLoginHandlerRejectsInvalidRequests(t *testing.T) {
	for _, tt := range []struct {
		name        string
		contentType string
		body        string
		status      int
	}{
		{"missing content type", "", loginBody, http.StatusUnsupportedMediaType},
		{"unsupported content type", "text/plain", loginBody, http.StatusUnsupportedMediaType},
		{"malformed content type", "application/json; charset", loginBody, http.StatusUnsupportedMediaType},
		{"empty body", "application/json", "", http.StatusBadRequest},
		{"malformed JSON", "application/json", "{", http.StatusBadRequest},
		{"null", "application/json", "null", http.StatusBadRequest},
		{"array", "application/json", "[]", http.StatusBadRequest},
		{"wrong field type", "application/json", `{"username":42}`, http.StatusBadRequest},
		{"unknown field", "application/json", `{"username":"ivan","admin":true}`, http.StatusBadRequest},
		{"multiple JSON values", "application/json", loginBody + `{}`, http.StatusBadRequest},
		{"trailing garbage", "application/json", loginBody + `garbage`, http.StatusBadRequest},
		{"oversized body", "application/json", strings.Repeat(" ", 4097) + loginBody, http.StatusRequestEntityTooLarge},
		{"oversized trailing whitespace", "application/json", loginBody + strings.Repeat(" ", 4097), http.StatusRequestEntityTooLarge},
	} {
		t.Run(tt.name, func(t *testing.T) {
			login := fakeUserLoginService(func(context.Context, string, string) (*service.UserSession, error) {
				t.Fatal("service must not be called for invalid requests")
				return nil, nil
			})
			req := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(tt.body))
			if tt.contentType != "" {
				req.Header.Set("Content-Type", tt.contentType)
			}
			rec := httptest.NewRecorder()
			handlers.NewUserLoginHandler(login, true).Login(rec, req)
			if rec.Code != tt.status {
				t.Fatalf("status = %d, want %d", rec.Code, tt.status)
			}
			assertLoginError(t, rec)
		})
	}
}

func TestUserLoginHandlerServiceErrors(t *testing.T) {
	for _, tt := range []struct {
		name    string
		err     error
		status  int
		message string
	}{
		{"invalid credentials", user.ErrInvalidCredentials, http.StatusUnauthorized, user.ErrInvalidCredentials.Error()},
		{"unexpected error", errors.New("private database error"), http.StatusInternalServerError, "internal server error"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			login := fakeUserLoginService(func(context.Context, string, string) (*service.UserSession, error) {
				return nil, fmt.Errorf("login failed: %w", tt.err)
			})
			req := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(loginBody))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			handlers.NewUserLoginHandler(login, true).Login(rec, req)
			if rec.Code != tt.status {
				t.Fatalf("status = %d, want %d", rec.Code, tt.status)
			}
			body := assertLoginError(t, rec)
			if body.Error != tt.message {
				t.Fatalf("error message = %q, want %q", body.Error, tt.message)
			}
		})
	}
}

func assertLoginError(t *testing.T, rec *httptest.ResponseRecorder) struct{ Error string } {
	t.Helper()
	if rec.Header().Get("Content-Type") != "application/json" || rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("Set-Cookie") != "" {
		t.Fatal("errors must be JSON with caching disabled and no session cookie")
	}
	var body struct{ Error string }
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Error == "" {
		t.Fatalf("expected a JSON error response, got %q", rec.Body.String())
	}
	return body
}
