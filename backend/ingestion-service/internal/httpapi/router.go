package httpapi

import (
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
	AuthMode                       string
	AuthToken                      string
	AllowedOrigins                 []string
	DataModelServiceURL            string
	HTTPClientTimeout              time.Duration
	AggregateQueryTimeout          time.Duration
	WorkerMaxAttempts              int
	UploadLogQueueName             string
	WritePathConcurrencyLimit      int
	ReadQueryConcurrencyLimit      int
	AggregateQueryConcurrencyLimit int
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
	router := gin.New()
	router.Use(corsMiddleware(cfg.AllowedOrigins))
	router.Use(requestContextMiddleware(logger))
	router.Use(gin.Recovery())
	registerDocsRoutes(router)

	if readDB == nil {
		readDB = db
	}

	healthHandler := handlers.NewHealthHandler(logger, db)
	router.GET("/healthz", healthHandler.Healthz)
	router.GET("/readyz", healthHandler.Readyz)
	readMetrics := newReadMetricsCollector(dbPoolStatsFromPool(readDB))
	readMetrics.SetThresholds(cfg.OverloadThresholds)
	var writePathLimiter chan struct{}
	if cfg.WritePathConcurrencyLimit > 0 {
		writePathLimiter = make(chan struct{}, cfg.WritePathConcurrencyLimit)
	}

	dataModelReader := datamodel.NewHTTPClient(cfg.DataModelServiceURL, cfg.HTTPClientTimeout)
	var txManager ports.TransactionManager
	var uploadLogRepository ports.UploadLogRepository
	var uploadLogEnqueuer riverjobs.UploadLogEnqueuer = riverjobs.NoopUploadLogEnqueuer{}
	if db != nil {
		txManager = storepostgres.NewTransactionManager(db)
		uploadLogRepository = storepostgres.NewUploadLogRepository(db)
		riverClient, _ := river.NewClient(riverpgxv5.New(db), &river.Config{})
		uploadLogEnqueuer = riverjobs.NewRiverUploadLogEnqueuer(riverClient, max(1, cfg.WorkerMaxAttempts), cfg.UploadLogQueueName)
	}
	var readDataReader ports.TenantDataReader
	if readDB != nil {
		readDataReader = storepostgres.NewTenantDataReader(readDB)
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
	ingestHandler := handlers.NewIngestHandler(ingestService, handlers.IngestHandlerConfig{
		WritePathLimiter:               writePathLimiter,
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

	v1 := router.Group("/v1")
	v1.Use(authMiddleware(AuthConfig{
		Mode:  cfg.AuthMode,
		Token: cfg.AuthToken,
	}))
	v1.POST("/tenants/:tenantId/ingest/:objectType", ingestHandler.PostIngest)
	v1.PATCH("/tenants/:tenantId/ingest/:objectType", ingestHandler.PatchIngest)
	v1.POST("/tenants/:tenantId/ingest/:objectType/batch", ingestHandler.PostBatchIngest)
	v1.PATCH("/tenants/:tenantId/ingest/:objectType/batch", ingestHandler.PatchBatchIngest)
	v1.GET("/admin/read-metrics", readMetricsHandler.Get)
	readRoutes := v1.Group("")
	readRoutes.GET("/tenants/:tenantId/records/:objectType", readMetrics.middleware("list_records"), ingestHandler.ListRecords)
	readRoutes.GET("/tenants/:tenantId/records/:objectType/search", readMetrics.middleware("query_records"), ingestHandler.QueryRecords)
	readRoutes.GET("/tenants/:tenantId/records/:objectType/:objectId", readMetrics.middleware("get_record"), ingestHandler.GetRecord)
	readRoutes.POST("/tenants/:tenantId/query/aggregate", readMetrics.middleware("aggregate_records"), ingestHandler.AggregateRecords)
	v1.POST("/tenants/:tenantId/ingest/:objectType/csv", uploadLogHandler.CreateCSV)
	v1.GET("/tenants/:tenantId/ingest/:objectType/upload-logs", uploadLogHandler.List)
	v1.GET("/upload-logs/:uploadLogId", uploadLogHandler.Get)

	return router
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
