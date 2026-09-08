package app

import (
	"strings"
	"testing"
)

func TestLoadConfigRejectsNonNumericPoolSetting(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("DATA_MODEL_SERVICE_URL", "http://data-model")
	t.Setenv("DATABASE_MAX_CONNS", "twelve")

	_, err := LoadConfig()
	if err == nil || !strings.Contains(err.Error(), "DATABASE_MAX_CONNS must be an integer") {
		t.Fatalf("LoadConfig() error = %v, want invalid integer error", err)
	}
}

func TestLoadConfigRejectsNonNumericConcurrencySetting(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("DATA_MODEL_SERVICE_URL", "http://data-model")
	t.Setenv("WRITE_PATH_CONCURRENCY_LIMIT", "unlimited")

	_, err := LoadConfig()
	if err == nil || !strings.Contains(err.Error(), "WRITE_PATH_CONCURRENCY_LIMIT must be an integer") {
		t.Fatalf("LoadConfig() error = %v, want invalid integer error", err)
	}
}

func TestLoadConfigDefaultsMatchComposeRuntime(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("DATA_MODEL_SERVICE_URL", "http://data-model")
	t.Setenv("DATABASE_MAX_CONNS", "")
	t.Setenv("DATABASE_MIN_CONNS", "")
	t.Setenv("WRITE_PATH_CONCURRENCY_LIMIT", "")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.DatabaseMaxConns != 12 || cfg.DatabaseMinConns != 2 {
		t.Fatalf("database pool defaults = %d/%d, want 12/2", cfg.DatabaseMaxConns, cfg.DatabaseMinConns)
	}
	if cfg.WritePathConcurrencyLimit != 100000 {
		t.Fatalf("write path concurrency default = %d, want 100000", cfg.WritePathConcurrencyLimit)
	}
}
