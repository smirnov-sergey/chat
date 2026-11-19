package handlers_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"chat/internal/handlers"
	"chat/internal/model/user"
)

func TestUserHandlerDeletePhoto(t *testing.T) {
	type traceKey struct{}
	ctx := context.WithValue(t.Context(), traceKey{}, "delete-trace")
	calls := 0
	users := fakeUserService{deletePhoto: func(gotCtx context.Context, id int64) error {
		calls++
		if id != 42 || gotCtx.Value(traceKey{}) != "delete-trace" {
			t.Fatal("service must receive the verified user's ID and request context")
		}
		return nil
	}}
	handler := handlers.SessionMiddleware(photoSessionAuthenticator{})(
		http.HandlerFunc(handlers.NewUserHandler(users, false, 1024).DeletePhoto),
	)
	// Repeated deletion also succeeds when the service reports no remaining photo.
	for range 2 {
		req := httptest.NewRequest(http.MethodDelete, "/users/42/photo", nil).WithContext(ctx)
		req.SetPathValue("id", "42")
		req.AddCookie(&http.Cookie{Name: "chat_session", Value: "session-token"})
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
			t.Fatalf("status=%d, body=%q; want 204 and empty body", rec.Code, rec.Body.String())
		}
		if rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("Set-Cookie") != "" {
			t.Fatal("photo deletion must disable caching without issuing a session cookie")
		}
	}
	if calls != 2 {
		t.Fatalf("service calls=%d, want 2", calls)
	}
}

func TestUserHandlerDeletePhotoRejectsInvalidRequests(t *testing.T) {
	for _, tt := range []struct {
		name          string
		id            string
		authenticated bool
		status        int
	}{
		{"missing verified identity", "42", false, http.StatusUnauthorized},
		{"another user", "43", true, http.StatusForbidden},
		{"invalid ID", "abc", true, http.StatusBadRequest},
		{"zero ID", "0", true, http.StatusBadRequest},
		{"negative ID", "-1", true, http.StatusBadRequest},
		{"ID overflow", "9223372036854775808", true, http.StatusBadRequest},
	} {
		t.Run(tt.name, func(t *testing.T) {
			users := fakeUserService{deletePhoto: func(context.Context, int64) error {
				t.Fatal("rejected request must not reach the service")
				return nil
			}}
			req := httptest.NewRequest(http.MethodDelete, "/users/id/photo", nil)
			req.SetPathValue("id", tt.id)
			var handler http.Handler = http.HandlerFunc(handlers.NewUserHandler(users, false, 1024).DeletePhoto)
			if tt.authenticated {
				req.AddCookie(&http.Cookie{Name: "chat_session", Value: "session-token"})
				handler = handlers.SessionMiddleware(photoSessionAuthenticator{})(handler)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tt.status {
				t.Fatalf("status=%d, want %d; body=%s", rec.Code, tt.status, rec.Body.String())
			}
			assertLoginError(t, rec)
		})
	}
}

func TestUserHandlerDeletePhotoServiceErrors(t *testing.T) {
	for _, tt := range []struct {
		err     error
		status  int
		message string
	}{
		{user.ErrNotFound, http.StatusNotFound, "user not found"},
		{errors.New("private database failure"), http.StatusInternalServerError, "internal server error"},
	} {
		t.Run(tt.message, func(t *testing.T) {
			users := fakeUserService{deletePhoto: func(context.Context, int64) error {
				return fmt.Errorf("delete photo: %w", tt.err)
			}}
			req := httptest.NewRequest(http.MethodDelete, "/users/42/photo", nil)
			req.SetPathValue("id", "42")
			req.AddCookie(&http.Cookie{Name: "chat_session", Value: "session-token"})
			rec := httptest.NewRecorder()
			handler := handlers.SessionMiddleware(photoSessionAuthenticator{})(
				http.HandlerFunc(handlers.NewUserHandler(users, false, 1024).DeletePhoto),
			)
			handler.ServeHTTP(rec, req)
			if rec.Code != tt.status {
				t.Fatalf("status=%d, want %d", rec.Code, tt.status)
			}
			if got := assertLoginError(t, rec); got.Error != tt.message {
				t.Fatalf("error=%q, want %q", got.Error, tt.message)
			}
		})
	}
}
