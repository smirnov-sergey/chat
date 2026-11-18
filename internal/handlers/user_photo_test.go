package handlers_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"chat/internal/handlers"
	"chat/internal/model/session"
	"chat/internal/model/user"
	"chat/internal/photo"
)

type photoSessionAuthenticator struct{}

func (photoSessionAuthenticator) Authenticate(context.Context, string) (*session.Session, error) {
	return &session.Session{UserID: 42}, nil
}

func newPhotoUploadHandler(users fakeUserService, limit int64) http.Handler {
	handler := handlers.NewUserHandler(users, false, limit)
	return handlers.SessionMiddleware(photoSessionAuthenticator{})(http.HandlerFunc(handler.UploadPhoto))
}

func TestUserHandlerUploadPhoto(t *testing.T) {
	for _, contentType := range []string{"image/jpeg", "image/png", "image/png; charset=binary"} {
		t.Run(contentType, func(t *testing.T) {
			type traceKey struct{}
			ctx := context.WithValue(t.Context(), traceKey{}, "upload-trace")
			req := httptest.NewRequest(http.MethodPut, "/users/42/photo", strings.NewReader("photo bytes")).WithContext(ctx)
			req.SetPathValue("id", "42")
			req.Header.Set("Content-Type", contentType)
			req.AddCookie(&http.Cookie{Name: "chat_session", Value: "session-token"})
			calls := 0
			users := fakeUserService{uploadPhoto: func(gotCtx context.Context, id int64, data []byte) error {
				calls++
				if gotCtx.Value(traceKey{}) != "upload-trace" || id != 42 || string(data) != "photo bytes" {
					t.Fatal("service must receive the user ID, unchanged bytes and request context")
				}
				return nil
			}}
			rec := httptest.NewRecorder()
			newPhotoUploadHandler(users, int64(len("photo bytes"))).ServeHTTP(rec, req)
			if rec.Code != http.StatusNoContent || calls != 1 || rec.Body.Len() != 0 {
				t.Fatalf("status=%d, calls=%d, body=%q; want 204, one call, empty body", rec.Code, calls, rec.Body.String())
			}
			if rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("Set-Cookie") != "" {
				t.Fatal("photo upload must disable caching and must not issue a session cookie")
			}
		})
	}
}

func TestUserHandlerUploadPhotoInvalidRequests(t *testing.T) {
	for _, tt := range []struct {
		name        string
		contentType string
		body        string
		unknownSize bool
		status      int
	}{
		{"missing content type", "", "PNG", false, http.StatusUnsupportedMediaType},
		{"unsupported content type", "application/octet-stream", "PNG", false, http.StatusUnsupportedMediaType},
		{"multipart is not supported", "multipart/form-data; boundary=test", "PNG", false, http.StatusUnsupportedMediaType},
		{"malformed content type", "image/png; charset", "PNG", false, http.StatusUnsupportedMediaType},
		{"empty body", "image/png", "", false, http.StatusBadRequest},
		{"one byte over limit", "image/png", "12345", false, http.StatusRequestEntityTooLarge},
		{"unknown size over limit", "image/jpeg", "12345", true, http.StatusRequestEntityTooLarge},
	} {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPut, "/users/42/photo", strings.NewReader(tt.body))
			req.SetPathValue("id", "42")
			req.Header.Set("Content-Type", tt.contentType)
			req.AddCookie(&http.Cookie{Name: "chat_session", Value: "session-token"})
			if tt.unknownSize {
				req.ContentLength = -1
			}
			users := fakeUserService{uploadPhoto: func(context.Context, int64, []byte) error {
				t.Fatal("invalid request must not reach the service")
				return nil
			}}
			rec := httptest.NewRecorder()
			newPhotoUploadHandler(users, 4).ServeHTTP(rec, req)
			if rec.Code != tt.status {
				t.Fatalf("status=%d, want %d; body=%s", rec.Code, tt.status, rec.Body.String())
			}
			assertLoginError(t, rec)
		})
	}
}

