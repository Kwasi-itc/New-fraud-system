package app

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Port                   string
	DatabaseURL            string
	ServiceAuthMode        string
	ServiceAuthToken       string
	ServiceAllowedOrigins  []string
	LogLevel               string
	GinMode                string
	IndexWorkerMaxAttempts int
	IndexJobQueueName      string
	IndexJobQueueWorkers   int
}

func LoadConfig() (Config, error) {
	loadDotEnvIfPresent()

	cfg := Config{
		Port:                   getEnv("PORT", "8080"),
		DatabaseURL:            os.Getenv("DATABASE_URL"),
		ServiceAuthMode:        getEnv("SERVICE_AUTH_MODE", "disabled"),
		ServiceAuthToken:       os.Getenv("SERVICE_AUTH_TOKEN"),
		ServiceAllowedOrigins:  getEnvCSV("SERVICE_ALLOWED_ORIGINS", []string{"http://localhost:3000"}),
		LogLevel:               getEnv("LOG_LEVEL", "info"),
		GinMode:                getEnv("GIN_MODE", "debug"),
		IndexWorkerMaxAttempts: getEnvInt("INDEX_WORKER_MAX_ATTEMPTS", 5),
		IndexJobQueueName:      getEnv("INDEX_JOB_QUEUE_NAME", "index_jobs"),
		IndexJobQueueWorkers:   getEnvInt("INDEX_JOB_QUEUE_WORKERS", 4),
	}

	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}
	if cfg.ServiceAuthMode == "token" && cfg.ServiceAuthToken == "" {
		return Config{}, fmt.Errorf("SERVICE_AUTH_TOKEN is required when SERVICE_AUTH_MODE=token")
	}
	if cfg.IndexJobQueueWorkers <= 0 {
		return Config{}, fmt.Errorf("INDEX_JOB_QUEUE_WORKERS must be greater than zero")
	}
	if strings.TrimSpace(cfg.IndexJobQueueName) == "" {
		return Config{}, fmt.Errorf("INDEX_JOB_QUEUE_NAME must not be empty")
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

func getEnvCSV(key string, fallback []string) []string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return append([]string(nil), fallback...)
	}

	parts := strings.Split(value, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		values = append(values, part)
	}
	if len(values) == 0 {
		return append([]string(nil), fallback...)
	}
	return values
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
