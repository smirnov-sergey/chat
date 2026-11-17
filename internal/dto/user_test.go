package dto_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"chat/internal/dto"
	"chat/internal/model/user"
)

func TestToUserResponse(t *testing.T) {
	zone := time.FixedZone("UTC+3", 3*60*60)
	u := user.User{
		ID:           42,
		Username:     "ivan",
		Name:         "Иван Петров",
		PasswordHash: "private-password-hash",
		CreatedAt:    time.Date(2026, 11, 17, 15, 0, 0, 123456789, zone),
		UpdatedAt:    time.Date(2026, 11, 18, 16, 0, 0, 987654321, zone),
	}
	original := u
	response := dto.ToUserResponse(&u)
	if response.ID != 42 || response.Username != "ivan" || response.Name != "Иван Петров" {
		t.Fatalf("unexpected profile fields: %+v", response)
	}
	if !response.CreatedAt.Equal(u.CreatedAt) || !response.UpdatedAt.Equal(u.UpdatedAt) {
		t.Fatal("conversion must preserve both timestamps, including nanosecond precision")
	}
	if response.CreatedAt.Location() != time.UTC || response.UpdatedAt.Location() != time.UTC {
		t.Fatal("both response timestamps must use UTC")
	}
	if u != original {
		t.Fatal("conversion must not modify the source user")
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
		"id":         `42`,
		"username":   `"ivan"`,
		"name":       `"Иван Петров"`,
		"created_at": `"2026-11-17T12:00:00.123456789Z"`,
		"updated_at": `"2026-11-18T13:00:00.987654321Z"`,
	}
	if len(fields) != len(want) {
		t.Fatalf("JSON must contain only the five public profile fields; got %s", data)
	}
	for name, value := range want {
		if string(fields[name]) != value {
			t.Errorf("JSON field %s = %s, want %s", name, fields[name], value)
		}
	}
	if strings.Contains(string(data), u.PasswordHash) {
		t.Fatal("JSON must not expose the password hash")
	}
}
