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

func TestConversationHandlerGetByID(t *testing.T) {
	type traceKey struct{}
	ctx := context.WithValue(t.Context(), traceKey{}, "read-trace")
	calls := 0
	s := fakeConversationService{getByID: func(gotCtx context.Context, conversationID, currentUserID int64) (*conversation.Conversation, error) {
		calls++
		if gotCtx.Value(traceKey{}) != "read-trace" || conversationID != 7 || currentUserID != 42 {
			t.Fatal("service must receive path ID, verified user ID and request context")
		}
		return &conversation.Conversation{
			ID: 7, Type: conversation.TypeDirect,
			CreatedAt: time.Date(2026, 11, 21, 15, 0, 0, 0, time.FixedZone("UTC+3", 3*60*60)),
		}, nil
	}}
	handler := handlers.SessionMiddleware(conversationSessionAuthenticator{})(http.HandlerFunc(handlers.NewConversationHandler(s).GetByID))
	req := httptest.NewRequest(http.MethodGet, "/conversations/7?user_id=999", nil).WithContext(ctx)
	req.SetPathValue("id", "7")
	req.Header.Set("X-User-ID", "999")
	req.AddCookie(&http.Cookie{Name: "chat_session", Value: "session-token"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || calls != 1 {
		t.Fatalf("status=%d, calls=%d; want 200 and 1", rec.Code, calls)
	}
	if rec.Header().Get("Content-Type") != "application/json" || rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("Set-Cookie") != "" {
		t.Fatal("read must return JSON with caching disabled and no new cookie")
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body) != 3 || body["id"] != float64(7) || body["type"] != "direct" || body["created_at"] != "2026-11-21T12:00:00Z" {
		t.Fatalf("unexpected response: %s", rec.Body.String())
	}
}

func TestConversationHandlerGetByIDInvalidRequests(t *testing.T) {
	for _, tt := range []struct {
		name          string
		id            string
		authenticated bool
		status        int
		message       string
	}{
		{"missing identity", "7", false, 401, session.ErrInvalid.Error()},
		{"identity checked before ID", "invalid", false, 401, session.ErrInvalid.Error()},
		{"empty ID", "", true, 400, "invalid conversation ID"},
		{"non numeric ID", "abc", true, 400, "invalid conversation ID"},
		{"zero ID", "0", true, 400, "invalid conversation ID"},
		{"negative ID", "-1", true, 400, "invalid conversation ID"},
		{"overflow", "9223372036854775808", true, 400, "invalid conversation ID"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/conversations/id", nil)
			req.SetPathValue("id", tt.id)
			var handler http.Handler = http.HandlerFunc(handlers.NewConversationHandler(nil).GetByID)
			if tt.authenticated {
				req.AddCookie(&http.Cookie{Name: "chat_session", Value: "session-token"})
				handler = handlers.SessionMiddleware(conversationSessionAuthenticator{})(handler)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			assertConversationError(t, rec, tt.status, tt.message)
		})
	}
}

func TestConversationHandlerGetByIDServiceErrors(t *testing.T) {
	for _, tt := range []struct {
		name    string
		err     error
		status  int
		message string
	}{
		{"missing conversation", conversation.ErrNotFound, 404, conversation.ErrNotFound.Error()},
		{"outsider", conversation.ErrNotFound, 404, conversation.ErrNotFound.Error()},
		{"storage failure", errors.New("private database details"), 500, "internal server error"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := fakeConversationService{getByID: func(context.Context, int64, int64) (*conversation.Conversation, error) {
				return nil, fmt.Errorf("read failed: %w", tt.err)
			}}
			handler := handlers.SessionMiddleware(conversationSessionAuthenticator{})(http.HandlerFunc(handlers.NewConversationHandler(s).GetByID))
			req := httptest.NewRequest(http.MethodGet, "/conversations/7", nil)
			req.SetPathValue("id", "7")
			req.AddCookie(&http.Cookie{Name: "chat_session", Value: "session-token"})
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			assertConversationError(t, rec, tt.status, tt.message)
		})
	}
}
