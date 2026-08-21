package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	sharedeventstore "github.com/Kwasi-itc/New-fraud-system/backend/event-store-service"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/clients/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/httpapi"
	storeclickhouse "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/clickhouse"
	storepostgres "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/postgres"
)

type App struct {
	cfg        Config
	logger     *slog.Logger
	db         *pgxpool.Pool
	eventStore *sharedeventstore.Repository
	httpServer *http.Server
}

func New(cfg Config, logger *slog.Logger) (*App, error) {
	gin.SetMode(cfg.GinMode)

	db, err := storepostgres.NewPool(context.Background(), cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	eventStore, err := sharedeventstore.NewRepository(cfg.EventStoreConfig(), logger)
	if err != nil {
		db.Close()
		return nil, err
	}
	initCtx, cancel := context.WithTimeout(context.Background(), cfg.ClickHouseTimeout)
	defer cancel()
	if err := eventStore.Initialize(initCtx); err != nil {
		eventStore.Close()
		db.Close()
		return nil, fmt.Errorf("initialize ClickHouse event repository: %w", err)
	}
	eventDataModels := datamodel.NewHTTPClient(cfg.DataModelServiceURL, cfg.HTTPClientTimeout)
	eventDataReader := storeclickhouse.NewTenantDataReader(eventStore, eventDataModels)

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
		EventDataReader:                     eventDataReader,
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
		eventStore: eventStore,
		httpServer: server,
	}, nil
}

func (a *App) Run() error {
	a.logger.Info("starting decision engine service", "port", a.cfg.Port)
	if err := a.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("listen and serve: %w", err)
	}
	return nil
}

func (a *App) Close() {
	if a.eventStore != nil {
		a.eventStore.Close()
	}
	if a.db != nil {
		a.db.Close()
	}
}
