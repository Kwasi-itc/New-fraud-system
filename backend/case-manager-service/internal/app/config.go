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
	ServiceAuthMode     string
	ServiceAuthToken    string
	DecisionEngineURL   string
	ScreeningServiceURL string
	IngestionServiceURL string
	DataModelServiceURL string
	BlobServiceURL      string
	OutboxPublisherURL  string
	LogLevel            string
	GinMode             string
	HTTPClientTimeout   time.Duration
	WorkerMode          string
	WorkerPollInterval  time.Duration
	WorkerBatchLimit    int
}

func LoadConfig() (Config, error) {
	loadDotEnvIfPresent()

	httpClientTimeout, err := getEnvDuration("HTTP_CLIENT_TIMEOUT", 10*time.Second)
	if err != nil {
		return Config{}, err
	}
	workerPollInterval, err := getEnvDuration("WORKER_POLL_INTERVAL", 15*time.Second)
	if err != nil {
		return Config{}, err
	}
	workerBatchLimit, err := getEnvInt("WORKER_BATCH_LIMIT", 100)
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		Port:                getEnv("PORT", "8086"),
		DatabaseURL:         os.Getenv("DATABASE_URL"),
		ServiceAuthMode:     strings.ToLower(getEnv("SERVICE_AUTH_MODE", "disabled")),
		ServiceAuthToken:    os.Getenv("SERVICE_AUTH_TOKEN"),
		DecisionEngineURL:   strings.TrimRight(os.Getenv("DECISION_ENGINE_URL"), "/"),
		ScreeningServiceURL: strings.TrimRight(os.Getenv("SCREENING_SERVICE_URL"), "/"),
		IngestionServiceURL: strings.TrimRight(os.Getenv("INGESTION_SERVICE_URL"), "/"),
		DataModelServiceURL: strings.TrimRight(os.Getenv("DATA_MODEL_SERVICE_URL"), "/"),
		BlobServiceURL:      strings.TrimRight(os.Getenv("BLOB_SERVICE_URL"), "/"),
		OutboxPublisherURL:  strings.TrimRight(os.Getenv("OUTBOX_PUBLISHER_URL"), "/"),
		LogLevel:            getEnv("LOG_LEVEL", "info"),
		GinMode:             getEnv("GIN_MODE", "debug"),
		HTTPClientTimeout:   httpClientTimeout,
		WorkerMode:          strings.ToLower(getEnv("WORKER_MODE", "batch")),
		WorkerPollInterval:  workerPollInterval,
		WorkerBatchLimit:    workerBatchLimit,
	}

	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}
	if cfg.ServiceAuthMode == "token" && cfg.ServiceAuthToken == "" {
		return Config{}, fmt.Errorf("SERVICE_AUTH_TOKEN is required when SERVICE_AUTH_MODE=token")
	}
	if cfg.WorkerMode != "batch" && cfg.WorkerMode != "poll" {
		return Config{}, fmt.Errorf("WORKER_MODE must be either batch or poll")
	}
	if cfg.WorkerPollInterval <= 0 {
		return Config{}, fmt.Errorf("WORKER_POLL_INTERVAL must be greater than zero")
	}
	if cfg.WorkerBatchLimit <= 0 {
		return Config{}, fmt.Errorf("WORKER_BATCH_LIMIT must be greater than zero")
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

func getEnvInt(key string, fallback int) (int, error) {
	value := os.Getenv(key)
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be a valid integer: %w", key, err)
	}
	return parsed, nil
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
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		_ = os.Setenv(key, value)
	}
}