type failingPhotoBody struct{ reads int }

func (body *failingPhotoBody) Read([]byte) (int, error) {
	body.reads++
	return 0, errors.New("private body read failure")
}

func TestUserHandlerUploadPhotoAuthorizationBeforeBodyRead(t *testing.T) {
	for _, tt := range []struct {
		name          string
		id            string
		authenticated bool
		status        int
	}{
		{"missing verified identity", "42", false, http.StatusUnauthorized},
		{"another user", "43", true, http.StatusForbidden},
		{"invalid ID", "abc", true, http.StatusBadRequest},
	} {
		t.Run(tt.name, func(t *testing.T) {
			body := &failingPhotoBody{}
			req := httptest.NewRequest(http.MethodPut, "/users/id/photo", body)
			req.SetPathValue("id", tt.id)
			req.Header.Set("Content-Type", "image/png")
			var handler http.Handler = http.HandlerFunc(handlers.NewUserHandler(fakeUserService{}, false, 4).UploadPhoto)
			if tt.authenticated {
				req.AddCookie(&http.Cookie{Name: "chat_session", Value: "session-token"})
				handler = handlers.SessionMiddleware(photoSessionAuthenticator{})(handler)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tt.status || body.reads != 0 {
				t.Fatalf("status=%d, body reads=%d; want %d and no body access", rec.Code, body.reads, tt.status)
			}
			assertLoginError(t, rec)
		})
	}
}

func TestUserHandlerUploadPhotoBodyReadFailure(t *testing.T) {
	req := httptest.NewRequest(http.MethodPut, "/users/42/photo", &failingPhotoBody{})
	req.SetPathValue("id", "42")
	req.Header.Set("Content-Type", "image/png")
	req.AddCookie(&http.Cookie{Name: "chat_session", Value: "session-token"})
	rec := httptest.NewRecorder()
	newPhotoUploadHandler(fakeUserService{}, 1024).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", rec.Code)
	}
	if got := assertLoginError(t, rec); got.Error != "invalid request body" {
		t.Fatal("body read errors must not expose internal details")
	}
}

func TestUserHandlerUploadPhotoServiceErrors(t *testing.T) {
	for _, tt := range []struct {
		err     error
		status  int
		message string
	}{
		{photo.ErrInvalidImage, http.StatusBadRequest, photo.ErrInvalidImage.Error()},
		{photo.ErrInvalidDimensions, http.StatusBadRequest, photo.ErrInvalidDimensions.Error()},
		{photo.ErrFileTooLarge, http.StatusRequestEntityTooLarge, photo.ErrFileTooLarge.Error()},
		{user.ErrNotFound, http.StatusNotFound, user.ErrNotFound.Error()},
		{errors.New("private database failure"), http.StatusInternalServerError, "internal server error"},
	} {
		t.Run(tt.message, func(t *testing.T) {
			users := fakeUserService{uploadPhoto: func(context.Context, int64, []byte) error {
				return fmt.Errorf("upload: %w", tt.err)
			}}
			req := httptest.NewRequest(http.MethodPut, "/users/42/photo", strings.NewReader("PNG"))
			req.SetPathValue("id", "42")
			req.Header.Set("Content-Type", "image/png")
			req.AddCookie(&http.Cookie{Name: "chat_session", Value: "session-token"})
			rec := httptest.NewRecorder()
			newPhotoUploadHandler(users, 1024).ServeHTTP(rec, req)
			if rec.Code != tt.status {
				t.Fatalf("status=%d, want %d", rec.Code, tt.status)
			}
			if got := assertLoginError(t, rec); got.Error != tt.message {
				t.Fatalf("error=%q, want %q", got.Error, tt.message)
			}
		})
	}
}

var _ io.Reader = (*failingPhotoBody)(nil)
