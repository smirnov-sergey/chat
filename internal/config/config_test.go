package config_test

import (
	"net/url"
	"os"
	"strings"
	"testing"

	"chat/internal/config"
)

func TestLoadPostgres(t *testing.T) {
	t.Setenv("POSTGRES_HOST", "127.0.0.1")
	t.Setenv("POSTGRES_PORT", "5433")
	t.Setenv("POSTGRES_DB", "chat")
	t.Setenv("POSTGRES_USER", "chat_user")
	t.Setenv("POSTGRES_PASSWORD", "test_password")
	t.Setenv("POSTGRES_SSLMODE", "disable")
	want := config.PostgresConfig{
		Host: "127.0.0.1", Port: "5433", Database: "chat",
		User: "chat_user", Password: "test_password", SSLMode: "disable",
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Postgres != want {
		t.Errorf("PostgreSQL configuration does not match environment")
	}
}

func TestPostgresConnectionString(t *testing.T) {
	cfg := config.PostgresConfig{
		Host: "::1", Port: "5432", Database: "chat database",
		User: "user@name", Password: "p@ss:/?#% word", SSLMode: "disable",
	}
	u, err := url.Parse(cfg.ConnectionString())
	if err != nil {
		t.Fatal(err)
	}
	password, _ := u.User.Password()
	if u.Scheme != "postgres" || u.Hostname() != cfg.Host || u.Port() != cfg.Port ||
		u.Path != "/"+cfg.Database || u.User.Username() != cfg.User ||
		password != cfg.Password || u.Query().Get("sslmode") != cfg.SSLMode {
		t.Error("connection string did not preserve configuration")
	}
}

func TestDefaultIgnoresEnvironment(t *testing.T) {
	t.Setenv("HTTP_ADDR", "127.0.0.1:9090")
	if got := config.Default().HTTPAddr; got != "127.0.0.1:8080" {
		t.Errorf("HTTPAddr = %q, want 127.0.0.1:8080", got)
	}
}

func TestLoadWithoutHTTPAddr(t *testing.T) {
	setPostgresEnvironment(t)
	t.Setenv("HTTP_ADDR", "")
	if err := os.Unsetenv("HTTP_ADDR"); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPAddr != "127.0.0.1:8080" {
		t.Errorf("HTTPAddr = %q, want 127.0.0.1:8080", cfg.HTTPAddr)
	}
}

func TestLoadHTTPAddr(t *testing.T) {
	setPostgresEnvironment(t)
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
			cfg, err := config.Load()
			if err != nil {
				t.Fatal(err)
			}
			if cfg.HTTPAddr != tt.want {
				t.Errorf("HTTPAddr = %q, want %q", cfg.HTTPAddr, tt.want)
			}
		})
	}
}

func TestLoadRejectsInvalidPostgres(t *testing.T) {
	for _, tt := range []struct{ name, value string }{
		{"POSTGRES_HOST", ""},
		{"POSTGRES_HOST", "   "},
		{"POSTGRES_PORT", ""},
		{"POSTGRES_PORT", "0"},
		{"POSTGRES_PORT", "65536"},
		{"POSTGRES_PORT", "-1"},
		{"POSTGRES_PORT", "abc"},
		{"POSTGRES_DB", ""},
		{"POSTGRES_USER", ""},
		{"POSTGRES_PASSWORD", ""},
		{"POSTGRES_SSLMODE", ""},
		{"POSTGRES_SSLMODE", "invalid"},
	} {
		t.Run(tt.name+"/"+tt.value, func(t *testing.T) {
			setPostgresEnvironment(t)
			t.Setenv(tt.name, tt.value)
			_, err := config.Load()
			if err == nil || !strings.Contains(err.Error(), tt.name) {
				t.Fatalf("error = %v, want error naming %s", err, tt.name)
			}
		})
	}
}

func TestLoadAcceptsPortBoundaries(t *testing.T) {
	for _, port := range []string{"1", "65535"} {
		t.Run(port, func(t *testing.T) {
			setPostgresEnvironment(t)
			t.Setenv("POSTGRES_PORT", port)
			if _, err := config.Load(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func setPostgresEnvironment(t *testing.T) {
	t.Helper()
	for key, value := range map[string]string{
		"POSTGRES_HOST": "127.0.0.1", "POSTGRES_PORT": "5432",
		"POSTGRES_DB": "chat", "POSTGRES_USER": "chat",
		"POSTGRES_PASSWORD": "test_password", "POSTGRES_SSLMODE": "disable",
	} {
		t.Setenv(key, value)
	}
}
