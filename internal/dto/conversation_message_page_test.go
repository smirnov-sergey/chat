package dto_test

import (
	"encoding/json"
	"testing"
	"time"

	"chat/internal/dto"
	"chat/internal/model/conversation"
)

func TestToConversationMessagePageResponse(t *testing.T) {
	cursor := int64(100)
	for _, next := range []*int64{nil, &cursor} {
		page := conversation.MessagePage{Messages: []conversation.Message{
			{ID: 101, ConversationID: 7, SenderUserID: 42, Text: "first", CreatedAt: time.Date(2026, 11, 23, 15, 0, 0, 0, time.FixedZone("UTC+3", 10800))},
			{ID: 100, ConversationID: 7, SenderUserID: 43, Text: "second"},
		}, NextCursor: next}
		original := page.Messages[0]
		got := dto.ToConversationMessagePageResponse(&page)
		if len(got.Messages) != 2 || got.Messages[0] != dto.ToConversationMessageResponse(&page.Messages[0]) || got.Messages[1] != dto.ToConversationMessageResponse(&page.Messages[1]) || got.NextCursor != next {
			t.Fatalf("unexpected conversion: %+v", got)
		}
		if page.Messages[0] != original {
			t.Fatal("conversion must not modify model")
		}
		got.Messages[0].Text = "changed"
		if page.Messages[0].Text != "first" {
			t.Fatal("response messages must not share the model slice")
		}
	}
	for _, messages := range [][]conversation.Message{nil, {}} {
		data, err := json.Marshal(dto.ToConversationMessagePageResponse(&conversation.MessagePage{Messages: messages}))
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != `{"messages":[],"next_cursor":null}` {
			t.Fatalf("unexpected empty response: %s", data)
		}
	}
}
