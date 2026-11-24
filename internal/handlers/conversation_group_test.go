package handlers_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"chat/internal/handlers"
	"chat/internal/model/conversation"
	"chat/internal/model/session"
	"chat/internal/model/user"
	"chat/internal/validation"
)

func conversationGroupHandler(s handlers.ConversationService) http.Handler {
	return handlers.SessionMiddleware(conversationSessionAuthenticator{})(http.HandlerFunc(handlers.NewConversationHandler(s).CreateGroup))
}

func conversationGroupRequest(body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/conversations/group?owner_user_id=999", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", "999")
	req.AddCookie(&http.Cookie{Name: "chat_session", Value: "session-token"})
	return req
}

func TestConversationHandlerCreateGroup(t *testing.T) {
	type traceKey struct{}
	ctx := context.WithValue(t.Context(), traceKey{}, "group-trace")
	calls := 0
	s := fakeConversationService{createGroup: func(gotCtx context.Context, ownerID int64, name string, usernames []string) (*conversation.Group, error) {
		calls++
		if gotCtx.Value(traceKey{}) != "group-trace" || ownerID != 42 || name != " Семья " || !reflect.DeepEqual(usernames, []string{" ANNA "}) {
			t.Fatal("handler must pass raw input and verified identity to service")
		}
		return &conversation.Group{
			Conversation: conversation.Conversation{ID: int64(calls), Type: conversation.TypeGroup, CreatedAt: time.Date(2026, 11, 24, 15, 0, 0, 0, time.FixedZone("UTC+3", 10800))},
			OwnerUserID:  ownerID, Name: "Семья",
		}, nil
	}}
	for i := 1; i <= 2; i++ {
		rec := httptest.NewRecorder()
		conversationGroupHandler(s).ServeHTTP(rec, conversationGroupRequest(`{"name":" Семья ","usernames":[" ANNA "]}`).WithContext(ctx))
		if rec.Code != http.StatusCreated || rec.Header().Get("Content-Type") != "application/json" || rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("Set-Cookie") != "" {
			t.Fatalf("unexpected response: %d %s", rec.Code, rec.Body.String())
		}
		var body map[string]json.RawMessage
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if len(body) != 5 || string(body["id"]) != fmt.Sprint(i) || string(body["type"]) != `"group"` || string(body["owner_user_id"]) != "42" || string(body["name"]) != `"Семья"` || string(body["created_at"]) != `"2026-11-24T12:00:00Z"` {
			t.Fatalf("unexpected body: %s", rec.Body.String())
		}
	}
	if calls != 2 {
		t.Fatalf("calls = %d", calls)
	}
}

func TestConversationHandlerCreateGroupInvalidRequest(t *testing.T) {
	for _, body := range []string{"", "null", "{", "[]", `{} {}`, `{"name":1}`, `{"usernames":"anna"}`, `{"owner_user_id":999}`, string([]byte{0xff})} {
		t.Run(body, func(t *testing.T) {
			rec := httptest.NewRecorder()
			conversationGroupHandler(nil).ServeHTTP(rec, conversationGroupRequest(body))
			assertConversationError(t, rec, http.StatusBadRequest, "invalid request body")
		})
	}
	rec := httptest.NewRecorder()
	conversationGroupHandler(nil).ServeHTTP(rec, conversationGroupRequest(strings.Repeat(" ", (16<<10)+1)))
	assertConversationError(t, rec, http.StatusRequestEntityTooLarge, "request body too large")
	req := conversationGroupRequest(`{}`)
	req.Header.Set("Content-Type", "text/plain")
	rec = httptest.NewRecorder()
	conversationGroupHandler(nil).ServeHTTP(rec, req)
	assertConversationError(t, rec, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
}

func TestConversationHandlerCreateGroupRequiresIdentity(t *testing.T) {
	for _, middleware := range []bool{false, true} {
		var handler http.Handler = http.HandlerFunc(handlers.NewConversationHandler(nil).CreateGroup)
		if middleware {
			handler = conversationGroupHandler(nil)
		}
		req := conversationGroupRequest(`{}`)
		req.Header.Del("Cookie")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		assertConversationError(t, rec, http.StatusUnauthorized, session.ErrInvalid.Error())
	}
}

func TestConversationHandlerCreateGroupErrors(t *testing.T) {
	for _, tt := range []struct {
		err     error
		status  int
		message string
	}{
		{validation.ErrInvalidConversationGroupName, 400, validation.ErrInvalidConversationGroupName.Error()},
		{conversation.ErrInvalidGroupParticipants, 400, conversation.ErrInvalidGroupParticipants.Error()},
		{validation.ErrInvalidUsername, 400, validation.ErrInvalidUsername.Error()},
		{user.ErrNotFound, 404, user.ErrNotFound.Error()},
		{errors.New("private database details"), 500, "internal server error"},
	} {
		t.Run(tt.message, func(t *testing.T) {
			s := fakeConversationService{createGroup: func(context.Context, int64, string, []string) (*conversation.Group, error) {
				return nil, fmt.Errorf("create group: %w", tt.err)
			}}
			rec := httptest.NewRecorder()
			conversationGroupHandler(s).ServeHTTP(rec, conversationGroupRequest(`{}`))
			assertConversationError(t, rec, tt.status, tt.message)
		})
	}
}
