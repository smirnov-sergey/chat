package router_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"chat/internal/handlers"
	"chat/internal/model/session"
)

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
