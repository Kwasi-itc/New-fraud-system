package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/app"
	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/clients/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/riverjobs"
	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/service"
	storepostgres "github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/store/postgres"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	cfg, err := app.LoadConfig()
	if err != nil {
		logger.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	db, err := storepostgres.NewPool(context.Background(), cfg.DatabaseURL)
	if err != nil {
		logger.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	dataModelReader := datamodel.NewHTTPClient(cfg.DataModelServiceURL, cfg.HTTPClientTimeout)
	ingestService := service.NewIngestService(
		dataModelReader,
		storepostgres.NewTransactionManager(db),
		uuidGenerator{},
		systemClock{},
	)
	uploadLogService := service.NewUploadLogService(
		storepostgres.NewUploadLogRepository(db),
		ingestService,
		storepostgres.NewTransactionManager(db),
		uuidGenerator{},
		systemClock{},
		cfg.WorkerMaxAttempts,
		riverjobs.NoopUploadLogEnqueuer{},
	)

	workers := river.NewWorkers()
	uploadLogWorker := riverjobs.NewUploadLogWorker(uploadLogService)
	river.AddWorker(workers, &uploadLogWorker)

	riverClient, err := river.NewClient(riverpgxv5.New(db), &river.Config{
		Workers: workers,
		Queues: map[string]river.QueueConfig{
			cfg.UploadLogQueueName: {
				MaxWorkers: cfg.UploadLogQueueWorkers,
			},
		},
	})
	if err != nil {
		logger.Error("failed to initialize river client", "error", err)
		os.Exit(1)
	}

	logger.Info("starting ingestion worker",
		"queue", cfg.UploadLogQueueName,
		"workers", cfg.UploadLogQueueWorkers,
		"max_attempts", cfg.WorkerMaxAttempts,
	)
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

type uuidGenerator struct{}

func (uuidGenerator) New() uuid.UUID {
	return uuid.New()
}

type systemClock struct{}

func (systemClock) Now() time.Time {
	return time.Now().UTC()
}
