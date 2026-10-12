package main

import (
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/app"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/observability"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "healthcheck" {
		port := os.Getenv("PORT")
		if port == "" {
			port = "8086"
		}
		client := http.Client{Timeout: 2 * time.Second}
		resp, err := client.Get("http://127.0.0.1:" + port + "/readyz")
		if err != nil {
			os.Exit(1)
		}
		resp.Body.Close()
		if resp.StatusCode != 200 {
			os.Exit(1)
		}
		return
	}
	logger := observability.New(os.Stdout, "api", os.Getenv("LOG_LEVEL"))

	cfg, err := app.LoadConfig()
	if err != nil {
		logger.Error("failed to load config", "error", err)
		os.Exit(1)
	}
	logger = observability.New(os.Stdout, "api", cfg.LogLevel)
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
