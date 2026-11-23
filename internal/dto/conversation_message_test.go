package dto_test

import (
	"encoding/json"
	"testing"
	"time"

	"chat/internal/dto"
	"chat/internal/model/conversation"
)

func TestToConversationMessageResponse(t *testing.T) {
	message := conversation.Message{
		ID: 9, ConversationID: 7, SenderUserID: 42, Text: " Привет\n",
		CreatedAt: time.Date(2026, 11, 23, 15, 0, 0, 123456000, time.FixedZone("UTC+3", 3*60*60)),
	}
	original := message
	response := dto.ToConversationMessageResponse(&message)
	if message != original || response.CreatedAt.Location() != time.UTC {
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
		"id": "9", "conversation_id": "7", "sender_user_id": "42",
		"text": `" Привет\n"`, "created_at": `"2026-11-23T12:00:00.123456Z"`,
	}
	if len(fields) != len(want) {
		t.Fatalf("unexpected fields: %s", data)
	}
	for name, value := range want {
		if string(fields[name]) != value {
			t.Errorf("field %s = %s, want %s", name, fields[name], value)
		}
	}
}
