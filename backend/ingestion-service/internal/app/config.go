package app

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Port                            string
	DatabaseURL                     string
	DatabaseMaxConns                int
	DatabaseMinConns                int
	ReadDatabaseURL                 string
	ReadDatabaseMaxConns            int
	ReadDatabaseMinConns            int
	WorkerDatabaseURL               string
	WorkerDatabaseMaxConns          int
	WorkerDatabaseMinConns          int
	DataModelServiceURL             string
	ServiceAuthMode                 string
	ServiceAuthToken                string
	AllowedOrigins                  []string
	LogLevel                        string
	GinMode                         string
	HTTPClientTimeout               time.Duration
	AggregateQueryTimeout           time.Duration
	WorkerPollInterval              time.Duration
	WorkerMaxAttempts               int
	UploadLogQueueName              string
	UploadLogQueueWorkers           int
	DeferredIngestQueueName         string
	DeferredIngestQueueWorkers      int
	WritePathConcurrencyLimit       int
	WritePathOverloadMode           string
	ReadQueryConcurrencyLimit       int
	AggregateQueryConcurrencyLimit  int
	DBPoolSaturationThresholdPct    int
	RequestQueueDepthThreshold      int
	ServiceCPUThresholdPct          int
	UpstreamTimeoutRateThresholdPct int
}

