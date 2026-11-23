package validation_test

import (
	"errors"
	"strings"
	"testing"

	"chat/internal/validation"
)

func TestValidateConversationMessageText(t *testing.T) {
	for _, tt := range []struct {
		name  string
		text  string
		valid bool
	}{
		{"empty", "", false},
		{"whitespace", " \n\r\t", false},
		{"Unicode whitespace", "\u00a0\u2003\u2028", false},
		{"single rune", "я", true},
		{"formatting preserved", "  Привет 👋\n\tмир  ", true},
		{"ASCII limit", strings.Repeat("a", 4000), true},
		{"ASCII over limit", strings.Repeat("a", 4001), false},
		{"Unicode limit", strings.Repeat("👋", 4000), true},
		{"Unicode over limit", strings.Repeat("я", 4001), false},
		{"combining code points", strings.Repeat("e\u0301", 2000), true},
		{"combining over limit", strings.Repeat("e\u0301", 2000) + "a", false},
		{"whitespace counts toward limit", "a" + strings.Repeat(" ", 4000), false},
		{"null", "hello\x00world", false},
		{"invalid UTF-8", "hello\xff", false},
		{"replacement character", "\ufffd", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := validation.ValidateConversationMessageText(tt.text)
			if tt.valid && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !tt.valid && !errors.Is(err, validation.ErrInvalidConversationMessageText) {
				t.Fatalf("error = %v, want invalid message text", err)
			}
		})
	}
}
