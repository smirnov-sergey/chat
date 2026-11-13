//go:build integration

package integration_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	for _, tt := range []struct {
		name      string
		file      string
		database  string
		unset     bool
		appDB     string
		wantDB    string
		wantError string
	}{
		{name: "environment", database: "chat_test", wantDB: "chat_test"},
		{name: "file", file: "POSTGRES_TEST_DB=file_test\n", unset: true, wantDB: "file_test"},
		{name: "environment overrides file", file: "POSTGRES_TEST_DB=file_test\nPOSTGRES_PASSWORD=other_password\n", database: "chat_test", wantDB: "chat_test"},
		{name: "empty environment overrides file", file: "POSTGRES_TEST_DB=file_test\n", wantError: "POSTGRES_TEST_DB is required"},
		{name: "missing setting", unset: true, wantError: "POSTGRES_TEST_DB is required"},
		{name: "wrong suffix", database: "other", wantError: "must end in _test"},
		{name: "development database forbidden", database: "chat_test", appDB: "chat_test", wantError: "must differ from POSTGRES_DB"},
		{name: "malformed file", file: "POSTGRES_TEST_DB=\"unterminated\n", wantError: "read test .env"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for key, value := range map[string]string{
				"POSTGRES_HOST": "127.0.0.1", "POSTGRES_PORT": "5432",
				"POSTGRES_DB": "chat", "POSTGRES_USER": "chat",
				"POSTGRES_PASSWORD": "test_password", "POSTGRES_SSLMODE": "disable",
			} {
				t.Setenv(key, value)
			}
			t.Setenv("POSTGRES_TEST_DB", tt.database)
			if tt.unset {
				if err := os.Unsetenv("POSTGRES_TEST_DB"); err != nil {
					t.Fatal(err)
				}
			}
			if tt.appDB != "" {
				t.Setenv("POSTGRES_DB", tt.appDB)
			}
			envPath := filepath.Join(t.TempDir(), ".env")
			if tt.file != "" {
				if err := os.WriteFile(envPath, []byte(tt.file), 0600); err != nil {
					t.Fatal(err)
				}
			}
			t.Chdir(t.TempDir())
			cfg, err := loadConfig(t, envPath)
			if tt.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantError) {
					t.Fatalf("error = %v, want %q", err, tt.wantError)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Database != tt.wantDB {
				t.Errorf("database = %q, want %q", cfg.Database, tt.wantDB)
			}
			if cfg.Host != "127.0.0.1" || cfg.Port != "5432" || cfg.User != "chat" || cfg.Password != "test_password" || cfg.SSLMode != "disable" {
				t.Error("test configuration must reuse application connection settings")
			}
		})
	}
}
