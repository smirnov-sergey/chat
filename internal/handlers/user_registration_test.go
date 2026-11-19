package handlers_test

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
	"chat/internal/service"
	"chat/internal/validation"
)

type fakeUserService struct {
	deletePhoto func(context.Context, int64) error
	uploadPhoto func(context.Context, int64, []byte) error
	getPhoto    func(context.Context, int64) ([]byte, error)
	logout      func(context.Context, string) error
	getByID     func(context.Context, int64) (*user.User, error)
	register    func(context.Context, string, string, string) (*service.UserSession, error)
	login       func(context.Context, string, string) (*service.UserSession, error)
}

func (fake fakeUserService) DeletePhoto(ctx context.Context, id int64) error {
	return fake.deletePhoto(ctx, id)
}

func (fake fakeUserService) GetPhoto(ctx context.Context, id int64) ([]byte, error) {
	return fake.getPhoto(ctx, id)
}

func (fake fakeUserService) UploadPhoto(ctx context.Context, id int64, data []byte) error {
	return fake.uploadPhoto(ctx, id, data)
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

var (
	_ handlers.UserService = fakeUserService{}
	_ handlers.UserService = (*service.UserService)(nil)
)

const registrationBody = `{"username":" IVAN ","name":" Иван ","password":"  LongPassword123!  "}`

func TestUserHandlerRegisterBodySizeBoundary(t *testing.T) {
	for _, size := range []int{4096, 4097} {
		for _, paddingPosition := range []string{"leading", "trailing"} {
			t.Run(fmt.Sprintf("%d_bytes_%s_padding", size, paddingPosition), func(t *testing.T) {
				padding := strings.Repeat(" ", size-len(registrationBody))
				body := registrationBody + padding
				if paddingPosition == "leading" {
					body = padding + registrationBody
				}
				calls := 0
				users := fakeUserService{register: func(context.Context, string, string, string) (*service.UserSession, error) {
					calls++
					return &service.UserSession{
						User:  &user.User{ID: 1, Username: "ivan", Name: "Иван"},
						Token: &session.Token{Value: "test-session-token", ExpiresAt: time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)},
					}, nil
				}}
				req := httptest.NewRequest(http.MethodPost, "/auth/register", strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				rec := httptest.NewRecorder()
				handlers.NewUserHandler(users, false, 5*1024*1024).Register(rec, req)
				if size == 4096 {
					if rec.Code != http.StatusCreated || calls != 1 {
						t.Fatalf("4096-byte body: status=%d, service calls=%d; want 201 and 1", rec.Code, calls)
					}
					return
				}
				if rec.Code != http.StatusRequestEntityTooLarge || calls != 0 {
					t.Fatalf("4097-byte body: status=%d, service calls=%d; want 413 and 0", rec.Code, calls)
				}
				if body := assertRegistrationError(t, rec); body.Error != "request body too large" {
					t.Fatalf("error=%q, want request body too large", body.Error)
				}
			})
		}
	}
}

func TestUserHandlerRegister(t *testing.T) {
	for _, secure := range []bool{false, true} {
		t.Run(fmt.Sprintf("secure_%v", secure), func(t *testing.T) {
			now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
			localTime := now.In(time.FixedZone("UTC+3", 3*60*60))
			const token = "test-session-token"
			req := httptest.NewRequest(http.MethodPost, "/auth/register", strings.NewReader(registrationBody))
			req.Header.Set("Content-Type", "application/json; charset=utf-8")
			calls := 0
			registration := fakeUserService{register: func(ctx context.Context, username, name, password string) (*service.UserSession, error) {
				calls++
				if ctx != req.Context() || username != " IVAN " || name != " Иван " || password != "  LongPassword123!  " {
					t.Fatal("service must receive the request context and unchanged input fields")
				}
				return &service.UserSession{
					User:  &user.User{ID: 42, Username: "ivan", Name: "Иван", PasswordHash: "private-password-hash", CreatedAt: localTime, UpdatedAt: localTime},
					Token: &session.Token{Value: token, ExpiresAt: now.Add(7 * 24 * time.Hour)},
				}, nil
			}}
			rec := httptest.NewRecorder()
			handlers.NewUserHandler(registration, secure, 5*1024*1024).Register(rec, req)
			if calls != 1 || rec.Code != http.StatusCreated {
				t.Fatalf("calls = %d, status = %d; want 1, 201", calls, rec.Code)
			}
			if rec.Header().Get("Content-Type") != "application/json" || rec.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("expected a JSON response with caching disabled")
			}
			var body map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if len(body) != 5 || body["id"] != float64(42) || body["username"] != "ivan" || body["name"] != "Иван" ||
				body["created_at"] != now.Format(time.RFC3339) || body["updated_at"] != now.Format(time.RFC3339) {
				t.Fatalf("unexpected user response: %v", body)
			}
			if strings.Contains(rec.Body.String(), token) || strings.Contains(rec.Body.String(), "LongPassword") || strings.Contains(rec.Body.String(), "private-password-hash") {
				t.Fatal("JSON must not contain the session token, password or password hash")
			}
			response := rec.Result()
			defer response.Body.Close()
			cookies := response.Cookies()
			if len(cookies) != 1 {
				t.Fatalf("cookie count = %d, want 1", len(cookies))
			}
			cookie := cookies[0]
			if cookie.Name != "chat_session" || cookie.Value != token || cookie.Path != "/" || cookie.Domain != "" ||
				!cookie.HttpOnly || cookie.Secure != secure || cookie.SameSite != http.SameSiteLaxMode ||
				!cookie.Expires.Equal(now.Add(7*24*time.Hour)) {
				t.Fatalf("unexpected session cookie: %+v", cookie)
			}
		})
	}
}

