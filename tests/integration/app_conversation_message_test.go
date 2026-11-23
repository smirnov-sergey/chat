//go:build integration

package integration_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"chat/internal/dto"
	"chat/internal/model/conversation"
	"chat/internal/repository"
)

func testAppConversationMessages(t *testing.T, ctx context.Context, db *sql.DB, client *http.Client, baseURL string, firstCookie, secondCookie *http.Cookie) {
	t.Helper()
	users := repository.NewUserRepository(db)
	first, err := users.GetByUsername(ctx, "ivan")
	if err != nil {
		t.Fatal(err)
	}
	second, err := users.GetByUsername(ctx, "anna")
	if err != nil {
		t.Fatal(err)
	}
	conversations := repository.NewConversationRepository(db)
	direct, err := conversations.GetDirectByUsers(ctx, first.ID, second.ID)
	if err != nil {
		t.Fatal(err)
	}
	personal, err := conversations.GetPersonalByUser(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	request := func(id int64, body string, cookie *http.Cookie, crossOrigin bool, status int) []byte {
		t.Helper()
		path := "/conversations/" + strconv.FormatInt(id, 10) + "/messages?user_id=999"
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-User-ID", "999")
		if cookie != nil {
			req.AddCookie(cookie)
		}
		if crossOrigin {
			req.Header.Set("Origin", "https://other.example")
			req.Header.Set("Sec-Fetch-Site", "cross-site")
		}
		response, data := doAppRequest(t, client, req, status)
		if response.Header.Get("Content-Type") != "application/json" || response.Header.Get("Set-Cookie") != "" {
			t.Fatal("message response must be JSON without a new cookie")
		}
		if !crossOrigin && response.Header.Get("Cache-Control") != "no-store" {
			t.Fatal("message response must disable caching")
		}
		return data
	}
	var initialCount int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM conversations_messages").Scan(&initialCount); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		conversationID, senderID int64
		cookie                   *http.Cookie
		text                     string
	}{
		{direct.ID, first.ID, firstCookie, "  Привет 👋\nВторая строка  "},
		{direct.ID, second.ID, secondCookie, "Ответ"},
		{personal.ID, first.ID, firstCookie, strings.Repeat("я", 4000)},
	} {
		body, err := json.Marshal(map[string]string{"text": tt.text})
		if err != nil {
			t.Fatal(err)
		}
		before := time.Now().UTC().Truncate(time.Microsecond)
		data := request(tt.conversationID, string(body), tt.cookie, false, http.StatusCreated)
		var got dto.ConversationMessageResponse
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatal(err)
		}
		if got.ID <= 0 || got.ConversationID != tt.conversationID || got.SenderUserID != tt.senderID || got.Text != tt.text || got.CreatedAt.Before(before) || got.CreatedAt.After(time.Now().UTC()) {
			t.Fatalf("unexpected message: %+v", got)
		}
		assertConversationMessage(t, ctx, db, conversation.Message{ID: got.ID, ConversationID: got.ConversationID, SenderUserID: got.SenderUserID, Text: got.Text, CreatedAt: got.CreatedAt})
	}
	foreign := request(personal.ID, `{"text":"forbidden"}`, secondCookie, false, 404)
	missing := request(9223372036854775807, `{"text":"missing"}`, secondCookie, false, 404)
	if string(foreign) != string(missing) {
		t.Fatal("foreign and missing conversations must produce identical responses")
	}
	request(direct.ID, `{"text":"anonymous"}`, nil, false, 401)
	request(direct.ID, `{"text":"cross origin"}`, firstCookie, true, 403)
	request(direct.ID, `{"text":"forged","sender_user_id":999}`, firstCookie, false, 400)
	for _, body := range []string{`{}`, `{"text":null}`, `{"text":" \n\t"}`, `{"text":"a\u0000b"}`, `{"text":"` + strings.Repeat("я", 4001) + `"}`, "{\"text\":\"\xff\"}"} {
		request(direct.ID, body, firstCookie, false, 400)
	}
	var finalCount int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM conversations_messages").Scan(&finalCount); err != nil {
		t.Fatal(err)
	}
	if finalCount != initialCount+3 {
		t.Fatalf("message count = %d, want %d; rejected requests must not insert messages", finalCount, initialCount+3)
	}
}
