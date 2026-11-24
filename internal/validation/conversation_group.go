package validation

import (
	"errors"
	"strings"
	"unicode/utf8"
)

var ErrInvalidConversationGroupName = errors.New("group name must contain 1 to 100 Unicode code points, must not be whitespace-only or contain U+0000")

func ValidateConversationGroupName(name string) error {
	if !utf8.ValidString(name) || strings.ContainsRune(name, '\x00') ||
		strings.TrimSpace(name) == "" || utf8.RuneCountInString(name) > 100 {
		return ErrInvalidConversationGroupName
	}
	return nil
}
