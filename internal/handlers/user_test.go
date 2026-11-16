package handlers

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseUserID(t *testing.T) {
	for _, tt := range []struct {
		value string
		want  int64
	}{
		{"1", 1},
		{"42", 42},
		{"9223372036854775807", 9223372036854775807},
		{"", 0},
		{"0", 0},
		{"-1", 0},
		{"abc", 0},
		{"1.5", 0},
		{" 42 ", 0},
		{"9223372036854775808", 0},
	} {
		t.Run(tt.value, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/users/id", nil)
			req.SetPathValue("id", tt.value)
			got, err := parseUserID(req)
			if got != tt.want {
				t.Fatalf("parseUserID(%q) = %d, want %d", tt.value, got, tt.want)
			}
			if tt.want == 0 {
				if !errors.Is(err, errInvalidUserID) {
					t.Fatalf("parseUserID error = %v, want errInvalidUserID", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
		})
	}
}
