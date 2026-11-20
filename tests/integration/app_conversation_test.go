//go:build integration

package integration_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
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
}
