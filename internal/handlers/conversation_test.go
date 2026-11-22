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
	"chat/internal/model/conversation"
	"chat/internal/model/session"
	"chat/internal/model/user"
	"chat/internal/service"
	"chat/internal/validation"
)

type fakeConversationService struct {
	createPersonal func(context.Context, int64) (*conversation.Conversation, error)
	listByUser     func(context.Context, int64) ([]conversation.Conversation, error)
	createDirect   func(context.Context, int64, string) (*conversation.Conversation, error)
	getByID        func(context.Context, int64, int64) (*conversation.Conversation, error)
}

func (fake fakeConversationService) CreatePersonal(ctx context.Context, currentUserID int64) (*conversation.Conversation, error) {
	return fake.createPersonal(ctx, currentUserID)
}

func (fake fakeConversationService) ListByUser(ctx context.Context, currentUserID int64) ([]conversation.Conversation, error) {
	return fake.listByUser(ctx, currentUserID)
}

func (fake fakeConversationService) CreateDirect(ctx context.Context, currentUserID int64, recipientUsername string) (*conversation.Conversation, error) {
	return fake.createDirect(ctx, currentUserID, recipientUsername)
}

func (fake fakeConversationService) GetByID(ctx context.Context, conversationID, currentUserID int64) (*conversation.Conversation, error) {
	return fake.getByID(ctx, conversationID, currentUserID)
}

type conversationSessionAuthenticator struct{}

func (conversationSessionAuthenticator) Authenticate(_ context.Context, token string) (*session.Session, error) {
	if token != "session-token" {
		return nil, session.ErrInvalid
	}
	return &session.Session{UserID: 42}, nil
}

var (
	_ handlers.ConversationService = fakeConversationService{}
	_ handlers.ConversationService = (*service.ConversationService)(nil)
)

const conversationDirectBody = `{"recipient_username":" ANNA "}`

func conversationDirectHandler(service handlers.ConversationService) http.Handler {
	handler := handlers.NewConversationHandler(service)
	return handlers.SessionMiddleware(conversationSessionAuthenticator{})(http.HandlerFunc(handler.CreateDirect))
}

func conversationDirectRequest(body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/conversations/direct", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "chat_session", Value: "session-token"})
	return req
}

func assertConversationError(t *testing.T, rec *httptest.ResponseRecorder, status int, message string) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, status, rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "application/json" || rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("Set-Cookie") != "" {
		t.Fatal("errors must be JSON without caching or a new session cookie")
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || len(body) != 1 || body["error"] != message {
		t.Fatalf("body = %s, want only error %q", rec.Body.String(), message)
	}
}

func TestConversationHandlerCreateDirect(t *testing.T) {
	type traceKey struct{}
	ctx := context.WithValue(t.Context(), traceKey{}, "conversation-trace")
	calls := 0
	createdAt := time.Date(2026, 11, 20, 15, 0, 0, 0, time.FixedZone("UTC+3", 3*60*60))
	s := fakeConversationService{createDirect: func(gotCtx context.Context, userID int64, username string) (*conversation.Conversation, error) {
		calls++
		if gotCtx.Value(traceKey{}) != "conversation-trace" || userID != 42 || username != " ANNA " {
			t.Fatal("service must receive verified identity, request context and unchanged username")
		}
		return &conversation.Conversation{ID: 7, Type: conversation.TypeDirect, CreatedAt: createdAt}, nil
	}}
	handler := conversationDirectHandler(s)
	// Both a new and an existing dialogue use the same successful response.
	for range 2 {
		req := conversationDirectRequest(conversationDirectBody).WithContext(ctx)
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/json" ||
			rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("Set-Cookie") != "" {
			t.Fatalf("unexpected response: status=%d, headers=%v", rec.Code, rec.Header())
		}
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if len(body) != 3 || body["id"] != float64(7) || body["type"] != "direct" || body["created_at"] != "2026-11-20T12:00:00Z" {
			t.Fatalf("unexpected conversation response: %s", rec.Body.String())
		}
	}
	if calls != 2 {
		t.Fatalf("service calls = %d, want 2", calls)
	}
}

