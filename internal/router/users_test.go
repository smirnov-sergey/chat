package router_test

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
	"chat/internal/router"
)

type fakeSessionAuthenticator func(context.Context, string) (*session.Session, error)

func (fake fakeSessionAuthenticator) Authenticate(ctx context.Context, token string) (*session.Session, error) {
	return fake(ctx, token)
}

var _ handlers.SessionAuthenticator = fakeSessionAuthenticator(nil)

func unexpectedSessionAuthenticator(t *testing.T) fakeSessionAuthenticator {
	t.Helper()
	return func(context.Context, string) (*session.Session, error) {
		t.Fatal("public routes must not authenticate a session")
		return nil, nil
	}
}

func TestUserProfileHTTP(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/users/42?user_id=999", nil)
	req.Header.Set("X-User-ID", "999")
	req.AddCookie(&http.Cookie{Name: "chat_session", Value: "session-token"})
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	authCalls, lookupCalls := 0, 0
	sessions := fakeSessionAuthenticator(func(ctx context.Context, token string) (*session.Session, error) {
		authCalls++
		if ctx != req.Context() || token != "session-token" {
			t.Fatal("middleware must authenticate the request cookie using the request context")
		}
		return &session.Session{UserID: 42}, nil
	})
	users := fakeUserService{getByID: func(ctx context.Context, id int64) (*user.User, error) {
		lookupCalls++
		if id != 42 || ctx == req.Context() {
			t.Fatal("profile lookup must use the verified user ID and middleware context")
		}
		return &user.User{ID: 42, Username: "ivan", Name: "Иван", PasswordHash: "private-hash", CreatedAt: now, UpdatedAt: now}, nil
	}}
	rec := httptest.NewRecorder()
	newTestHTTPHandler(handlers.NewUserHandler(users, true), sessions).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || authCalls != 1 || lookupCalls != 1 {
		t.Fatalf("status = %d, authentication calls = %d, lookups = %d; want 200, 1, 1", rec.Code, authCalls, lookupCalls)
	}
	var profile map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &profile); err != nil {
		t.Fatal(err)
	}
	if len(profile) != 5 || profile["id"] != float64(42) || profile["username"] != "ivan" || profile["name"] != "Иван" ||
		profile["created_at"] != now.Format(time.RFC3339) || profile["updated_at"] != now.Format(time.RFC3339) {
		t.Fatalf("unexpected profile: %v", profile)
	}
	if strings.Contains(rec.Body.String(), "private-hash") || strings.Contains(rec.Body.String(), "session-token") {
		t.Fatal("profile must not expose credentials or session tokens")
	}
	if rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("Set-Cookie") != "" {
		t.Fatal("profile requests must disable caching without issuing or extending cookies")
	}
}

