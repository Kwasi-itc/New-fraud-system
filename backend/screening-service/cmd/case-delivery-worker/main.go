package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/app"
	caseclient "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/clients/case"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/service"
	store "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/store/postgres"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := app.LoadConfig()
	if err != nil {
		logger.Error("invalid configuration", "error", err)
		os.Exit(1)
	}
	if cfg.CaseServiceURL == "" || cfg.CaseServiceAuthToken == "" {
		logger.Error("CASE_SERVICE_URL and CASE_SERVICE_AUTH_TOKEN are required")
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	db, err := store.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("database initialization failed", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	delivery := service.NewCaseDeliveryService(store.NewCaseEventRepository(db), caseclient.NewHTTPClient(cfg.CaseServiceURL, cfg.CaseServiceAuthToken, 10*time.Second), logger)
	ticker := time.NewTicker(cfg.WorkerPollInterval)
	defer ticker.Stop()
	for {
		if err := delivery.RunBatch(ctx, cfg.WorkerBatchLimit); err != nil && ctx.Err() == nil {
			logger.Error("case delivery cycle failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
