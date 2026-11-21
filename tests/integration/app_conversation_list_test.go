//go:build integration

package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"chat/internal/dto"
)

func testAppConversationList(t *testing.T, ctx context.Context, client *http.Client, baseURL string, first dto.ConversationResponse, firstCookie, secondCookie, thirdCookie *http.Cookie) {
	t.Helper()
	list := func(cookie *http.Cookie, want ...dto.ConversationResponse) {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/conversations?user_id=999", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("X-User-ID", "999")
		req.AddCookie(cookie)
		response, data := doAppRequest(t, client, req, http.StatusOK)
		if response.Header.Get("Content-Type") != "application/json" || response.Header.Get("Cache-Control") != "no-store" || response.Header.Get("Set-Cookie") != "" {
			t.Fatal("list must return JSON without caching or a new cookie")
		}
		var got []dto.ConversationResponse
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatal(err)
		}
		if got == nil || len(got) != len(want) {
			t.Fatalf("list = %s, want an array of %d conversations", data, len(want))
		}
		for i := range want {
			if got[i].ID != want[i].ID || got[i].Type != want[i].Type || !got[i].CreatedAt.Equal(want[i].CreatedAt) {
				t.Fatalf("item %d = %+v, want %+v", i, got[i], want[i])
			}
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/conversations", nil)
	if err != nil {
		t.Fatal(err)
	}
	doAppRequest(t, client, req, http.StatusUnauthorized)
	list(firstCookie, first)
	list(secondCookie, first)
	list(thirdCookie)

	create, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/conversations/direct",
		strings.NewReader(`{"recipient_username":"oleg"}`))
	if err != nil {
		t.Fatal(err)
	}
	create.Header.Set("Content-Type", "application/json")
	create.AddCookie(secondCookie)
	_, data := doAppRequest(t, client, create, http.StatusOK)
	var second dto.ConversationResponse
	if err := json.Unmarshal(data, &second); err != nil {
		t.Fatal(err)
	}
	if second.ID <= 0 || second.ID == first.ID {
		t.Fatal("another pair must create a distinct conversation")
	}

	list(firstCookie, first)
	list(secondCookie, second, first)
	list(thirdCookie, second)
}
