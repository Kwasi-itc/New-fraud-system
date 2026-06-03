package httpapi

import (
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	blobclient "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/clients/blob"
	caseclient "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/clients/case"
	decisionclient "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/clients/decisionengine"
	inboxclient "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/clients/inbox"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/clients/provider"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/httpapi/handlers"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/service"
	storepostgres "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/store/postgres"
)

type RouterConfig struct {
	AuthMode               string
	AuthToken              string
	ScreeningProviderURL   string
	ScreeningProviderURLs  string
	OpenSanctionsAPIHost   string
	OpenSanctionsAuthMode  string
	OpenSanctionsAPIKey    string
	OpenSanctionsScope     string
	OpenSanctionsAlgorithm string
	IngestionServiceURL    string
	InboxServiceURL        string
	CaseServiceURL         string
	BlobServiceURL         string
	DecisionEngineURL      string
	HTTPClientTimeout      time.Duration
}

type uuidGenerator struct{}

func (uuidGenerator) New() uuid.UUID { return uuid.New() }

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now().UTC() }

func NewRouter(logger *slog.Logger, db *pgxpool.Pool, cfg RouterConfig) *gin.Engine {
	router := gin.New()
	metrics := newServiceMetrics()
	router.Use(requestContextMiddleware(logger))
	router.Use(requestLoggingMiddleware(logger, metrics))
	router.Use(gin.Recovery())

	healthHandler := handlers.NewHealthHandler(logger, db)
	router.GET("/healthz", healthHandler.Healthz)
	router.GET("/readyz", healthHandler.Readyz)
	router.GET("/metrics", func(c *gin.Context) {
		c.JSON(200, metrics.Snapshot())
	})

	var txManager ports.TransactionManager
	var screeningRepo ports.ScreeningRepository
	var matchRepo ports.ScreeningMatchRepository
	var commentRepo ports.ScreeningCommentRepository
	var whitelistRepo ports.ScreeningWhitelistRepository
	var fileRepo ports.ScreeningFileRepository
	var continuousRepo ports.ContinuousConfigRepository
	var monitoredObjRepo ports.MonitoredObjectRepository
	if db != nil {
		txManager = storepostgres.NewTransactionManager(db)
		screeningRepo = storepostgres.NewScreeningRepository(db)
		matchRepo = storepostgres.NewScreeningMatchRepository(db)
		commentRepo = storepostgres.NewScreeningCommentRepository(db)
		whitelistRepo = storepostgres.NewScreeningWhitelistRepository(db)
		fileRepo = storepostgres.NewScreeningFileRepository(db)
		continuousRepo = storepostgres.NewContinuousConfigRepository(db)
		monitoredObjRepo = storepostgres.NewMonitoredObjectRepository(db)
	}

	providerClient := provider.NewHTTPClient(cfg.ScreeningProviderURL, provider.ParseProviderURLs(cfg.ScreeningProviderURLs), cfg.HTTPClientTimeout, provider.OpenSanctionsConfig{
		APIHost:   cfg.OpenSanctionsAPIHost,
		AuthMode:  cfg.OpenSanctionsAuthMode,
		APIKey:    cfg.OpenSanctionsAPIKey,
		Scope:     cfg.OpenSanctionsScope,
		Algorithm: cfg.OpenSanctionsAlgorithm,
	})
	inboxReader := inboxclient.NewHTTPClient(cfg.InboxServiceURL, cfg.HTTPClientTimeout)
	casePublisher := caseclient.NewHTTPClient(cfg.CaseServiceURL, cfg.HTTPClientTimeout)
	blobStore := blobclient.NewHTTPClient(cfg.BlobServiceURL, cfg.HTTPClientTimeout)
	decisionPublisher := decisionclient.NewHTTPClient(cfg.DecisionEngineURL, cfg.AuthMode, cfg.AuthToken, cfg.HTTPClientTimeout)
	var datasetJobRepo ports.DatasetUpdateJobRepository
	if db != nil {
		datasetJobRepo = storepostgres.NewDatasetUpdateJobRepository(db)
	}
	screeningService := service.NewScreeningService(txManager, uuidGenerator{}, systemClock{}, screeningRepo, matchRepo, commentRepo, whitelistRepo, fileRepo, continuousRepo, monitoredObjRepo, datasetJobRepo, providerClient, inboxReader, casePublisher, blobStore, decisionPublisher)
	screeningHandler := handlers.NewScreeningHandler(screeningService)
	whitelistHandler := handlers.NewWhitelistHandler(screeningService)
	continuousHandler := handlers.NewContinuousHandler(screeningService)
	providerHandler := handlers.NewProviderHandler(screeningService)
	datasetUpdateHandler := handlers.NewDatasetUpdateHandler(screeningService)
	internalDecisionHandler := handlers.NewInternalDecisionHandler(screeningService)

	v1 := router.Group("/v1")
	v1.Use(authMiddleware(AuthConfig{
		Mode:  cfg.AuthMode,
		Token: cfg.AuthToken,
	}))
	v1.GET("/service-info", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"service":                 "screening-service",
			"screening_provider_url":  cfg.ScreeningProviderURL,
			"screening_provider_urls": cfg.ScreeningProviderURLs,
			"opensanctions_api_host":  cfg.OpenSanctionsAPIHost,
			"opensanctions_auth_mode": cfg.OpenSanctionsAuthMode,
			"opensanctions_scope":     cfg.OpenSanctionsScope,
			"opensanctions_algorithm": cfg.OpenSanctionsAlgorithm,
			"ingestion_service_url":   cfg.IngestionServiceURL,
			"inbox_service_url":       cfg.InboxServiceURL,
			"case_service_url":        cfg.CaseServiceURL,
			"blob_service_url":        cfg.BlobServiceURL,
			"decision_engine_url":     cfg.DecisionEngineURL,
		})
	})
	v1.GET("/screening-provider/catalog", providerHandler.GetCatalog)
	v1.GET("/screening-provider/freshness", providerHandler.GetFreshness)
	v1.GET("/tenants/:tenantId/dataset-update-jobs", datasetUpdateHandler.List)
	v1.POST("/tenants/:tenantId/dataset-update-jobs", datasetUpdateHandler.Create)
	v1.GET("/tenants/:tenantId/dataset-update-jobs/:jobId", datasetUpdateHandler.Get)
	v1.POST("/tenants/:tenantId/dataset-update-jobs/:jobId/retry", datasetUpdateHandler.Retry)
	v1.POST("/tenants/:tenantId/screenings", screeningHandler.Create)
	v1.POST("/tenants/:tenantId/screenings/freeform-search", screeningHandler.CreateFreeform)
	v1.GET("/tenants/:tenantId/decisions/:decisionId/screenings", screeningHandler.ListByDecision)
	v1.GET("/tenants/:tenantId/screenings/:screeningId", screeningHandler.Get)
	v1.POST("/tenants/:tenantId/screenings/:screeningId/retry", screeningHandler.Retry)
	v1.POST("/tenants/:tenantId/screening-matches/:matchId/review", screeningHandler.ReviewMatch)
	v1.POST("/tenants/:tenantId/screening-matches/:matchId/comments", screeningHandler.AddComment)
	v1.POST("/tenants/:tenantId/screening-matches/:matchId/enrich", screeningHandler.EnrichMatch)
	v1.POST("/tenants/:tenantId/screenings/:screeningId/files", screeningHandler.CreateFile)
	v1.POST("/tenants/:tenantId/screenings/:screeningId/file-uploads", screeningHandler.CreateFileUpload)
	v1.GET("/tenants/:tenantId/screenings/:screeningId/files", screeningHandler.ListFiles)
	v1.GET("/tenants/:tenantId/screenings/:screeningId/files/:fileId", screeningHandler.GetFile)
	v1.GET("/tenants/:tenantId/screenings/:screeningId/files/:fileId/download", screeningHandler.GetFileDownload)
	v1.GET("/tenants/:tenantId/screening-whitelist", whitelistHandler.Search)
	v1.POST("/tenants/:tenantId/screening-whitelist", whitelistHandler.Create)
	v1.DELETE("/tenants/:tenantId/screening-whitelist", whitelistHandler.Delete)
	v1.GET("/tenants/:tenantId/continuous-screening-configs", continuousHandler.ListConfigs)
	v1.POST("/tenants/:tenantId/continuous-screening-configs", continuousHandler.CreateConfig)
	v1.GET("/tenants/:tenantId/continuous-screening-configs/:configId", continuousHandler.GetConfig)
	v1.PUT("/tenants/:tenantId/continuous-screening-configs/:configId", continuousHandler.UpdateConfig)
	v1.DELETE("/tenants/:tenantId/continuous-screening-configs/:configId", continuousHandler.DeleteConfig)
	v1.GET("/tenants/:tenantId/continuous-screening-configs/:configId/monitored-objects", continuousHandler.ListMonitoredObjects)
	v1.POST("/tenants/:tenantId/continuous-screening-configs/:configId/monitored-objects", continuousHandler.CreateMonitoredObject)
	v1.GET("/tenants/:tenantId/monitored-objects/:monitoredObjectId", continuousHandler.GetMonitoredObject)
	v1.DELETE("/tenants/:tenantId/monitored-objects/:monitoredObjectId", continuousHandler.DeleteMonitoredObject)
	v1.POST("/tenants/:tenantId/monitored-objects/:monitoredObjectId/requeue", continuousHandler.RequeueMonitoredObject)

	internal := router.Group("/internal")
	internal.Use(authMiddleware(AuthConfig{
		Mode:  cfg.AuthMode,
		Token: cfg.AuthToken,
	}))
	internal.POST("/v1/tenants/:tenantId/decision-screenings", internalDecisionHandler.Create)

	return router
}
