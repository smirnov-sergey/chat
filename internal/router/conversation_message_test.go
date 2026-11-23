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

type fakeConversationMessageService func(context.Context, int64, int64, string) (*conversation.Message, error)

func (fake fakeConversationMessageService) Create(ctx context.Context, conversationID, userID int64, text string) (*conversation.Message, error) {
	return fake(ctx, conversationID, userID, text)
}

func TestConversationMessageRoute(t *testing.T) {
	for _, tt := range []struct {
		name, method, path, cookie string
		crossOrigin                bool
		status, calls              int
	}{
		{"create", "POST", "/conversations/7/messages", "valid", false, 201, 1},
		{"missing session", "POST", "/conversations/7/messages", "", false, 401, 0},
		{"invalid session", "POST", "/conversations/7/messages", "invalid", false, 401, 0},
		{"cross origin", "POST", "/conversations/7/messages", "valid", true, 403, 0},
		{"invalid ID", "POST", "/conversations/abc/messages", "valid", false, 400, 0},
		{"wrong method", "GET", "/conversations/7/messages", "valid", false, 405, 0},
		{"wrong path", "POST", "/conversations/7/messages/extra", "valid", false, 404, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			messages := fakeConversationMessageService(func(_ context.Context, conversationID, userID int64, text string) (*conversation.Message, error) {
				calls++
				if conversationID != 7 || userID != 42 || text != "hello" {
					t.Fatal("route must use path ID, session user and body text")
				}
				return &conversation.Message{ID: 9, ConversationID: conversationID, SenderUserID: userID, Text: text}, nil
			})
			sessions := fakeSessionAuthenticator(func(_ context.Context, token string) (*session.Session, error) {
				if token != "valid" {
					return nil, session.ErrInvalid
				}
				return &session.Session{UserID: 42}, nil
			})
			appRouter := router.New(handlers.NewUserHandler(fakeUserService{}, true, 1024), handlers.NewConversationHandler(nil), handlers.NewConversationMessageHandler(messages))
			handler := handlers.CrossOriginProtectionMiddleware()(handlers.SessionMiddleware(sessions)(appRouter))
			req := httptest.NewRequest(tt.method, tt.path+"?user_id=999", strings.NewReader(`{"text":"hello"}`))
			req.Header.Set("Content-Type", "application/json")
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
				t.Fatalf("status=%d calls=%d, want %d/%d; body=%s", rec.Code, calls, tt.status, tt.calls, rec.Body.String())
			}
		})
	}
}
