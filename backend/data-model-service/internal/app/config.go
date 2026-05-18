package app

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Port             string
	DatabaseURL      string
	ServiceAuthMode  string
	ServiceAuthToken string
	LogLevel         string
	GinMode          string
	IndexWorkerPollInterval time.Duration
	IndexWorkerMaxAttempts  int
	IndexWorkerRetryBaseDelay time.Duration
	IndexWorkerRetryMaxDelay  time.Duration
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
		IndexWorkerMaxAttempts: getEnvInt("INDEX_WORKER_MAX_ATTEMPTS", 5),
	}
	pollInterval, err := getEnvDuration("INDEX_WORKER_POLL_INTERVAL", 2*time.Second)
	if err != nil {
		return Config{}, err
	}
	cfg.IndexWorkerPollInterval = pollInterval
	retryBaseDelay, err := getEnvDuration("INDEX_WORKER_RETRY_BASE_DELAY", 5*time.Second)
	if err != nil {
		return Config{}, err
	}
	cfg.IndexWorkerRetryBaseDelay = retryBaseDelay
	retryMaxDelay, err := getEnvDuration("INDEX_WORKER_RETRY_MAX_DELAY", 2*time.Minute)
	if err != nil {
		return Config{}, err
	}
	cfg.IndexWorkerRetryMaxDelay = retryMaxDelay

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
