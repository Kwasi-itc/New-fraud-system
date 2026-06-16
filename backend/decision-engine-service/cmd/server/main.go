package main

import (
	"log/slog"
	"os"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/app"
)

func main() {
	cfg, err := app.LoadConfig()
	if err != nil {
		logger := app.NewLogger(os.Stdout, "info")
		logger.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	logger := app.NewLogger(os.Stdout, cfg.LogLevel)
	slog.SetDefault(logger)

	application, err := app.New(cfg, logger)
	if err != nil {
		logger.Error("failed to initialize application", "error", err)
		os.Exit(1)
	}
	defer application.Close()

	if err := application.Run(); err != nil {
		logger.Error("server exited with error", "error", err)
		os.Exit(1)
	}
}
