package router_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"chat/internal/handlers"
	"chat/internal/model/conversation"
	"chat/internal/model/session"
	"chat/internal/router"
)

func TestConversationListRoute(t *testing.T) {
	for _, tt := range []struct {
		name   string
		method string
		cookie string
		status int
		calls  int
	}{
		{"list", "GET", "valid", 200, 1},
		{"missing session", "GET", "", 401, 0},
		{"invalid session", "GET", "invalid", 401, 0},
		{"wrong method", "POST", "valid", 405, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			conversations := fakeConversationService{listByUser: func(ctx context.Context, currentUserID int64) ([]conversation.Conversation, error) {
				calls++
				if currentUserID != 42 {
					t.Fatal("route must use the verified user ID")
				}
				return []conversation.Conversation{}, nil
			}}
			sessions := fakeSessionAuthenticator(func(_ context.Context, token string) (*session.Session, error) {
				if token != "valid" {
					return nil, session.ErrInvalid
				}
				return &session.Session{UserID: 42}, nil
			})
			appRouter := router.New(handlers.NewUserHandler(fakeUserService{}, true, 1024), handlers.NewConversationHandler(conversations))
			handler := handlers.SessionMiddleware(sessions)(appRouter)
			req := httptest.NewRequest(tt.method, "/conversations?user_id=999", nil)
			req.Header.Set("X-User-ID", "999")
			if tt.cookie != "" {
				req.AddCookie(&http.Cookie{Name: "chat_session", Value: tt.cookie})
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tt.status || calls != tt.calls {
				t.Fatalf("status=%d, calls=%d; want %d and %d", rec.Code, calls, tt.status, tt.calls)
			}
			if tt.status == http.StatusOK && strings.TrimSpace(rec.Body.String()) != "[]" {
				t.Fatalf("body=%q, want []", rec.Body.String())
			}
			if tt.status == http.StatusMethodNotAllowed && rec.Header().Get("Allow") != "GET, HEAD" {
				t.Fatalf("Allow=%q, want GET, HEAD", rec.Header().Get("Allow"))
			}
		})
	}
}
