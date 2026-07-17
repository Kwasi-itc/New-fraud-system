package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/app"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/riverjobs"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/postgres"
	tenantdbpostgres "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/tenantdb/postgres"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/worker"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
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
		cfg.IndexWorkerMaxAttempts,
	)

	logger.Info("starting index job worker",
		"max_attempts", cfg.IndexWorkerMaxAttempts,
	)

	workers := river.NewWorkers()
	indexWorker := riverjobs.NewIndexJobWorker(runner)
	river.AddWorker(workers, &indexWorker)

	riverClient, err := river.NewClient(riverpgxv5.New(db), &river.Config{
		Workers: workers,
		Queues: map[string]river.QueueConfig{
			cfg.IndexJobQueueName: {
				MaxWorkers: cfg.IndexJobQueueWorkers,
			},
		},
	})
	if err != nil {
		logger.Error("failed to initialize river client", "error", err)
		os.Exit(1)
	}

	if err := riverClient.Start(ctx); err != nil {
		logger.Error("failed to start river client", "error", err)
		os.Exit(1)
	}
	defer func() {
		stopCtx, cancel := context.WithCancel(context.Background())
		defer cancel()
		_ = riverClient.Stop(stopCtx)
	}()

	<-ctx.Done()
	if err := ctx.Err(); err != nil && !errors.Is(err, context.Canceled) {
		logger.Error("worker exited with error", "error", err)
		os.Exit(1)
	}
}
