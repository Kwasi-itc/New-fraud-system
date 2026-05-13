package app

import (
	"fmt"
	"os"
	"strings"
)

type Config struct {
	Port             string
	DatabaseURL      string
	ServiceAuthMode  string
	ServiceAuthToken string
	LogLevel         string
	GinMode          string
}

func LoadConfig() (Config, error) {
	loadDotEnvIfPresent()

	cfg := Config{
		Port:             getEnv("PORT", "8080"),
		DatabaseURL:      os.Getenv("DATABASE_URL"),
		ServiceAuthMode:  getEnv("SERVICE_AUTH_MODE", "disabled"),
		ServiceAuthToken: os.Getenv("SERVICE_AUTH_TOKEN"),
		LogLevel:         getEnv("LOG_LEVEL", "info"),
		GinMode:          getEnv("GIN_MODE", "debug"),
	}

	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}
	if cfg.ServiceAuthMode == "token" && cfg.ServiceAuthToken == "" {
		return Config{}, fmt.Errorf("SERVICE_AUTH_TOKEN is required when SERVICE_AUTH_MODE=token")
	}

	return cfg, nil
}

func getEnv(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func loadDotEnvIfPresent() {
	content, err := os.ReadFile(".env")
	if err != nil {
		return
	}

	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}

		key = strings.TrimSpace(key)
		if key == "" || os.Getenv(key) != "" {
			continue
		}

		value = strings.TrimSpace(value)
		value = strings.Trim(value, `"'`)
		_ = os.Setenv(key, value)
	}
}
