package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"

	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/app"
	blobclient "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/clients/blob"
	caseclient "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/clients/case"
	decisionclient "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/clients/decisionengine"
	inboxclient "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/clients/inbox"
	ingestionclient "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/clients/ingestion"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/clients/provider"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/service"
	storepostgres "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/store/postgres"
)

type uuidGenerator struct{}

func (uuidGenerator) New() uuid.UUID { return uuid.New() }

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now().UTC() }

type workerRunner struct {
	logger           *slog.Logger
	screeningWorker  service.DispatchService
	continuousWorker service.ContinuousWorkerService
	datasetWorker    service.DatasetUpdateWorkerService
	batchLimit       int
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	cfg, err := app.LoadConfig()
	if err != nil {
		logger.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	db, err := storepostgres.NewPool(context.Background(), cfg.DatabaseURL)
	if err != nil {
		logger.Error("failed to connect database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	var screeningRepo ports.ScreeningRepository = storepostgres.NewScreeningRepository(db)
	var matchRepo ports.ScreeningMatchRepository = storepostgres.NewScreeningMatchRepository(db)
	var commentRepo ports.ScreeningCommentRepository = storepostgres.NewScreeningCommentRepository(db)
	var whitelistRepo ports.ScreeningWhitelistRepository = storepostgres.NewScreeningWhitelistRepository(db)
	var fileRepo ports.ScreeningFileRepository = storepostgres.NewScreeningFileRepository(db)
	var continuousRepo ports.ContinuousConfigRepository = storepostgres.NewContinuousConfigRepository(db)
	var monitoredObjRepo ports.MonitoredObjectRepository = storepostgres.NewMonitoredObjectRepository(db)
	var datasetJobRepo ports.DatasetUpdateJobRepository = storepostgres.NewDatasetUpdateJobRepository(db)
	var txManager ports.TransactionManager = storepostgres.NewTransactionManager(db)

	providerClient := provider.NewHTTPClient(cfg.ScreeningProviderURL, provider.ParseProviderURLs(cfg.ScreeningProviderURLs), cfg.HTTPClientTimeout, provider.OpenSanctionsConfig{
		APIHost:   cfg.OpenSanctionsAPIHost,
		AuthMode:  cfg.OpenSanctionsAuthMode,
		APIKey:    cfg.OpenSanctionsAPIKey,
		Scope:     cfg.OpenSanctionsScope,
		Algorithm: cfg.OpenSanctionsAlgorithm,
	})
	ingestionReader := ingestionclient.NewHTTPClient(cfg.IngestionServiceURL, cfg.HTTPClientTimeout)
	inboxReader := inboxclient.NewHTTPClient(cfg.InboxServiceURL, cfg.HTTPClientTimeout)
	casePublisher := caseclient.NewHTTPClient(cfg.CaseServiceURL, cfg.HTTPClientTimeout)
	blobStore := blobclient.NewHTTPClient(cfg.BlobServiceURL, cfg.HTTPClientTimeout)
	decisionPublisher := decisionclient.NewHTTPClient(cfg.DecisionEngineURL, cfg.ServiceAuthMode, cfg.ServiceAuthToken, cfg.HTTPClientTimeout)
	screeningService := service.NewScreeningService(txManager, uuidGenerator{}, systemClock{}, screeningRepo, matchRepo, commentRepo, whitelistRepo, fileRepo, continuousRepo, monitoredObjRepo, datasetJobRepo, providerClient, inboxReader, casePublisher, blobStore, decisionPublisher)
	dispatchService := service.NewDispatchService(txManager, systemClock{}, screeningRepo, matchRepo, providerClient, decisionPublisher)
	continuousWorker := service.NewContinuousWorkerService(txManager, systemClock{}, continuousRepo, monitoredObjRepo, ingestionReader, screeningService)
	datasetWorker := service.NewDatasetUpdateWorkerService(txManager, systemClock{}, datasetJobRepo, continuousRepo, monitoredObjRepo, providerClient)

	runner := workerRunner{
		logger:           logger,
		screeningWorker:  dispatchService,
		continuousWorker: continuousWorker,
		datasetWorker:    datasetWorker,
		batchLimit:       cfg.WorkerBatchLimit,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	switch cfg.WorkerMode {
	case "poll":
		logger.Info("screening worker poll loop starting", "poll_interval", cfg.WorkerPollInterval.String(), "batch_limit", cfg.WorkerBatchLimit)
		if err := runner.runPollLoop(ctx, cfg.WorkerPollInterval); err != nil {
			logger.Error("worker poll loop failed", "error", err)
			os.Exit(1)
		}
	default:
		logger.Info("screening worker batch run starting", "batch_limit", cfg.WorkerBatchLimit)
		if err := runner.runOnce(ctx); err != nil {
			logger.Error("worker batch run failed", "error", err)
			os.Exit(1)
		}
		logger.Info("screening worker batch run completed")
	}
}

func (w workerRunner) runPollLoop(ctx context.Context, interval time.Duration) error {
	if err := w.runOnce(ctx); err != nil {
		return err
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			w.logger.Info("screening worker poll loop stopping")
			return nil
		case <-ticker.C:
			if err := w.runOnce(ctx); err != nil {
				return err
			}
		}
	}
}

func (w workerRunner) runOnce(ctx context.Context) error {
	cycleStartedAt := time.Now()
	w.logger.Info("screening worker cycle started", "batch_limit", w.batchLimit)

	screeningStartedAt := time.Now()
	if err := w.screeningWorker.ProcessPendingScreenings(ctx, w.batchLimit); err != nil {
		w.logger.Error("screening worker phase failed", "phase", "screening_dispatch", "duration_ms", time.Since(screeningStartedAt).Milliseconds(), "error", err)
		return err
	}
	w.logger.Info("screening worker phase completed", "phase", "screening_dispatch", "duration_ms", time.Since(screeningStartedAt).Milliseconds())

	continuousStartedAt := time.Now()
	if err := w.continuousWorker.ProcessPendingMonitoredObjects(ctx, w.batchLimit); err != nil {
		w.logger.Error("screening worker phase failed", "phase", "continuous_screening", "duration_ms", time.Since(continuousStartedAt).Milliseconds(), "error", err)
		return err
	}
	w.logger.Info("screening worker phase completed", "phase", "continuous_screening", "duration_ms", time.Since(continuousStartedAt).Milliseconds())

	datasetStartedAt := time.Now()
	if err := w.datasetWorker.ProcessPendingJobs(ctx, w.batchLimit); err != nil {
		w.logger.Error("screening worker phase failed", "phase", "dataset_update_jobs", "duration_ms", time.Since(datasetStartedAt).Milliseconds(), "error", err)
		return err
	}
	w.logger.Info("screening worker phase completed", "phase", "dataset_update_jobs", "duration_ms", time.Since(datasetStartedAt).Milliseconds())
	w.logger.Info("screening worker cycle completed", "duration_ms", time.Since(cycleStartedAt).Milliseconds())
	return nil
}
