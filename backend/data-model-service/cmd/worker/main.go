package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/app"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/postgres"
	tenantdbpostgres "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/tenantdb/postgres"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/worker"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	cfg, err := app.LoadConfig()
	if err != nil {
		logger.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	db, err := postgres.NewPool(context.Background(), cfg.DatabaseURL)
	if err != nil {
		logger.Error("failed to initialize database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	runner := worker.NewRunner(
		logger,
		postgres.NewTenantRepository(db),
		postgres.NewTableRepository(db),
		postgres.NewIndexJobRepository(db),
		postgres.NewSchemaChangeRepository(db),
		tenantdbpostgres.NewSchemaManager(db),
		app.UUIDGenerator{},
		app.SystemClock{},
		cfg.IndexWorkerPollInterval,
		cfg.IndexWorkerMaxAttempts,
		cfg.IndexWorkerRetryBaseDelay,
		cfg.IndexWorkerRetryMaxDelay,
	)

	logger.Info("starting index job worker",
		"poll_interval", cfg.IndexWorkerPollInterval.String(),
		"max_attempts", cfg.IndexWorkerMaxAttempts,
		"retry_base_delay", cfg.IndexWorkerRetryBaseDelay.String(),
		"retry_max_delay", cfg.IndexWorkerRetryMaxDelay.String(),
	)
	if err := runner.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		logger.Error("worker exited with error", "error", err)
		os.Exit(1)
	}
}
