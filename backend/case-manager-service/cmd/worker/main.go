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
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/worker"
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

	w := &worker.Worker{Store: storepostgres.Maintenance{DB: db}, Limit: cfg.WorkerBatchLimit, PublisherURL: cfg.OutboxPublisherURL, PublisherToken: os.Getenv("OUTBOX_PUBLISHER_AUTH_TOKEN")}
	client, err := worker.NewClient(db, w, cfg.WorkerPollInterval)
	if err != nil {
		logger.Error("invalid worker configuration", "error", err)
		os.Exit(1)
	}
	if cfg.OutboxPublisherURL == "" {
		logger.Warn("outbound delivery disabled; events remain pending")
	}
	if cfg.WorkerMode != "poll" {
		if err := w.Run(ctx); err != nil {
			logger.Error("maintenance failed", "error", err)
			os.Exit(1)
		}
		return
	}
	if err := client.Start(ctx); err != nil {
		logger.Error("start River", "error", err)
		os.Exit(1)
	}
	<-ctx.Done()
	stopCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := client.Stop(stopCtx); err != nil {
		logger.Error("stop River", "error", err)
		os.Exit(1)
	}
}
