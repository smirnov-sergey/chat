package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"chat/internal/model/session"
	"chat/internal/service"
)

type fakeSessionAuthenticator func(context.Context, string) (*session.Session, error)

func (fake fakeSessionAuthenticator) Authenticate(ctx context.Context, token string) (*session.Session, error) {
	return fake(ctx, token)
}

var (
	_ SessionAuthenticator = fakeSessionAuthenticator(nil)
	_ SessionAuthenticator = (*service.SessionService)(nil)
)

func TestRequireSession(t *testing.T) {
	type traceKey struct{}
	ctx := context.WithValue(t.Context(), traceKey{}, "request-trace")
	req := httptest.NewRequest(http.MethodGet, "/me", nil).WithContext(ctx)
	req.AddCookie(&http.Cookie{Name: "chat_session", Value: "raw-token"})
	req.Header.Set("X-User-ID", "999")
	authCalls, nextCalls := 0, 0
	sessions := fakeSessionAuthenticator(func(gotCtx context.Context, token string) (*session.Session, error) {
		authCalls++
		if gotCtx != ctx || token != "raw-token" {
			t.Fatal("authentication must receive the request context and cookie value")
		}
		return &session.Session{UserID: 42}, nil
	})
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalls++
		id, ok := userIDFromContext(r.Context())
		if !ok || id != 42 {
			t.Fatal("handler must receive the user ID from the verified session")
		}
		if r.Context().Value(traceKey{}) != "request-trace" {
			t.Fatal("middleware must preserve existing context values")
		}
		w.WriteHeader(http.StatusNoContent)
	})
	rec := httptest.NewRecorder()
	RequireSession(sessions)(next).ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent || authCalls != 1 || nextCalls != 1 {
		t.Fatalf("status = %d, authentication calls = %d, handler calls = %d; want 204, 1, 1", rec.Code, authCalls, nextCalls)
	}
	if rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("Set-Cookie") != "" {
		t.Fatal("authentication must disable caching without issuing or extending a cookie")
	}
	if _, ok := userIDFromContext(req.Context()); ok {
		t.Fatal("middleware must not modify the original request context")
	}
}

func TestRequireSessionRejectsRequests(t *testing.T) {
	for _, tt := range []struct {
		name      string
		cookie    *http.Cookie
		err       error
		wantCalls int
		status    int
		message   string
	}{
		{"missing cookie", nil, nil, 0, http.StatusUnauthorized, session.ErrInvalid.Error()},
		{"empty cookie", &http.Cookie{Name: "chat_session"}, nil, 0, http.StatusUnauthorized, session.ErrInvalid.Error()},
		{"unrelated cookie", &http.Cookie{Name: "other", Value: "token"}, nil, 0, http.StatusUnauthorized, session.ErrInvalid.Error()},
		{"malformed token", &http.Cookie{Name: "chat_session", Value: "malformed"}, session.ErrInvalid, 1, http.StatusUnauthorized, session.ErrInvalid.Error()},
		{"expired or revoked session", &http.Cookie{Name: "chat_session", Value: "expired-token"}, fmt.Errorf("authenticate: %w", session.ErrInvalid), 1, http.StatusUnauthorized, session.ErrInvalid.Error()},
		{"storage error", &http.Cookie{Name: "chat_session", Value: "raw-token"}, errors.New("private database error"), 1, http.StatusInternalServerError, "internal server error"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			sessions := fakeSessionAuthenticator(func(context.Context, string) (*session.Session, error) {
				calls++
				return nil, tt.err
			})
			req := httptest.NewRequest(http.MethodGet, "/me", nil)
			req.Header.Set("X-User-ID", "42")
			if tt.cookie != nil {
				req.AddCookie(tt.cookie)
			}
			next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				t.Fatal("rejected requests must not reach the protected handler")
			})
			rec := httptest.NewRecorder()
			RequireSession(sessions)(next).ServeHTTP(rec, req)
			if rec.Code != tt.status || calls != tt.wantCalls {
				t.Fatalf("status = %d, authentication calls = %d; want %d, %d", rec.Code, calls, tt.status, tt.wantCalls)
			}
			if rec.Header().Get("Content-Type") != "application/json" || rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("Set-Cookie") != "" {
				t.Fatal("rejection must be JSON with caching disabled and no session cookie")
			}
			var body struct{ Error string }
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body.Error != tt.message {
				t.Fatalf("error = %q, want %q", body.Error, tt.message)
			}
		})
	}
}

func TestUserIDFromContextWithoutSession(t *testing.T) {
	id, ok := userIDFromContext(t.Context())
	if id != 0 || ok {
		t.Fatalf("UserIDFromContext = (%d, %v), want (0, false)", id, ok)
	}
}
