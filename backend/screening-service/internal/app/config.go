package app

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Port                   string
	DatabaseURL            string
	ServiceAuthMode        string
	ServiceAuthToken       string
	ScreeningProviderURL   string
	ScreeningProviderURLs  string
	OpenSanctionsAPIHost   string
	OpenSanctionsAuthMode  string
	OpenSanctionsAPIKey    string
	OpenSanctionsScope     string
	OpenSanctionsAlgorithm string
	IngestionServiceURL    string
	InboxServiceURL        string
	CaseServiceURL         string
	BlobServiceURL         string
	DecisionEngineURL      string
	LogLevel               string
	GinMode                string
	HTTPClientTimeout      time.Duration
	WorkerMode             string
	WorkerPollInterval     time.Duration
	WorkerBatchLimit       int
	ScreeningQueueName     string
	ScreeningQueueWorkers  int
	DatasetJobQueueName    string
	DatasetJobQueueWorkers int
	MonitoredQueueName     string
	MonitoredQueueWorkers  int
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
	screeningQueueWorkers, err := getEnvInt("SCREENING_QUEUE_WORKERS", 10)
	if err != nil {
		return Config{}, err
	}
	datasetJobQueueWorkers, err := getEnvInt("DATASET_JOB_QUEUE_WORKERS", 5)
	if err != nil {
		return Config{}, err
	}
	monitoredQueueWorkers, err := getEnvInt("MONITORED_OBJECT_QUEUE_WORKERS", 5)
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		Port:                   getEnv("PORT", "8085"),
		DatabaseURL:            os.Getenv("DATABASE_URL"),
		ServiceAuthMode:        getEnv("SERVICE_AUTH_MODE", "disabled"),
		ServiceAuthToken:       os.Getenv("SERVICE_AUTH_TOKEN"),
		ScreeningProviderURL:   strings.TrimRight(os.Getenv("SCREENING_PROVIDER_URL"), "/"),
		ScreeningProviderURLs:  os.Getenv("SCREENING_PROVIDER_URLS"),
		OpenSanctionsAPIHost:   strings.TrimRight(os.Getenv("OPENSANCTIONS_API_HOST"), "/"),
		OpenSanctionsAuthMode:  strings.ToLower(strings.TrimSpace(getEnv("OPENSANCTIONS_AUTH_METHOD", ""))),
		OpenSanctionsAPIKey:    os.Getenv("OPENSANCTIONS_API_KEY"),
		OpenSanctionsScope:     strings.TrimSpace(getEnv("OPENSANCTIONS_SCOPE", "default")),
		OpenSanctionsAlgorithm: strings.TrimSpace(getEnv("OPENSANCTIONS_ALGORITHM", "logic-v1")),
		IngestionServiceURL:    strings.TrimRight(os.Getenv("INGESTION_SERVICE_URL"), "/"),
		InboxServiceURL:        strings.TrimRight(os.Getenv("INBOX_SERVICE_URL"), "/"),
		CaseServiceURL:         strings.TrimRight(os.Getenv("CASE_SERVICE_URL"), "/"),
		BlobServiceURL:         strings.TrimRight(os.Getenv("BLOB_SERVICE_URL"), "/"),
		DecisionEngineURL:      strings.TrimRight(os.Getenv("DECISION_ENGINE_URL"), "/"),
		LogLevel:               getEnv("LOG_LEVEL", "info"),
		GinMode:                getEnv("GIN_MODE", "debug"),
		HTTPClientTimeout:      httpClientTimeout,
		WorkerMode:             strings.ToLower(getEnv("WORKER_MODE", "batch")),
		WorkerPollInterval:     workerPollInterval,
		WorkerBatchLimit:       workerBatchLimit,
		ScreeningQueueName:     strings.TrimSpace(getEnv("SCREENING_QUEUE_NAME", "screening_dispatch")),
		ScreeningQueueWorkers:  screeningQueueWorkers,
		DatasetJobQueueName:    strings.TrimSpace(getEnv("DATASET_JOB_QUEUE_NAME", "dataset_update_jobs")),
		DatasetJobQueueWorkers: datasetJobQueueWorkers,
		MonitoredQueueName:     strings.TrimSpace(getEnv("MONITORED_OBJECT_QUEUE_NAME", "monitored_objects")),
		MonitoredQueueWorkers:  monitoredQueueWorkers,
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
	if cfg.ScreeningQueueName == "" {
		return Config{}, fmt.Errorf("SCREENING_QUEUE_NAME is required")
	}
	if cfg.ScreeningQueueWorkers <= 0 {
		return Config{}, fmt.Errorf("SCREENING_QUEUE_WORKERS must be greater than zero")
	}
	if cfg.DatasetJobQueueName == "" {
		return Config{}, fmt.Errorf("DATASET_JOB_QUEUE_NAME is required")
	}
	if cfg.DatasetJobQueueWorkers <= 0 {
		return Config{}, fmt.Errorf("DATASET_JOB_QUEUE_WORKERS must be greater than zero")
	}
	if cfg.MonitoredQueueName == "" {
		return Config{}, fmt.Errorf("MONITORED_OBJECT_QUEUE_NAME is required")
	}
	if cfg.MonitoredQueueWorkers <= 0 {
		return Config{}, fmt.Errorf("MONITORED_OBJECT_QUEUE_WORKERS must be greater than zero")
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

		value = strings.TrimSpace(value)
		value = strings.Trim(value, `"'`)
		_ = os.Setenv(key, value)
	}
}
