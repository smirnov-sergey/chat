package handlers_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"chat/internal/handlers"
	"chat/internal/model/user"
)

func newPhotoReadHandler(users fakeUserService) http.Handler {
	handler := handlers.NewUserHandler(users, false, 5*1024*1024)
	return handlers.SessionMiddleware(photoSessionAuthenticator{})(http.HandlerFunc(handler.GetPhoto))
}

func TestUserHandlerGetPhoto(t *testing.T) {
	for _, id := range []int64{42, 43} {
		t.Run(strconv.FormatInt(id, 10), func(t *testing.T) {
			type traceKey struct{}
			ctx := context.WithValue(t.Context(), traceKey{}, "photo-read")
			data := []byte{137, 'P', 'N', 'G', 13, 10, 26, 10, 0}
			calls := 0
			users := fakeUserService{getPhoto: func(gotCtx context.Context, gotID int64) ([]byte, error) {
				calls++
				if gotCtx.Value(traceKey{}) != "photo-read" || gotID != id {
					t.Fatal("service must receive the requested user ID and request context")
				}
				return data, nil
			}}
			req := httptest.NewRequest(http.MethodGet, "/users/id/photo", nil).WithContext(ctx)
			req.SetPathValue("id", strconv.FormatInt(id, 10))
			req.AddCookie(&http.Cookie{Name: "chat_session", Value: "session-token"})
			rec := httptest.NewRecorder()
			newPhotoReadHandler(users).ServeHTTP(rec, req)
			if rec.Code != http.StatusOK || calls != 1 || !bytes.Equal(rec.Body.Bytes(), data) {
				t.Fatalf("status=%d, calls=%d; want 200, one call and unchanged photo bytes", rec.Code, calls)
			}
			if rec.Header().Get("Content-Type") != "image/png" || rec.Header().Get("Cache-Control") != "no-store" ||
				rec.Header().Get("Set-Cookie") != "" {
				t.Fatalf("unexpected photo response headers: %v", rec.Header())
			}
		})
	}
}

func TestUserHandlerGetPhotoRejectsInvalidRequests(t *testing.T) {
	for _, tt := range []struct {
		name          string
		id            string
		authenticated bool
		status        int
	}{
		{"missing verified identity", "42", false, http.StatusUnauthorized},
		{"invalid ID", "abc", true, http.StatusBadRequest},
	} {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/users/id/photo", nil)
			req.SetPathValue("id", tt.id)
			var handler http.Handler = http.HandlerFunc(handlers.NewUserHandler(fakeUserService{}, false, 1024).GetPhoto)
			if tt.authenticated {
				req.AddCookie(&http.Cookie{Name: "chat_session", Value: "session-token"})
				handler = newPhotoReadHandler(fakeUserService{})
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tt.status {
				t.Fatalf("status=%d, want %d", rec.Code, tt.status)
			}
			assertLoginError(t, rec)
		})
	}
}

func TestUserHandlerGetPhotoServiceErrors(t *testing.T) {
	for _, tt := range []struct {
		err     error
		status  int
		message string
	}{
		{user.ErrNotFound, http.StatusNotFound, "user not found"},
		{user.ErrPhotoNotFound, http.StatusNotFound, "user photo not found"},
		{errors.New("private storage path failure"), http.StatusInternalServerError, "internal server error"},
	} {
		t.Run(tt.message, func(t *testing.T) {
			users := fakeUserService{getPhoto: func(context.Context, int64) ([]byte, error) {
				return nil, fmt.Errorf("read photo: %w", tt.err)
			}}
			req := httptest.NewRequest(http.MethodGet, "/users/42/photo", nil)
			req.SetPathValue("id", "42")
			req.AddCookie(&http.Cookie{Name: "chat_session", Value: "session-token"})
			rec := httptest.NewRecorder()
			newPhotoReadHandler(users).ServeHTTP(rec, req)
			if rec.Code != tt.status {
				t.Fatalf("status=%d, want %d", rec.Code, tt.status)
			}
			if got := assertLoginError(t, rec); got.Error != tt.message {
				t.Fatalf("error=%q, want %q", got.Error, tt.message)
			}
			if length := rec.Header().Get("Content-Length"); length != "" && length != strconv.Itoa(rec.Body.Len()) {
				t.Fatal("error response Content-Length must match the JSON body")
			}
		})
	}
}
