package httpapi

import (
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/httpapi/handlers"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/service"
	storepostgres "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/store/postgres"
)

type RouterConfig struct {
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

	healthHandler := handlers.NewHealthHandler(logger, db)
	router.GET("/healthz", healthHandler.Healthz)
	router.GET("/readyz", healthHandler.Readyz)

	inboxRepo := storepostgres.NewInboxRepository(db)
	caseRepo := storepostgres.NewCaseRepository(db)
	decisionRepo := storepostgres.NewDecisionLinkRepository(db)
	screeningRepo := storepostgres.NewScreeningLinkRepository(db)
	tagRepo := storepostgres.NewTagRepository(db)
	eventRepo := storepostgres.NewEventRepository(db)
	fileRepo := storepostgres.NewFileRepository(db)
	caseService := service.NewCaseService(uuidGenerator{}, systemClock{}, inboxRepo, caseRepo, decisionRepo, screeningRepo, tagRepo, eventRepo, fileRepo)
	caseHandler := handlers.NewCaseHandler(caseService)
	tagHandler := handlers.NewTagHandler(caseService)
	inboxHandler := handlers.NewInboxHandler(caseService)
	integrationHandler := handlers.NewIntegrationHandler(caseService)

	v1 := router.Group("/v1")
	v1.Use(authMiddleware(AuthConfig{Mode: cfg.AuthMode, Token: cfg.AuthToken}))
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
	v1.POST("/tenants/:tenantId/cases/:caseId/assign", caseHandler.Assign)
	v1.POST("/tenants/:tenantId/cases/:caseId/unassign", caseHandler.Unassign)
	v1.POST("/tenants/:tenantId/cases/:caseId/snooze", caseHandler.Snooze)
	v1.POST("/tenants/:tenantId/cases/:caseId/unsnooze", caseHandler.Unsnooze)
	v1.POST("/tenants/:tenantId/cases/:caseId/escalate", caseHandler.Escalate)
	v1.GET("/tenants/:tenantId/tags", tagHandler.List)
	v1.POST("/tenants/:tenantId/tags", tagHandler.Create)
	v1.POST("/screening-events/reviewed", integrationHandler.ScreeningReviewed)
	v1.POST("/screening-events/evidence-uploaded", integrationHandler.ScreeningEvidenceUploaded)

	internal := router.Group("/internal")
	internal.Use(authMiddleware(AuthConfig{Mode: cfg.AuthMode, Token: cfg.AuthToken}))
	internal.POST("/v1/workflow-actions", integrationHandler.WorkflowAction)
	internal.POST("/v1/ai-case-reviews/:reviewId/run", integrationHandler.NotImplemented)
	internal.POST("/v1/auto-assignment/run", integrationHandler.NotImplemented)

	return router
}
