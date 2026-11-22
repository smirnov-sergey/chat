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
)

func TestConversationHandlerCreatePersonal(t *testing.T) {
	for _, tt := range []struct {
		name string
		body string
	}{
		{"no body", ""},
		{"body cannot select owner", `{"user_id":999}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			type traceKey struct{}
			ctx := context.WithValue(t.Context(), traceKey{}, "personal-trace")
			calls := 0
			s := fakeConversationService{createPersonal: func(gotCtx context.Context, currentUserID int64) (*conversation.Conversation, error) {
				calls++
				if gotCtx.Value(traceKey{}) != "personal-trace" || currentUserID != 42 {
					t.Fatal("service must receive the request context and verified user ID")
				}
				return &conversation.Conversation{
					ID: 7, Type: conversation.TypePersonal,
					CreatedAt: time.Date(2026, 11, 22, 15, 0, 0, 123456000, time.FixedZone("UTC+3", 3*60*60)),
				}, nil
			}}
			handler := handlers.SessionMiddleware(conversationSessionAuthenticator{})(http.HandlerFunc(handlers.NewConversationHandler(s).CreatePersonal))
			for range 2 {
				req := httptest.NewRequest(http.MethodPost, "/conversations/personal?user_id=999", strings.NewReader(tt.body)).WithContext(ctx)
				req.Header.Set("X-User-ID", "999")
				req.AddCookie(&http.Cookie{Name: "chat_session", Value: "session-token"})
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, req)
				if rec.Code != http.StatusOK {
					t.Fatalf("status=%d, want 200; body=%s", rec.Code, rec.Body.String())
				}
				if rec.Header().Get("Content-Type") != "application/json" || rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("Set-Cookie") != "" {
					t.Fatal("response must be JSON without caching or a new session cookie")
				}
				var body map[string]json.RawMessage
				if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				if len(body) != 3 || string(body["id"]) != "7" || string(body["type"]) != `"personal"` ||
					string(body["created_at"]) != `"2026-11-22T12:00:00.123456Z"` {
					t.Fatalf("unexpected personal conversation: %s", rec.Body.String())
				}
			}
			if calls != 2 {
				t.Fatalf("service calls=%d, want 2", calls)
			}
		})
	}
}

func TestConversationHandlerCreatePersonalRequiresIdentity(t *testing.T) {
	for _, withMiddleware := range []bool{false, true} {
		t.Run(fmt.Sprintf("middleware_%v", withMiddleware), func(t *testing.T) {
			var handler http.Handler = http.HandlerFunc(handlers.NewConversationHandler(nil).CreatePersonal)
			if withMiddleware {
				handler = handlers.SessionMiddleware(conversationSessionAuthenticator{})(handler)
			}
			req := httptest.NewRequest(http.MethodPost, "/conversations/personal?user_id=42", nil)
			req.Header.Set("X-User-ID", "42")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			assertConversationError(t, rec, http.StatusUnauthorized, session.ErrInvalid.Error())
		})
	}
}

func TestConversationHandlerCreatePersonalServiceErrors(t *testing.T) {
	for _, tt := range []struct {
		err     error
		status  int
		message string
	}{
		{user.ErrNotFound, http.StatusNotFound, user.ErrNotFound.Error()},
		{conversation.ErrNotFound, http.StatusNotFound, conversation.ErrNotFound.Error()},
		{errors.New("private database details"), http.StatusInternalServerError, "internal server error"},
	} {
		t.Run(tt.message, func(t *testing.T) {
			s := fakeConversationService{createPersonal: func(context.Context, int64) (*conversation.Conversation, error) {
				return nil, fmt.Errorf("create personal conversation: %w", tt.err)
			}}
			handler := handlers.SessionMiddleware(conversationSessionAuthenticator{})(http.HandlerFunc(handlers.NewConversationHandler(s).CreatePersonal))
			req := httptest.NewRequest(http.MethodPost, "/conversations/personal", nil)
			req.AddCookie(&http.Cookie{Name: "chat_session", Value: "session-token"})
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			assertConversationError(t, rec, tt.status, tt.message)
		})
	}
}
