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

func TestConversationMessageListRoute(t *testing.T) {
	for _, tt := range []struct {
		name, method, path, cookie string
		status, calls              int
		cursor                     bool
	}{
		{"first page", "GET", "/conversations/7/messages", "valid", 200, 1, false},
		{"next page", "GET", "/conversations/7/messages?before_id=150", "valid", 200, 1, true},
		{"missing session", "GET", "/conversations/7/messages", "", 401, 0, false},
		{"invalid session", "GET", "/conversations/7/messages", "invalid", 401, 0, false},
		{"invalid ID", "GET", "/conversations/abc/messages", "valid", 400, 0, false},
		{"invalid cursor", "GET", "/conversations/7/messages?before_id=0", "valid", 400, 0, false},
		{"wrong method", "DELETE", "/conversations/7/messages", "valid", 405, 0, false},
		{"wrong path", "GET", "/conversations/7/messages/extra", "valid", 404, 0, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			messages := fakeConversationMessageService{list: func(_ context.Context, id, userID int64, beforeID *int64) (*conversation.MessagePage, error) {
				calls++
				if id != 7 || userID != 42 || (beforeID != nil) != tt.cursor {
					t.Fatal("incorrect route arguments")
				}
				if beforeID != nil && *beforeID != 150 {
					t.Fatal("incorrect cursor")
				}
				return &conversation.MessagePage{}, nil
			}}
			sessions := fakeSessionAuthenticator(func(_ context.Context, token string) (*session.Session, error) {
				if token != "valid" {
					return nil, session.ErrInvalid
				}
				return &session.Session{UserID: 42}, nil
			})
			appRouter := router.New(handlers.NewUserHandler(fakeUserService{}, true, 1024), handlers.NewConversationHandler(nil), handlers.NewConversationMessageHandler(messages))
			handler := handlers.CrossOriginProtectionMiddleware()(handlers.SessionMiddleware(sessions)(appRouter))
			req := httptest.NewRequest(tt.method, tt.path, nil)
			query := req.URL.Query()
			query.Set("user_id", "999")
			req.URL.RawQuery = query.Encode()
			req.Header.Set("X-User-ID", "999")
			if tt.cookie != "" {
				req.AddCookie(&http.Cookie{Name: "chat_session", Value: tt.cookie})
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tt.status || calls != tt.calls {
				t.Fatalf("status=%d calls=%d, want %d/%d; body=%s", rec.Code, calls, tt.status, tt.calls, rec.Body.String())
			}
		})
	}
}
