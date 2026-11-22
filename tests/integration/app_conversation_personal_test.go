//go:build integration

package integration_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"chat/internal/dto"
	"chat/internal/model/conversation"
	"chat/internal/repository"
)

func testAppConversationPersonal(t *testing.T, ctx context.Context, db *sql.DB, client *http.Client, baseURL string, firstCookie, secondCookie *http.Cookie) {
	t.Helper()
	request := func(method, path string, cookie *http.Cookie, crossOrigin bool, status int) []byte {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, method, baseURL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		if cookie != nil {
			req.AddCookie(cookie)
		}
		if crossOrigin {
			req.Header.Set("Origin", "https://other.example")
			req.Header.Set("Sec-Fetch-Site", "cross-site")
		}
		response, data := doAppRequest(t, client, req, status)
		if response.Header.Get("Content-Type") != "application/json" || response.Header.Get("Set-Cookie") != "" {
			t.Fatal("personal conversation response must be JSON without a new cookie")
		}
		if !crossOrigin && response.Header.Get("Cache-Control") != "no-store" {
			t.Fatal("personal conversation response must disable caching")
		}
		return data
	}
	list := func(cookie *http.Cookie) []dto.ConversationResponse {
		t.Helper()
		var got []dto.ConversationResponse
		if err := json.Unmarshal(request(http.MethodGet, "/conversations", cookie, false, http.StatusOK), &got); err != nil {
			t.Fatal(err)
		}
		return got
	}
	firstList, secondList := list(firstCookie), list(secondCookie)
	request(http.MethodGet, "/conversations/personal", firstCookie, false, http.StatusBadRequest)
	request(http.MethodPost, "/conversations/personal", nil, false, http.StatusUnauthorized)
	request(http.MethodPost, "/conversations/personal", firstCookie, true, http.StatusForbidden)
	var count int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM conversations_personal").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("registration, reads and rejected requests must not create personal conversations")
	}

	var created [2]dto.ConversationResponse
	for i, cookie := range []*http.Cookie{firstCookie, secondCookie} {
		if err := json.Unmarshal(request(http.MethodPost, "/conversations/personal?user_id=999", cookie, false, http.StatusOK), &created[i]); err != nil {
			t.Fatal(err)
		}
		if created[i].ID <= 0 || created[i].Type != conversation.TypePersonal || created[i].CreatedAt.IsZero() {
			t.Fatalf("unexpected personal conversation: %+v", created[i])
		}
		var repeated dto.ConversationResponse
		if err := json.Unmarshal(request(http.MethodPost, "/conversations/personal", cookie, false, http.StatusOK), &repeated); err != nil {
			t.Fatal(err)
		}
		if repeated.ID != created[i].ID || repeated.Type != created[i].Type || !repeated.CreatedAt.Equal(created[i].CreatedAt) {
			t.Fatal("repeated creation must preserve ID, type and creation time")
		}
		var read dto.ConversationResponse
		path := "/conversations/" + strconv.FormatInt(created[i].ID, 10)
		if err := json.Unmarshal(request(http.MethodGet, path, cookie, false, http.StatusOK), &read); err != nil {
			t.Fatal(err)
		}
		if read.ID != created[i].ID || read.Type != created[i].Type || !read.CreatedAt.Equal(created[i].CreatedAt) {
			t.Fatal("owner must read the same personal conversation through the shared endpoint")
		}
	}
	if created[0].ID == created[1].ID {
		t.Fatal("different owners must have different personal conversations")
	}
	for i, cookie := range []*http.Cookie{firstCookie, secondCookie} {
		foreignPath := "/conversations/" + strconv.FormatInt(created[1-i].ID, 10)
		foreign := request(http.MethodGet, foreignPath, cookie, false, http.StatusNotFound)
		missing := request(http.MethodGet, "/conversations/9223372036854775807", cookie, false, http.StatusNotFound)
		if string(foreign) != string(missing) {
			t.Fatal("foreign and missing personal conversations must be indistinguishable")
		}
		original := [][]dto.ConversationResponse{firstList, secondList}[i]
		want := append([]dto.ConversationResponse{created[i]}, original...)
		got := list(cookie)
		if len(got) != len(want) {
			t.Fatalf("list length=%d, want %d", len(got), len(want))
		}
		for j := range want {
			if got[j].ID != want[j].ID || got[j].Type != want[j].Type || !got[j].CreatedAt.Equal(want[j].CreatedAt) {
				t.Fatal("list must include only the owner's personal conversation and preserve existing conversations")
			}
		}
	}
	for i, username := range []string{"ivan", "anna"} {
		owner, err := repository.NewUserRepository(db).GetByUsername(ctx, username)
		if err != nil {
			t.Fatal(err)
		}
		c := conversation.Conversation{ID: created[i].ID, Type: created[i].Type, CreatedAt: created[i].CreatedAt}
		assertConversationPersonal(t, ctx, db, &c, owner.ID)
	}
	assertConversationCounts(t, ctx, db, 4, 2, 6)
}
