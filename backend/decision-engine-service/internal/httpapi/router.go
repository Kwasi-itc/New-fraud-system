package httpapi

import (
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/clients/datamodel"
	ingestionclient "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/clients/ingestion"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/httpapi/handlers"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/service"
	storepostgres "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/postgres"
)

type RouterConfig struct {
	AuthMode                    string
	AuthToken                   string
	AllowedOrigins              []string
	DataModelServiceURL         string
	IngestionServiceURL         string
	HTTPClientTimeout           time.Duration
	AggregatePushdownMode       string
	AggregatePushdownAggregates []string
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
	router.Use(requestContextMiddleware(logger))
	router.Use(corsMiddleware(cfg.AllowedOrigins))
	router.Use(gin.Recovery())
	registerDocsRoutes(router)

	healthHandler := handlers.NewHealthHandler(logger, db)
	router.GET("/healthz", healthHandler.Healthz)
	router.GET("/readyz", healthHandler.Readyz)

	var txManager ports.TransactionManager
	var scenarioRepo ports.ScenarioRepository
	var iterationRepo ports.ScenarioIterationRepository
	var publicationRepo ports.ScenarioPublicationRepository
	var ruleRepo ports.RuleRepository
	var dataModelReader ports.DataModelReader
	var tenantDataReader ports.TenantDataReader
	var decisionRepo ports.DecisionRepository
	var ruleExecutionRepo ports.RuleExecutionRepository
	var testRunRepo ports.TestRunRepository
	var phantomDecisionRepo ports.PhantomDecisionRepository
	var phantomRuleExecRepo ports.PhantomRuleExecutionRepository
	var workflowRepo ports.WorkflowRepository
	var workflowRuleRepo ports.WorkflowRuleRepository
	var workflowConditionRepo ports.WorkflowConditionRepository
	var workflowActionRepo ports.WorkflowActionRepository
	var workflowExecutionRepo ports.WorkflowExecutionRepository
	var ruleSnoozeRepo ports.RuleSnoozeRepository
	var outboxRepo ports.OutboxEventRepository
	var scheduledExecutionRepo ports.ScheduledExecutionRepository
	var asyncDecisionExecutionRepo ports.AsyncDecisionExecutionRepository
	var screeningConfigRepo ports.ScreeningConfigRepository
	var screeningExecutionRepo ports.ScreeningExecutionRepository
	var scoringConfigRepo ports.ScoringConfigRepository
	var scoringRequestRepo ports.ScoringRequestRepository
	var customListRepo ports.CustomListRepository
	var recordTagRepo ports.RecordTagRepository
	var riskRepo ports.RiskSnapshotRepository
	var ipFlagRepo ports.IPFlagRepository
	if db != nil {
		txManager = storepostgres.NewTransactionManager(db)
		scenarioRepo = storepostgres.NewScenarioRepository(db)
		iterationRepo = storepostgres.NewScenarioIterationRepository(db)
		publicationRepo = storepostgres.NewScenarioPublicationRepository(db)
		ruleRepo = storepostgres.NewRuleRepository(db)
		decisionRepo = storepostgres.NewDecisionRepository(db)
		ruleExecutionRepo = storepostgres.NewRuleExecutionRepository(db)
		testRunRepo = storepostgres.NewTestRunRepository(db)
		phantomDecisionRepo = storepostgres.NewPhantomDecisionRepository(db)
		phantomRuleExecRepo = storepostgres.NewPhantomRuleExecutionRepository(db)
		workflowRepo = storepostgres.NewWorkflowRepository(db)
		workflowRuleRepo = storepostgres.NewWorkflowRuleRepository(db)
		workflowConditionRepo = storepostgres.NewWorkflowConditionRepository(db)
		workflowActionRepo = storepostgres.NewWorkflowActionRepository(db)
		workflowExecutionRepo = storepostgres.NewWorkflowExecutionRepository(db)
		ruleSnoozeRepo = storepostgres.NewRuleSnoozeRepository(db)
		outboxRepo = storepostgres.NewOutboxEventRepository(db)
		scheduledExecutionRepo = storepostgres.NewScheduledExecutionRepository(db)
		asyncDecisionExecutionRepo = storepostgres.NewAsyncDecisionExecutionRepository(db)
		screeningConfigRepo = storepostgres.NewScreeningConfigRepository(db)
		screeningExecutionRepo = storepostgres.NewScreeningExecutionRepository(db)
		scoringConfigRepo = storepostgres.NewScoringConfigRepository(db)
		scoringRequestRepo = storepostgres.NewScoringRequestRepository(db)
		customListRepo = storepostgres.NewCustomListRepository(db)
		recordTagRepo = storepostgres.NewRecordTagRepository(db)
		riskRepo = storepostgres.NewRiskSnapshotRepository(db)
		ipFlagRepo = storepostgres.NewIPFlagRepository(db)
	}
	dataModelReader = datamodel.NewHTTPClient(cfg.DataModelServiceURL, cfg.HTTPClientTimeout)
	tenantDataReader = ingestionclient.NewHTTPClient(cfg.IngestionServiceURL, cfg.HTTPClientTimeout)

	scenarioService := service.NewScenarioService(txManager, uuidGenerator{}, systemClock{}, dataModelReader, scenarioRepo, iterationRepo, ruleRepo, workflowRuleRepo, workflowConditionRepo, workflowActionRepo)
	accessorService := service.NewAccessorService(scenarioRepo, dataModelReader)
	validationService := service.NewValidationService(dataModelReader, scenarioRepo, iterationRepo, ruleRepo)
	iterationService := service.NewIterationService(txManager, uuidGenerator{}, systemClock{}, iterationRepo, ruleRepo, validationService)
	publicationService := service.NewPublicationService(txManager, uuidGenerator{}, systemClock{}, publicationRepo, scenarioRepo, iterationRepo, ruleRepo, dataModelReader)
	ruleService := service.NewRuleService(txManager, uuidGenerator{}, systemClock{}, ruleRepo, iterationRepo)
	decisionService := service.NewDecisionService(txManager, uuidGenerator{}, systemClock{}, dataModelReader, scenarioRepo, iterationRepo, ruleRepo, tenantDataReader, decisionRepo, ruleExecutionRepo, workflowRepo, workflowRuleRepo, workflowConditionRepo, workflowActionRepo, workflowExecutionRepo, ruleSnoozeRepo, outboxRepo, customListRepo, recordTagRepo, riskRepo, ipFlagRepo, screeningConfigRepo, screeningExecutionRepo, scoringConfigRepo, scoringRequestRepo, cfg.AggregatePushdownMode, cfg.AggregatePushdownAggregates)
	testRunService := service.NewTestRunService(txManager, uuidGenerator{}, systemClock{}, scenarioRepo, iterationRepo, ruleRepo, dataModelReader, tenantDataReader, decisionRepo, testRunRepo, phantomDecisionRepo, phantomRuleExecRepo, customListRepo, recordTagRepo, riskRepo, ipFlagRepo, cfg.AggregatePushdownMode, cfg.AggregatePushdownAggregates)
	workflowService := service.NewWorkflowService(txManager, uuidGenerator{}, systemClock{}, scenarioRepo, workflowRepo, workflowExecutionRepo)
	workflowRuleService := service.NewWorkflowRuleService(txManager, uuidGenerator{}, systemClock{}, dataModelReader, scenarioRepo, workflowRuleRepo, workflowConditionRepo, workflowActionRepo)
	snoozeService := service.NewSnoozeService(txManager, uuidGenerator{}, systemClock{}, scenarioRepo, ruleSnoozeRepo)
	outboxService := service.NewOutboxService(outboxRepo)
	executionService := service.NewExecutionService(txManager, uuidGenerator{}, systemClock{}, scenarioRepo, iterationRepo, tenantDataReader, scheduledExecutionRepo, asyncDecisionExecutionRepo, decisionService)
	screeningService := service.NewScreeningService(txManager, uuidGenerator{}, systemClock{}, scenarioRepo, screeningConfigRepo, screeningExecutionRepo)
	scoringService := service.NewScoringService(txManager, uuidGenerator{}, systemClock{}, scenarioRepo, scoringConfigRepo, scoringRequestRepo)
	platformService := service.NewPlatformService(txManager, uuidGenerator{}, systemClock{}, customListRepo, recordTagRepo, riskRepo, ipFlagRepo)
	scenarioHandler := handlers.NewScenarioHandler(scenarioService, iterationService)
	accessorHandler := handlers.NewAccessorHandler(accessorService)
	publicationHandler := handlers.NewPublicationHandler(iterationService, publicationService)
	ruleHandler := handlers.NewRuleHandler(ruleService)
	validationHandler := handlers.NewValidationHandler(validationService)
	decisionHandler := handlers.NewDecisionHandler(decisionService)
	testRunHandler := handlers.NewTestRunHandler(testRunService)
	workflowHandler := handlers.NewWorkflowHandler(workflowService)
	workflowRuleHandler := handlers.NewWorkflowRuleHandler(workflowRuleService)
	snoozeHandler := handlers.NewSnoozeHandler(snoozeService)
	outboxHandler := handlers.NewOutboxHandler(outboxService)
	executionHandler := handlers.NewExecutionHandler(executionService)
	screeningHandler := handlers.NewScreeningHandler(screeningService)
	internalScreeningHandler := handlers.NewInternalScreeningHandler(screeningService)
	scoringHandler := handlers.NewScoringHandler(scoringService)
	platformHandler := handlers.NewPlatformHandler(platformService)

	v1 := router.Group("/v1")
	v1.Use(authMiddleware(AuthConfig{
		Mode:  cfg.AuthMode,
		Token: cfg.AuthToken,
	}))
	v1.GET("/service-info", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"service":                "decision-engine-service",
			"data_model_service_url": cfg.DataModelServiceURL,
			"ingestion_service_url":  cfg.IngestionServiceURL,
		})
	})
	v1.GET("/rule-functions", validationHandler.ListRuleFunctions)
	v1.GET("/tenants/:tenantId/scenarios", scenarioHandler.ListScenarios)
	v1.POST("/tenants/:tenantId/scenarios", scenarioHandler.CreateScenario)
	v1.GET("/tenants/:tenantId/scenarios/:scenarioId", scenarioHandler.GetScenario)
	v1.GET("/tenants/:tenantId/scenarios/:scenarioId/editor-identifiers", accessorHandler.ListByScenario)
	v1.PUT("/tenants/:tenantId/scenarios/:scenarioId", scenarioHandler.UpdateScenario)
	v1.DELETE("/tenants/:tenantId/scenarios/:scenarioId", scenarioHandler.DeleteScenario)
	v1.POST("/tenants/:tenantId/scenarios/:scenarioId/copy", scenarioHandler.CopyScenario)
	v1.GET("/tenants/:tenantId/scenarios/:scenarioId/rules/latest", scenarioHandler.ListLatestRules)
	v1.POST("/tenants/:tenantId/scenarios/:scenarioId/ast-ai-description", scenarioHandler.DescribeASTWithAI)
	v1.POST("/tenants/:tenantId/scenarios/:scenarioId/generate-ast", scenarioHandler.GenerateRuleWithAI)
	v1.GET("/tenants/:tenantId/scenarios/:scenarioId/iterations", scenarioHandler.ListIterations)
	v1.GET("/tenants/:tenantId/scenarios/:scenarioId/iterations/metadata", scenarioHandler.ListIterationMetadata)
	v1.POST("/tenants/:tenantId/scenarios/:scenarioId/iterations", scenarioHandler.CreateIteration)
	v1.GET("/tenants/:tenantId/scenarios/:scenarioId/iterations/:iterationId", scenarioHandler.GetIteration)
	v1.POST("/tenants/:tenantId/scenarios/:scenarioId/iterations/:iterationId/draft", scenarioHandler.CreateDraftFromIteration)
	v1.PUT("/tenants/:tenantId/scenarios/:scenarioId/iterations/:iterationId", scenarioHandler.UpdateIteration)
	v1.POST("/tenants/:tenantId/scenarios/:scenarioId/iterations/:iterationId/commit", publicationHandler.CommitIteration)
	v1.GET("/tenants/:tenantId/scenarios/:scenarioId/publications", publicationHandler.ListPublications)
	v1.POST("/tenants/:tenantId/scenarios/:scenarioId/publications", publicationHandler.ExecutePublicationAction)
	v1.GET("/tenants/:tenantId/scenarios/:scenarioId/publications/preparation", publicationHandler.GetPreparationStatus)
	v1.POST("/tenants/:tenantId/scenarios/:scenarioId/publications/preparation", publicationHandler.StartPreparation)
	v1.GET("/tenants/:tenantId/scenarios/:scenarioId/iterations/:iterationId/rules", ruleHandler.ListRules)
	v1.POST("/tenants/:tenantId/scenarios/:scenarioId/iterations/:iterationId/rules", ruleHandler.CreateRule)
	v1.PUT("/tenants/:tenantId/scenarios/:scenarioId/iterations/:iterationId/rules/:ruleId", ruleHandler.UpdateRule)
	v1.DELETE("/tenants/:tenantId/scenarios/:scenarioId/iterations/:iterationId/rules/:ruleId", ruleHandler.DeleteRule)
	v1.GET("/tenants/:tenantId/scenarios/:scenarioId/iterations/:iterationId/rules/:ruleId/ai-description", scenarioHandler.DescribeRuleWithAI)
	v1.POST("/tenants/:tenantId/scenarios/:scenarioId/iterations/:iterationId/validate", validationHandler.ValidateIteration)
	v1.POST("/tenants/:tenantId/scenarios/:scenarioId/evaluate", decisionHandler.EvaluateScenario)
	v1.GET("/tenants/:tenantId/scenarios/:scenarioId/decisions", decisionHandler.ListDecisionsByScenario)
	v1.GET("/tenants/:tenantId/scenarios/:scenarioId/test-runs", testRunHandler.ListByScenario)
	v1.POST("/tenants/:tenantId/scenarios/:scenarioId/test-runs", testRunHandler.Create)
	v1.GET("/tenants/:tenantId/test-runs/:testRunId", testRunHandler.Get)
	v1.POST("/tenants/:tenantId/test-runs/:testRunId/cancel", testRunHandler.Cancel)
	v1.GET("/tenants/:tenantId/test-runs/:testRunId/decision-data-by-score", testRunHandler.DecisionSummaries)
	v1.GET("/tenants/:tenantId/test-runs/:testRunId/data-by-rule-execution", testRunHandler.RuleStats)
	v1.GET("/tenants/:tenantId/scenarios/:scenarioId/workflows", workflowHandler.ListByScenario)
	v1.POST("/tenants/:tenantId/scenarios/:scenarioId/workflows", workflowHandler.Create)
	v1.POST("/tenants/:tenantId/scenarios/:scenarioId/workflows/reorder", workflowHandler.Reorder)
	v1.GET("/tenants/:tenantId/scenarios/:scenarioId/workflows/:workflowId", workflowHandler.Get)
	v1.PUT("/tenants/:tenantId/scenarios/:scenarioId/workflows/:workflowId", workflowHandler.Update)
	v1.DELETE("/tenants/:tenantId/scenarios/:scenarioId/workflows/:workflowId", workflowHandler.Delete)
	v1.GET("/tenants/:tenantId/scenarios/:scenarioId/workflow-rules", workflowRuleHandler.ListByScenario)
	v1.POST("/tenants/:tenantId/scenarios/:scenarioId/workflow-rules", workflowRuleHandler.Create)
	v1.POST("/tenants/:tenantId/scenarios/:scenarioId/workflow-rules/reorder", workflowRuleHandler.Reorder)
	v1.GET("/tenants/:tenantId/scenarios/:scenarioId/workflow-rules/:ruleId", workflowRuleHandler.Get)
	v1.PUT("/tenants/:tenantId/scenarios/:scenarioId/workflow-rules/:ruleId", workflowRuleHandler.Update)
	v1.DELETE("/tenants/:tenantId/scenarios/:scenarioId/workflow-rules/:ruleId", workflowRuleHandler.Delete)
	v1.POST("/tenants/:tenantId/scenarios/:scenarioId/workflow-rules/:ruleId/conditions", workflowRuleHandler.CreateCondition)
	v1.PUT("/tenants/:tenantId/scenarios/:scenarioId/workflow-rules/:ruleId/conditions/:conditionId", workflowRuleHandler.UpdateCondition)
	v1.DELETE("/tenants/:tenantId/scenarios/:scenarioId/workflow-rules/:ruleId/conditions/:conditionId", workflowRuleHandler.DeleteCondition)
	v1.POST("/tenants/:tenantId/scenarios/:scenarioId/workflow-rules/:ruleId/actions", workflowRuleHandler.CreateAction)
	v1.PUT("/tenants/:tenantId/scenarios/:scenarioId/workflow-rules/:ruleId/actions/:actionId", workflowRuleHandler.UpdateAction)
	v1.DELETE("/tenants/:tenantId/scenarios/:scenarioId/workflow-rules/:ruleId/actions/:actionId", workflowRuleHandler.DeleteAction)
	v1.GET("/tenants/:tenantId/scenarios/:scenarioId/screening-configs", screeningHandler.ListConfigsByScenario)
	v1.POST("/tenants/:tenantId/scenarios/:scenarioId/screening-configs", screeningHandler.CreateConfig)
	v1.GET("/tenants/:tenantId/scenarios/:scenarioId/screening-configs/:configId", screeningHandler.GetConfig)
	v1.PUT("/tenants/:tenantId/scenarios/:scenarioId/screening-configs/:configId", screeningHandler.UpdateConfig)
	v1.DELETE("/tenants/:tenantId/scenarios/:scenarioId/screening-configs/:configId", screeningHandler.DeleteConfig)
	v1.GET("/tenants/:tenantId/scenarios/:scenarioId/scoring-configs", scoringHandler.ListConfigsByScenario)
	v1.POST("/tenants/:tenantId/scenarios/:scenarioId/scoring-configs", scoringHandler.CreateConfig)
	v1.GET("/tenants/:tenantId/scenarios/:scenarioId/scoring-configs/:configId", scoringHandler.GetConfig)
	v1.PUT("/tenants/:tenantId/scenarios/:scenarioId/scoring-configs/:configId", scoringHandler.UpdateConfig)
	v1.DELETE("/tenants/:tenantId/scenarios/:scenarioId/scoring-configs/:configId", scoringHandler.DeleteConfig)
	v1.GET("/tenants/:tenantId/scenarios/:scenarioId/rule-snoozes", snoozeHandler.ListActive)
	v1.POST("/tenants/:tenantId/scenarios/:scenarioId/rule-snoozes", snoozeHandler.Create)
	v1.GET("/tenants/:tenantId/scenarios/:scenarioId/recurring-schedule", executionHandler.GetRecurringSchedule)
	v1.PUT("/tenants/:tenantId/scenarios/:scenarioId/recurring-schedule", executionHandler.UpdateRecurringSchedule)
	v1.GET("/tenants/:tenantId/scenarios/:scenarioId/scheduled-executions", executionHandler.ListScheduledExecutionsByScenario)
	v1.POST("/tenants/:tenantId/scenarios/:scenarioId/scheduled-executions", executionHandler.CreateScheduledExecution)
	v1.GET("/tenants/:tenantId/platform/custom-list-entries", platformHandler.ListCustomListEntries)
	v1.POST("/tenants/:tenantId/platform/custom-list-entries", platformHandler.CreateCustomListEntry)
	v1.GET("/tenants/:tenantId/platform/record-tags", platformHandler.ListRecordTags)
	v1.POST("/tenants/:tenantId/platform/record-tags", platformHandler.CreateRecordTag)
	v1.POST("/tenants/:tenantId/platform/risk-snapshots", platformHandler.CreateRiskSnapshot)
	v1.GET("/tenants/:tenantId/platform/ip-flags", platformHandler.ListIPFlags)
	v1.POST("/tenants/:tenantId/platform/ip-flags", platformHandler.CreateIPFlag)
	v1.GET("/tenants/:tenantId/decisions/:decisionId", decisionHandler.GetDecision)
	v1.GET("/tenants/:tenantId/decisions", decisionHandler.ListDecisions)
	v1.POST("/tenants/:tenantId/decisions", decisionHandler.CreateDecision)
	v1.POST("/tenants/:tenantId/decisions/all", decisionHandler.CreateAllDecisions)
	v1.GET("/tenants/:tenantId/decisions/:decisionId/workflow-executions", workflowHandler.ListByDecision)
	v1.GET("/tenants/:tenantId/decisions/:decisionId/screening-executions", screeningHandler.ListExecutionsByDecision)
	v1.GET("/tenants/:tenantId/screening-executions/:executionId", screeningHandler.GetExecution)
	v1.POST("/tenants/:tenantId/screening-executions/:executionId/status", screeningHandler.UpdateExecutionStatus)
	v1.POST("/tenants/:tenantId/screening-executions/:executionId/retry", screeningHandler.RetryExecution)
	v1.GET("/tenants/:tenantId/decisions/:decisionId/scoring-requests", scoringHandler.ListRequestsByDecision)
	v1.GET("/tenants/:tenantId/scoring-requests/:requestId", scoringHandler.GetRequest)
	v1.POST("/tenants/:tenantId/scoring-requests/:requestId/status", scoringHandler.UpdateRequestStatus)
	v1.POST("/tenants/:tenantId/scoring-requests/:requestId/retry", scoringHandler.RetryRequest)
	v1.GET("/tenants/:tenantId/outbox-events", outboxHandler.ListByTenant)
	v1.GET("/tenants/:tenantId/async-decision-executions", executionHandler.ListAsyncDecisionExecutionsByTenant)
	v1.POST("/tenants/:tenantId/async-decision-executions", executionHandler.CreateAsyncDecisionExecution)
	v1.POST("/tenants/:tenantId/test-runs/:testRunId/evaluate", testRunHandler.Evaluate)
	v1.POST("/tenants/:tenantId/ingestion-events/record-ingested", decisionHandler.HandleRecordIngested)

	internal := router.Group("/internal")
	internal.Use(authMiddleware(AuthConfig{
		Mode:  cfg.AuthMode,
		Token: cfg.AuthToken,
	}))
	internal.POST("/screening-status-updates", internalScreeningHandler.UpdateStatus)

	return router
}