func TestUserProfileHTTPRejectsUnauthenticatedRequests(t *testing.T) {
	for _, tt := range []struct {
		name   string
		cookie *http.Cookie
		err    error
	}{
		{"missing cookie", nil, nil},
		{"invalid session", &http.Cookie{Name: "chat_session", Value: "invalid"}, session.ErrInvalid},
	} {
		t.Run(tt.name, func(t *testing.T) {
			users := fakeUserService{getByID: func(context.Context, int64) (*user.User, error) {
				t.Fatal("unauthenticated request must not read a profile")
				return nil, nil
			}}
			sessions := fakeSessionAuthenticator(func(context.Context, string) (*session.Session, error) {
				return nil, tt.err
			})
			req := httptest.NewRequest(http.MethodGet, "/users/42", nil)
			if tt.cookie != nil {
				req.AddCookie(tt.cookie)
			}
			rec := httptest.NewRecorder()
			newTestHTTPHandler(handlers.NewUserHandler(users, true), sessions).ServeHTTP(rec, req)
			if rec.Code != http.StatusUnauthorized || rec.Body.String() != "{\"error\":\"invalid or expired session\"}\n" {
				t.Fatalf("status = %d, body = %q; want 401 with the public session error", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestUserProfileHTTPErrors(t *testing.T) {
	for _, tt := range []struct {
		name        string
		authError   error
		lookupError error
		status      int
		message     string
	}{
		{"authentication storage error", errors.New("private auth error"), nil, http.StatusInternalServerError, "internal server error"},
		{"missing user", nil, fmt.Errorf("lookup: %w", user.ErrNotFound), http.StatusNotFound, user.ErrNotFound.Error()},
		{"profile storage error", nil, errors.New("private lookup error"), http.StatusInternalServerError, "internal server error"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			sessions := fakeSessionAuthenticator(func(context.Context, string) (*session.Session, error) {
				if tt.authError != nil {
					return nil, tt.authError
				}
				return &session.Session{UserID: 42}, nil
			})
			users := fakeUserService{getByID: func(context.Context, int64) (*user.User, error) {
				if tt.authError != nil {
					t.Fatal("authentication failure must stop the profile lookup")
				}
				return nil, tt.lookupError
			}}
			req := httptest.NewRequest(http.MethodGet, "/users/42", nil)
			req.AddCookie(&http.Cookie{Name: "chat_session", Value: "session-token"})
			rec := httptest.NewRecorder()
			newTestHTTPHandler(handlers.NewUserHandler(users, true), sessions).ServeHTTP(rec, req)
			var body struct{ Error string }
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if rec.Code != tt.status || body.Error != tt.message {
				t.Fatalf("status = %d, error = %q; want %d, %q", rec.Code, body.Error, tt.status, tt.message)
			}
		})
	}
}

func TestAuthRoutesRejectCrossOrigin(t *testing.T) {
	for _, path := range []string{"/auth/register", "/auth/login", "/auth/logout"} {
		t.Run(path, func(t *testing.T) {
			for _, headers := range []map[string]string{
				{"Origin": "https://evil.example"},
				{"Origin": "null"},
				{"Sec-Fetch-Site": "cross-site"},
			} {
				req := httptest.NewRequest(http.MethodPost, "http://chat.local"+path, strings.NewReader(`{}`))
				req.Header.Set("Content-Type", "application/json")
				for name, value := range headers {
					req.Header.Set(name, value)
				}
				rec := httptest.NewRecorder()
				newTestHTTPHandler(handlers.NewUserHandler(fakeUserService{}, true), unexpectedSessionAuthenticator(t)).ServeHTTP(rec, req)
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
		})
	}
}

func TestPublicRoutesDoNotAuthenticateSessions(t *testing.T) {
	users := fakeUserService{logout: func(context.Context, string) error { return nil }}
	h := newTestHTTPHandler(handlers.NewUserHandler(users, true), unexpectedSessionAuthenticator(t))
	for _, tt := range []struct {
		method string
		path   string
		status int
	}{
		{http.MethodGet, "/health", http.StatusOK},
		{http.MethodPost, "/auth/register", http.StatusBadRequest},
		{http.MethodPost, "/auth/login", http.StatusBadRequest},
		{http.MethodPost, "/auth/logout", http.StatusNoContent},
	} {
		t.Run(tt.path, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader("{"))
			req.Header.Set("Content-Type", "application/json")
			req.AddCookie(&http.Cookie{Name: "chat_session", Value: "expired-token"})
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tt.status {
				t.Fatalf("public route status = %d, want %d", rec.Code, tt.status)
			}
		})
	}
}

func TestUserProfileIDAccess(t *testing.T) {
	for _, tt := range []struct {
		id      string
		status  int
		message string
	}{
		{"0", http.StatusBadRequest, "invalid user ID"},
		{"-1", http.StatusBadRequest, "invalid user ID"},
		{"abc", http.StatusBadRequest, "invalid user ID"},
		{"9223372036854775808", http.StatusBadRequest, "invalid user ID"},
		{"999", http.StatusForbidden, "access denied"},
	} {
		t.Run(tt.id, func(t *testing.T) {
			users := fakeUserService{getByID: func(context.Context, int64) (*user.User, error) {
				t.Fatal("invalid or foreign IDs must not reach the user service")
				return nil, nil
			}}
			sessions := fakeSessionAuthenticator(func(context.Context, string) (*session.Session, error) {
				return &session.Session{UserID: 42}, nil
			})
			req := httptest.NewRequest(http.MethodGet, "/users/"+tt.id, nil)
			req.AddCookie(&http.Cookie{Name: "chat_session", Value: "session-token"})
			rec := httptest.NewRecorder()
			newTestHTTPHandler(handlers.NewUserHandler(users, true), sessions).ServeHTTP(rec, req)
			var body struct{ Error string }
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if rec.Code != tt.status || body.Error != tt.message {
				t.Fatalf("status = %d, error = %q; want %d, %q", rec.Code, body.Error, tt.status, tt.message)
			}
		})
	}
}

func newTestHTTPHandler(userHandler *handlers.UserHandler, sessions handlers.SessionAuthenticator) http.Handler {
	appRouter := router.New(userHandler)
	crossOriginProtectionMiddleware := handlers.CrossOriginProtectionMiddleware()
	sessionMiddleware := handlers.SessionMiddleware(sessions)
	return crossOriginProtectionMiddleware(sessionMiddleware(appRouter))
}

func TestUserRouterRejectsUnsupportedMethods(t *testing.T) {
	sessions := fakeSessionAuthenticator(func(context.Context, string) (*session.Session, error) {
		return &session.Session{UserID: 42}, nil
	})
	req := httptest.NewRequest(http.MethodPost, "/users/42", nil)
	req.AddCookie(&http.Cookie{Name: "chat_session", Value: "session-token"})
	rec := httptest.NewRecorder()
	newTestHTTPHandler(handlers.NewUserHandler(fakeUserService{}, true), sessions).ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func TestUnknownPathRequiresSession(t *testing.T) {
	sessions := fakeSessionAuthenticator(func(context.Context, string) (*session.Session, error) {
		return &session.Session{UserID: 42}, nil
	})
	h := newTestHTTPHandler(handlers.NewUserHandler(fakeUserService{}, true), sessions)
	for _, authenticated := range []bool{false, true} {
		t.Run(fmt.Sprintf("authenticated_%v", authenticated), func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/missing", nil)
			want := http.StatusUnauthorized
			if authenticated {
				req.AddCookie(&http.Cookie{Name: "chat_session", Value: "session-token"})
				want = http.StatusNotFound
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != want {
				t.Fatalf("status = %d, want %d", rec.Code, want)
			}
		})
	}
}
