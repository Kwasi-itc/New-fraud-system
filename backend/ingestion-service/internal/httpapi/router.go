package httpapi

import (
	platformauth "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth"
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/clients/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/httpapi/handlers"
	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/riverjobs"
	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/service"
	storepostgres "github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/store/postgres"
)

type RouterConfig struct {
	PolicyObserver func([]platformauth.RoutePolicy)
	Verifier       platformauth.TokenVerifier

	AllowedOrigins                 []string
	DataModelServiceURL            string
	DataModelReadMode              string
	DataModelCacheMaxEntries       int
	DataModelCacheMaxBytes         int
	HTTPClientTimeout              time.Duration
	AggregateQueryTimeout          time.Duration
	WorkerMaxAttempts              int
	UploadLogQueueName             string
	DeferredIngestQueueName        string
	WritePathConcurrencyLimit      int
	WritePathOverloadMode          string
	ReadQueryConcurrencyLimit      int
	AggregateQueryConcurrencyLimit int
	EnableRollupAggregates         bool
	RollupShadowMode               bool
	OverloadThresholds             OverloadThresholds
}

type uuidGenerator struct{}

func (uuidGenerator) New() uuid.UUID {
	return uuid.New()
}

type systemClock struct{}

func (systemClock) Now() time.Time {
	return time.Now().UTC()
}

