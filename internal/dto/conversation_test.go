package dto_test

import (
	"encoding/json"
	"testing"
	"time"

	"chat/internal/dto"
	"chat/internal/model/conversation"
)

func TestToConversationResponse(t *testing.T) {
	c := conversation.Conversation{
		ID:        7,
		Type:      conversation.TypeDirect,
		CreatedAt: time.Date(2026, 11, 20, 15, 0, 0, 123456000, time.FixedZone("UTC+3", 3*60*60)),
	}
	original := c
	response := dto.ToConversationResponse(&c)
	if response.ID != c.ID || response.Type != c.Type || !response.CreatedAt.Equal(c.CreatedAt) {
		t.Fatalf("unexpected response: %+v", response)
	}
	if response.CreatedAt.Location() != time.UTC || c != original {
		t.Fatal("conversion must use UTC without modifying the source")
	}
	data, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"id":         `7`,
		"type":       `"direct"`,
		"created_at": `"2026-11-20T12:00:00.123456Z"`,
	}
	if len(fields) != len(want) {
		t.Fatalf("unexpected JSON fields: %s", data)
	}
	for name, value := range want {
		if string(fields[name]) != value {
			t.Errorf("field %s = %s, want %s", name, fields[name], value)
		}
	}
}

func TestToConversationListItemResponse(t *testing.T) {
	for _, tt := range []struct {
		kind  conversation.Type
		title string
	}{
		{conversation.TypeDirect, "Анна"},
		{conversation.TypePersonal, "Избранное"},
		{conversation.TypeGroup, "Семья"},
	} {
		t.Run(string(tt.kind), func(t *testing.T) {
			item := conversation.ListItem{ID: 7, Type: tt.kind, Title: tt.title, CreatedAt: time.Date(2026, 11, 24, 15, 0, 0, 0, time.FixedZone("UTC+3", 10800))}
			before := item
			response := dto.ToConversationListItemResponse(item)
			data, err := json.Marshal(response)
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(data, &fields); err != nil {
				t.Fatal(err)
			}
			var title string
			if err := json.Unmarshal(fields["title"], &title); err != nil {
				t.Fatal(err)
			}
			if len(fields) != 4 || title != tt.title || string(fields["type"]) != `"`+string(tt.kind)+`"` || string(fields["id"]) != "7" || string(fields["created_at"]) != `"2026-11-24T12:00:00Z"` {
				t.Fatalf("unexpected list item: %s", data)
			}
			if item != before || response.CreatedAt.Location() != time.UTC {
				t.Fatal("conversion must use UTC without changing the model")
			}
		})
	}
}
