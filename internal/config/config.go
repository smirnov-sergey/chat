package config

import (
	"net"
	"net/url"
	"os"
)

type Config struct {
	HTTPAddr string
	Postgres PostgresConfig
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
		HTTPAddr: "127.0.0.1:8080",
	}
}

func Load() Config {
	cfg := Default()

	if addr := os.Getenv("HTTP_ADDR"); addr != "" {
		cfg.HTTPAddr = addr
	}

	cfg.Postgres = PostgresConfig{
		Host:     os.Getenv("POSTGRES_HOST"),
		Port:     os.Getenv("POSTGRES_PORT"),
		Database: os.Getenv("POSTGRES_DB"),
		User:     os.Getenv("POSTGRES_USER"),
		Password: os.Getenv("POSTGRES_PASSWORD"),
		SSLMode:  os.Getenv("POSTGRES_SSLMODE"),
	}

	return cfg
}
