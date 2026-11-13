package config_test

import (
	"net/url"
	"os"
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
	if got := config.Load().Postgres; got != want {
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
