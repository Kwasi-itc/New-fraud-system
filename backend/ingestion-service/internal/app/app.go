package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/httpapi"
	storepostgres "github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/store/postgres"
)

type App struct {
	cfg        Config
	logger     *slog.Logger
	db         *pgxpool.Pool
	readDB     *pgxpool.Pool
	httpServer *http.Server
}

func New(cfg Config, logger *slog.Logger) (*App, error) {
	gin.SetMode(cfg.GinMode)

	db, err := storepostgres.NewPool(context.Background(), cfg.DatabaseURL, storepostgres.PoolConfig{
		MaxConns: int32(cfg.DatabaseMaxConns),
		MinConns: int32(cfg.DatabaseMinConns),
	})
	if err != nil {
		return nil, err
	}
	readDB := db
	if strings.TrimSpace(cfg.ReadDatabaseURL) != "" {
		readDB, err = storepostgres.NewPool(context.Background(), cfg.ReadDatabaseURL, storepostgres.PoolConfig{
			MaxConns: int32(cfg.ReadDatabaseMaxConns),
			MinConns: int32(cfg.ReadDatabaseMinConns),
		})
		if err != nil {
			db.Close()
			return nil, err
		}
	}

	router := httpapi.NewRouter(logger, db, readDB, httpapi.RouterConfig{
		AuthMode:                       cfg.ServiceAuthMode,
		AuthToken:                      cfg.ServiceAuthToken,
		AllowedOrigins:                 cfg.AllowedOrigins,
		DataModelServiceURL:            cfg.DataModelServiceURL,
		HTTPClientTimeout:              cfg.HTTPClientTimeout,
		AggregateQueryTimeout:          cfg.AggregateQueryTimeout,
		WorkerMaxAttempts:              cfg.WorkerMaxAttempts,
		UploadLogQueueName:             cfg.UploadLogQueueName,
		WritePathConcurrencyLimit:      cfg.WritePathConcurrencyLimit,
		ReadQueryConcurrencyLimit:      cfg.ReadQueryConcurrencyLimit,
		AggregateQueryConcurrencyLimit: cfg.AggregateQueryConcurrencyLimit,
		OverloadThresholds: httpapi.OverloadThresholds{
			DBPoolSaturationPct:    cfg.DBPoolSaturationThresholdPct,
			RequestQueueDepth:      cfg.RequestQueueDepthThreshold,
			ServiceCPUPercent:      cfg.ServiceCPUThresholdPct,
			UpstreamTimeoutRatePct: cfg.UpstreamTimeoutRateThresholdPct,
		},
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
		readDB:     readDB,
		httpServer: server,
	}, nil
}

func (a *App) Run() error {
	a.logger.Info("starting ingestion service", "port", a.cfg.Port)
	if err := a.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("listen and serve: %w", err)
	}
	return nil
}

func (a *App) Close() {
	if a.readDB != nil && a.readDB != a.db {
		a.readDB.Close()
	}
	if a.db != nil {
		a.db.Close()
	}
}
