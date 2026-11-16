package router_test

import (
	"context"
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

type fakeUserService struct {
	logout   func(context.Context, string) error
	getByID  func(context.Context, int64) (*user.User, error)
	register func(context.Context, string, string, string) (*service.UserSession, error)
	login    func(context.Context, string, string) (*service.UserSession, error)
}

func (fake fakeUserService) Logout(ctx context.Context, token string) error {
	return fake.logout(ctx, token)
}

func (fake fakeUserService) GetByID(ctx context.Context, id int64) (*user.User, error) {
	return fake.getByID(ctx, id)
}

func (fake fakeUserService) Register(ctx context.Context, username, name, password string) (*service.UserSession, error) {
	return fake.register(ctx, username, name, password)
}

func (fake fakeUserService) Login(ctx context.Context, username, password string) (*service.UserSession, error) {
	return fake.login(ctx, username, password)
}

var _ handlers.UserService = fakeUserService{}

func newRouter(t *testing.T) http.Handler {
	t.Helper()
	users := fakeUserService{register: func(context.Context, string, string, string) (*service.UserSession, error) {
		t.Fatal("registration must not be called for this request")
		return nil, nil
	}, login: func(context.Context, string, string) (*service.UserSession, error) {
		t.Fatal("login must not be called for this request")
		return nil, nil
	}}
	return router.New(handlers.NewUserHandler(users, true))
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
		{"old profile path", http.MethodGet, "/me", http.StatusNotFound},
		{"unsupported method", http.MethodPost, "/health", http.StatusMethodNotAllowed},
		{"registration requires POST", http.MethodGet, "/auth/register", http.StatusMethodNotAllowed},
		{"login requires POST", http.MethodGet, "/auth/login", http.StatusMethodNotAllowed},
		{"logout requires POST", http.MethodGet, "/auth/logout", http.StatusMethodNotAllowed},
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
			registration := fakeUserService{register: func(context.Context, string, string, string) (*service.UserSession, error) {
				calls++
				return &service.UserSession{
					User:  &user.User{ID: 42, Username: "ivan", Name: "Иван"},
					Token: &session.Token{Value: "test-token", ExpiresAt: time.Now().Add(time.Hour)},
				}, nil
			}}
			h := router.New(handlers.NewUserHandler(registration, true))
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

func TestLoginRoute(t *testing.T) {
	for _, origin := range []string{"", "http://chat.local"} {
		t.Run(origin, func(t *testing.T) {
			calls := 0
			users := fakeUserService{login: func(_ context.Context, username, password string) (*service.UserSession, error) {
				calls++
				if username != "ivan" || password != "long password phrase" {
					t.Fatal("login route must pass the submitted credentials to the service")
				}
				return &service.UserSession{
					User:  &user.User{ID: 42, Username: "ivan", Name: "Иван"},
					Token: &session.Token{Value: "test-token", ExpiresAt: time.Now().Add(time.Hour)},
				}, nil
			}}
			h := router.New(handlers.NewUserHandler(users, true))
			req := httptest.NewRequest(http.MethodPost, "http://chat.local/auth/login",
				strings.NewReader(`{"username":"ivan","password":"long password phrase"}`))
			req.Header.Set("Content-Type", "application/json")
			if origin != "" {
				req.Header.Set("Origin", origin)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK || calls != 1 || rec.Header().Get("Set-Cookie") == "" {
				t.Fatalf("status = %d, service calls = %d; want 200, 1 and a session cookie", rec.Code, calls)
			}
		})
	}
}

func TestUserProfileRouteRequiresVerifiedIdentity(t *testing.T) {
	h := router.New(handlers.NewUserHandler(fakeUserService{}, true))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/users/42", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 from the profile handler without a verified identity", rec.Code)
	}
}
