package validation

import (
	"errors"
	"unicode"
	"unicode/utf8"
)

var (
	ErrInvalidUsername = errors.New("username must contain 3 to 32 lowercase ASCII letters, digits or underscores")
	ErrInvalidName     = errors.New("name must contain 1 to 80 Unicode code points without control characters or line breaks")
	ErrInvalidPassword = errors.New("password must contain 15 to 128 Unicode code points")
)

func ValidateUsername(username string) error {
	if len(username) < 3 || len(username) > 32 {
		return ErrInvalidUsername
	}

	for _, char := range username {
		if char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || char == '_' {
			continue
		}
		return ErrInvalidUsername
	}

	return nil
}

func ValidateName(name string) error {
	if !utf8.ValidString(name) {
		return ErrInvalidName
	}
	length := utf8.RuneCountInString(name)
	if length < 1 || length > 80 {
		return ErrInvalidName
	}

	for _, char := range name {
		if unicode.IsControl(char) || char == '\u2028' || char == '\u2029' {
			return ErrInvalidName
		}
	}

	return nil
}

func ValidatePassword(password string) error {
	if !utf8.ValidString(password) {
		return ErrInvalidPassword
	}
	length := utf8.RuneCountInString(password)
	if length < 15 || length > 128 {
		return ErrInvalidPassword
	}
	return nil
}
