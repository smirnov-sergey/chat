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
	"chat/internal/service"
	"chat/internal/validation"
)

type fakeConversationMessageService func(context.Context, int64, int64, string) (*conversation.Message, error)

func (fake fakeConversationMessageService) Create(ctx context.Context, conversationID, userID int64, text string) (*conversation.Message, error) {
	return fake(ctx, conversationID, userID, text)
}

var _ handlers.ConversationMessageService = (*service.ConversationMessageService)(nil)

func conversationMessageHandler(s handlers.ConversationMessageService) http.Handler {
	h := handlers.NewConversationMessageHandler(s)
	return handlers.SessionMiddleware(conversationSessionAuthenticator{})(http.HandlerFunc(h.Create))
}

func conversationMessageRequest(body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/conversations/7/messages", strings.NewReader(body))
	req.SetPathValue("id", "7")
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "chat_session", Value: "session-token"})
	return req
}

func TestConversationMessageHandlerCreate(t *testing.T) {
	type traceKey struct{}
	ctx := context.WithValue(t.Context(), traceKey{}, "trace")
	text := "  Привет 👋\n\tмир  "
	body, err := json.Marshal(map[string]string{"text": text})
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	s := fakeConversationMessageService(func(gotCtx context.Context, conversationID, userID int64, gotText string) (*conversation.Message, error) {
		calls++
		if gotCtx.Value(traceKey{}) != "trace" || conversationID != 7 || userID != 42 || gotText != text {
			t.Fatal("service must receive request context, path ID, session user and unchanged text")
		}
		return &conversation.Message{ID: 9, ConversationID: 7, SenderUserID: 42, Text: text,
			CreatedAt: time.Date(2026, 11, 23, 15, 0, 0, 0, time.FixedZone("UTC+3", 3*60*60))}, nil
	})
	req := conversationMessageRequest(string(body)).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	rec := httptest.NewRecorder()
	conversationMessageHandler(s).ServeHTTP(rec, req)
	if rec.Code != 201 || calls != 1 || rec.Header().Get("Content-Type") != "application/json" || rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("Set-Cookie") != "" {
		t.Fatalf("unexpected response: %d %v %s", rec.Code, rec.Header(), rec.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 5 || got["id"] != float64(9) || got["conversation_id"] != float64(7) || got["sender_user_id"] != float64(42) || got["text"] != text || got["created_at"] != "2026-11-23T12:00:00Z" {
		t.Fatalf("unexpected response: %s", rec.Body.String())
	}
}

func TestConversationMessageHandlerInvalidRequests(t *testing.T) {
	for _, tt := range []struct {
		name, id, contentType, body string
		status                      int
		message                     string
	}{
		{"empty ID", "", "application/json", `{}`, 400, "invalid conversation ID"},
		{"zero ID", "0", "application/json", `{}`, 400, "invalid conversation ID"},
		{"negative ID", "-1", "application/json", `{}`, 400, "invalid conversation ID"},
		{"invalid ID", "abc", "application/json", `{}`, 400, "invalid conversation ID"},
		{"overflow ID", "9223372036854775808", "application/json", `{}`, 400, "invalid conversation ID"},
		{"missing content type", "7", "", `{}`, 415, "Content-Type must be application/json"},
		{"wrong content type", "7", "text/plain", `{}`, 415, "Content-Type must be application/json"},
		{"empty", "7", "application/json", "", 400, "invalid request body"},
		{"malformed", "7", "application/json", "{", 400, "invalid request body"},
		{"null", "7", "application/json", "null", 400, "invalid request body"},
		{"array", "7", "application/json", "[]", 400, "invalid request body"},
		{"wrong text type", "7", "application/json", `{"text":42}`, 400, "invalid request body"},
		{"forged sender", "7", "application/json", `{"text":"hi","sender_user_id":99}`, 400, "invalid request body"},
		{"forged conversation", "7", "application/json", `{"text":"hi","conversation_id":99}`, 400, "invalid request body"},
		{"multiple values", "7", "application/json", `{"text":"hi"}{}`, 400, "invalid request body"},
		{"trailing garbage", "7", "application/json", `{"text":"hi"}x`, 400, "invalid request body"},
		{"invalid UTF-8", "7", "application/json", "{\"text\":\"\xff\"}", 400, "invalid request body"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			req := conversationMessageRequest(tt.body)
			req.SetPathValue("id", tt.id)
			req.Header.Set("Content-Type", tt.contentType)
			rec := httptest.NewRecorder()
			conversationMessageHandler(nil).ServeHTTP(rec, req)
			assertConversationError(t, rec, tt.status, tt.message)
		})
	}
}

func TestConversationMessageHandlerRequiresIdentity(t *testing.T) {
	for _, cookie := range []string{"", "invalid"} {
		req := conversationMessageRequest("invalid JSON")
		req.Header.Del("Cookie")
		if cookie != "" {
			req.AddCookie(&http.Cookie{Name: "chat_session", Value: cookie})
		}
		rec := httptest.NewRecorder()
		conversationMessageHandler(nil).ServeHTTP(rec, req)
		assertConversationError(t, rec, 401, session.ErrInvalid.Error())
	}
}

func TestConversationMessageHandlerServiceErrors(t *testing.T) {
	for _, tt := range []struct {
		err     error
		status  int
		message string
	}{
		{conversation.ErrNotFound, 404, conversation.ErrNotFound.Error()},
		{validation.ErrInvalidConversationMessageText, 400, validation.ErrInvalidConversationMessageText.Error()},
		{errors.New("database password must not leak"), 500, "internal server error"},
	} {
		s := fakeConversationMessageService(func(context.Context, int64, int64, string) (*conversation.Message, error) {
			return nil, fmt.Errorf("wrapped: %w", tt.err)
		})
		rec := httptest.NewRecorder()
		conversationMessageHandler(s).ServeHTTP(rec, conversationMessageRequest(`{"text":"hi"}`))
		assertConversationError(t, rec, tt.status, tt.message)
	}
}

func TestConversationMessageHandlerBodyLimit(t *testing.T) {
	const limit = 64 << 10
	for _, size := range []int{limit, limit + 1} {
		body := `{"text":"hi"}`
		body += strings.Repeat(" ", size-len(body))
		called := false
		s := fakeConversationMessageService(func(context.Context, int64, int64, string) (*conversation.Message, error) {
			called = true
			return &conversation.Message{ID: 9}, nil
		})
		req := conversationMessageRequest(body)
		req.ContentLength = -1
		rec := httptest.NewRecorder()
		conversationMessageHandler(s).ServeHTTP(rec, req)
		if size == limit {
			if rec.Code != 201 || !called {
				t.Fatalf("boundary rejected: %d", rec.Code)
			}
		} else {
			if called {
				t.Fatal("oversized request reached service")
			}
			assertConversationError(t, rec, 413, "request body too large")
		}
	}
}

func TestConversationMessageHandlerEscapedUnicode(t *testing.T) {
	want := strings.Repeat("👋", 4000)
	s := fakeConversationMessageService(func(_ context.Context, _, _ int64, text string) (*conversation.Message, error) {
		if text != want {
			t.Fatal("escaped Unicode must decode unchanged")
		}
		return &conversation.Message{Text: text}, nil
	})
	rec := httptest.NewRecorder()
	body := `{"text":"` + strings.Repeat(`\ud83d\udc4b`, 4000) + `"}`
	conversationMessageHandler(s).ServeHTTP(rec, conversationMessageRequest(body))
	if rec.Code != 201 {
		t.Fatalf("escaped text rejected: %d %s", rec.Code, rec.Body.String())
	}
}
