package main

import (
	"os"
	"strings"
	"testing"
)

func TestRunRejectsInvalidArguments(t *testing.T) {
	for _, args := range [][]string{nil, {"down"}, {"up", "extra"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			err := run(args)
			if err == nil || !strings.HasPrefix(err.Error(), "usage:") {
				t.Fatalf("run error = %v, want usage error", err)
			}
		})
	}
}

func TestRunRejectsMalformedDotEnv(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile(".env", []byte("POSTGRES_HOST=\"unterminated\n"), 0600); err != nil {
		t.Fatal(err)
	}
	err := run([]string{"up"})
	if err == nil || !strings.HasPrefix(err.Error(), "load .env:") {
		t.Fatalf("run error = %v, want .env loading error", err)
	}
}
