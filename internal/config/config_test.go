package config_test

import (
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"

	"chat/internal/config"
)

func TestLoadPostgres(t *testing.T) {
	setRequiredEnvironment(t)
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
	t.Setenv("SESSION_COOKIE_SECURE", "false")
	t.Setenv("STORAGE_DIR", "custom/photos")
	t.Setenv("MAX_PHOTO_SIZE", "1")
	if got := config.Default().HTTPAddr; got != "127.0.0.1:8080" {
		t.Errorf("HTTPAddr = %q, want 127.0.0.1:8080", got)
	}
	if !config.Default().SessionCookieSecure {
		t.Error("default session cookie must be Secure regardless of environment")
	}
	if got := config.Default().StorageDir; got != "" {
		t.Errorf("StorageDir = %q, want no default for a required setting", got)
	}
	if got := config.Default().MaxPhotoSize; got != 0 {
		t.Errorf("MaxPhotoSize = %d, want no default for a required setting", got)
	}
}

func TestLoadRequiresPhotoSettings(t *testing.T) {
	for _, key := range []string{"STORAGE_DIR", "MAX_PHOTO_SIZE"} {
		for _, state := range []string{"empty", "missing"} {
			t.Run(key+"/"+state, func(t *testing.T) {
				setRequiredEnvironment(t)
				t.Setenv(key, "")
				if state == "missing" {
					if err := os.Unsetenv(key); err != nil {
						t.Fatal(err)
					}
				}
				cfg, err := config.Load()
				if err == nil || !strings.Contains(err.Error(), key) {
					t.Fatalf("Load error = %v, want an error naming %s", err, key)
				}
				if cfg != (config.Config{}) {
					t.Fatal("missing settings must not return a partial configuration")
				}
			})
		}
	}
}

func TestLoadPhotoSettings(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("STORAGE_DIR", "custom/photo files")
	t.Setenv("MAX_PHOTO_SIZE", "1048576")
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.StorageDir != "custom/photo files" || cfg.MaxPhotoSize != 1048576 {
		t.Fatalf("photo settings: directory=%q, size=%d; want custom/photo files, 1048576", cfg.StorageDir, cfg.MaxPhotoSize)
	}
}

func TestLoadRejectsInvalidPhotoSettings(t *testing.T) {
	for _, tt := range []struct{ key, value string }{
		{"STORAGE_DIR", " \t "},
		{"MAX_PHOTO_SIZE", "0"},
		{"MAX_PHOTO_SIZE", "-1"},
		{"MAX_PHOTO_SIZE", "abc"},
		{"MAX_PHOTO_SIZE", "1.5"},
		{"MAX_PHOTO_SIZE", " "},
		{"MAX_PHOTO_SIZE", "9223372036854775808"},
	} {
		t.Run(tt.key+"/"+tt.value, func(t *testing.T) {
			setRequiredEnvironment(t)
			t.Setenv("STORAGE_DIR", "storage/files")
			t.Setenv("MAX_PHOTO_SIZE", "5242880")
			t.Setenv(tt.key, tt.value)
			cfg, err := config.Load()
			if err == nil || !strings.Contains(err.Error(), tt.key) {
				t.Fatalf("Load error = %v, want an error naming %s", err, tt.key)
			}
			if cfg != (config.Config{}) {
				t.Fatal("invalid settings must not return a partial configuration")
			}
		})
	}
}

func TestLoadAcceptsPositivePhotoSizeBoundaries(t *testing.T) {
	for _, value := range []string{"1", "9223372036854775807"} {
		t.Run(value, func(t *testing.T) {
			setRequiredEnvironment(t)
			t.Setenv("STORAGE_DIR", "storage/files")
			t.Setenv("MAX_PHOTO_SIZE", value)
			cfg, err := config.Load()
			if err != nil {
				t.Fatal(err)
			}
			if strconv.FormatInt(cfg.MaxPhotoSize, 10) != value {
				t.Fatal("photo size must preserve the configured positive int64 value")
			}
		})
	}
}

func TestLoadSessionCookieSecure(t *testing.T) {
	for _, tt := range []struct {
		value  string
		secure bool
		valid  bool
	}{
		{"", true, true},
		{"true", true, true},
		{"false", false, true},
		{"invalid", false, false},
	} {
		t.Run(tt.value, func(t *testing.T) {
			setRequiredEnvironment(t)
			t.Setenv("SESSION_COOKIE_SECURE", tt.value)
			cfg, err := config.Load()
			if !tt.valid {
				if err == nil || !strings.Contains(err.Error(), "SESSION_COOKIE_SECURE") {
					t.Fatalf("Load error = %v, want SESSION_COOKIE_SECURE validation error", err)
				}
				return
			}
			if err != nil || cfg.SessionCookieSecure != tt.secure {
				t.Fatalf("Load cookie Secure = %v, error = %v; want %v, nil", cfg.SessionCookieSecure, err, tt.secure)
			}
		})
	}
}

func TestLoadWithoutHTTPAddr(t *testing.T) {
	setRequiredEnvironment(t)
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
	setRequiredEnvironment(t)
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
			setRequiredEnvironment(t)
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
			setRequiredEnvironment(t)
			t.Setenv("POSTGRES_PORT", port)
			if _, err := config.Load(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func setRequiredEnvironment(t *testing.T) {
	t.Helper()
	for key, value := range map[string]string{
		"POSTGRES_HOST": "127.0.0.1", "POSTGRES_PORT": "5432",
		"POSTGRES_DB": "chat", "POSTGRES_USER": "chat",
		"POSTGRES_PASSWORD": "test_password", "POSTGRES_SSLMODE": "disable",
		"STORAGE_DIR": "storage/files", "MAX_PHOTO_SIZE": "5242880",
	} {
		t.Setenv(key, value)
	}
}
