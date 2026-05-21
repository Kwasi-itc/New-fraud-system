package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/google/uuid"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/app"
	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/clients/datamodel"
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
		uuidGenerator{},
		systemClock{},
		cfg.WorkerMaxAttempts,
	)

	ticker := time.NewTicker(cfg.WorkerPollInterval)
	defer ticker.Stop()

	logger.Info("starting ingestion worker", "poll_interval", cfg.WorkerPollInterval.String())
	for {
		processed, err := uploadLogService.ProcessNextUploaded(context.Background())
		if err != nil {
			logger.Error("failed to process uploaded csv log", "error", err)
		}
		if !processed {
			<-ticker.C
		}
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
