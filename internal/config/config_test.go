package config_test

import (
	"os"
	"testing"

	"chat/internal/config"
)

func TestDefaultIgnoresEnvironment(t *testing.T) {
	t.Setenv("HTTP_ADDR", "127.0.0.1:9090")
	if got := config.Default().HTTPAddr; got != "127.0.0.1:8080" {
		t.Errorf("HTTPAddr = %q, want 127.0.0.1:8080", got)
	}
}

func TestLoadWithoutEnvironment(t *testing.T) {
	t.Setenv("HTTP_ADDR", "")
	if err := os.Unsetenv("HTTP_ADDR"); err != nil {
		t.Fatal(err)
	}
	if got := config.Load().HTTPAddr; got != "127.0.0.1:8080" {
		t.Errorf("HTTPAddr = %q, want 127.0.0.1:8080", got)
	}
}

func TestLoadHTTPAddr(t *testing.T) {
	tests := []struct {
		name string
		env  string
		want string
	}{
		{"empty uses default", "", "127.0.0.1:8080"},
		{"environment override", "127.0.0.1:9090", "127.0.0.1:9090"},
		{"IPv6", "[::1]:8080", "[::1]:8080"},
		{"validation belongs to listener", "invalid-address", "invalid-address"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("HTTP_ADDR", tt.env)
			if got := config.Load().HTTPAddr; got != tt.want {
				t.Errorf("HTTPAddr = %q, want %q", got, tt.want)
			}
		})
	}
}
