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
	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/store/redisfacts"
)

type App struct {
	cfg        Config
	logger     *slog.Logger
	db         *pgxpool.Pool
	readDB     *pgxpool.Pool
	httpServer *http.Server
	factWriter *redisfacts.Writer
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
	LogPostgresRuntimeSettings(logger, db)
	factWriter, err := redisfacts.New(cfg.AggregateFactRedisURL, db)
	if err != nil {
		db.Close()
		if readDB != nil && readDB != db {
			readDB.Close()
		}
		return nil, err
	}
	factWriter.SetLogger(logger)
	if err := factWriter.Ping(context.Background()); err != nil {
		logger.Warn("aggregate fact store is unavailable at startup; readiness will fail until it recovers", "error", err)
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
		DeferredIngestQueueName:        cfg.DeferredIngestQueueName,
		WritePathConcurrencyLimit:      cfg.WritePathConcurrencyLimit,
		WritePathOverloadMode:          cfg.WritePathOverloadMode,
		ReadQueryConcurrencyLimit:      cfg.ReadQueryConcurrencyLimit,
		AggregateQueryConcurrencyLimit: cfg.AggregateQueryConcurrencyLimit,
		OverloadThresholds: httpapi.OverloadThresholds{
			DBPoolSaturationPct:    cfg.DBPoolSaturationThresholdPct,
			RequestQueueDepth:      cfg.RequestQueueDepthThreshold,
			ServiceCPUPercent:      cfg.ServiceCPUThresholdPct,
			UpstreamTimeoutRatePct: cfg.UpstreamTimeoutRateThresholdPct,
		},
		AggregateFactWriter:     factWriter,
		AggregateFactBackfiller: factWriter,
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
		factWriter: factWriter,
		httpServer: server,
	}, nil
}

func (a *App) Run() error {
	a.logger.Info("starting ingestion service",
		"port", a.cfg.Port,
		"database_max_conns", a.db.Stat().MaxConns(),
		"database_min_conns", a.cfg.DatabaseMinConns,
		"separate_read_pool", a.readDB != nil && a.readDB != a.db,
		"write_path_concurrency_limit", a.cfg.WritePathConcurrencyLimit,
		"write_path_overload_mode", a.cfg.WritePathOverloadMode,
		"read_query_concurrency_limit", a.cfg.ReadQueryConcurrencyLimit,
		"aggregate_query_concurrency_limit", a.cfg.AggregateQueryConcurrencyLimit,
		"aggregate_query_timeout", a.cfg.AggregateQueryTimeout.String(),
	)
	if err := a.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("listen and serve: %w", err)
	}
	return nil
}

func LogPostgresRuntimeSettings(logger *slog.Logger, db *pgxpool.Pool) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	settings, err := storepostgres.ReadRuntimeSettings(ctx, db)
	if err != nil {
		logger.Warn("unable to read effective postgres runtime settings", "error", err)
		return
	}
	logger.Info("effective postgres runtime settings",
		"synchronous_commit", settings.SynchronousCommit,
		"max_wal_size", settings.MaxWALSize,
		"checkpoint_timeout", settings.CheckpointTimeout,
		"checkpoint_completion_target", settings.CheckpointCompletionTarget,
		"wal_compression", settings.WALCompression,
	)
}

func (a *App) Close() {
	if a.factWriter != nil {
		_ = a.factWriter.Close()
	}
	if a.readDB != nil && a.readDB != a.db {
		a.readDB.Close()
	}
	if a.db != nil {
		a.db.Close()
	}
}