func TestConversationHandlerCreateDirectInvalidRequests(t *testing.T) {
	for _, tt := range []struct {
		name        string
		contentType string
		body        string
		status      int
	}{
		{"missing content type", "", conversationDirectBody, 415},
		{"unsupported content type", "text/plain", conversationDirectBody, 415},
		{"empty body", "application/json", "", 400},
		{"malformed JSON", "application/json", "{", 400},
		{"null", "application/json", "null", 400},
		{"array", "application/json", "[]", 400},
		{"wrong type", "application/json", `{"recipient_username":42}`, 400},
		{"forged identity", "application/json", `{"recipient_username":"anna","current_user_id":99}`, 400},
		{"multiple values", "application/json", conversationDirectBody + `{}`, 400},
		{"trailing garbage", "application/json", conversationDirectBody + `x`, 400},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := fakeConversationService{createDirect: func(context.Context, int64, string) (*conversation.Conversation, error) {
				t.Fatal("invalid request must not reach service")
				return nil, nil
			}}
			req := conversationDirectRequest(tt.body)
			req.Header.Set("Content-Type", tt.contentType)
			rec := httptest.NewRecorder()
			conversationDirectHandler(s).ServeHTTP(rec, req)
			message := "invalid request body"
			if tt.status == 415 {
				message = "Content-Type must be application/json"
			}
			assertConversationError(t, rec, tt.status, message)
		})
	}
}

func TestConversationHandlerCreateDirectRequiresIdentity(t *testing.T) {
	for _, withMiddleware := range []bool{false, true} {
		t.Run(fmt.Sprintf("middleware_%v", withMiddleware), func(t *testing.T) {
			req := conversationDirectRequest("invalid JSON")
			req.Header.Del("Cookie")
			var handler http.Handler = http.HandlerFunc(handlers.NewConversationHandler(nil).CreateDirect)
			if withMiddleware {
				handler = handlers.SessionMiddleware(conversationSessionAuthenticator{})(handler)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			assertConversationError(t, rec, 401, session.ErrInvalid.Error())
		})
	}
}

func TestConversationHandlerCreateDirectBodyBoundary(t *testing.T) {
	for _, size := range []int{4096, 4097} {
		for _, leading := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d_leading_%v", size, leading), func(t *testing.T) {
				padding := strings.Repeat(" ", size-len(conversationDirectBody))
				body := conversationDirectBody + padding
				if leading {
					body = padding + conversationDirectBody
				}
				calls := 0
				s := fakeConversationService{createDirect: func(context.Context, int64, string) (*conversation.Conversation, error) {
					calls++
					return &conversation.Conversation{ID: 7, Type: conversation.TypeDirect}, nil
				}}
				rec := httptest.NewRecorder()
				conversationDirectHandler(s).ServeHTTP(rec, conversationDirectRequest(body))
				if size == 4096 {
					if rec.Code != 200 || calls != 1 {
						t.Fatalf("status=%d, calls=%d; want 200 and 1", rec.Code, calls)
					}
				} else {
					assertConversationError(t, rec, 413, "request body too large")
					if calls != 0 {
						t.Fatal("oversized request must not reach service")
					}
				}
			})
		}
	}
}

func TestConversationHandlerCreateDirectServiceErrors(t *testing.T) {
	for _, tt := range []struct {
		err     error
		status  int
		message string
	}{
		{validation.ErrInvalidUsername, 400, validation.ErrInvalidUsername.Error()},
		{conversation.ErrInvalidParticipants, 400, conversation.ErrInvalidParticipants.Error()},
		{user.ErrNotFound, 404, user.ErrNotFound.Error()},
		{conversation.ErrNotFound, 404, conversation.ErrNotFound.Error()},
		{errors.New("private database details"), 500, "internal server error"},
	} {
		t.Run(tt.message, func(t *testing.T) {
			s := fakeConversationService{createDirect: func(context.Context, int64, string) (*conversation.Conversation, error) {
				return nil, fmt.Errorf("create conversation: %w", tt.err)
			}}
			rec := httptest.NewRecorder()
			conversationDirectHandler(s).ServeHTTP(rec, conversationDirectRequest(conversationDirectBody))
			assertConversationError(t, rec, tt.status, tt.message)
		})
	}
}