func LoadConfig() (Config, error) {
	loadDotEnvIfPresent()

	httpClientTimeout, err := getEnvDuration("HTTP_CLIENT_TIMEOUT", 10*time.Second)
	if err != nil {
		return Config{}, err
	}
	aggregateQueryTimeout, err := getEnvDuration("AGGREGATE_QUERY_TIMEOUT", 3*time.Second)
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		Port:                            getEnv("PORT", "8081"),
		DatabaseURL:                     os.Getenv("DATABASE_URL"),
		DatabaseMaxConns:                getEnvInt("DATABASE_MAX_CONNS", 0),
		DatabaseMinConns:                getEnvInt("DATABASE_MIN_CONNS", 0),
		ReadDatabaseURL:                 os.Getenv("READ_DATABASE_URL"),
		ReadDatabaseMaxConns:            getEnvInt("READ_DATABASE_MAX_CONNS", 0),
		ReadDatabaseMinConns:            getEnvInt("READ_DATABASE_MIN_CONNS", 0),
		WorkerDatabaseURL:               os.Getenv("WORKER_DATABASE_URL"),
		WorkerDatabaseMaxConns:          getEnvInt("WORKER_DATABASE_MAX_CONNS", 4),
		WorkerDatabaseMinConns:          getEnvInt("WORKER_DATABASE_MIN_CONNS", 0),
		DataModelServiceURL:             strings.TrimRight(os.Getenv("DATA_MODEL_SERVICE_URL"), "/"),
		ServiceAuthMode:                 getEnv("SERVICE_AUTH_MODE", "disabled"),
		ServiceAuthToken:                os.Getenv("SERVICE_AUTH_TOKEN"),
		AllowedOrigins:                  splitCSVEnv("ALLOWED_ORIGINS", "http://localhost:3000"),
		LogLevel:                        getEnv("LOG_LEVEL", "info"),
		GinMode:                         getEnv("GIN_MODE", "debug"),
		HTTPClientTimeout:               httpClientTimeout,
		AggregateQueryTimeout:           aggregateQueryTimeout,
		WorkerMaxAttempts:               getEnvInt("WORKER_MAX_ATTEMPTS", 3),
		UploadLogQueueName:              getEnv("UPLOAD_LOG_QUEUE_NAME", "upload_logs"),
		UploadLogQueueWorkers:           getEnvInt("UPLOAD_LOG_QUEUE_WORKERS", 4),
		DeferredIngestQueueName:         getEnv("DEFERRED_INGEST_QUEUE_NAME", "deferred_ingests"),
		DeferredIngestQueueWorkers:      getEnvInt("DEFERRED_INGEST_QUEUE_WORKERS", 4),
		WritePathConcurrencyLimit:       getEnvInt("WRITE_PATH_CONCURRENCY_LIMIT", 200),
		WritePathOverloadMode:           strings.ToLower(strings.TrimSpace(getEnv("WRITE_PATH_OVERLOAD_MODE", "defer_async"))),
		ReadQueryConcurrencyLimit:       getEnvInt("READ_QUERY_CONCURRENCY_LIMIT", 64),
		AggregateQueryConcurrencyLimit:  getEnvInt("AGGREGATE_QUERY_CONCURRENCY_LIMIT", 16),
		DBPoolSaturationThresholdPct:    getEnvInt("DB_POOL_SATURATION_THRESHOLD_PCT", 80),
		RequestQueueDepthThreshold:      getEnvInt("REQUEST_QUEUE_DEPTH_THRESHOLD", 8),
		ServiceCPUThresholdPct:          getEnvInt("SERVICE_CPU_THRESHOLD_PCT", 80),
		UpstreamTimeoutRateThresholdPct: getEnvInt("UPSTREAM_TIMEOUT_RATE_THRESHOLD_PCT", 5),
	}
	workerPollInterval, err := getEnvDuration("WORKER_POLL_INTERVAL", 5*time.Second)
	if err != nil {
		return Config{}, err
	}
	cfg.WorkerPollInterval = workerPollInterval

	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}
	if cfg.DatabaseMaxConns < 0 {
		return Config{}, fmt.Errorf("DATABASE_MAX_CONNS must be greater than or equal to zero")
	}
	if cfg.DatabaseMinConns < 0 {
		return Config{}, fmt.Errorf("DATABASE_MIN_CONNS must be greater than or equal to zero")
	}
	if cfg.DatabaseMaxConns > 0 && cfg.DatabaseMinConns > cfg.DatabaseMaxConns {
		return Config{}, fmt.Errorf("DATABASE_MIN_CONNS must be less than or equal to DATABASE_MAX_CONNS")
	}
	if cfg.ReadDatabaseMaxConns < 0 {
		return Config{}, fmt.Errorf("READ_DATABASE_MAX_CONNS must be greater than or equal to zero")
	}
	if cfg.ReadDatabaseMinConns < 0 {
		return Config{}, fmt.Errorf("READ_DATABASE_MIN_CONNS must be greater than or equal to zero")
	}
	if cfg.ReadDatabaseMaxConns > 0 && cfg.ReadDatabaseMinConns > cfg.ReadDatabaseMaxConns {
		return Config{}, fmt.Errorf("READ_DATABASE_MIN_CONNS must be less than or equal to READ_DATABASE_MAX_CONNS")
	}
	if cfg.WorkerDatabaseMaxConns < 0 {
		return Config{}, fmt.Errorf("WORKER_DATABASE_MAX_CONNS must be greater than or equal to zero")
	}
	if cfg.WorkerDatabaseMinConns < 0 {
		return Config{}, fmt.Errorf("WORKER_DATABASE_MIN_CONNS must be greater than or equal to zero")
	}
	if cfg.WorkerDatabaseMaxConns > 0 && cfg.WorkerDatabaseMinConns > cfg.WorkerDatabaseMaxConns {
		return Config{}, fmt.Errorf("WORKER_DATABASE_MIN_CONNS must be less than or equal to WORKER_DATABASE_MAX_CONNS")
	}
	if cfg.DataModelServiceURL == "" {
		return Config{}, fmt.Errorf("DATA_MODEL_SERVICE_URL is required")
	}
	if cfg.ServiceAuthMode == "token" && cfg.ServiceAuthToken == "" {
		return Config{}, fmt.Errorf("SERVICE_AUTH_TOKEN is required when SERVICE_AUTH_MODE=token")
	}
	if strings.TrimSpace(cfg.UploadLogQueueName) == "" {
		return Config{}, fmt.Errorf("UPLOAD_LOG_QUEUE_NAME must not be empty")
	}
	if cfg.UploadLogQueueWorkers <= 0 {
		return Config{}, fmt.Errorf("UPLOAD_LOG_QUEUE_WORKERS must be greater than zero")
	}
	if strings.TrimSpace(cfg.DeferredIngestQueueName) == "" {
		return Config{}, fmt.Errorf("DEFERRED_INGEST_QUEUE_NAME must not be empty")
	}
	if cfg.DeferredIngestQueueWorkers <= 0 {
		return Config{}, fmt.Errorf("DEFERRED_INGEST_QUEUE_WORKERS must be greater than zero")
	}
	if cfg.AggregateQueryTimeout <= 0 {
		return Config{}, fmt.Errorf("AGGREGATE_QUERY_TIMEOUT must be greater than zero")
	}
	if cfg.ReadQueryConcurrencyLimit < 0 {
		return Config{}, fmt.Errorf("READ_QUERY_CONCURRENCY_LIMIT must be greater than or equal to zero")
	}
	if cfg.WritePathConcurrencyLimit < 0 {
		return Config{}, fmt.Errorf("WRITE_PATH_CONCURRENCY_LIMIT must be greater than or equal to zero")
	}
	if cfg.WritePathOverloadMode != "reject" && cfg.WritePathOverloadMode != "defer_async" {
		return Config{}, fmt.Errorf("WRITE_PATH_OVERLOAD_MODE must be one of reject,defer_async")
	}
	if cfg.AggregateQueryConcurrencyLimit < 0 {
		return Config{}, fmt.Errorf("AGGREGATE_QUERY_CONCURRENCY_LIMIT must be greater than or equal to zero")
	}
	if cfg.DBPoolSaturationThresholdPct < 1 || cfg.DBPoolSaturationThresholdPct > 100 {
		return Config{}, fmt.Errorf("DB_POOL_SATURATION_THRESHOLD_PCT must be between 1 and 100")
	}
	if cfg.RequestQueueDepthThreshold < 0 {
		return Config{}, fmt.Errorf("REQUEST_QUEUE_DEPTH_THRESHOLD must be greater than or equal to zero")
	}
	if cfg.ServiceCPUThresholdPct < 1 || cfg.ServiceCPUThresholdPct > 100 {
		return Config{}, fmt.Errorf("SERVICE_CPU_THRESHOLD_PCT must be between 1 and 100")
	}
	if cfg.UpstreamTimeoutRateThresholdPct < 1 || cfg.UpstreamTimeoutRateThresholdPct > 100 {
		return Config{}, fmt.Errorf("UPSTREAM_TIMEOUT_RATE_THRESHOLD_PCT must be between 1 and 100")
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
