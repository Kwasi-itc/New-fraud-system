package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/app"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/observability"
	storepostgres "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/store/postgres"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/worker"
)

func main() {
	logger := observability.New(os.Stdout, "worker", os.Getenv("LOG_LEVEL"))
	cfg, err := app.LoadConfig()
	if err != nil {
		logger.Error("failed to load config", "error", err)
		os.Exit(1)
	}
	logger = observability.New(os.Stdout, "worker", cfg.LogLevel)
	slog.SetDefault(logger)
	db, err := storepostgres.NewPool(context.Background(), cfg.DatabaseURL)
	if err != nil {
		logger.Error("failed to connect database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx = observability.WithLogger(ctx, logger)
	logger.Info("case worker starting", "event", "worker.starting", "mode", cfg.WorkerMode, "poll_interval", cfg.WorkerPollInterval.String(), "batch_limit", cfg.WorkerBatchLimit, "pool_max_connections", db.Config().MaxConns, "outbound_delivery_enabled", cfg.OutboxPublisherURL != "")

	w := &worker.Worker{Store: storepostgres.Maintenance{DB: db}, Limit: cfg.WorkerBatchLimit, PublisherURL: cfg.OutboxPublisherURL, PublisherToken: os.Getenv("OUTBOX_PUBLISHER_AUTH_TOKEN"), Logger: logger}
	client, err := worker.NewClient(db, w, cfg.WorkerPollInterval)
	if err != nil {
		logger.Error("invalid worker configuration", "error", err)
		os.Exit(1)
	}
	if cfg.OutboxPublisherURL == "" {
		logger.Warn("outbound delivery disabled; events remain pending", "event", "outbox.disabled", "reason", "OUTBOX_PUBLISHER_URL is empty", "action", "Set CASE_OUTBOX_PUBLISHER_URL in Compose to a real event receiver; assignment and snooze maintenance remain enabled")
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
	logger.Info("case worker running", "event", "worker.started", "maintenance_queue", worker.Queue, "delivery_queue", worker.DeliveryQueue)
	<-ctx.Done()
	logger.Info("case worker stopping", "event", "worker.stopping")
	stopCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := client.Stop(stopCtx); err != nil {
		logger.Error("stop River", "error", err)
		os.Exit(1)
	}
	logger.Info("case worker stopped", "event", "worker.stopped")
}
