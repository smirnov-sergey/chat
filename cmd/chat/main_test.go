package main

import (
	"errors"
	"net"
	"os"
	"strings"
	"testing"
)

func TestRunConfigurationSources(t *testing.T) {
	// Invalid addresses make run return at net.Listen without starting a server.
	tests := []struct {
		name string
		file bool
		env  string
		want string
	}{
		{"file", true, "", "from-file"},
		{"environment overrides file", true, "from-environment", "from-environment"},
		{"missing file is optional", false, "from-environment", "from-environment"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			t.Setenv("HTTP_ADDR", tt.env)
			if tt.env == "" {
				if err := os.Unsetenv("HTTP_ADDR"); err != nil {
					t.Fatal(err)
				}
			}
			if tt.file {
				if err := os.WriteFile(".env", []byte("HTTP_ADDR=from-file\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}

			err := run()
			var addrErr *net.AddrError
			if !errors.As(err, &addrErr) {
				t.Fatalf("run error = %v, want address error", err)
			}
			if addrErr.Addr != tt.want {
				t.Errorf("listener address = %q, want %q", addrErr.Addr, tt.want)
			}
		})
	}
}

func TestRunRejectsMalformedDotEnv(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("HTTP_ADDR", "invalid-address")
	if err := os.WriteFile(".env", []byte("HTTP_ADDR=\"unterminated\n"), 0600); err != nil {
		t.Fatal(err)
	}
	err := run()
	if err == nil || !strings.HasPrefix(err.Error(), "load .env:") {
		t.Fatalf("run error = %v, want .env loading error", err)
	}
}
