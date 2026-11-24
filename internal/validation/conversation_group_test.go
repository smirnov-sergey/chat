package validation_test

import (
	"errors"
	"strings"
	"testing"

	"chat/internal/validation"
)

func TestValidateConversationGroupName(t *testing.T) {
	for _, name := range []string{"Семья", "😀", strings.Repeat("я", 100)} {
		if err := validation.ValidateConversationGroupName(name); err != nil {
			t.Fatalf("valid name %q: %v", name, err)
		}
	}
	for _, name := range []string{"", " \t\n\u2003", strings.Repeat("я", 101), "a\x00b", string([]byte{0xff})} {
		if err := validation.ValidateConversationGroupName(name); !errors.Is(err, validation.ErrInvalidConversationGroupName) {
			t.Fatalf("invalid name %q: %v", name, err)
		}
	}
}
