//go:build integration

package integration_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"chat/internal/dto"
)

func testAppConversationMessageHistory(t *testing.T, ctx context.Context, db *sql.DB, client *http.Client, baseURL string, directID, personalID int64, firstCookie, secondCookie *http.Cookie) {
	t.Helper()
	request := func(method string, id int64, query, body string, cookie *http.Cookie, status int) []byte {
		t.Helper()
		path := baseURL + "/conversations/" + strconv.FormatInt(id, 10) + "/messages?user_id=999" + query
		req, err := http.NewRequestWithContext(ctx, method, path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		if method == http.MethodPost {
			req.Header.Set("Content-Type", "application/json")
		}
		if cookie != nil {
			req.AddCookie(cookie)
		}
		response, data := doAppRequest(t, client, req, status)
		if response.Header.Get("Content-Type") != "application/json" || response.Header.Get("Cache-Control") != "no-store" || response.Header.Get("Set-Cookie") != "" {
			t.Fatal("history must return uncached JSON without changing the session")
		}
		return data
	}
	read := func(id int64, query string, cookie *http.Cookie) dto.ConversationMessagePageResponse {
		t.Helper()
		var page dto.ConversationMessagePageResponse
		if err := json.Unmarshal(request(http.MethodGet, id, query, "", cookie, 200), &page); err != nil {
			t.Fatal(err)
		}
		if page.Messages == nil {
			t.Fatal("messages must be an array")
		}
		return page
	}
	first := read(directID, "", firstCookie)
	second := read(directID, "", secondCookie)
	if len(first.Messages) != 2 || len(second.Messages) != 2 || first.NextCursor != nil || second.NextCursor != nil {
		t.Fatal("both participants must see both previously sent messages")
	}
	for i := range first.Messages {
		if first.Messages[i] != second.Messages[i] {
			t.Fatal("participants must see the same history")
		}
	}
	if first.Messages[0].ID <= first.Messages[1].ID {
		t.Fatal("history must use descending IDs")
	}
	personal := read(personalID, "", firstCookie)
	if len(personal.Messages) != 1 || personal.Messages[0].ConversationID != personalID || personal.NextCursor != nil {
		t.Fatal("personal history must contain only its own message")
	}
	foreign := request(http.MethodGet, personalID, "", "", secondCookie, 404)
	missing := request(http.MethodGet, 9223372036854775807, "", "", secondCookie, 404)
	if string(foreign) != string(missing) {
		t.Fatal("foreign and missing histories must produce the same response")
	}
	request(http.MethodGet, directID, "", "", nil, 401)
	request(http.MethodGet, directID, "&before_id=0", "", firstCookie, 400)

	want := []dto.ConversationMessageResponse{first.Messages[1], first.Messages[0]}
	for i := 0; i < 49; i++ {
		body, err := json.Marshal(map[string]string{"text": fmt.Sprintf("History message %d", i)})
		if err != nil {
			t.Fatal(err)
		}
		cookie := firstCookie
		if i%2 != 0 {
			cookie = secondCookie
		}
		var message dto.ConversationMessageResponse
		if err := json.Unmarshal(request(http.MethodPost, directID, "", string(body), cookie, 201), &message); err != nil {
			t.Fatal(err)
		}
		want = append(want, message)
	}
	page := read(directID, "", firstCookie)
	if len(page.Messages) != 50 || page.NextCursor == nil || *page.NextCursor != want[1].ID {
		t.Fatal("51 messages must produce 50 messages and a continuation")
	}
	for i, message := range page.Messages {
		if message != want[len(want)-1-i] {
			t.Fatalf("unexpected message at position %d", i)
		}
	}
	request(http.MethodPost, directID, "", `{"text":"new between pages"}`, secondCookie, 201)
	last := read(directID, "&before_id="+strconv.FormatInt(*page.NextCursor, 10), secondCookie)
	if len(last.Messages) != 1 || last.Messages[0] != want[0] || last.NextCursor != nil {
		t.Fatal("new message must not shift the older page")
	}
	var beforeCount, afterCount int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM conversations_messages").Scan(&beforeCount); err != nil {
		t.Fatal(err)
	}
	empty := read(directID, "&before_id="+strconv.FormatInt(want[0].ID, 10), firstCookie)
	if len(empty.Messages) != 0 || empty.NextCursor != nil {
		t.Fatal("history before the oldest message must be empty")
	}
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM conversations_messages").Scan(&afterCount); err != nil {
		t.Fatal(err)
	}
	if afterCount != beforeCount {
		t.Fatal("reading history must not create messages")
	}
}