func TestUserHandlerRegisterRejectsInvalidRequests(t *testing.T) {
	for _, tt := range []struct {
		name        string
		contentType string
		body        string
		status      int
	}{
		{"missing content type", "", registrationBody, http.StatusUnsupportedMediaType},
		{"unsupported content type", "text/plain", registrationBody, http.StatusUnsupportedMediaType},
		{"malformed content type", "application/json; charset", registrationBody, http.StatusUnsupportedMediaType},
		{"empty body", "application/json", "", http.StatusBadRequest},
		{"malformed JSON", "application/json", "{", http.StatusBadRequest},
		{"null", "application/json", "null", http.StatusBadRequest},
		{"array", "application/json", "[]", http.StatusBadRequest},
		{"wrong field type", "application/json", `{"username":42}`, http.StatusBadRequest},
		{"unknown field", "application/json", `{"username":"ivan","admin":true}`, http.StatusBadRequest},
		{"multiple JSON values", "application/json", registrationBody + `{}`, http.StatusBadRequest},
		{"trailing garbage", "application/json", registrationBody + `garbage`, http.StatusBadRequest},
		{"oversized body", "application/json", strings.Repeat(" ", 4097) + registrationBody, http.StatusRequestEntityTooLarge},
		{"oversized trailing whitespace", "application/json", registrationBody + strings.Repeat(" ", 4097), http.StatusRequestEntityTooLarge},
	} {
		t.Run(tt.name, func(t *testing.T) {
			registration := fakeUserService{register: func(context.Context, string, string, string) (*service.UserSession, error) {
				t.Fatal("service must not be called for invalid requests")
				return nil, nil
			}}
			req := httptest.NewRequest(http.MethodPost, "/auth/register", strings.NewReader(tt.body))
			if tt.contentType != "" {
				req.Header.Set("Content-Type", tt.contentType)
			}
			rec := httptest.NewRecorder()
			handlers.NewUserHandler(registration, true, 5*1024*1024).Register(rec, req)
			if rec.Code != tt.status {
				t.Fatalf("status = %d, want %d", rec.Code, tt.status)
			}
			assertRegistrationError(t, rec)
		})
	}
}

func TestUserHandlerRegisterServiceErrors(t *testing.T) {
	for _, tt := range []struct {
		name    string
		err     error
		status  int
		message string
	}{
		{"username validation", validation.ErrInvalidUsername, http.StatusBadRequest, validation.ErrInvalidUsername.Error()},
		{"name validation", validation.ErrInvalidName, http.StatusBadRequest, validation.ErrInvalidName.Error()},
		{"password validation", validation.ErrInvalidPassword, http.StatusBadRequest, validation.ErrInvalidPassword.Error()},
		{"username taken", user.ErrUsernameTaken, http.StatusConflict, user.ErrUsernameTaken.Error()},
		{"unexpected error", errors.New("private database error"), http.StatusInternalServerError, "internal server error"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			registration := fakeUserService{register: func(context.Context, string, string, string) (*service.UserSession, error) {
				return nil, fmt.Errorf("registration failed: %w", tt.err)
			}}
			req := httptest.NewRequest(http.MethodPost, "/auth/register", strings.NewReader(registrationBody))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			handlers.NewUserHandler(registration, true, 5*1024*1024).Register(rec, req)
			if rec.Code != tt.status {
				t.Fatalf("status = %d, want %d", rec.Code, tt.status)
			}
			body := assertRegistrationError(t, rec)
			if body.Error != tt.message {
				t.Fatalf("error message = %q, want %q", body.Error, tt.message)
			}
		})
	}
}

func assertRegistrationError(t *testing.T, rec *httptest.ResponseRecorder) struct{ Error string } {
	t.Helper()
	if rec.Header().Get("Content-Type") != "application/json" || rec.Header().Get("Set-Cookie") != "" {
		t.Fatal("errors must be JSON and must not issue a session cookie")
	}
	var body struct{ Error string }
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Error == "" {
		t.Fatalf("expected a JSON error response, got %q", rec.Body.String())
	}
	return body
}
