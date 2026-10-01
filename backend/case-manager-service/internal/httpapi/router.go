package httpapi

import (
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
	UserVerifier        *access.Verifier
	ServiceTenantIDs    []uuid.UUID
	RequestTimeout      time.Duration
	AuthMode            string
	AuthToken           string
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
	router := gin.New()
	router.Use(requestContextMiddleware(logger))
	router.Use(gin.Recovery())
	if cfg.RequestTimeout <= 0 {
		cfg.RequestTimeout = 15 * time.Second
	}
	router.Use(requestDeadline(cfg.RequestTimeout))

	healthHandler := handlers.NewHealthHandler(logger, db)
	router.GET("/healthz", healthHandler.Healthz)
	router.GET("/readyz", healthHandler.Readyz)

	caseService := service.NewCaseService(uuidGenerator{}, systemClock{}, storepostgres.NewRepositories(db), storepostgres.NewUnitOfWork(db))
	caseHandler := handlers.NewCaseHandler(caseService)
	tagHandler := handlers.NewTagHandler(caseService)
	inboxHandler := handlers.NewInboxHandler(caseService)
	integrationHandler := handlers.NewIntegrationHandler(caseService)

	v1 := router.Group("/v1")
	v1.Use(userMiddleware(cfg.UserVerifier))
	v1.GET("/service-info", func(c *gin.Context) {
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

	v1.GET("/tenants/:tenantId/inboxes", inboxHandler.List)
	v1.POST("/tenants/:tenantId/inboxes", inboxHandler.Create)
	v1.GET("/tenants/:tenantId/inboxes/:inboxId", inboxHandler.Get)
	v1.PATCH("/tenants/:tenantId/inboxes/:inboxId", inboxHandler.Update)
	v1.GET("/tenants/:tenantId/inboxes/:inboxId/users", inboxHandler.ListUsers)
	v1.PUT("/tenants/:tenantId/inboxes/:inboxId/users/:userId", inboxHandler.PutUser)
	v1.DELETE("/tenants/:tenantId/inboxes/:inboxId/users/:userId", inboxHandler.RemoveUser)
	v1.GET("/tenants/:tenantId/case-queue", caseHandler.Queue)
	v1.GET("/tenants/:tenantId/case-analytics", caseHandler.Analytics)
	v1.GET("/tenants/:tenantId/case-session", caseHandler.Session)
	v1.POST("/tenants/:tenantId/case-bulk", caseHandler.Bulk)
	v1.GET("/tenants/:tenantId/cases/:caseId/evidence-access/:kind/:resourceId", caseHandler.EvidenceAccess)
	v1.GET("/tenants/:tenantId/cases/:caseId/overview", caseHandler.Overview)
	v1.GET("/tenants/:tenantId/cases/:caseId/reports", caseHandler.ListReports)
	v1.POST("/tenants/:tenantId/cases/:caseId/reports", caseHandler.CreateReport)
	v1.GET("/tenants/:tenantId/cases/:caseId/reports/:reportId", caseHandler.GetReport)
	v1.PATCH("/tenants/:tenantId/cases/:caseId/reports/:reportId", caseHandler.UpdateReport)
	v1.POST("/tenants/:tenantId/cases/:caseId/reports/:reportId/complete", caseHandler.CompleteReport)
	v1.GET("/tenants/:tenantId/cases/:caseId/reports/:reportId/export", caseHandler.GetReport)
	v1.GET("/tenants/:tenantId/cases/:caseId/links/:kind", caseHandler.Links)
	v1.POST("/tenants/:tenantId/cases/:caseId/close", caseHandler.Close)
	v1.GET("/tenants/:tenantId/cases", caseHandler.List)
	v1.POST("/tenants/:tenantId/cases", caseHandler.Create)
	v1.GET("/tenants/:tenantId/cases/:caseId", caseHandler.Get)
	v1.PATCH("/tenants/:tenantId/cases/:caseId", caseHandler.Update)
	v1.POST("/tenants/:tenantId/cases/:caseId/decisions", caseHandler.AddDecision)
	v1.GET("/tenants/:tenantId/cases/:caseId/events", caseHandler.ListEvents)
	v1.POST("/tenants/:tenantId/cases/:caseId/comments", caseHandler.CreateComment)
	v1.POST("/tenants/:tenantId/cases/:caseId/tags", caseHandler.AddTag)
	v1.DELETE("/tenants/:tenantId/cases/:caseId/tags/:tagId", caseHandler.RemoveTag)
	v1.POST("/tenants/:tenantId/cases/:caseId/files", caseHandler.AddFile)
	v1.POST("/tenants/:tenantId/cases/:caseId/uploads", caseHandler.StartUpload)
	v1.PUT("/tenants/:tenantId/cases/:caseId/uploads/:uploadId/content", caseHandler.UploadContent)
	v1.POST("/tenants/:tenantId/cases/:caseId/uploads/:uploadId/finalize", caseHandler.FinalizeUpload)
	v1.GET("/tenants/:tenantId/cases/:caseId/files/:fileId/download", caseHandler.DownloadEvidence)
	v1.POST("/tenants/:tenantId/cases/:caseId/assign", caseHandler.Assign)
	v1.POST("/tenants/:tenantId/cases/:caseId/unassign", caseHandler.Unassign)
	v1.POST("/tenants/:tenantId/cases/:caseId/snooze", caseHandler.Snooze)
	v1.POST("/tenants/:tenantId/cases/:caseId/unsnooze", caseHandler.Unsnooze)
	v1.POST("/tenants/:tenantId/cases/:caseId/escalate", caseHandler.Escalate)
	v1.GET("/tenants/:tenantId/tags", tagHandler.List)
	v1.POST("/tenants/:tenantId/tags", tagHandler.Create)
	v1.PATCH("/tenants/:tenantId/tags/:tagId", tagHandler.Update)
	v1.DELETE("/tenants/:tenantId/tags/:tagId", tagHandler.Archive)
	callbacks := router.Group("/v1/screening-events")
	callbacks.Use(authMiddleware(AuthConfig{Mode: cfg.AuthMode, Token: cfg.AuthToken, TenantIDs: cfg.ServiceTenantIDs}))
	callbacks.POST("/reviewed", integrationHandler.ScreeningReviewed)
	callbacks.POST("/evidence-uploaded", integrationHandler.ScreeningEvidenceUploaded)

	internal := router.Group("/internal")
	internal.Use(authMiddleware(AuthConfig{Mode: cfg.AuthMode, Token: cfg.AuthToken, TenantIDs: cfg.ServiceTenantIDs}))
	internal.GET("/v1/tenants/:tenantId/inboxes/:inboxId", inboxHandler.Get)
	internal.POST("/v1/workflow-actions", integrationHandler.WorkflowAction)
	internal.POST("/v1/ai-case-reviews/:reviewId/run", integrationHandler.NotImplemented)
	internal.POST("/v1/auto-assignment/run", integrationHandler.NotImplemented)

	return router
}