func NewRouter(logger *slog.Logger, db *pgxpool.Pool, readDB *pgxpool.Pool, cfg RouterConfig) *gin.Engine {
	engine := gin.New()
	router := platformauth.NewRouter(engine, cfg.Verifier)
	router.Use(corsMiddleware(cfg.AllowedOrigins))
	router.Use(requestContextMiddleware(logger))
	router.Use(gin.Recovery())
	registerDocsRoutes(router)

	if readDB == nil {
		readDB = db
	}

	healthHandler := handlers.NewHealthHandler(logger, db)
	router.GET("/healthz", platformauth.Policy{Public: true}, healthHandler.Healthz)
	router.GET("/readyz", platformauth.Policy{Public: true}, healthHandler.Readyz)
	readMetrics := newReadMetricsCollector(dbPoolStatsFromPool(db), dbPoolStatsFromPool(readDB), readDB == db)
	readMetrics.SetThresholds(cfg.OverloadThresholds)
	var writePathLimiter chan struct{}
	if cfg.WritePathConcurrencyLimit > 0 {
		writePathLimiter = make(chan struct{}, cfg.WritePathConcurrencyLimit)
	}

	var dataModelReader ports.DataModelReader = datamodel.NewHTTPClientWithCacheLimits(cfg.DataModelServiceURL, cfg.HTTPClientTimeout, cfg.DataModelCacheMaxEntries, cfg.DataModelCacheMaxBytes)
	if cfg.DataModelReadMode == "sql" {
		dataModelReader = datamodel.NewContractReader(storepostgres.NewIngestionContractStore(db), dataModelReader, cfg.DataModelCacheMaxEntries, cfg.DataModelCacheMaxBytes)
	}
	var txManager ports.TransactionManager
	var rollupCache *storepostgres.RollupDefinitionCache
	var uploadLogRepository ports.UploadLogRepository
	var deferredIngestRepository ports.DeferredIngestRepository
	var uploadLogEnqueuer riverjobs.UploadLogEnqueuer = riverjobs.NoopUploadLogEnqueuer{}
	var deferredIngestEnqueuer riverjobs.DeferredIngestEnqueuer = riverjobs.NoopDeferredIngestEnqueuer{}
	if db != nil {
		pgTxManager := storepostgres.NewTransactionManager(db)
		rollupCache = pgTxManager.RollupCache()
		txManager = pgTxManager
		uploadLogRepository = storepostgres.NewUploadLogRepository(db)
		deferredIngestRepository = storepostgres.NewDeferredIngestRepository(db)
		riverClient, _ := river.NewClient(riverpgxv5.New(db), &river.Config{})
		uploadLogEnqueuer = riverjobs.NewRiverUploadLogEnqueuer(riverClient, max(1, cfg.WorkerMaxAttempts), cfg.UploadLogQueueName)
		deferredIngestEnqueuer = riverjobs.NewRiverDeferredIngestEnqueuer(riverClient, max(1, cfg.WorkerMaxAttempts), cfg.DeferredIngestQueueName)
	}
	var readDataReader ports.TenantDataReader
	if readDB != nil {
		readDataReader = storepostgres.NewTenantDataReader(readDB).
			WithStatementTimeout(cfg.AggregateQueryTimeout).
			WithRollups(rollupCache, cfg.EnableRollupAggregates, cfg.RollupShadowMode)
	}
	modelContractService := service.NewModelContractService(dataModelReader)
	_ = modelContractService
	ingestService := service.NewIngestService(
		dataModelReader,
		txManager,
		readDataReader,
		uuidGenerator{},
		systemClock{},
	)
	deferredIngestService := service.NewDeferredIngestService(
		deferredIngestRepository,
		ingestService,
		txManager,
		uuidGenerator{},
		systemClock{},
		max(1, cfg.WorkerMaxAttempts),
		deferredIngestEnqueuer,
	)
	deferredIngestHandler := handlers.NewDeferredIngestHandler(deferredIngestService)
	ingestHandler := handlers.NewIngestHandler(ingestService, handlers.IngestHandlerConfig{
		WritePathLimiter:               writePathLimiter,
		WritePathOverloadMode:          cfg.WritePathOverloadMode,
		DeferredIngestService:          deferredIngestService,
		ReadQueryConcurrencyLimit:      cfg.ReadQueryConcurrencyLimit,
		AggregateQueryConcurrencyLimit: cfg.AggregateQueryConcurrencyLimit,
		AggregateQueryTimeout:          cfg.AggregateQueryTimeout,
	})
	uploadLogService := service.NewUploadLogService(
		uploadLogRepository,
		ingestService,
		txManager,
		uuidGenerator{},
		systemClock{},
		max(1, cfg.WorkerMaxAttempts),
		uploadLogEnqueuer,
	)
	uploadLogHandler := handlers.NewUploadLogHandler(uploadLogService, handlers.UploadLogHandlerConfig{
		WritePathLimiter: writePathLimiter,
	})
	readMetricsHandler := handlers.NewReadMetricsHandler(readMetrics)
	deferredIngestMetricsHandler := handlers.NewDeferredIngestMetricsHandler(func(c *gin.Context) (any, error) {
		return deferredIngestService.MetricsSnapshot(c.Request.Context())
	})
	rollupHandler := handlers.NewRollupHandler(storepostgres.NewRollupRepository(db), rollupCache, dataModelReader, handlers.DefaultMaxRollupsPerTable)

	v1 := router.Group("/v1")
	v1.POST("/tenants/:tenantId/ingest/:objectType", platformauth.Policy{Capability: "ingest:write"}, ingestHandler.PostIngest)
	v1.PATCH("/tenants/:tenantId/ingest/:objectType", platformauth.Policy{Capability: "ingest:write"}, ingestHandler.PatchIngest)
	v1.POST("/tenants/:tenantId/ingest/:objectType/batch", platformauth.Policy{Capability: "ingest:write"}, ingestHandler.PostBatchIngest)
	v1.PATCH("/tenants/:tenantId/ingest/:objectType/batch", platformauth.Policy{Capability: "ingest:write"}, ingestHandler.PatchBatchIngest)
	v1.GET("/admin/read-metrics", platformauth.Policy{Capability: "metrics:read"}, readMetricsHandler.Get)
	v1.GET("/admin/deferred-ingest-metrics", platformauth.Policy{Capability: "metrics:read"}, deferredIngestMetricsHandler.Get)
	v1.GET("/admin/rollup-metrics", platformauth.Policy{Capability: "metrics:read"}, rollupHandler.Metrics)
	v1.POST("/tenants/:tenantId/rollups", platformauth.Policy{Capability: "ingest:write"}, rollupHandler.Create)
	v1.GET("/tenants/:tenantId/rollups", platformauth.Policy{Capability: "records:read"}, rollupHandler.List)
	v1.POST("/tenants/:tenantId/rollups/:definitionId/disable", platformauth.Policy{Capability: "ingest:write"}, rollupHandler.Disable)
	readRoutes := v1.Group("")
	readRoutes.GET("/tenants/:tenantId/records/:objectType", platformauth.Policy{Capability: "records:read"}, readMetrics.middleware("list_records"), ingestHandler.ListRecords)
	readRoutes.GET("/tenants/:tenantId/records/:objectType/search", platformauth.Policy{Capability: "records:read"}, readMetrics.middleware("query_records"), ingestHandler.QueryRecords)
	readRoutes.GET("/tenants/:tenantId/records/:objectType/:objectId", platformauth.Policy{Capability: "records:read"}, readMetrics.middleware("get_record"), ingestHandler.GetRecord)
	readRoutes.POST("/tenants/:tenantId/query/aggregate", platformauth.Policy{Capability: "records:read"}, readMetrics.middleware("aggregate_records"), ingestHandler.AggregateRecords)
	v1.POST("/tenants/:tenantId/ingest/:objectType/csv", platformauth.Policy{Capability: "ingest:write"}, uploadLogHandler.CreateCSV)
	v1.GET("/tenants/:tenantId/ingest/:objectType/upload-logs", platformauth.Policy{Capability: "ingest:read"}, uploadLogHandler.List)
	v1.GET("/upload-logs/:uploadLogId", platformauth.Policy{Capability: "ingest:read"}, uploadLogHandler.Get)
	v1.GET("/deferred-ingests/:deferredIngestId", platformauth.Policy{Capability: "ingest:read"}, deferredIngestHandler.Get)

	if err := router.ValidateCoverage(); err != nil {
		panic(err)
	}
	if cfg.PolicyObserver != nil {
		cfg.PolicyObserver(router.Inventory())
	}
	return engine
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
