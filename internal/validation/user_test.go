package validation_test

import (
	"errors"
	"strings"
	"testing"

	"chat/internal/validation"
)

func TestValidateUsername(t *testing.T) {
	for _, tt := range []struct {
		name     string
		username string
		valid    bool
	}{
		{"minimum length", "abc", true},
		{"maximum length", strings.Repeat("a", 32), true},
		{"letters digits underscores", "ivan_123", true},
		{"digits only", "123", true},
		{"underscores only", "___", true},
		{"empty", "", false},
		{"too short", "ab", false},
		{"too long", strings.Repeat("a", 33), false},
		{"uppercase", "Ivan", false},
		{"outer spaces", " ivan ", false},
		{"inner space", "iv an", false},
		{"hyphen", "iv-an", false},
		{"Cyrillic", "иван", false},
		{"emoji", "ivan😀", false},
		{"control character", "iv\tan", false},
		{"line break", "ivan\n", false},
		{"invalid UTF-8", "iv\xffan", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := validation.ValidateUsername(tt.username)
			if tt.valid {
				if err != nil {
					t.Fatalf("ValidateUsername(%q) = %v, want nil", tt.username, err)
				}
			} else if !errors.Is(err, validation.ErrInvalidUsername) {
				t.Fatalf("ValidateUsername(%q) = %v, want ErrInvalidUsername", tt.username, err)
			}
		})
	}
}

func TestValidateName(t *testing.T) {
	for _, tt := range []struct {
		name  string
		value string
		valid bool
	}{
		{"minimum length", "Я", true},
		{"maximum Unicode length", strings.Repeat("Я", 80), true},
		{"full name", "Иван Петров", true},
		{"preserve inner spaces", "Иван  Петров", true},
		{"punctuation", "Анна-Мария О’Коннор", true},
		{"emoji", "Иван 👨‍👩‍👧‍👦", true},
		{"maximum emoji length", strings.Repeat("😀", 80), true},
		{"empty", "", false},
		{"too long", strings.Repeat("Я", 81), false},
		{"line break", "Иван\nПетров", false},
		{"carriage return", "Иван\rПетров", false},
		{"tab", "Иван\tПетров", false},
		{"NUL", "Иван\x00", false},
		{"Unicode control", "Иван\u0085Петров", false},
		{"line separator", "Иван\u2028Петров", false},
		{"paragraph separator", "Иван\u2029Петров", false},
		{"invalid UTF-8", "Иван\xff", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := validation.ValidateName(tt.value)
			if tt.valid {
				if err != nil {
					t.Fatalf("ValidateName(%q) = %v, want nil", tt.value, err)
				}
			} else if !errors.Is(err, validation.ErrInvalidName) {
				t.Fatalf("ValidateName(%q) = %v, want ErrInvalidName", tt.value, err)
			}
		})
	}
}
