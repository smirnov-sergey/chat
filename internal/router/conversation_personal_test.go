package router_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"chat/internal/handlers"
	"chat/internal/model/conversation"
	"chat/internal/model/session"
	"chat/internal/router"
)

func TestConversationPersonalRoute(t *testing.T) {
	for _, tt := range []struct {
		name        string
		method      string
		path        string
		cookie      string
		crossOrigin bool
		status      int
		calls       int
	}{
		{"create", "POST", "/conversations/personal", "valid", false, 200, 1},
		{"missing session", "POST", "/conversations/personal", "", false, 401, 0},
		{"invalid session", "POST", "/conversations/personal", "invalid", false, 401, 0},
		{"cross origin", "POST", "/conversations/personal", "valid", true, 403, 0},
		{"GET does not create", "GET", "/conversations/personal", "valid", false, 400, 0},
		{"wrong method", "DELETE", "/conversations/personal", "valid", false, 405, 0},
		{"wrong path", "POST", "/conversations/personal/extra", "valid", false, 404, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			conversations := fakeConversationService{createPersonal: func(ctx context.Context, currentUserID int64) (*conversation.Conversation, error) {
				calls++
				if currentUserID != 42 {
					t.Fatal("route must use the verified user ID")
				}
				return &conversation.Conversation{ID: 7, Type: conversation.TypePersonal}, nil
			}}
			sessions := fakeSessionAuthenticator(func(_ context.Context, token string) (*session.Session, error) {
				if token != "valid" {
					return nil, session.ErrInvalid
				}
				return &session.Session{UserID: 42}, nil
			})
			appRouter := router.New(handlers.NewUserHandler(fakeUserService{}, true, 1024), handlers.NewConversationHandler(conversations), handlers.NewConversationMessageHandler(nil))
			handler := handlers.CrossOriginProtectionMiddleware()(handlers.SessionMiddleware(sessions)(appRouter))
			req := httptest.NewRequest(tt.method, tt.path+"?user_id=999", nil)
			req.Header.Set("X-User-ID", "999")
			if tt.cookie != "" {
				req.AddCookie(&http.Cookie{Name: "chat_session", Value: tt.cookie})
			}
			if tt.crossOrigin {
				req.Header.Set("Origin", "https://other.example")
				req.Header.Set("Sec-Fetch-Site", "cross-site")
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tt.status || calls != tt.calls {
				t.Fatalf("status=%d, calls=%d; want %d and %d; body=%s", rec.Code, calls, tt.status, tt.calls, rec.Body.String())
			}
		})
	}
}
