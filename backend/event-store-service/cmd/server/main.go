package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/event-store-service/internal/eventstore"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg := eventstore.Config{
		Port:                    env("PORT", "8083"),
		ClickHouseURL:           strings.TrimRight(env("CLICKHOUSE_URL", "http://clickhouse:8123"), "/"),
		ClickHouseDatabase:      env("CLICKHOUSE_DATABASE", "fraud_events"),
		ClickHouseUser:          env("CLICKHOUSE_USER", "default"),
		ClickHousePassword:      os.Getenv("CLICKHOUSE_PASSWORD"),
		ValkeyAddress:           os.Getenv("VALKEY_ADDRESS"),
		DisableAggregateFacts:   !envBool("EVENT_AGGREGATE_FACTS_ENABLED", true),
		FeatureNamespace:        env("FEATURE_NAMESPACE", "fraud:event-feature:v1"),
		FeatureMaxKeys:          envInt("FEATURE_MAX_KEYS", 10000),
		FeatureMaxKeysPerTenant: envInt("FEATURE_MAX_KEYS_PER_TENANT", 1000),
		FeatureAdmissionHits:    envInt("FEATURE_ADMISSION_HITS", 3),
		FeatureSlowQueryMS:      envInt("FEATURE_SLOW_QUERY_MS", 100),
		FeatureTTL:              envDuration("FEATURE_TTL", time.Hour),
		HTTPTimeout:             envDuration("CLICKHOUSE_HTTP_TIMEOUT", 10*time.Second),
	}
	server, err := eventstore.NewServer(cfg, logger)
	if err != nil {
		logger.Error("configure event store", "error", err)
		os.Exit(1)
	}
	if err := server.Initialize(context.Background()); err != nil {
		logger.Error("initialize event store", "error", err)
		os.Exit(1)
	}

	httpServer := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           server.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	done := make(chan os.Signal, 1)
	signal.Notify(done, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-done
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(ctx)
	}()
	logger.Info("starting event store service", "port", cfg.Port)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("event store stopped", "error", err)
		os.Exit(1)
	}
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envInt(key string, fallback int) int {
	value, err := strconv.Atoi(os.Getenv(key))
	if err != nil {
		return fallback
	}
	return value
}

func envBool(key string, fallback bool) bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	switch value {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

func envDuration(key string, fallback time.Duration) time.Duration {
	value, err := time.ParseDuration(os.Getenv(key))
	if err != nil {
		return fallback
	}
	return value
}
