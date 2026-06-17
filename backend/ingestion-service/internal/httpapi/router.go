package httpapi

import (
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/clients/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/httpapi/handlers"
	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/service"
	storepostgres "github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/store/postgres"
)

type RouterConfig struct {
	AuthMode            string
	AuthToken           string
	AllowedOrigins      []string
	DataModelServiceURL string
	HTTPClientTimeout   time.Duration
}

type uuidGenerator struct{}

func (uuidGenerator) New() uuid.UUID {
	return uuid.New()
}

type systemClock struct{}

func (systemClock) Now() time.Time {
	return time.Now().UTC()
}

func NewRouter(logger *slog.Logger, db *pgxpool.Pool, cfg RouterConfig) *gin.Engine {
	router := gin.New()
	router.Use(corsMiddleware(cfg.AllowedOrigins))
	router.Use(requestContextMiddleware(logger))
	router.Use(gin.Recovery())
	registerDocsRoutes(router)

	healthHandler := handlers.NewHealthHandler(logger, db)
	router.GET("/healthz", healthHandler.Healthz)
	router.GET("/readyz", healthHandler.Readyz)

	dataModelReader := datamodel.NewHTTPClient(cfg.DataModelServiceURL, cfg.HTTPClientTimeout)
	var txManager ports.TransactionManager
	var uploadLogRepository ports.UploadLogRepository
	if db != nil {
		txManager = storepostgres.NewTransactionManager(db)
		uploadLogRepository = storepostgres.NewUploadLogRepository(db)
	}
	modelContractService := service.NewModelContractService(dataModelReader)
	_ = modelContractService
	ingestService := service.NewIngestService(
		dataModelReader,
		txManager,
		uuidGenerator{},
		systemClock{},
	)
	ingestHandler := handlers.NewIngestHandler(ingestService)
	uploadLogService := service.NewUploadLogService(
		uploadLogRepository,
		ingestService,
		uuidGenerator{},
		systemClock{},
		3,
	)
	uploadLogHandler := handlers.NewUploadLogHandler(uploadLogService)

	v1 := router.Group("/v1")
	v1.Use(authMiddleware(AuthConfig{
		Mode:  cfg.AuthMode,
		Token: cfg.AuthToken,
	}))
	v1.POST("/tenants/:tenantId/ingest/:objectType", ingestHandler.PostIngest)
	v1.PATCH("/tenants/:tenantId/ingest/:objectType", ingestHandler.PatchIngest)
	v1.POST("/tenants/:tenantId/ingest/:objectType/batch", ingestHandler.PostBatchIngest)
	v1.PATCH("/tenants/:tenantId/ingest/:objectType/batch", ingestHandler.PatchBatchIngest)
	v1.GET("/tenants/:tenantId/records/:objectType", ingestHandler.ListRecords)
	v1.GET("/tenants/:tenantId/records/:objectType/search", ingestHandler.QueryRecords)
	v1.GET("/tenants/:tenantId/records/:objectType/:objectId", ingestHandler.GetRecord)
	v1.POST("/tenants/:tenantId/query/aggregate", ingestHandler.AggregateRecords)
	v1.POST("/tenants/:tenantId/ingest/:objectType/csv", uploadLogHandler.CreateCSV)
	v1.GET("/tenants/:tenantId/ingest/:objectType/upload-logs", uploadLogHandler.List)
	v1.GET("/upload-logs/:uploadLogId", uploadLogHandler.Get)

	return router
}
