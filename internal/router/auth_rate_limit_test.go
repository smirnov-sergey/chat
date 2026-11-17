package router_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"chat/internal/handlers"
	"chat/internal/model/user"
	"chat/internal/service"
	"chat/internal/validation"
)

func TestAuthRoutesRateLimitBeforeService(t *testing.T) {
	for _, tt := range []struct {
		path   string
		body   string
		limit  int
		status int
	}{
		{"/auth/login", `{"username":"ivan","password":"long personal password"}`, 10, http.StatusUnauthorized},
		{"/auth/register", `{"username":"x","name":"Иван","password":"long personal password"}`, 5, http.StatusBadRequest},
	} {
		t.Run(tt.path, func(t *testing.T) {
			calls, logoutCalls := 0, 0
			users := fakeUserService{
				login: func(context.Context, string, string) (*service.UserSession, error) {
					calls++
					return nil, user.ErrInvalidCredentials
				},
				register: func(context.Context, string, string, string) (*service.UserSession, error) {
					calls++
					return nil, validation.ErrInvalidUsername
				},
				logout: func(context.Context, string) error {
					logoutCalls++
					return nil
				},
			}
			h := newTestHTTPHandler(handlers.NewUserHandler(users, false), unexpectedSessionAuthenticator(t))
			for range tt.limit + 1 {
				req := httptest.NewRequest(http.MethodPost, tt.path, strings.NewReader(tt.body))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Origin", "https://evil.example")
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, req)
				if rec.Code != http.StatusForbidden || calls != 0 {
					t.Fatal("CSRF protection must reject cross-origin requests before counting attempts or invoking the service")
				}
			}
			for i := 0; i <= tt.limit; i++ {
				req := httptest.NewRequest(http.MethodPost, tt.path, strings.NewReader(tt.body))
				req.Header.Set("Content-Type", "application/json")
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, req)
				if i < tt.limit {
					if rec.Code != tt.status || calls != i+1 {
						t.Fatalf("request %d: status = %d, service calls = %d; want %d, %d", i+1, rec.Code, calls, tt.status, i+1)
					}
				} else if rec.Code != http.StatusTooManyRequests || calls != tt.limit || rec.Header().Get("Retry-After") == "" {
					t.Fatalf("blocked request: status = %d, service calls = %d; want 429 without another service call", rec.Code, calls)
				}
			}
			req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusNoContent || logoutCalls != 1 {
				t.Fatal("logout must remain available after the authentication quota is exhausted")
			}
		})
	}
}
