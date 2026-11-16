package user_test

import (
	"encoding/json"
	"strings"
	"testing"

	"chat/internal/model/user"
)

func TestUserJSONExcludesPasswordHash(t *testing.T) {
	u := user.User{ID: 42, Username: "ivan", PasswordHash: "private-password-hash"}
	data, err := json.Marshal(u)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	if _, exists := fields["PasswordHash"]; exists || strings.Contains(string(data), u.PasswordHash) {
		t.Fatal("JSON must not contain the password hash field or its value")
	}
}
