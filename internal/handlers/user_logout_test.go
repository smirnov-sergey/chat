package handlers_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"chat/internal/handlers"
)

func TestUserHandlerLogout(t *testing.T) {
	for _, secure := range []bool{false, true} {
		for _, tt := range []struct {
			name   string
			cookie *http.Cookie
			token  string
		}{
			{"present cookie", &http.Cookie{Name: "chat_session", Value: "raw-token"}, "raw-token"},
			{"missing cookie", nil, ""},
			{"empty cookie", &http.Cookie{Name: "chat_session"}, ""},
			{"unrelated cookie", &http.Cookie{Name: "other", Value: "other-token"}, ""},
		} {
			t.Run(fmt.Sprintf("secure_%v/%s", secure, tt.name), func(t *testing.T) {
				req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
				if tt.cookie != nil {
					req.AddCookie(tt.cookie)
				}
				calls := 0
				users := fakeUserService{logout: func(ctx context.Context, token string) error {
					calls++
					if ctx != req.Context() || token != tt.token {
						t.Fatal("logout must receive the request context and only the session cookie's value")
					}
					return nil
				}}
				rec := httptest.NewRecorder()
				handlers.NewUserHandler(users, secure).Logout(rec, req)
				if rec.Code != http.StatusNoContent || calls != 1 || rec.Body.Len() != 0 {
					t.Fatalf("status = %d, calls = %d, body = %q; want 204, 1 and no body", rec.Code, calls, rec.Body.String())
				}
				if rec.Header().Get("Cache-Control") != "no-store" {
					t.Fatal("logout must disable caching")
				}
				response := rec.Result()
				defer response.Body.Close()
				cookies := response.Cookies()
				if len(cookies) != 1 {
					t.Fatalf("cookie count = %d, want 1", len(cookies))
				}
				cookie := cookies[0]
				if cookie.Name != "chat_session" || cookie.Value != "" || cookie.Path != "/" || cookie.Domain != "" ||
					cookie.MaxAge != -1 || !cookie.Expires.Equal(time.Unix(1, 0).UTC()) || !cookie.HttpOnly || cookie.Secure != secure || cookie.SameSite != http.SameSiteLaxMode {
					t.Fatalf("unexpected cookie deletion: %+v", cookie)
				}
			})
		}
	}
}

func TestUserHandlerLogoutStorageError(t *testing.T) {
	users := fakeUserService{logout: func(context.Context, string) error {
		return errors.New("private database error")
	}}
	req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	req.AddCookie(&http.Cookie{Name: "chat_session", Value: "raw-token"})
	rec := httptest.NewRecorder()
	handlers.NewUserHandler(users, true).Logout(rec, req)
	if rec.Code != http.StatusInternalServerError || rec.Header().Get("Set-Cookie") != "" {
		t.Fatalf("status = %d; want 500 without clearing the cookie when revocation fails", rec.Code)
	}
	var body struct{ Error string }
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error != "internal server error" || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("logout failure must return only the public error with caching disabled")
	}
}
