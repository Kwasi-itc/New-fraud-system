package app

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Port                string
	DatabaseURL         string
	DataModelServiceURL string
	ServiceAuthMode     string
	ServiceAuthToken    string
	AllowedOrigins      []string
	LogLevel            string
	GinMode             string
	HTTPClientTimeout   time.Duration
	WorkerPollInterval  time.Duration
	WorkerMaxAttempts   int
}

func LoadConfig() (Config, error) {
	loadDotEnvIfPresent()

	httpClientTimeout, err := getEnvDuration("HTTP_CLIENT_TIMEOUT", 10*time.Second)
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		Port:                getEnv("PORT", "8081"),
		DatabaseURL:         os.Getenv("DATABASE_URL"),
		DataModelServiceURL: strings.TrimRight(os.Getenv("DATA_MODEL_SERVICE_URL"), "/"),
		ServiceAuthMode:     getEnv("SERVICE_AUTH_MODE", "disabled"),
		ServiceAuthToken:    os.Getenv("SERVICE_AUTH_TOKEN"),
		AllowedOrigins:      splitCSVEnv("ALLOWED_ORIGINS", "http://localhost:3000"),
		LogLevel:            getEnv("LOG_LEVEL", "info"),
		GinMode:             getEnv("GIN_MODE", "debug"),
		HTTPClientTimeout:   httpClientTimeout,
		WorkerMaxAttempts:   getEnvInt("WORKER_MAX_ATTEMPTS", 3),
	}
	workerPollInterval, err := getEnvDuration("WORKER_POLL_INTERVAL", 5*time.Second)
	if err != nil {
		return Config{}, err
	}
	cfg.WorkerPollInterval = workerPollInterval

	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}
	if cfg.DataModelServiceURL == "" {
		return Config{}, fmt.Errorf("DATA_MODEL_SERVICE_URL is required")
	}
	if cfg.ServiceAuthMode == "token" && cfg.ServiceAuthToken == "" {
		return Config{}, fmt.Errorf("SERVICE_AUTH_TOKEN is required when SERVICE_AUTH_MODE=token")
	}

	return cfg, nil
}

func splitCSVEnv(key, fallback string) []string {
	value := os.Getenv(key)
	if value == "" {
		value = fallback
	}

	parts := strings.Split(value, ",")
	origins := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		origins = append(origins, part)
	}
	return origins
}

func getEnv(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func getEnvDuration(key string, fallback time.Duration) (time.Duration, error) {
	value := os.Getenv(key)
	if value == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be a valid duration: %w", key, err)
	}
	return parsed, nil
}

func getEnvInt(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
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
