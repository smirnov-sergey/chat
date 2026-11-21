package handlers_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"chat/internal/handlers"
	"chat/internal/model/conversation"
	"chat/internal/model/session"
	"chat/internal/model/user"
)

func TestConversationHandlerListByUser(t *testing.T) {
	createdAt := time.Date(2026, 11, 21, 15, 0, 0, 123456000, time.FixedZone("UTC+3", 3*60*60))
	for _, tt := range []struct {
		name          string
		conversations []conversation.Conversation
	}{
		{"ordered list", []conversation.Conversation{
			{ID: 7, Type: conversation.TypeDirect, CreatedAt: createdAt},
			{ID: 3, Type: conversation.TypeDirect, CreatedAt: createdAt.Add(-time.Hour)},
		}},
		{"empty list", []conversation.Conversation{}},
		{"nil list", nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			type traceKey struct{}
			ctx := context.WithValue(t.Context(), traceKey{}, "list-trace")
			original := slices.Clone(tt.conversations)
			calls := 0
			s := fakeConversationService{listByUser: func(gotCtx context.Context, currentUserID int64) ([]conversation.Conversation, error) {
				calls++
				if gotCtx.Value(traceKey{}) != "list-trace" || currentUserID != 42 {
					t.Fatal("service must receive the request context and verified user ID")
				}
				return tt.conversations, nil
			}}
			handler := handlers.SessionMiddleware(conversationSessionAuthenticator{})(http.HandlerFunc(handlers.NewConversationHandler(s).ListByUser))
			req := httptest.NewRequest(http.MethodGet, "/conversations?user_id=999", nil).WithContext(ctx)
			req.Header.Set("X-User-ID", "999")
			req.AddCookie(&http.Cookie{Name: "chat_session", Value: "session-token"})
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK || calls != 1 {
				t.Fatalf("status=%d, calls=%d; want 200 and 1", rec.Code, calls)
			}
			if rec.Header().Get("Content-Type") != "application/json" || rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("Set-Cookie") != "" {
				t.Fatal("list must return JSON without caching or a new cookie")
			}
			var body []map[string]json.RawMessage
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body == nil || len(body) != len(tt.conversations) {
				t.Fatalf("unexpected JSON array: %s", rec.Body.String())
			}
			for i, c := range tt.conversations {
				if len(body[i]) != 3 || string(body[i]["id"]) != fmt.Sprint(c.ID) || string(body[i]["type"]) != `"direct"` ||
					string(body[i]["created_at"]) != `"`+c.CreatedAt.UTC().Format(time.RFC3339Nano)+`"` {
					t.Fatalf("unexpected item %d: %s", i, rec.Body.String())
				}
			}
			if len(tt.conversations) == 0 && strings.TrimSpace(rec.Body.String()) != "[]" {
				t.Fatal("empty result must be an empty array, not null")
			}
			if !slices.Equal(tt.conversations, original) {
				t.Fatal("response conversion must not modify the service result")
			}
		})
	}
}

func TestConversationHandlerListByUserRequiresIdentity(t *testing.T) {
	for _, withMiddleware := range []bool{false, true} {
		t.Run(fmt.Sprintf("middleware_%v", withMiddleware), func(t *testing.T) {
			var handler http.Handler = http.HandlerFunc(handlers.NewConversationHandler(nil).ListByUser)
			if withMiddleware {
				handler = handlers.SessionMiddleware(conversationSessionAuthenticator{})(handler)
			}
			req := httptest.NewRequest(http.MethodGet, "/conversations", nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			assertConversationError(t, rec, http.StatusUnauthorized, session.ErrInvalid.Error())
		})
	}
}

func TestConversationHandlerListByUserServiceErrors(t *testing.T) {
	for _, tt := range []struct {
		err     error
		status  int
		message string
	}{
		{user.ErrNotFound, http.StatusNotFound, user.ErrNotFound.Error()},
		{errors.New("private database details"), http.StatusInternalServerError, "internal server error"},
	} {
		t.Run(tt.message, func(t *testing.T) {
			s := fakeConversationService{listByUser: func(context.Context, int64) ([]conversation.Conversation, error) {
				return nil, fmt.Errorf("list failed: %w", tt.err)
			}}
			handler := handlers.SessionMiddleware(conversationSessionAuthenticator{})(http.HandlerFunc(handlers.NewConversationHandler(s).ListByUser))
			req := httptest.NewRequest(http.MethodGet, "/conversations", nil)
			req.AddCookie(&http.Cookie{Name: "chat_session", Value: "session-token"})
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			assertConversationError(t, rec, tt.status, tt.message)
		})
	}
}
