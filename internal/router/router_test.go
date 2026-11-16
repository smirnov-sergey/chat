package router_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"chat/internal/handlers"
	"chat/internal/model/session"
	"chat/internal/model/user"
	"chat/internal/router"
	"chat/internal/service"
)

type fakeUserRegistrationService func(context.Context, string, string, string) (*service.UserSession, error)

func (fake fakeUserRegistrationService) Register(ctx context.Context, username, name, password string) (*service.UserSession, error) {
	return fake(ctx, username, name, password)
}

func newRouter(t *testing.T) http.Handler {
	t.Helper()
	registration := fakeUserRegistrationService(func(context.Context, string, string, string) (*service.UserSession, error) {
		t.Fatal("registration must not be called for this request")
		return nil, nil
	})
	return router.New(handlers.NewUserRegistrationHandler(registration, true))
}

func TestHealth(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	newRouter(t).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}

	if got := rec.Body.String(); got != "{\"status\":\"ok\"}\n" {
		t.Errorf("body = %q, want status ok JSON", got)
	}
}

func TestRouterRejectsUnsupportedRequests(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
		status int
	}{
		{"unknown path", http.MethodGet, "/missing", http.StatusNotFound},
		{"unsupported method", http.MethodPost, "/health", http.StatusMethodNotAllowed},
		{"registration requires POST", http.MethodGet, "/auth/register", http.StatusMethodNotAllowed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			rec := httptest.NewRecorder()

			newRouter(t).ServeHTTP(rec, req)

			if rec.Code != tt.status {
				t.Errorf("status = %d, want %d", rec.Code, tt.status)
			}
		})
	}
}

func TestRegistrationRoute(t *testing.T) {
	for _, origin := range []string{"", "http://chat.local"} {
		t.Run(origin, func(t *testing.T) {
			calls := 0
			registration := fakeUserRegistrationService(func(context.Context, string, string, string) (*service.UserSession, error) {
				calls++
				return &service.UserSession{
					User:  &user.User{ID: 42, Username: "ivan", Name: "Иван"},
					Token: &session.Token{Value: "test-token", ExpiresAt: time.Now().Add(time.Hour)},
				}, nil
			})
			h := router.New(handlers.NewUserRegistrationHandler(registration, true))
			req := httptest.NewRequest(http.MethodPost, "http://chat.local/auth/register",
				strings.NewReader(`{"username":"ivan","name":"Иван","password":"long password phrase"}`))
			req.Header.Set("Content-Type", "application/json")
			if origin != "" {
				req.Header.Set("Origin", origin)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusCreated || calls != 1 {
				t.Fatalf("status = %d, service calls = %d; want 201, 1", rec.Code, calls)
			}
		})
	}
}

func TestRegistrationRejectsCrossOrigin(t *testing.T) {
	for _, headers := range []map[string]string{
		{"Origin": "https://evil.example"},
		{"Origin": "null"},
		{"Sec-Fetch-Site": "cross-site"},
	} {
		req := httptest.NewRequest(http.MethodPost, "http://chat.local/auth/register", strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		for name, value := range headers {
			req.Header.Set(name, value)
		}
		rec := httptest.NewRecorder()
		newRouter(t).ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden || rec.Header().Get("Content-Type") != "application/json" || rec.Header().Get("Set-Cookie") != "" {
			t.Fatalf("cross-origin response status = %d, headers = %v; want JSON 403 without cookie", rec.Code, rec.Header())
		}
		var body struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Error != "cross-origin request blocked" {
			t.Fatalf("unexpected rejection body: %q", rec.Body.String())
		}
	}
}
