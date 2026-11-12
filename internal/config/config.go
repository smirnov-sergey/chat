package config

import "os"

type Config struct {
	HTTPAddr string
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
	return cfg
}
