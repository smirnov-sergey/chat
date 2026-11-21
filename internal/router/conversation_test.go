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

type fakeConversationService struct {
	createDirect func(context.Context, int64, string) (*conversation.Conversation, error)
	getByID      func(context.Context, int64, int64) (*conversation.Conversation, error)
}

func (fake fakeConversationService) CreateDirect(ctx context.Context, currentUserID int64, recipientUsername string) (*conversation.Conversation, error) {
	return fake.createDirect(ctx, currentUserID, recipientUsername)
}

func (fake fakeConversationService) GetByID(ctx context.Context, conversationID, currentUserID int64) (*conversation.Conversation, error) {
	return fake.getByID(ctx, conversationID, currentUserID)
}

var _ handlers.ConversationService = fakeConversationService{}

func TestConversationDirectRoute(t *testing.T) {
	for _, tt := range []struct {
		name        string
		method      string
		path        string
		cookie      string
		crossOrigin bool
		status      int
		calls       int
	}{
		{"create", "POST", "/conversations/direct", "valid", false, 200, 1},
		{"missing session", "POST", "/conversations/direct", "", false, 401, 0},
		{"invalid session", "POST", "/conversations/direct", "invalid", false, 401, 0},
		{"cross origin", "POST", "/conversations/direct", "valid", true, 403, 0},
		{"non numeric conversation ID", "GET", "/conversations/direct", "valid", false, 400, 0},
		{"wrong path", "POST", "/conversations/direct/extra", "valid", false, 404, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			conversations := fakeConversationService{createDirect: func(ctx context.Context, userID int64, username string) (*conversation.Conversation, error) {
				calls++
				if userID != 42 || username != "anna" {
					t.Fatal("route must pass the verified identity and recipient to service")
				}
				return &conversation.Conversation{ID: 7, Type: conversation.TypeDirect}, nil
			}}
			sessions := fakeSessionAuthenticator(func(_ context.Context, token string) (*session.Session, error) {
				if token != "valid" {
					return nil, session.ErrInvalid
				}
				return &session.Session{UserID: 42}, nil
			})
			appRouter := router.New(handlers.NewUserHandler(fakeUserService{}, true, 1024), handlers.NewConversationHandler(conversations))
			handler := handlers.CrossOriginProtectionMiddleware()(handlers.SessionMiddleware(sessions)(appRouter))
			req := httptest.NewRequest(tt.method, tt.path+"?user_id=999", strings.NewReader(`{"recipient_username":"anna"}`))
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
				t.Fatalf("status=%d, calls=%d; want %d and %d; body=%s", rec.Code, calls, tt.status, tt.calls, rec.Body.String())
			}
		})
	}
}

func TestConversationReadRoute(t *testing.T) {
	for _, tt := range []struct {
		name   string
		method string
		path   string
		cookie string
		status int
		calls  int
	}{
		{"read", "GET", "/conversations/7", "valid", 200, 1},
		{"missing session", "GET", "/conversations/7", "", 401, 0},
		{"invalid session", "GET", "/conversations/7", "invalid", 401, 0},
		{"invalid ID", "GET", "/conversations/abc", "valid", 400, 0},
		{"identity before ID", "GET", "/conversations/abc", "", 401, 0},
		{"wrong method", "POST", "/conversations/7", "valid", 405, 0},
		{"wrong path", "GET", "/conversations/7/extra", "valid", 404, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			conversations := fakeConversationService{getByID: func(ctx context.Context, conversationID, currentUserID int64) (*conversation.Conversation, error) {
				calls++
				if conversationID != 7 || currentUserID != 42 {
					t.Fatal("route must use the path ID and verified user ID")
				}
				return &conversation.Conversation{ID: 7, Type: conversation.TypeDirect}, nil
			}}
			sessions := fakeSessionAuthenticator(func(_ context.Context, token string) (*session.Session, error) {
				if token != "valid" {
					return nil, session.ErrInvalid
				}
				return &session.Session{UserID: 42}, nil
			})
			appRouter := router.New(handlers.NewUserHandler(fakeUserService{}, true, 1024), handlers.NewConversationHandler(conversations))
			handler := handlers.SessionMiddleware(sessions)(appRouter)
			req := httptest.NewRequest(tt.method, tt.path+"?user_id=999", nil)
			req.Header.Set("X-User-ID", "999")
			if tt.cookie != "" {
				req.AddCookie(&http.Cookie{Name: "chat_session", Value: tt.cookie})
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tt.status || calls != tt.calls {
				t.Fatalf("status=%d, calls=%d; want %d and %d; body=%s", rec.Code, calls, tt.status, tt.calls, rec.Body.String())
			}
			if tt.status == http.StatusMethodNotAllowed && rec.Header().Get("Allow") != "GET, HEAD" {
				t.Fatalf("Allow=%q, want GET, HEAD", rec.Header().Get("Allow"))
			}
		})
	}
}
