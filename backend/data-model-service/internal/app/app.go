package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/httpapi"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/postgres"
)

type App struct {
	cfg        Config
	logger     *slog.Logger
	db         *pgxpool.Pool
	httpServer *http.Server
}

func New(cfg Config, logger *slog.Logger) (*App, error) {
	gin.SetMode(cfg.GinMode)

	db, err := postgres.NewPool(context.Background(), cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}

	router := httpapi.NewRouter(logger, db, httpapi.RouterConfig{
		AuthMode:               cfg.ServiceAuthMode,
		AuthToken:              cfg.ServiceAuthToken,
		AllowedOrigins:         cfg.ServiceAllowedOrigins,
		IndexWorkerMaxAttempts: cfg.IndexWorkerMaxAttempts,
		IndexJobQueueName:      cfg.IndexJobQueueName,
	})
	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
	}

	return &App{
		cfg:        cfg,
		logger:     logger,
		db:         db,
		httpServer: server,
	}, nil
}

func (a *App) Run() error {
	a.logger.Info("starting data model service", "port", a.cfg.Port)
	if err := a.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("listen and serve: %w", err)
	}
	return nil
}

func (a *App) Close() {
	if a.db != nil {
		a.db.Close()
	}
}
