package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/httpapi"
	storepostgres "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/postgres"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/redisfacts"
)

type App struct {
	cfg        Config
	logger     *slog.Logger
	db         *pgxpool.Pool
	httpServer *http.Server
	factReader *redisfacts.Reader
}

func New(cfg Config, logger *slog.Logger) (*App, error) {
	gin.SetMode(cfg.GinMode)

	db, err := storepostgres.NewPoolWithConfig(context.Background(), cfg.DatabaseURL, storepostgres.PoolConfig{
		MaxConns: cfg.DatabaseMaxConns,
		MinConns: cfg.DatabaseMinConns,
	})
	if err != nil {
		return nil, err
	}
	LogPostgresRuntimeSettings(logger, db)
	factReader, err := redisfacts.New(cfg.AggregateFactRedisURL)
	if err != nil {
		db.Close()
		return nil, err
	}
	if err := factReader.Ping(context.Background()); err != nil {
		logger.Warn("aggregate fact store is unavailable at startup; readiness will fail until it recovers", "error", err)
	}

	router := httpapi.NewRouter(logger, db, httpapi.RouterConfig{
		AuthMode:                            cfg.ServiceAuthMode,
		AuthToken:                           cfg.ServiceAuthToken,
		AllowedOrigins:                      cfg.ServiceAllowedOrigins,
		DataModelServiceURL:                 cfg.DataModelServiceURL,
		IngestionServiceURL:                 cfg.IngestionServiceURL,
		TenantDataReadMode:                  cfg.TenantDataReadMode,
		HTTPClientTimeout:                   cfg.HTTPClientTimeout,
		AggregatePushdownMode:               cfg.AggregatePushdownMode,
		AggregatePushdownAggregates:         cfg.AggregatePushdownAggregates,
		LiveDecisionMode:                    cfg.LiveDecisionMode,
		LiveAsyncObjectTypes:                cfg.LiveAsyncObjectTypes,
		LiveDecisionConcurrencyLimit:        cfg.LiveDecisionConcurrencyLimit,
		LiveAsyncFallbackEnabled:            cfg.LiveAsyncFallbackEnabled,
		RuleEvaluationConcurrency:           cfg.RuleEvaluationConcurrency,
		ScenarioEvaluationConcurrency:       cfg.ScenarioEvaluationConcurrency,
		AggregateRemoteConcurrencyLimit:     cfg.AggregateRemoteConcurrencyLimit,
		ScheduledExecutionMaxAttempts:       cfg.ScheduledExecutionMaxAttempts,
		ScheduledExecutionRetryBackoff:      cfg.ScheduledExecutionRetryBackoff,
		ScheduledExecutionQueueName:         cfg.ScheduledExecutionQueueName,
		AsyncExecutionMaxAttempts:           cfg.AsyncExecutionMaxAttempts,
		AsyncExecutionRetryBackoff:          cfg.AsyncExecutionRetryBackoff,
		AsyncExecutionDefaultWaitWindow:     cfg.AsyncExecutionDefaultWaitWindow,
		AsyncExecutionMaxWaitWindow:         cfg.AsyncExecutionMaxWaitWindow,
		AsyncExecutionCallbackTimeout:       cfg.AsyncExecutionCallbackTimeout,
		AsyncExecutionCallbackSigningSecret: cfg.AsyncExecutionCallbackSigningSecret,
		AsyncExecutionQueueName:             cfg.AsyncExecutionQueueName,
		AsyncExecutionCallbackQueueName:     cfg.AsyncExecutionCallbackQueueName,
		WorkflowDispatchQueueName:           cfg.WorkflowDispatchQueueName,
		ScreeningDispatchQueueName:          cfg.ScreeningDispatchQueueName,
		ScoringDispatchQueueName:            cfg.ScoringDispatchQueueName,
		OutboxQueueName:                     cfg.OutboxQueueName,
		AggregateFactBuckets:                factReader,
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
		factReader: factReader,
		httpServer: server,
	}, nil
}

func (a *App) Run() error {
	a.logger.Info("starting decision engine service",
		"port", a.cfg.Port,
		"database_max_conns", a.db.Stat().MaxConns(),
		"database_min_conns", a.cfg.DatabaseMinConns,
		"tenant_data_read_mode", a.cfg.TenantDataReadMode,
		"live_decision_mode", a.cfg.LiveDecisionMode,
		"live_async_fallback_enabled", a.cfg.LiveAsyncFallbackEnabled,
		"rule_evaluation_concurrency", a.cfg.RuleEvaluationConcurrency,
		"scenario_evaluation_concurrency", a.cfg.ScenarioEvaluationConcurrency,
		"aggregate_remote_concurrency_limit", a.cfg.AggregateRemoteConcurrencyLimit,
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
	if a.factReader != nil {
		_ = a.factReader.Close()
	}
	if a.db != nil {
		a.db.Close()
	}
}
