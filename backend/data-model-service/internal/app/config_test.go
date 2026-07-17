package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigRequiresAuthTokenForTokenMode(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("SERVICE_AUTH_MODE", "token")
	t.Setenv("SERVICE_AUTH_TOKEN", "")

	_, err := LoadConfig()
	if err == nil {
		t.Fatal("expected missing auth token error")
	}
}

func TestLoadConfigAcceptsDisabledAuthWithoutToken(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("SERVICE_AUTH_MODE", "disabled")
	t.Setenv("SERVICE_AUTH_TOKEN", "")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ServiceAuthMode != "disabled" {
		t.Fatalf("unexpected auth mode: %s", cfg.ServiceAuthMode)
	}
}

func TestLoadConfigRejectsInvalidQueueWorkers(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("INDEX_JOB_QUEUE_WORKERS", "0")

	_, err := LoadConfig()
	if err == nil {
		t.Fatal("expected invalid queue worker error")
	}
}

func TestLoadConfigReadsDotEnvFromWorkingDirectory(t *testing.T) {
	tempDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tempDir, ".env"), []byte("DATABASE_URL=postgres://from-dotenv\nSERVICE_AUTH_MODE=disabled\nPORT=9090\n"), 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalWD)
	})

	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("chdir temp dir: %v", err)
	}

	t.Setenv("DATABASE_URL", "")
	t.Setenv("SERVICE_AUTH_MODE", "")
	t.Setenv("PORT", "")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.DatabaseURL != "postgres://from-dotenv" {
		t.Fatalf("unexpected database url: %s", cfg.DatabaseURL)
	}
	if cfg.Port != "9090" {
		t.Fatalf("unexpected port: %s", cfg.Port)
	}
}

func TestLoadConfigEnvironmentOverridesDotEnv(t *testing.T) {
	tempDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tempDir, ".env"), []byte("DATABASE_URL=postgres://from-dotenv\nSERVICE_AUTH_MODE=disabled\n"), 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalWD)
	})

	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("chdir temp dir: %v", err)
	}

	t.Setenv("DATABASE_URL", "postgres://from-env")
	t.Setenv("SERVICE_AUTH_MODE", "disabled")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.DatabaseURL != "postgres://from-env" {
		t.Fatalf("unexpected database url: %s", cfg.DatabaseURL)
	}
}
