package httpapi

import (
	platformauth "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth"
	"log/slog"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/access"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/httpapi/handlers"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/service"
	storepostgres "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/store/postgres"
)

type RouterConfig struct {
	PolicyObserver func([]platformauth.RoutePolicy)
	Verifier       platformauth.TokenVerifier
	UserVerifier   *access.Verifier

	RequestTimeout time.Duration

	DecisionEngineURL   string
	ScreeningServiceURL string
	IngestionServiceURL string
	DataModelServiceURL string
	BlobServiceURL      string
	OutboxPublisherURL  string
	HTTPClientTimeout   time.Duration
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
		c.Request = c.Request.WithContext(access.WithPrincipal(c.Request.Context(), access.FromPlatform(*p)))
		c.Next()
	})
	router.Use(requestContextMiddleware(logger))
	router.Use(recoverRequest())
	if cfg.RequestTimeout <= 0 {
		cfg.RequestTimeout = 15 * time.Second
	}
	router.Use(requestDeadline(cfg.RequestTimeout))

	healthHandler := handlers.NewHealthHandler(logger, db)
	router.GET("/healthz", platformauth.Policy{Public: true}, healthHandler.Healthz)
	router.GET("/readyz", platformauth.Policy{Public: true}, healthHandler.Readyz)

	caseService := service.NewCaseService(uuidGenerator{}, systemClock{}, storepostgres.NewRepositories(db), storepostgres.NewUnitOfWork(db))
	caseHandler := handlers.NewCaseHandler(caseService)
	tagHandler := handlers.NewTagHandler(caseService)
	inboxHandler := handlers.NewInboxHandler(caseService)
	integrationHandler := handlers.NewIntegrationHandler(caseService)

	v1 := router.Group("/v1")
	v1.GET("/service-info", platformauth.Policy{Capability: "services:read"}, func(c *gin.Context) {
		c.JSON(200, gin.H{
			"service":                "case-manager-service",
			"decision_engine_url":    cfg.DecisionEngineURL,
			"screening_service_url":  cfg.ScreeningServiceURL,
			"ingestion_service_url":  cfg.IngestionServiceURL,
			"data_model_service_url": cfg.DataModelServiceURL,
			"blob_service_url":       cfg.BlobServiceURL,
			"outbox_publisher_url":   cfg.OutboxPublisherURL,
		})
	})

	v1.GET("/tenants/:tenantId/inboxes", platformauth.Policy{Capability: "inboxes:read", PrincipalType: platformauth.PrincipalUser}, inboxHandler.List)
	v1.POST("/tenants/:tenantId/inboxes", platformauth.Policy{Capability: "inboxes:manage", PrincipalType: platformauth.PrincipalUser}, inboxHandler.Create)
	v1.GET("/tenants/:tenantId/inboxes/:inboxId", platformauth.Policy{Capability: "inboxes:read", PrincipalType: platformauth.PrincipalUser}, inboxHandler.Get)
	v1.PATCH("/tenants/:tenantId/inboxes/:inboxId", platformauth.Policy{Capability: "inboxes:manage", PrincipalType: platformauth.PrincipalUser}, inboxHandler.Update)
	v1.GET("/tenants/:tenantId/inboxes/:inboxId/users", platformauth.Policy{Capability: "inboxes:manage", PrincipalType: platformauth.PrincipalUser}, inboxHandler.ListUsers)
	v1.PUT("/tenants/:tenantId/inboxes/:inboxId/users/:userId", platformauth.Policy{Capability: "inboxes:manage", PrincipalType: platformauth.PrincipalUser}, inboxHandler.PutUser)
	v1.DELETE("/tenants/:tenantId/inboxes/:inboxId/users/:userId", platformauth.Policy{Capability: "inboxes:manage", PrincipalType: platformauth.PrincipalUser}, inboxHandler.RemoveUser)
	v1.GET("/tenants/:tenantId/case-queue", platformauth.Policy{Capability: "cases:read", PrincipalType: platformauth.PrincipalUser}, caseHandler.Queue)
	v1.GET("/tenants/:tenantId/case-analytics", platformauth.Policy{Capability: "cases:read", PrincipalType: platformauth.PrincipalUser}, caseHandler.Analytics)
	v1.GET("/tenants/:tenantId/case-session", platformauth.Policy{Capability: "cases:read", PrincipalType: platformauth.PrincipalUser}, caseHandler.Session)
	v1.POST("/tenants/:tenantId/decision-case-memberships", platformauth.Policy{Capability: "cases:read", PrincipalType: platformauth.PrincipalUser}, caseHandler.DecisionSelections)
	v1.POST("/tenants/:tenantId/manual-screening-investigations", platformauth.Policy{Capability: "cases:write", PrincipalType: platformauth.PrincipalUser}, caseHandler.InvestigateManualScreening)
	v1.POST("/tenants/:tenantId/case-bulk", platformauth.Policy{Capability: "cases:write", PrincipalType: platformauth.PrincipalUser}, caseHandler.Bulk)
	v1.GET("/tenants/:tenantId/cases/:caseId/evidence-access/:kind/:resourceId", platformauth.Policy{Capability: "cases:read", PrincipalType: platformauth.PrincipalUser}, caseHandler.EvidenceAccess)
	v1.GET("/tenants/:tenantId/cases/:caseId/overview", platformauth.Policy{Capability: "cases:read", PrincipalType: platformauth.PrincipalUser}, caseHandler.Overview)
	v1.POST("/tenants/:tenantId/cases/:caseId/explore", platformauth.Policy{Capability: "cases:read", PrincipalType: platformauth.PrincipalUser}, caseHandler.Explore)
	v1.POST("/tenants/:tenantId/cases/:caseId/explore/attach", platformauth.Policy{Capability: "cases:write", PrincipalType: platformauth.PrincipalUser}, caseHandler.AttachExploredDecision)
	v1.GET("/tenants/:tenantId/cases/:caseId/reports", platformauth.Policy{Capability: "reports:read", PrincipalType: platformauth.PrincipalUser}, caseHandler.ListReports)
	v1.POST("/tenants/:tenantId/cases/:caseId/reports", platformauth.Policy{Capability: "reports:write", PrincipalType: platformauth.PrincipalUser}, caseHandler.CreateReport)
	v1.GET("/tenants/:tenantId/cases/:caseId/reports/:reportId", platformauth.Policy{Capability: "reports:read", PrincipalType: platformauth.PrincipalUser}, caseHandler.GetReport)
	v1.PATCH("/tenants/:tenantId/cases/:caseId/reports/:reportId", platformauth.Policy{Capability: "reports:write", PrincipalType: platformauth.PrincipalUser}, caseHandler.UpdateReport)
	v1.POST("/tenants/:tenantId/cases/:caseId/reports/:reportId/complete", platformauth.Policy{Capability: "reports:write", PrincipalType: platformauth.PrincipalUser}, caseHandler.CompleteReport)
	v1.GET("/tenants/:tenantId/cases/:caseId/reports/:reportId/export", platformauth.Policy{Capability: "reports:export", PrincipalType: platformauth.PrincipalUser}, caseHandler.GetReport)
	v1.GET("/tenants/:tenantId/cases/:caseId/links/:kind", platformauth.Policy{Capability: "cases:read", PrincipalType: platformauth.PrincipalUser}, caseHandler.Links)
	v1.POST("/tenants/:tenantId/cases/:caseId/close", platformauth.Policy{Capability: "cases:write", PrincipalType: platformauth.PrincipalUser}, caseHandler.Close)
	v1.GET("/tenants/:tenantId/cases", platformauth.Policy{Capability: "cases:read"}, clientCaseRead(db, caseHandler.List))
	v1.POST("/tenants/:tenantId/cases", platformauth.Policy{Capability: "cases:write", PrincipalType: platformauth.PrincipalUser}, caseHandler.Create)
	v1.GET("/tenants/:tenantId/cases/:caseId", platformauth.Policy{Capability: "cases:read"}, clientCaseRead(db, caseHandler.Get))
	v1.PATCH("/tenants/:tenantId/cases/:caseId", platformauth.Policy{Capability: "cases:write", PrincipalType: platformauth.PrincipalUser}, caseHandler.Update)
	v1.POST("/tenants/:tenantId/cases/:caseId/decisions", platformauth.Policy{Capability: "cases:write", PrincipalType: platformauth.PrincipalUser}, caseHandler.AddDecision)
	v1.POST("/tenants/:tenantId/cases/:caseId/decision-transfers", platformauth.Policy{Capability: "cases:write", PrincipalType: platformauth.PrincipalUser}, caseHandler.TransferDecision)
	v1.GET("/tenants/:tenantId/cases/:caseId/events", platformauth.Policy{Capability: "cases:read", PrincipalType: platformauth.PrincipalUser}, caseHandler.ListEvents)
	v1.POST("/tenants/:tenantId/cases/:caseId/comments", platformauth.Policy{Capability: "cases:write", PrincipalType: platformauth.PrincipalUser}, caseHandler.CreateComment)
	v1.POST("/tenants/:tenantId/cases/:caseId/tags", platformauth.Policy{Capability: "cases:write", PrincipalType: platformauth.PrincipalUser}, caseHandler.AddTag)
	v1.DELETE("/tenants/:tenantId/cases/:caseId/tags/:tagId", platformauth.Policy{Capability: "cases:write", PrincipalType: platformauth.PrincipalUser}, caseHandler.RemoveTag)
	v1.POST("/tenants/:tenantId/cases/:caseId/files", platformauth.Policy{Capability: "cases:write", PrincipalType: platformauth.PrincipalUser}, caseHandler.AddFile)
	v1.POST("/tenants/:tenantId/cases/:caseId/uploads", platformauth.Policy{Capability: "cases:write", PrincipalType: platformauth.PrincipalUser}, caseHandler.StartUpload)
	v1.PUT("/tenants/:tenantId/cases/:caseId/uploads/:uploadId/content", platformauth.Policy{Capability: "cases:write", PrincipalType: platformauth.PrincipalUser}, caseHandler.UploadContent)
	v1.POST("/tenants/:tenantId/cases/:caseId/uploads/:uploadId/finalize", platformauth.Policy{Capability: "cases:write", PrincipalType: platformauth.PrincipalUser}, caseHandler.FinalizeUpload)
	v1.GET("/tenants/:tenantId/cases/:caseId/files/:fileId/download", platformauth.Policy{Capability: "cases:read", PrincipalType: platformauth.PrincipalUser}, caseHandler.DownloadEvidence)
	v1.POST("/tenants/:tenantId/cases/:caseId/assign", platformauth.Policy{Capability: "cases:assign", PrincipalType: platformauth.PrincipalUser}, caseHandler.Assign)
	v1.POST("/tenants/:tenantId/cases/:caseId/unassign", platformauth.Policy{Capability: "cases:assign", PrincipalType: platformauth.PrincipalUser}, caseHandler.Unassign)
	v1.POST("/tenants/:tenantId/cases/:caseId/snooze", platformauth.Policy{Capability: "cases:write", PrincipalType: platformauth.PrincipalUser}, caseHandler.Snooze)
	v1.POST("/tenants/:tenantId/cases/:caseId/unsnooze", platformauth.Policy{Capability: "cases:write", PrincipalType: platformauth.PrincipalUser}, caseHandler.Unsnooze)
	v1.POST("/tenants/:tenantId/cases/:caseId/escalate", platformauth.Policy{Capability: "cases:write", PrincipalType: platformauth.PrincipalUser}, caseHandler.Escalate)
	v1.GET("/tenants/:tenantId/tags", platformauth.Policy{Capability: "cases:read", PrincipalType: platformauth.PrincipalUser}, tagHandler.List)
	v1.POST("/tenants/:tenantId/tags", platformauth.Policy{Capability: "inboxes:manage", PrincipalType: platformauth.PrincipalUser}, tagHandler.Create)
	v1.PATCH("/tenants/:tenantId/tags/:tagId", platformauth.Policy{Capability: "inboxes:manage", PrincipalType: platformauth.PrincipalUser}, tagHandler.Update)
	v1.DELETE("/tenants/:tenantId/tags/:tagId", platformauth.Policy{Capability: "inboxes:manage", PrincipalType: platformauth.PrincipalUser}, tagHandler.Archive)
	callbacks := router.Group("/v1/screening-events")
	callbacks.POST("/reviewed", platformauth.Policy{Capability: "cases:events-write", PrincipalType: platformauth.PrincipalService, BodyTenant: true}, integrationHandler.ScreeningReviewed)
	callbacks.POST("/requires-review", platformauth.Policy{Capability: "cases:events-write", PrincipalType: platformauth.PrincipalService, BodyTenant: true}, integrationHandler.ScreeningRequiresReview)
	callbacks.POST("/evidence-uploaded", platformauth.Policy{Capability: "cases:events-write", PrincipalType: platformauth.PrincipalService, BodyTenant: true}, integrationHandler.ScreeningEvidenceUploaded)

	internal := router.Group("/internal")
	internal.GET("/v1/tenants/:tenantId/inboxes/:inboxId", platformauth.Policy{Capability: "inboxes:read", PrincipalType: platformauth.PrincipalService}, inboxHandler.Get)
	internal.POST("/v1/workflow-actions", platformauth.Policy{Capability: "workflows:execute", PrincipalType: platformauth.PrincipalService, BodyTenant: true}, integrationHandler.WorkflowAction)
	internal.POST("/v1/ai-case-reviews/:reviewId/run", platformauth.Policy{Capability: "workflows:execute", PrincipalType: platformauth.PrincipalService, BodyTenant: true}, integrationHandler.NotImplemented)
	internal.POST("/v1/auto-assignment/run", platformauth.Policy{Capability: "workflows:execute", PrincipalType: platformauth.PrincipalService, BodyTenant: true}, integrationHandler.NotImplemented)

	registerClientInboxAccess(router, db)
	if err := router.ValidateCoverage(); err != nil {
		panic(err)
	}
	if cfg.PolicyObserver != nil {
		cfg.PolicyObserver(router.Inventory())
	}
	return engine
}
