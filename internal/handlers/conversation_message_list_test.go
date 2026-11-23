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
	"chat/internal/model/conversation"
	"chat/internal/model/session"
)

func conversationMessageListRequest(query string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/conversations/7/messages", nil)
	req.URL.RawQuery = query
	req.SetPathValue("id", "7")
	req.AddCookie(&http.Cookie{Name: "chat_session", Value: "session-token"})
	return req
}

func conversationMessageListHandler(s handlers.ConversationMessageService) http.Handler {
	return handlers.SessionMiddleware(conversationSessionAuthenticator{})(http.HandlerFunc(handlers.NewConversationMessageHandler(s).List))
}

func TestConversationMessageHandlerList(t *testing.T) {
	for _, query := range []string{"", "before_id=150", "before_id=9223372036854775807"} {
		t.Run(query, func(t *testing.T) {
			type traceKey struct{}
			ctx := context.WithValue(t.Context(), traceKey{}, "history")
			next := int64(100)
			calls := 0
			s := fakeConversationMessageService{list: func(gotCtx context.Context, id, userID int64, beforeID *int64) (*conversation.MessagePage, error) {
				calls++
				if gotCtx.Value(traceKey{}) != "history" || id != 7 || userID != 42 {
					t.Fatal("incorrect service arguments")
				}
				if query == "" {
					if beforeID != nil {
						t.Fatal("first page must use nil cursor")
					}
				} else {
					want := int64(150)
					if query != "before_id=150" {
						want = 9223372036854775807
					}
					if beforeID == nil || *beforeID != want {
						t.Fatal("incorrect cursor")
					}
				}
				return &conversation.MessagePage{Messages: []conversation.Message{{ID: 100, ConversationID: 7, SenderUserID: 42, Text: "Привет", CreatedAt: time.Date(2026, 11, 23, 15, 0, 0, 0, time.FixedZone("UTC+3", 10800))}}, NextCursor: &next}, nil
			}}
			req := conversationMessageListRequest(query + "&user_id=999").WithContext(ctx)
			req.Header.Set("X-User-ID", "999")
			rec := httptest.NewRecorder()
			conversationMessageListHandler(s).ServeHTTP(rec, req)
			if rec.Code != 200 || calls != 1 || rec.Header().Get("Content-Type") != "application/json" || rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("Set-Cookie") != "" {
				t.Fatalf("unexpected response: %d %v %s", rec.Code, rec.Header(), rec.Body.String())
			}
			var body struct {
				Messages   []map[string]any `json:"messages"`
				NextCursor *int64           `json:"next_cursor"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if len(body.Messages) != 1 || body.NextCursor == nil || *body.NextCursor != next {
				t.Fatalf("unexpected page: %s", rec.Body.String())
			}
			message := body.Messages[0]
			if len(message) != 5 || message["id"] != float64(100) || message["conversation_id"] != float64(7) || message["sender_user_id"] != float64(42) || message["text"] != "Привет" || message["created_at"] != "2026-11-23T12:00:00Z" {
				t.Fatalf("unexpected message: %v", message)
			}
		})
	}
}

func TestConversationMessageHandlerListEmpty(t *testing.T) {
	s := fakeConversationMessageService{list: func(context.Context, int64, int64, *int64) (*conversation.MessagePage, error) {
		return &conversation.MessagePage{}, nil
	}}
	rec := httptest.NewRecorder()
	conversationMessageListHandler(s).ServeHTTP(rec, conversationMessageListRequest(""))
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &fields); err != nil {
		t.Fatal(err)
	}
	if rec.Code != 200 || len(fields) != 2 || string(fields["messages"]) != "[]" || string(fields["next_cursor"]) != "null" {
		t.Fatalf("unexpected empty page: %s", rec.Body.String())
	}
}

func TestConversationMessageHandlerListInvalidRequests(t *testing.T) {
	for _, query := range []string{"before_id=", "before_id", "before_id=0", "before_id=-1", "before_id=abc", "before_id=1.5", "before_id=9223372036854775808", "before_id=1&before_id=2", "before_id=%XX", "before_id=1;other=2"} {
		t.Run(query, func(t *testing.T) {
			rec := httptest.NewRecorder()
			conversationMessageListHandler(nil).ServeHTTP(rec, conversationMessageListRequest(query))
			assertConversationError(t, rec, 400, conversation.ErrInvalidCursor.Error())
		})
	}
	for _, id := range []string{"", "0", "-1", "abc", "9223372036854775808"} {
		req := conversationMessageListRequest("")
		req.SetPathValue("id", id)
		rec := httptest.NewRecorder()
		conversationMessageListHandler(nil).ServeHTTP(rec, req)
		assertConversationError(t, rec, 400, "invalid conversation ID")
	}
}

func TestConversationMessageHandlerListRequiresIdentity(t *testing.T) {
	for _, cookie := range []string{"", "invalid"} {
		req := conversationMessageListRequest("before_id=bad")
		req.SetPathValue("id", "bad")
		req.Header.Del("Cookie")
		if cookie != "" {
			req.AddCookie(&http.Cookie{Name: "chat_session", Value: cookie})
		}
		rec := httptest.NewRecorder()
		conversationMessageListHandler(nil).ServeHTTP(rec, req)
		assertConversationError(t, rec, 401, session.ErrInvalid.Error())
	}
}

func TestConversationMessageHandlerListErrors(t *testing.T) {
	for _, tt := range []struct {
		err     error
		status  int
		message string
	}{
		{conversation.ErrNotFound, 404, conversation.ErrNotFound.Error()},
		{conversation.ErrInvalidCursor, 400, conversation.ErrInvalidCursor.Error()},
		{errors.New("database details"), 500, "internal server error"},
	} {
		s := fakeConversationMessageService{list: func(context.Context, int64, int64, *int64) (*conversation.MessagePage, error) {
			return nil, fmt.Errorf("wrapped: %w", tt.err)
		}}
		rec := httptest.NewRecorder()
		conversationMessageListHandler(s).ServeHTTP(rec, conversationMessageListRequest(""))
		assertConversationError(t, rec, tt.status, tt.message)
	}
}
