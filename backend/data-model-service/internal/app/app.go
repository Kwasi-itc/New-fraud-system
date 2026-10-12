package app

import (
	"context"
	"fmt"
	"github.com/Kwasi-itc/New-fraud-system/backend/investigator"
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
	verifier, err := investigator.FromEnvironment()
	if err != nil {
		return nil, fmt.Errorf("platform JWT configuration: %w", err)
	}
	if verifier != nil && cfg.ServiceAuthMode != "token" {
		return nil, fmt.Errorf("platform user authentication requires SERVICE_AUTH_MODE=token")
	}

	db, err := postgres.NewPool(context.Background(), cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}

	routerConfig := httpapi.RouterConfig{
		AuthMode:               cfg.ServiceAuthMode,
		AuthToken:              cfg.ServiceAuthToken,
		AllowedOrigins:         cfg.ServiceAllowedOrigins,
		IndexWorkerMaxAttempts: cfg.IndexWorkerMaxAttempts,
		IndexJobQueueName:      cfg.IndexJobQueueName,
	}
	if verifier != nil {
		routerConfig.UserVerifier = verifier
	}
	router := httpapi.NewRouter(logger, db, routerConfig)
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
