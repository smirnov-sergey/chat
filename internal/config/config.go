package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	HTTPAddr            string
	SessionCookieSecure bool
	Postgres            PostgresConfig
}

type PostgresConfig struct {
	Host     string
	Port     string
	Database string
	User     string
	Password string
	SSLMode  string
}

func (cfg PostgresConfig) ConnectionString() string {
	u := url.URL{
		Scheme:   "postgres",
		Host:     net.JoinHostPort(cfg.Host, cfg.Port),
		User:     url.UserPassword(cfg.User, cfg.Password),
		Path:     "/" + cfg.Database,
		RawQuery: url.Values{"sslmode": {cfg.SSLMode}}.Encode(),
	}
	return u.String()
}

func Default() Config {
	return Config{
		HTTPAddr:            "127.0.0.1:8080",
		SessionCookieSecure: true,
	}
}

func Load() (Config, error) {
	cfg := Default()

	if addr := os.Getenv("HTTP_ADDR"); addr != "" {
		cfg.HTTPAddr = addr
	}
	if sessionCookieSecureValue := os.Getenv("SESSION_COOKIE_SECURE"); sessionCookieSecureValue != "" {
		secure, err := strconv.ParseBool(sessionCookieSecureValue)
		if err != nil {
			return Config{}, fmt.Errorf("SESSION_COOKIE_SECURE must be a valid boolean")
		}
		cfg.SessionCookieSecure = secure
	}

	cfg.Postgres = PostgresConfig{
		Host:     os.Getenv("POSTGRES_HOST"),
		Port:     os.Getenv("POSTGRES_PORT"),
		Database: os.Getenv("POSTGRES_DB"),
		User:     os.Getenv("POSTGRES_USER"),
		Password: os.Getenv("POSTGRES_PASSWORD"),
		SSLMode:  os.Getenv("POSTGRES_SSLMODE"),
	}

	if err := cfg.Postgres.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (cfg PostgresConfig) validate() error {
	for _, field := range []struct{ name, value string }{
		{"POSTGRES_HOST", cfg.Host},
		{"POSTGRES_PORT", cfg.Port},
		{"POSTGRES_DB", cfg.Database},
		{"POSTGRES_USER", cfg.User},
		{"POSTGRES_SSLMODE", cfg.SSLMode},
	} {
		if strings.TrimSpace(field.value) == "" {
			return fmt.Errorf("%s is required", field.name)
		}
	}
	if cfg.Password == "" {
		return fmt.Errorf("POSTGRES_PASSWORD is required")
	}
	port, err := strconv.ParseUint(cfg.Port, 10, 16)
	if err != nil || port == 0 {
		return fmt.Errorf("POSTGRES_PORT must be an integer between 1 and 65535")
	}

	switch cfg.SSLMode {
	case "disable", "allow", "prefer", "require", "verify-ca", "verify-full":
		return nil
	default:
		return fmt.Errorf("POSTGRES_SSLMODE must be disable, allow, prefer, require, verify-ca or verify-full")
	}
}
