package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"strings"
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

	workerDatabaseURL := strings.TrimSpace(cfg.WorkerDatabaseURL)
	if workerDatabaseURL == "" {
		workerDatabaseURL = cfg.DatabaseURL
	}
	db, err := storepostgres.NewPool(context.Background(), workerDatabaseURL, storepostgres.PoolConfig{
		MaxConns: int32(cfg.WorkerDatabaseMaxConns),
		MinConns: int32(cfg.WorkerDatabaseMinConns),
	})
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
		nil,
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
	deferredIngestService := service.NewDeferredIngestService(
		storepostgres.NewDeferredIngestRepository(db),
		ingestService,
		storepostgres.NewTransactionManager(db),
		uuidGenerator{},
		systemClock{},
		cfg.WorkerMaxAttempts,
		riverjobs.NoopDeferredIngestEnqueuer{},
	)

	workers := river.NewWorkers()
	uploadLogWorker := riverjobs.NewUploadLogWorker(uploadLogService)
	river.AddWorker(workers, &uploadLogWorker)
	deferredIngestWorker := riverjobs.NewDeferredIngestWorker(deferredIngestService)
	river.AddWorker(workers, &deferredIngestWorker)

	riverClient, err := river.NewClient(riverpgxv5.New(db), &river.Config{
		Workers: workers,
		Queues: map[string]river.QueueConfig{
			cfg.UploadLogQueueName: {
				MaxWorkers: cfg.UploadLogQueueWorkers,
			},
			cfg.DeferredIngestQueueName: {
				MaxWorkers: cfg.DeferredIngestQueueWorkers,
			},
		},
	})
	if err != nil {
		logger.Error("failed to initialize river client", "error", err)
		os.Exit(1)
	}

	logger.Info("starting ingestion worker",
		"database_url_source", workerDatabaseURLSource(cfg),
		"upload_log_queue", cfg.UploadLogQueueName,
		"upload_log_workers", cfg.UploadLogQueueWorkers,
		"deferred_ingest_queue", cfg.DeferredIngestQueueName,
		"deferred_ingest_workers", cfg.DeferredIngestQueueWorkers,
		"db_max_conns", cfg.WorkerDatabaseMaxConns,
		"db_min_conns", cfg.WorkerDatabaseMinConns,
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

func workerDatabaseURLSource(cfg app.Config) string {
	if strings.TrimSpace(cfg.WorkerDatabaseURL) != "" {
		return "WORKER_DATABASE_URL"
	}
	return "DATABASE_URL"
}
