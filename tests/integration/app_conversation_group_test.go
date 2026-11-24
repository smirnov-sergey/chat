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

func testAppConversationGroup(t *testing.T, ctx context.Context, db *sql.DB, client *http.Client, baseURL string, firstCookie, secondCookie *http.Cookie) {
	t.Helper()
	request := func(method, path, body string, cookie *http.Cookie, status int) []byte {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, method, baseURL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		if cookie != nil {
			req.AddCookie(cookie)
		}
		response, data := doAppRequest(t, client, req, status)
		if response.Header.Get("Cache-Control") != "no-store" || response.Header.Get("Content-Type") != "application/json" || response.Header.Get("Set-Cookie") != "" {
			t.Fatal("group response must be JSON without caching or a new cookie")
		}
		return data
	}
	countGroups := func() int {
		t.Helper()
		var count int
		if err := db.QueryRowContext(ctx, "SELECT count(*) FROM conversations_group").Scan(&count); err != nil {
			t.Fatal(err)
		}
		return count
	}
	initialCount := countGroups()
	const body = `{"name":" Семья ","usernames":[" ANNA "]}`
	request("POST", "/conversations/group", body, nil, 401)
	for _, tt := range []struct {
		body   string
		status int
	}{
		{`{"name":" ","usernames":["anna"]}`, 400},
		{`{"name":"Team","usernames":[]}`, 400},
		{`{"name":"Team","usernames":["ivan"]}`, 400},
		{`{"name":"Team","usernames":["anna","ANNA"]}`, 400},
		{`{"name":"Team","usernames":["anna","missing"]}`, 404},
		{`{"name":"Team","usernames":["anna"],"owner_user_id":999}`, 400},
	} {
		request("POST", "/conversations/group", tt.body, firstCookie, tt.status)
	}
	if countGroups() != initialCount {
		t.Fatal("rejected requests must not create groups")
	}
	owner, err := repository.NewUserRepository(db).GetByUsername(ctx, "ivan")
	if err != nil {
		t.Fatal(err)
	}
	var created [2]dto.ConversationGroupResponse
	for i := range created {
		data := request("POST", "/conversations/group?owner_user_id=999", body, firstCookie, 201)
		if err := json.Unmarshal(data, &created[i]); err != nil {
			t.Fatal(err)
		}
		if created[i].ID <= 0 || created[i].Type != conversation.TypeGroup || created[i].OwnerUserID != owner.ID || created[i].Name != "Семья" || created[i].CreatedAt.IsZero() {
			t.Fatalf("unexpected group: %+v", created[i])
		}
	}
	if created[0].ID == created[1].ID || countGroups() != initialCount+2 {
		t.Fatal("repeated creation must create distinct groups")
	}
	path := "/conversations/" + strconv.FormatInt(created[0].ID, 10)
	for _, cookie := range []*http.Cookie{firstCookie, secondCookie} {
		var read dto.ConversationResponse
		if err := json.Unmarshal(request("GET", path, "", cookie, 200), &read); err != nil {
			t.Fatal(err)
		}
		if read.ID != created[0].ID || read.Type != conversation.TypeGroup {
			t.Fatalf("unexpected conversation: %+v", read)
		}
		var list []dto.ConversationListItemResponse
		if err := json.Unmarshal(request("GET", "/conversations", "", cookie, 200), &list); err != nil {
			t.Fatal(err)
		}
		found := 0
		for _, c := range list {
			if c.ID == created[0].ID || c.ID == created[1].ID {
				if c.Title != "Семья" {
					t.Fatalf("unexpected group title: %q", c.Title)
				}
				found++
			}
			if c.Type == conversation.TypePersonal && c.Title != "Избранное" {
				t.Fatalf("unexpected personal title: %q", c.Title)
			}
		}
		if found != 2 {
			t.Fatal("both groups must appear in each member's list")
		}
		request("POST", path+"/messages", `{"text":"Привет группе!"}`, cookie, 201)
	}
	var history dto.ConversationMessagePageResponse
	if err := json.Unmarshal(request("GET", path+"/messages", "", secondCookie, 200), &history); err != nil {
		t.Fatal(err)
	}
	if len(history.Messages) != 2 || history.NextCursor != nil {
		t.Fatalf("unexpected group history: %+v", history)
	}
	for _, message := range history.Messages {
		if message.ConversationID != created[0].ID || message.Text != "Привет группе!" {
			t.Fatalf("unexpected group message: %+v", message)
		}
	}
	if history.Messages[0].SenderUserID == history.Messages[1].SenderUserID {
		t.Fatal("history must include messages from both participants")
	}
	// Removing a non-owner membership simulates an outsider without another registration.
	member, err := repository.NewUserRepository(db).GetByUsername(ctx, "anna")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "DELETE FROM conversations_members WHERE conversation_id = $1 AND user_id = $2", created[0].ID, member.ID); err != nil {
		t.Fatal(err)
	}
	request("GET", path, "", secondCookie, 404)
	request("POST", path+"/messages", `{"text":"Нет доступа"}`, secondCookie, 404)
	request("GET", path+"/messages", "", secondCookie, 404)
	request("GET", path, "", firstCookie, 200)
}
