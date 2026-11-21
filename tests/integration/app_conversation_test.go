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

	"chat/internal/dto"
	"chat/internal/model/conversation"
	"chat/internal/repository"
)

func testAppConversationDirect(t *testing.T, ctx context.Context, db *sql.DB, client *http.Client, baseURL string, firstCookie, secondCookie *http.Cookie) {
	t.Helper()
	request := func(body string, cookie *http.Cookie, crossOrigin bool, wantStatus int) []byte {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/conversations/direct", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		if cookie != nil {
			req.AddCookie(cookie)
		}
		if crossOrigin {
			req.Header.Set("Origin", "https://other.example")
			req.Header.Set("Sec-Fetch-Site", "cross-site")
		}
		response, data := doAppRequest(t, client, req, wantStatus)
		if response.Header.Get("Content-Type") != "application/json" || response.Header.Get("Set-Cookie") != "" {
			t.Fatal("conversation response must be JSON without a new session cookie")
		}
		if !crossOrigin && response.Header.Get("Cache-Control") != "no-store" {
			t.Fatal("conversation response must disable caching")
		}
		return data
	}

	const body = `{"recipient_username":" ANNA "}`
	request(body, nil, false, http.StatusUnauthorized)
	request(body, firstCookie, true, http.StatusForbidden)
	request(`{"recipient_username":"ivan"}`, firstCookie, false, http.StatusBadRequest)
	request(`{"recipient_username":"missing"}`, firstCookie, false, http.StatusNotFound)
	request(`{"recipient_username":"a"}`, firstCookie, false, http.StatusBadRequest)
	request(`{"recipient_username":"anna","current_user_id":999}`, firstCookie, false, http.StatusBadRequest)
	assertConversationCounts(t, ctx, db, 0, 0, 0)

	var first dto.ConversationResponse
	if err := json.Unmarshal(request(body, firstCookie, false, http.StatusOK), &first); err != nil {
		t.Fatal(err)
	}
	if first.ID <= 0 || first.Type != conversation.TypeDirect || first.CreatedAt.IsZero() {
		t.Fatalf("unexpected conversation: %+v", first)
	}
	for _, tt := range []struct {
		body   string
		cookie *http.Cookie
	}{
		{body, firstCookie},
		{`{"recipient_username":" IVAN "}`, secondCookie},
	} {
		var repeated dto.ConversationResponse
		if err := json.Unmarshal(request(tt.body, tt.cookie, false, http.StatusOK), &repeated); err != nil {
			t.Fatal(err)
		}
		if repeated.ID != first.ID || repeated.Type != first.Type || !repeated.CreatedAt.Equal(first.CreatedAt) {
			t.Fatal("both users must receive the same conversation without changing its creation time")
		}
	}
	users := repository.NewUserRepository(db)
	ivan, err := users.GetByUsername(ctx, "ivan")
	if err != nil {
		t.Fatal(err)
	}
	anna, err := users.GetByUsername(ctx, "anna")
	if err != nil {
		t.Fatal(err)
	}
	stored, err := repository.NewConversationRepository(db).GetDirectByUsers(ctx, ivan.ID, anna.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.ID != first.ID || !stored.CreatedAt.Equal(first.CreatedAt) {
		t.Fatal("HTTP response must match the stored conversation")
	}
	assertConversationDirect(t, ctx, db, stored, ivan.ID, anna.ID)
	assertConversationCounts(t, ctx, db, 1, 1, 2)
	testAppConversationRead(t, ctx, client, baseURL, first, firstCookie, secondCookie)
	assertConversationDirect(t, ctx, db, stored, ivan.ID, anna.ID)
	assertConversationCounts(t, ctx, db, 2, 2, 4)
}

func testAppConversationRead(t *testing.T, ctx context.Context, client *http.Client, baseURL string, want dto.ConversationResponse, firstCookie, secondCookie *http.Cookie) {
	t.Helper()
	read := func(id string, cookie *http.Cookie, wantStatus int) []byte {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/conversations/"+id+"?user_id=999", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("X-User-ID", "999")
		if cookie != nil {
			req.AddCookie(cookie)
		}
		response, data := doAppRequest(t, client, req, wantStatus)
		if response.Header.Get("Content-Type") != "application/json" || response.Header.Get("Cache-Control") != "no-store" || response.Header.Get("Set-Cookie") != "" {
			t.Fatal("reading must return JSON without caching or changing the session cookie")
		}
		return data
	}

	id := strconv.FormatInt(want.ID, 10)
	read(id, nil, http.StatusUnauthorized)
	read("invalid", nil, http.StatusUnauthorized)
	read("invalid", firstCookie, http.StatusBadRequest)
	for _, cookie := range []*http.Cookie{firstCookie, secondCookie} {
		var got dto.ConversationResponse
		if err := json.Unmarshal(read(id, cookie, http.StatusOK), &got); err != nil {
			t.Fatal(err)
		}
		if got.ID != want.ID || got.Type != want.Type || !got.CreatedAt.Equal(want.CreatedAt) {
			t.Fatalf("read = %+v, want %+v", got, want)
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/auth/register",
		strings.NewReader(`{"username":"oleg","name":"Oleg","password":"third personal password"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	registered, _ := doAppRequest(t, client, req, http.StatusCreated)
	cookies := registered.Cookies()
	if len(cookies) != 1 {
		t.Fatal("third registration must create a session")
	}
	outsider := read(id, cookies[0], http.StatusNotFound)
	missing := read("9223372036854775807", firstCookie, http.StatusNotFound)
	if string(outsider) != string(missing) {
		t.Fatal("outsider and missing conversation must receive identical responses")
	}
	var body map[string]string
	if err := json.Unmarshal(outsider, &body); err != nil || len(body) != 1 || body["error"] != conversation.ErrNotFound.Error() {
		t.Fatalf("unexpected inaccessible conversation response: %s", outsider)
	}
	testAppConversationList(t, ctx, client, baseURL, want, firstCookie, secondCookie, cookies[0])

	logout, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/auth/logout", nil)
	if err != nil {
		t.Fatal(err)
	}
	logout.AddCookie(cookies[0])
	doAppRequest(t, client, logout, http.StatusNoContent)
	read(id, cookies[0], http.StatusUnauthorized)
	list, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/conversations", nil)
	if err != nil {
		t.Fatal(err)
	}
	list.AddCookie(cookies[0])
	doAppRequest(t, client, list, http.StatusUnauthorized)
}
