package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/app"
	storepostgres "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/store/postgres"
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
		logger.Error("failed to connect database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	runOnce := func() {
		logger.Info("case manager worker cycle completed", "batch_limit", cfg.WorkerBatchLimit)
	}
	if cfg.WorkerMode != "poll" {
		runOnce()
		return
	}
	ticker := time.NewTicker(cfg.WorkerPollInterval)
	defer ticker.Stop()
	runOnce()
	for {
		select {
		case <-ctx.Done():
			logger.Info("case manager worker stopping")
			return
		case <-ticker.C:
			runOnce()
		}
	}
}
