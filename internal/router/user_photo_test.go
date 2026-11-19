package router_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"chat/internal/handlers"
	"chat/internal/model/session"
	"chat/internal/model/user"
)

func TestUserPhotoReadRoute(t *testing.T) {
	for _, tt := range []struct {
		name          string
		path          string
		authenticated bool
		serviceError  error
		status        int
		wantID        int64
	}{
		{"own photo", "/users/42/photo", true, nil, http.StatusOK, 42},
		{"another user's photo", "/users/43/photo", true, nil, http.StatusOK, 43},
		{"missing session", "/users/42/photo", false, nil, http.StatusUnauthorized, 0},
		{"invalid ID", "/users/invalid/photo", true, nil, http.StatusBadRequest, 0},
		{"missing photo", "/users/42/photo", true, user.ErrPhotoNotFound, http.StatusNotFound, 42},
		{"missing user", "/users/42/photo", true, user.ErrNotFound, http.StatusNotFound, 42},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			users := fakeUserService{getPhoto: func(ctx context.Context, id int64) ([]byte, error) {
				calls++
				if id != tt.wantID {
					t.Fatalf("photo user ID=%d, want %d", id, tt.wantID)
				}
				return []byte("photo bytes"), tt.serviceError
			}}
			sessions := fakeSessionAuthenticator(func(context.Context, string) (*session.Session, error) {
				return &session.Session{UserID: 42}, nil
			})
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			if tt.authenticated {
				req.AddCookie(&http.Cookie{Name: "chat_session", Value: "session-token"})
			}
			rec := httptest.NewRecorder()
			newTestHTTPHandler(handlers.NewUserHandler(users, false, 1024), sessions).ServeHTTP(rec, req)
			if rec.Code != tt.status {
				t.Fatalf("status=%d, want %d; body=%s", rec.Code, tt.status, rec.Body.String())
			}
			wantCalls := 0
			if tt.wantID != 0 {
				wantCalls = 1
			}
			if calls != wantCalls {
				t.Fatalf("service calls=%d, want %d", calls, wantCalls)
			}
			if tt.status == http.StatusOK && (rec.Body.String() != "photo bytes" || rec.Header().Get("Content-Type") != "image/png") {
				t.Fatal("route must return the photo bytes as PNG")
			}
		})
	}
}

func TestUserPhotoDeleteRoute(t *testing.T) {
	for _, tt := range []struct {
		name          string
		path          string
		authenticated bool
		crossOrigin   bool
		status        int
	}{
		{"own photo", "/users/42/photo", true, false, http.StatusNoContent},
		{"missing session", "/users/42/photo", false, false, http.StatusUnauthorized},
		{"another user", "/users/43/photo", true, false, http.StatusForbidden},
		{"cross origin", "/users/42/photo", true, true, http.StatusForbidden},
		{"invalid ID", "/users/invalid/photo", true, false, http.StatusBadRequest},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			users := fakeUserService{deletePhoto: func(ctx context.Context, id int64) error {
				calls++
				if id != 42 {
					t.Fatalf("photo user ID=%d, want 42", id)
				}
				return nil
			}}
			sessions := fakeSessionAuthenticator(func(context.Context, string) (*session.Session, error) {
				return &session.Session{UserID: 42}, nil
			})
			req := httptest.NewRequest(http.MethodDelete, tt.path, nil)
			if tt.authenticated {
				req.AddCookie(&http.Cookie{Name: "chat_session", Value: "session-token"})
			}
			if tt.crossOrigin {
				req.Header.Set("Origin", "https://other.example")
				req.Header.Set("Sec-Fetch-Site", "cross-site")
			}
			rec := httptest.NewRecorder()
			newTestHTTPHandler(handlers.NewUserHandler(users, false, 1024), sessions).ServeHTTP(rec, req)
			if rec.Code != tt.status {
				t.Fatalf("status=%d, want %d; body=%s", rec.Code, tt.status, rec.Body.String())
			}
			wantCalls := 0
			if tt.status == http.StatusNoContent {
				wantCalls = 1
				if rec.Body.Len() != 0 {
					t.Fatal("successful deletion must have no response body")
				}
			}
			if calls != wantCalls {
				t.Fatalf("service calls=%d, want %d", calls, wantCalls)
			}
		})
	}
}

func TestUserPhotoUploadRoute(t *testing.T) {
	for _, tt := range []struct {
		name          string
		method        string
		path          string
		authenticated bool
		crossOrigin   bool
		status        int
	}{
		{"own photo", http.MethodPut, "/users/42/photo", true, false, http.StatusNoContent},
		{"missing session", http.MethodPut, "/users/42/photo", false, false, http.StatusUnauthorized},
		{"another user", http.MethodPut, "/users/43/photo", true, false, http.StatusForbidden},
		{"cross origin", http.MethodPut, "/users/42/photo", true, true, http.StatusForbidden},
		{"unsupported method", http.MethodPost, "/users/42/photo", true, false, http.StatusMethodNotAllowed},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			users := fakeUserService{uploadPhoto: func(ctx context.Context, id int64, data []byte) error {
				calls++
				if id != 42 || string(data) != "photo bytes" {
					t.Fatal("route must pass the parsed user ID and unchanged file bytes to the service")
				}
				return nil
			}}
			sessions := fakeSessionAuthenticator(func(context.Context, string) (*session.Session, error) {
				return &session.Session{UserID: 42}, nil
			})
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader("photo bytes"))
			req.Header.Set("Content-Type", "image/png")
			if tt.authenticated {
				req.AddCookie(&http.Cookie{Name: "chat_session", Value: "session-token"})
			}
			if tt.crossOrigin {
				req.Header.Set("Origin", "https://other.example")
				req.Header.Set("Sec-Fetch-Site", "cross-site")
			}
			rec := httptest.NewRecorder()
			newTestHTTPHandler(handlers.NewUserHandler(users, false, 1024), sessions).ServeHTTP(rec, req)
			if rec.Code != tt.status {
				t.Fatalf("status=%d, want %d; body=%s", rec.Code, tt.status, rec.Body.String())
			}
			wantCalls := 0
			if tt.status == http.StatusNoContent {
				wantCalls = 1
			}
			if calls != wantCalls {
				t.Fatalf("service calls=%d, want %d", calls, wantCalls)
			}
		})
	}
}
