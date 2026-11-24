package httpapi

import (
	identity "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth"
	platformauth "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth"
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	blobclient "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/clients/blob"
	caseclient "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/clients/case"
	decisionclient "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/clients/decisionengine"
	inboxclient "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/clients/inbox"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/clients/provider"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/httpapi/handlers"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/privatelists"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/provideraccess"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/providerproduct"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/providerregistry"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/riverjobs"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/service"
	storepostgres "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/store/postgres"
)

type RouterConfig struct {
	PolicyObserver    func([]platformauth.RoutePolicy)
	Verifier          platformauth.TokenVerifier
	RemoteProvider    *providerproduct.Client
	PrivateListWriter *pgxpool.Pool
	PrivateListPolicy privatelists.Policy

	InvestigatorVerifier *identity.Verifier

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
	ScreeningQueueName     string
	ScreeningQueueWorkers  int
	DatasetJobQueueName    string
	DatasetJobQueueWorkers int
	MonitoredQueueName     string
	MonitoredQueueWorkers  int
}

type uuidGenerator struct{}

func (uuidGenerator) New() uuid.UUID { return uuid.New() }

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now().UTC() }

func NewRouter(logger *slog.Logger, db *pgxpool.Pool, cfg RouterConfig) *gin.Engine {
	engine := gin.New()
	router := platformauth.NewRouter(engine, cfg.Verifier)
	router.UseAuthenticated(func(c *gin.Context) {
		p := platformauth.PrincipalFromContext(c)
		if p.IsHuman() {
			if scope, err := provideraccess.FromPrincipal(*p); err == nil {
				c.Request = c.Request.WithContext(provideraccess.WithScope(c.Request.Context(), scope))
			}
		}
		if p.IsService() && p.Can("screening:intake") {
			c.Request = c.Request.WithContext(provideraccess.WithScenarioIntake(provideraccess.WithScope(c.Request.Context(), provideraccess.GlobalOperator())))
		}
		c.Next()
	})
	metrics := newServiceMetrics()
	router.Use(requestContextMiddleware(logger))
	router.Use(requestLoggingMiddleware(logger, metrics))
	router.Use(gin.Recovery())
	registerDocsRoutes(router)

	healthHandler := handlers.NewHealthHandler(logger, db)
	router.GET("/healthz", platformauth.Policy{Public: true}, healthHandler.Healthz)
	router.GET("/readyz", platformauth.Policy{Public: true}, healthHandler.Readyz)
	router.GET("/metrics", platformauth.Policy{Capability: "metrics:read"}, func(c *gin.Context) {
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
	var screeningEnqueuer riverjobs.ScreeningEnqueuer = riverjobs.NoopScreeningEnqueuer{}
	var datasetJobEnqueuer riverjobs.DatasetJobEnqueuer = riverjobs.NoopDatasetJobEnqueuer{}
	var monitoredEnqueuer riverjobs.MonitoredObjectEnqueuer = riverjobs.NoopMonitoredObjectEnqueuer{}
	manualQueueReady := false
	if db != nil {
		txManager = storepostgres.NewTransactionManager(db)
		screeningRepo = storepostgres.NewScreeningRepository(db)
		matchRepo = storepostgres.NewScreeningMatchRepository(db)
		commentRepo = storepostgres.NewScreeningCommentRepository(db)
		whitelistRepo = storepostgres.NewScreeningWhitelistRepository(db)
		fileRepo = storepostgres.NewScreeningFileRepository(db)
		continuousRepo = storepostgres.NewContinuousConfigRepository(db)
		monitoredObjRepo = storepostgres.NewMonitoredObjectRepository(db)
		// The API only inserts jobs. An empty non-nil Workers bundle makes River
		// reject every job kind during insertion; cmd/worker owns registration.
		riverClient, err := river.NewClient(riverpgxv5.New(db), &river.Config{})
		if err == nil {
			manualQueueReady = true
			screeningEnqueuer = riverjobs.NewRiverScreeningEnqueuer(riverClient, riverjobs.IntakeMaxAttempts, cfg.ScreeningQueueName)
			datasetJobEnqueuer = riverjobs.NewRiverDatasetJobEnqueuer(riverClient, 1, cfg.DatasetJobQueueName)
			monitoredEnqueuer = riverjobs.NewRiverMonitoredObjectEnqueuer(riverClient, riverjobs.IntakeMaxAttempts, cfg.MonitoredQueueName)
		}
	}

	providerClient := provider.NewHTTPClient(cfg.ScreeningProviderURL, provider.ParseProviderURLs(cfg.ScreeningProviderURLs), cfg.HTTPClientTimeout, provider.OpenSanctionsConfig{
		APIHost:   cfg.OpenSanctionsAPIHost,
		AuthMode:  cfg.OpenSanctionsAuthMode,
		APIKey:    cfg.OpenSanctionsAPIKey,
		Scope:     cfg.OpenSanctionsScope,
		Algorithm: cfg.OpenSanctionsAlgorithm,
	})
	providers := providerregistry.New(db, providerClient)
	providers.Remote = cfg.RemoteProvider
	inboxReader := inboxclient.NewHTTPClient(cfg.InboxServiceURL, cfg.HTTPClientTimeout)
	casePublisher := caseclient.NewHTTPClient(cfg.CaseServiceURL, cfg.HTTPClientTimeout)
	blobStore := blobclient.NewHTTPClient(cfg.BlobServiceURL, cfg.HTTPClientTimeout)
	decisionPublisher := decisionclient.NewHTTPClient(cfg.DecisionEngineURL, cfg.HTTPClientTimeout)
	var datasetJobRepo ports.DatasetUpdateJobRepository
	if db != nil {
		datasetJobRepo = storepostgres.NewDatasetUpdateJobRepository(db)
	}
	screeningService := service.NewScreeningService(txManager, uuidGenerator{}, systemClock{}, screeningRepo, matchRepo, commentRepo, whitelistRepo, fileRepo, continuousRepo, monitoredObjRepo, datasetJobRepo, providers, inboxReader, casePublisher, blobStore, decisionPublisher, screeningEnqueuer, datasetJobEnqueuer, monitoredEnqueuer)
	registerInvestigator(router, db, cfg.InvestigatorVerifier, screeningService)
	registerManualSearch(router, db, cfg.InvestigatorVerifier, screeningService, manualQueueReady, providerClient.ManualCapabilities())
	registerSourceMonitor(router, db, cfg.InvestigatorVerifier, screeningService, manualQueueReady)
	registerPrivateLists(router, db, cfg.PrivateListWriter, cfg.InvestigatorVerifier, cfg.PrivateListPolicy)
	screeningHandler := handlers.NewScreeningHandler(screeningService)
	whitelistHandler := handlers.NewWhitelistHandler(screeningService)
	continuousHandler := handlers.NewContinuousHandler(screeningService)
	providerHandler := handlers.NewProviderHandler(screeningService)
	datasetUpdateHandler := handlers.NewDatasetUpdateHandler(screeningService)
	internalDecisionHandler := handlers.NewInternalDecisionHandler(screeningService)

	v1 := router.Group("/v1")
	v1.GET("/service-info", platformauth.Policy{Capability: "services:read"}, func(c *gin.Context) {
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
	v1.GET("/screening-provider/catalog", platformauth.Policy{Capability: "sources:read", PrincipalType: platformauth.PrincipalUser}, providerHandler.GetCatalog)
	v1.GET("/screening-provider/freshness", platformauth.Policy{Capability: "sources:read", PrincipalType: platformauth.PrincipalUser}, providerHandler.GetFreshness)
	v1.GET("/tenants/:tenantId/dataset-update-jobs", platformauth.Policy{Capability: "sources:read", PrincipalType: platformauth.PrincipalUser}, datasetUpdateHandler.List)
	v1.POST("/tenants/:tenantId/dataset-update-jobs", platformauth.Policy{Capability: "sources:manage", PrincipalType: platformauth.PrincipalUser}, datasetUpdateHandler.Create)
	v1.GET("/tenants/:tenantId/dataset-update-jobs/:jobId", platformauth.Policy{Capability: "sources:read", PrincipalType: platformauth.PrincipalUser}, datasetUpdateHandler.Get)
	v1.POST("/tenants/:tenantId/dataset-update-jobs/:jobId/retry", platformauth.Policy{Capability: "sources:manage", PrincipalType: platformauth.PrincipalUser}, datasetUpdateHandler.Retry)
	v1.POST("/tenants/:tenantId/screenings", platformauth.Policy{Capability: "screening:write", PrincipalType: platformauth.PrincipalUser}, screeningHandler.Create)
	v1.POST("/tenants/:tenantId/screenings/freeform-search", platformauth.Policy{Capability: "screening:write", PrincipalType: platformauth.PrincipalUser}, screeningHandler.CreateFreeform)
	v1.GET("/tenants/:tenantId/decisions/:decisionId/screenings", platformauth.Policy{Capability: "screening:read", PrincipalType: platformauth.PrincipalUser}, screeningHandler.ListByDecision)
	v1.GET("/tenants/:tenantId/screenings/:screeningId", platformauth.Policy{Capability: "screening:read", PrincipalType: platformauth.PrincipalUser}, screeningHandler.Get)
	v1.POST("/tenants/:tenantId/screenings/:screeningId/retry", platformauth.Policy{Capability: "screening:write", PrincipalType: platformauth.PrincipalUser}, screeningHandler.Retry)
	v1.POST("/tenants/:tenantId/screening-matches/:matchId/review", platformauth.Policy{Capability: "screening:review", PrincipalType: platformauth.PrincipalUser}, screeningHandler.ReviewMatch)
	v1.POST("/tenants/:tenantId/screening-matches/:matchId/comments", platformauth.Policy{Capability: "screening:write", PrincipalType: platformauth.PrincipalUser}, screeningHandler.AddComment)
	v1.POST("/tenants/:tenantId/screening-matches/:matchId/enrich", platformauth.Policy{Capability: "screening:write", PrincipalType: platformauth.PrincipalUser}, screeningHandler.EnrichMatch)
	v1.POST("/tenants/:tenantId/screenings/:screeningId/files", platformauth.Policy{Capability: "screening:write", PrincipalType: platformauth.PrincipalUser}, screeningHandler.CreateFile)
	v1.POST("/tenants/:tenantId/screenings/:screeningId/file-uploads", platformauth.Policy{Capability: "screening:write", PrincipalType: platformauth.PrincipalUser}, screeningHandler.CreateFileUpload)
	v1.GET("/tenants/:tenantId/screenings/:screeningId/files", platformauth.Policy{Capability: "screening:read", PrincipalType: platformauth.PrincipalUser}, screeningHandler.ListFiles)
	v1.GET("/tenants/:tenantId/screenings/:screeningId/files/:fileId", platformauth.Policy{Capability: "screening:read", PrincipalType: platformauth.PrincipalUser}, screeningHandler.GetFile)
	v1.GET("/tenants/:tenantId/screenings/:screeningId/files/:fileId/download", platformauth.Policy{Capability: "screening:read", PrincipalType: platformauth.PrincipalUser}, screeningHandler.GetFileDownload)
	v1.GET("/tenants/:tenantId/screening-whitelist", platformauth.Policy{Capability: "screening:read", PrincipalType: platformauth.PrincipalUser}, whitelistHandler.Search)
	v1.POST("/tenants/:tenantId/screening-whitelist", platformauth.Policy{Capability: "screening:admin", PrincipalType: platformauth.PrincipalUser}, whitelistHandler.Create)
	v1.DELETE("/tenants/:tenantId/screening-whitelist", platformauth.Policy{Capability: "screening:admin", PrincipalType: platformauth.PrincipalUser}, whitelistHandler.Delete)
	v1.GET("/tenants/:tenantId/continuous-screening-configs", platformauth.Policy{Capability: "screening:read", PrincipalType: platformauth.PrincipalUser}, continuousHandler.ListConfigs)
	v1.POST("/tenants/:tenantId/continuous-screening-configs", platformauth.Policy{Capability: "screening:admin", PrincipalType: platformauth.PrincipalUser}, continuousHandler.CreateConfig)
	v1.GET("/tenants/:tenantId/continuous-screening-configs/:configId", platformauth.Policy{Capability: "screening:read", PrincipalType: platformauth.PrincipalUser}, continuousHandler.GetConfig)
	v1.PUT("/tenants/:tenantId/continuous-screening-configs/:configId", platformauth.Policy{Capability: "screening:admin", PrincipalType: platformauth.PrincipalUser}, continuousHandler.UpdateConfig)
	v1.DELETE("/tenants/:tenantId/continuous-screening-configs/:configId", platformauth.Policy{Capability: "screening:admin", PrincipalType: platformauth.PrincipalUser}, continuousHandler.DeleteConfig)
	v1.GET("/tenants/:tenantId/continuous-screening-configs/:configId/monitored-objects", platformauth.Policy{Capability: "screening:read", PrincipalType: platformauth.PrincipalUser}, continuousHandler.ListMonitoredObjects)
	v1.POST("/tenants/:tenantId/continuous-screening-configs/:configId/monitored-objects", platformauth.Policy{Capability: "screening:admin", PrincipalType: platformauth.PrincipalUser}, continuousHandler.CreateMonitoredObject)
	v1.GET("/tenants/:tenantId/monitored-objects/:monitoredObjectId", platformauth.Policy{Capability: "screening:read", PrincipalType: platformauth.PrincipalUser}, continuousHandler.GetMonitoredObject)
	v1.DELETE("/tenants/:tenantId/monitored-objects/:monitoredObjectId", platformauth.Policy{Capability: "screening:admin", PrincipalType: platformauth.PrincipalUser}, continuousHandler.DeleteMonitoredObject)
	v1.POST("/tenants/:tenantId/monitored-objects/:monitoredObjectId/requeue", platformauth.Policy{Capability: "screening:admin", PrincipalType: platformauth.PrincipalUser}, continuousHandler.RequeueMonitoredObject)

	internal := router.Group("/internal")
	internal.POST("/v1/tenants/:tenantId/decision-screenings", platformauth.Policy{Capability: "screening:intake", PrincipalType: platformauth.PrincipalService}, internalDecisionHandler.Create)
	internal.GET("/screening-capabilities", platformauth.Policy{Capability: "screening:intake", PrincipalType: platformauth.PrincipalService}, func(c *gin.Context) {
		capability := providerClient.ManualCapabilities()
		supported := capability.Configured && capability.Adapter == "opensanctions_match"
		c.JSON(200, gin.H{"provider_configured": capability.Configured, "matching_settings_supported": supported, "address_supported": supported, "birth_date_supported": supported, "nationality_supported": supported, "country_supported": supported, "registration_number_supported": supported})
	})

	router.GET("/v1/tenants/:tenantId/cases/:caseId/evidence/screenings/:screeningId", platformauth.Policy{Capability: "cases:read", PrincipalType: platformauth.PrincipalUser}, platformauth.CaseEvidence(db, "screening-evidence", "screeningId", screeningHandler.Get))

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
