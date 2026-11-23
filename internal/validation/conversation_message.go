package validation

import (
	"errors"
	"strings"
	"unicode/utf8"
)

var ErrInvalidConversationMessageText = errors.New("message text must contain 1 to 4000 Unicode code points, must not be whitespace-only or contain U+0000")

func ValidateConversationMessageText(text string) error {
	if !utf8.ValidString(text) || strings.ContainsRune(text, '\x00') {
		return ErrInvalidConversationMessageText
	}

	if utf8.RuneCountInString(text) > 4000 || strings.TrimSpace(text) == "" {
		return ErrInvalidConversationMessageText
	}

	return nil
}
