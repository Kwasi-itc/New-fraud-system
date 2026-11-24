package httpapi

import (
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/clients/namerecognition"
	identity "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth"
	platformauth "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth"
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/clients/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/clients/screeningcapabilities"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/httpapi/handlers"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/riverjobs"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/service"
	storepostgres "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/postgres"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/tenantdata"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/lookup"
)

type RouterConfig struct {
	PolicyObserver          func([]platformauth.RoutePolicy)
	Verifier                platformauth.TokenVerifier
	ProviderTransport       *lookup.Transport
	InternalScreeningPolicy service.InlinePolicy
	NameRecognitionURL      string
	NameRecognitionToken    string
	ScreeningServiceURL     string

	InvestigatorVerifier *identity.Verifier

	AllowedOrigins                      []string
	DataModelServiceURL                 string
	IngestionServiceURL                 string
	TenantDataReadMode                  string
	GeoIPLookup                         ports.GeoIPLookup
	HTTPClientTimeout                   time.Duration
	AggregatePushdownMode               string
	AggregatePushdownAggregates         []string
	LiveDecisionMode                    string
	LiveAsyncObjectTypes                []string
	LiveDecisionConcurrencyLimit        int
	LiveAsyncFallbackEnabled            bool
	RuleEvaluationConcurrency           int
	ScenarioEvaluationConcurrency       int
	AggregateRemoteConcurrencyLimit     int
	ScheduledExecutionMaxAttempts       int
	ScheduledExecutionRetryBackoff      time.Duration
	AsyncExecutionMaxAttempts           int
	AsyncExecutionRetryBackoff          time.Duration
	AsyncExecutionDefaultWaitWindow     time.Duration
	AsyncExecutionMaxWaitWindow         time.Duration
	AsyncExecutionCallbackTimeout       time.Duration
	AsyncExecutionCallbackSigningSecret string
	ScheduledExecutionQueueName         string
	AsyncExecutionQueueName             string
	AsyncExecutionCallbackQueueName     string
	WorkflowDispatchQueueName           string
	ScreeningDispatchQueueName          string
	ScoringDispatchQueueName            string
	OutboxQueueName                     string
}

type uuidGenerator struct{}

func (uuidGenerator) New() uuid.UUID {
	return uuid.New()
}

type systemClock struct{}

func (systemClock) Now() time.Time {
	return time.Now().UTC()
}

func dbPoolStatsProvider(db *pgxpool.Pool) service.DBPoolStatsProvider {
	if db == nil {
		return nil
	}
	return func() service.DBPoolStats {
		stats := db.Stat()
		return service.DBPoolStats{
			AcquireCount:           stats.AcquireCount(),
			AcquireDurationMicros:  stats.AcquireDuration().Microseconds(),
			EmptyAcquireCount:      stats.EmptyAcquireCount(),
			EmptyAcquireWaitMicros: stats.EmptyAcquireWaitTime().Microseconds(),
			CanceledAcquireCount:   stats.CanceledAcquireCount(),
			MaxConns:               stats.MaxConns(),
			TotalConns:             stats.TotalConns(),
			AcquiredConns:          stats.AcquiredConns(),
			IdleConns:              stats.IdleConns(),
			ConstructingConns:      stats.ConstructingConns(),
		}
	}
}

func NewRouter(logger *slog.Logger, db *pgxpool.Pool, cfg RouterConfig) *gin.Engine {
	engine := gin.New()
	router := platformauth.NewRouter(engine, cfg.Verifier)
	router.Use(requestContextMiddleware(logger))
	router.Use(corsMiddleware(cfg.AllowedOrigins))
	router.Use(gin.Recovery())
	registerInvestigator(router, db, cfg.InvestigatorVerifier)
	registerDocsRoutes(router)

	healthHandler := handlers.NewHealthHandler(logger, db)
	router.GET("/healthz", platformauth.Policy{Public: true}, healthHandler.Healthz)
	router.GET("/readyz", platformauth.Policy{Public: true}, healthHandler.Readyz)

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
	var scheduledEnqueuer riverjobs.ScheduledExecutionEnqueuer = riverjobs.NoopScheduledExecutionEnqueuer{}
	var asyncEnqueuer riverjobs.AsyncDecisionExecutionEnqueuer = riverjobs.NoopAsyncDecisionExecutionEnqueuer{}
	var asyncCallbackEnqueuer riverjobs.AsyncDecisionExecutionCallbackEnqueuer = riverjobs.NoopAsyncDecisionExecutionCallbackEnqueuer{}
	var workflowEnqueuer riverjobs.WorkflowExecutionEnqueuer = riverjobs.NoopWorkflowExecutionEnqueuer{}
	var screeningEnqueuer riverjobs.ScreeningExecutionEnqueuer = riverjobs.NoopScreeningExecutionEnqueuer{}
	var scoringEnqueuer riverjobs.ScoringRequestEnqueuer = riverjobs.NoopScoringRequestEnqueuer{}
	var outboxEnqueuer riverjobs.OutboxEventEnqueuer = riverjobs.NoopOutboxEventEnqueuer{}
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
		riverClient, _ := river.NewClient(riverpgxv5.New(db), &river.Config{})
		scheduledEnqueuer = riverjobs.NewRiverScheduledExecutionEnqueuer(riverClient, max(1, cfg.ScheduledExecutionMaxAttempts), cfg.ScheduledExecutionQueueName)
		asyncEnqueuer = riverjobs.NewRiverAsyncDecisionExecutionEnqueuer(riverClient, max(1, cfg.AsyncExecutionMaxAttempts), cfg.AsyncExecutionQueueName)
		asyncCallbackEnqueuer = riverjobs.NewRiverAsyncDecisionExecutionCallbackEnqueuer(riverClient, max(1, cfg.AsyncExecutionMaxAttempts), cfg.AsyncExecutionCallbackQueueName)
		workflowEnqueuer = riverjobs.NewRiverWorkflowExecutionEnqueuer(riverClient, 1, cfg.WorkflowDispatchQueueName)
		screeningEnqueuer = riverjobs.NewRiverScreeningExecutionEnqueuer(riverClient, 1, cfg.ScreeningDispatchQueueName)
		scoringEnqueuer = riverjobs.NewRiverScoringRequestEnqueuer(riverClient, 1, cfg.ScoringDispatchQueueName)
		outboxEnqueuer = riverjobs.NewRiverOutboxEventEnqueuer(riverClient, 1, cfg.OutboxQueueName)
	}
	dataModelReader = datamodel.NewHTTPClient(cfg.DataModelServiceURL, cfg.HTTPClientTimeout)
	tenantDataReader = tenantdata.NewReader(cfg.TenantDataReadMode, db, dataModelReader, cfg.IngestionServiceURL, cfg.HTTPClientTimeout)

	scenarioService := service.NewScenarioService(txManager, uuidGenerator{}, systemClock{}, dataModelReader, scenarioRepo, iterationRepo, ruleRepo, workflowRuleRepo, workflowConditionRepo, workflowActionRepo)
	accessorService := service.NewAccessorService(scenarioRepo, dataModelReader)
	validationService := service.NewValidationService(dataModelReader, scenarioRepo, iterationRepo, ruleRepo)
	iterationService := service.NewIterationService(txManager, uuidGenerator{}, systemClock{}, iterationRepo, ruleRepo, validationService)
	publicationService := service.NewPublicationService(txManager, uuidGenerator{}, systemClock{}, publicationRepo, scenarioRepo, iterationRepo, ruleRepo, dataModelReader)
	ruleService := service.NewRuleService(txManager, uuidGenerator{}, systemClock{}, ruleRepo, iterationRepo)
	decisionService := service.NewDecisionService(txManager, uuidGenerator{}, systemClock{}, dataModelReader, scenarioRepo, iterationRepo, ruleRepo, tenantDataReader, decisionRepo, ruleExecutionRepo, workflowRepo, workflowRuleRepo, workflowConditionRepo, workflowActionRepo, workflowExecutionRepo, ruleSnoozeRepo, outboxRepo, customListRepo, recordTagRepo, riskRepo, ipFlagRepo, screeningConfigRepo, screeningExecutionRepo, scoringConfigRepo, scoringRequestRepo, workflowEnqueuer, screeningEnqueuer, scoringEnqueuer, outboxEnqueuer, cfg.AggregatePushdownMode, cfg.AggregatePushdownAggregates, cfg.RuleEvaluationConcurrency, cfg.ScenarioEvaluationConcurrency, cfg.AggregateRemoteConcurrencyLimit, dbPoolStatsProvider(db))
	testRunService := service.NewTestRunService(txManager, uuidGenerator{}, systemClock{}, scenarioRepo, iterationRepo, ruleRepo, dataModelReader, tenantDataReader, decisionRepo, testRunRepo, phantomDecisionRepo, phantomRuleExecRepo, customListRepo, recordTagRepo, riskRepo, ipFlagRepo, cfg.AggregatePushdownMode, cfg.AggregatePushdownAggregates, cfg.RuleEvaluationConcurrency)
	decisionService.NameRecognizer = namerecognition.New(cfg.NameRecognitionURL, cfg.NameRecognitionToken, cfg.HTTPClientTimeout)
	decisionService.SetGeoIPLookup(cfg.GeoIPLookup)
	decisionService.InlinePolicy = cfg.InternalScreeningPolicy
	if db != nil && cfg.InternalScreeningPolicy.Enabled && cfg.InternalScreeningPolicy.Validate() == nil {
		decisionService.InlineScreening = lookup.Facade{DB: db, Transport: cfg.ProviderTransport}
	}
	testRunService.SetGeoIPLookup(cfg.GeoIPLookup)
	workflowService := service.NewWorkflowService(txManager, uuidGenerator{}, systemClock{}, scenarioRepo, workflowRepo, workflowExecutionRepo)
	workflowRuleService := service.NewWorkflowRuleService(txManager, uuidGenerator{}, systemClock{}, dataModelReader, scenarioRepo, workflowRuleRepo, workflowConditionRepo, workflowActionRepo)
	snoozeService := service.NewSnoozeService(txManager, uuidGenerator{}, systemClock{}, scenarioRepo, ruleSnoozeRepo)
	outboxService := service.NewOutboxService(outboxRepo)
	executionService := service.NewExecutionService(
		txManager,
		uuidGenerator{},
		systemClock{},
		scenarioRepo,
		iterationRepo,
		tenantDataReader,
		scheduledExecutionRepo,
		asyncDecisionExecutionRepo,
		outboxRepo,
		decisionService,
		service.ExecutionRetryPolicy{
			ScheduledMaxAttempts: cfg.ScheduledExecutionMaxAttempts,
			ScheduledBaseBackoff: cfg.ScheduledExecutionRetryBackoff,
			AsyncMaxAttempts:     cfg.AsyncExecutionMaxAttempts,
			AsyncBaseBackoff:     cfg.AsyncExecutionRetryBackoff,
		},
		service.AsyncExecutionBehavior{
			DefaultWaitWindow:     cfg.AsyncExecutionDefaultWaitWindow,
			MaxWaitWindow:         cfg.AsyncExecutionMaxWaitWindow,
			CallbackTimeout:       cfg.AsyncExecutionCallbackTimeout,
			CallbackSigningSecret: cfg.AsyncExecutionCallbackSigningSecret,
		},
		scheduledEnqueuer,
		asyncEnqueuer,
		asyncCallbackEnqueuer,
		outboxEnqueuer,
	)
	screeningService := service.NewScreeningService(txManager, uuidGenerator{}, systemClock{}, scenarioRepo, screeningConfigRepo, screeningExecutionRepo, screeningEnqueuer)
	scoringService := service.NewScoringService(txManager, uuidGenerator{}, systemClock{}, scenarioRepo, scoringConfigRepo, scoringRequestRepo, scoringEnqueuer)
	platformService := service.NewPlatformService(txManager, uuidGenerator{}, systemClock{}, customListRepo, recordTagRepo, riskRepo, ipFlagRepo)
	cacheInvalidator := decisionService.CacheInvalidator()
	scenarioService.SetCacheInvalidator(cacheInvalidator)
	iterationService.SetCacheInvalidator(cacheInvalidator)
	publicationService.SetCacheInvalidator(cacheInvalidator)
	ruleService.SetCacheInvalidator(cacheInvalidator)
	workflowService.SetCacheInvalidator(cacheInvalidator)
	workflowRuleService.SetCacheInvalidator(cacheInvalidator)
	screeningService.SetCacheInvalidator(cacheInvalidator)
	scoringService.SetCacheInvalidator(cacheInvalidator)
	scenarioHandler := handlers.NewScenarioHandler(scenarioService, iterationService)
	accessorHandler := handlers.NewAccessorHandler(accessorService)
	publicationHandler := handlers.NewPublicationHandler(iterationService, publicationService)
	ruleHandler := handlers.NewRuleHandler(ruleService)
	validationHandler := handlers.NewValidationHandler(validationService)
	decisionHandler := handlers.NewDecisionHandler(decisionService, executionService, cfg.LiveDecisionMode, cfg.LiveAsyncObjectTypes, cfg.LiveDecisionConcurrencyLimit, cfg.LiveAsyncFallbackEnabled)
	testRunHandler := handlers.NewTestRunHandler(testRunService)
	workflowHandler := handlers.NewWorkflowHandler(workflowService)
	workflowRuleHandler := handlers.NewWorkflowRuleHandler(workflowRuleService)
	// Dedicated gateway credential: never use the browser-visible legacy API token.
	if db != nil {
		author := handlers.WorkflowAuthoringHandler{Service: service.WorkflowAuthoringService{Store: storepostgres.WorkflowAuthoringRepository{DB: db}, Cache: cacheInvalidator}}
		group := router.Group("/internal/workflow-authoring")
		group.GET("/tenants/:tenantId/scenarios/:scenarioId", platformauth.Policy{Capability: "workflows:read", PrincipalType: platformauth.PrincipalUser}, author.Handle)
		group.PUT("/tenants/:tenantId/scenarios/:scenarioId/rules/:ruleId", platformauth.Policy{Capability: "workflows:write", PrincipalType: platformauth.PrincipalUser}, author.Handle)
		group.DELETE("/tenants/:tenantId/scenarios/:scenarioId/rules/:ruleId", platformauth.Policy{Capability: "workflows:write", PrincipalType: platformauth.PrincipalUser}, author.Handle)
	}
	snoozeHandler := handlers.NewSnoozeHandler(snoozeService)
	if db != nil {
		author := handlers.ScreeningAuthoringHandler{Service: service.ScreeningAuthoringService{InternalInlineSupported: decisionService.InlineScreening != nil, NameRecognitionSupported: decisionService.NameRecognizer != nil, Capabilities: screeningcapabilities.Reader(cfg.ScreeningServiceURL, cfg.HTTPClientTimeout), Store: storepostgres.ScreeningAuthoringRepository{DB: db}, Cache: cacheInvalidator}}
		group := router.Group("/internal/screening-authoring")
		group.GET("/tenants/:tenantId/scenarios/:scenarioId", platformauth.Policy{Capability: "screening:read", PrincipalType: platformauth.PrincipalUser}, author.Handle)
		group.PUT("/tenants/:tenantId/scenarios/:scenarioId/configs/:configId", platformauth.Policy{Capability: "screening:write", PrincipalType: platformauth.PrincipalUser}, author.Handle)
		read := handlers.ScreeningReadHandler{Store: storepostgres.ScreeningReadRepository{DB: db}}
		group.GET("/tenants/:tenantId/decisions/:decisionId/screenings", platformauth.Policy{Capability: "screening:read", PrincipalType: platformauth.PrincipalUser}, read.List)
	}
	outboxHandler := handlers.NewOutboxHandler(outboxService)
	executionHandler := handlers.NewExecutionHandler(executionService)
	screeningHandler := handlers.NewScreeningHandler(screeningService)
	internalScreeningHandler := handlers.NewInternalScreeningHandler(screeningService)
	scoringHandler := handlers.NewScoringHandler(scoringService)
	platformHandler := handlers.NewPlatformHandler(platformService)
	runtimeMetricsHandler := handlers.NewRuntimeMetricsHandler(decisionService)

	v1 := router.Group("/v1")
	v1.GET("/admin/runtime-metrics", platformauth.Policy{Capability: "metrics:read"}, runtimeMetricsHandler.Get)
	v1.GET("/service-info", platformauth.Policy{Capability: "services:read"}, func(c *gin.Context) {
		c.JSON(200, gin.H{
			"service":                "decision-engine-service",
			"data_model_service_url": cfg.DataModelServiceURL,
			"ingestion_service_url":  cfg.IngestionServiceURL,
			"tenant_data_read_mode":  cfg.TenantDataReadMode,
			"geoip_enabled":          cfg.GeoIPLookup != nil,
		})
	})
	v1.GET("/rule-functions", platformauth.Policy{Capability: "scenarios:read"}, validationHandler.ListRuleFunctions)
	v1.GET("/tenants/:tenantId/scenarios", platformauth.Policy{Capability: "scenarios:read"}, scenarioHandler.ListScenarios)
	v1.POST("/tenants/:tenantId/scenarios", platformauth.Policy{Capability: "scenarios:write", PrincipalType: platformauth.PrincipalUser}, scenarioHandler.CreateScenario)
	v1.GET("/tenants/:tenantId/scenarios/:scenarioId", platformauth.Policy{Capability: "scenarios:read"}, scenarioHandler.GetScenario)
	v1.GET("/tenants/:tenantId/scenarios/:scenarioId/editor-identifiers", platformauth.Policy{Capability: "scenarios:read"}, accessorHandler.ListByScenario)
	v1.PUT("/tenants/:tenantId/scenarios/:scenarioId", platformauth.Policy{Capability: "scenarios:write", PrincipalType: platformauth.PrincipalUser}, scenarioHandler.UpdateScenario)
	v1.DELETE("/tenants/:tenantId/scenarios/:scenarioId", platformauth.Policy{Capability: "scenarios:write", PrincipalType: platformauth.PrincipalUser}, scenarioHandler.DeleteScenario)
	v1.POST("/tenants/:tenantId/scenarios/:scenarioId/copy", platformauth.Policy{Capability: "scenarios:write", PrincipalType: platformauth.PrincipalUser}, scenarioHandler.CopyScenario)
	v1.GET("/tenants/:tenantId/scenarios/:scenarioId/rules/latest", platformauth.Policy{Capability: "scenarios:read"}, scenarioHandler.ListLatestRules)
	v1.POST("/tenants/:tenantId/scenarios/:scenarioId/ast-ai-description", platformauth.Policy{Capability: "scenarios:write", PrincipalType: platformauth.PrincipalUser}, scenarioHandler.DescribeASTWithAI)
	v1.POST("/tenants/:tenantId/scenarios/:scenarioId/generate-ast", platformauth.Policy{Capability: "scenarios:write", PrincipalType: platformauth.PrincipalUser}, scenarioHandler.GenerateRuleWithAI)
	v1.GET("/tenants/:tenantId/scenarios/:scenarioId/iterations", platformauth.Policy{Capability: "scenarios:read"}, scenarioHandler.ListIterations)
	v1.GET("/tenants/:tenantId/scenarios/:scenarioId/iterations/metadata", platformauth.Policy{Capability: "scenarios:read"}, scenarioHandler.ListIterationMetadata)
	v1.POST("/tenants/:tenantId/scenarios/:scenarioId/iterations", platformauth.Policy{Capability: "scenarios:write", PrincipalType: platformauth.PrincipalUser}, scenarioHandler.CreateIteration)
	v1.GET("/tenants/:tenantId/scenarios/:scenarioId/iterations/:iterationId", platformauth.Policy{Capability: "scenarios:read"}, scenarioHandler.GetIteration)
	v1.POST("/tenants/:tenantId/scenarios/:scenarioId/iterations/:iterationId/draft", platformauth.Policy{Capability: "scenarios:write", PrincipalType: platformauth.PrincipalUser}, scenarioHandler.CreateDraftFromIteration)
	v1.PUT("/tenants/:tenantId/scenarios/:scenarioId/iterations/:iterationId", platformauth.Policy{Capability: "scenarios:write", PrincipalType: platformauth.PrincipalUser}, scenarioHandler.UpdateIteration)
	v1.POST("/tenants/:tenantId/scenarios/:scenarioId/iterations/:iterationId/commit", platformauth.Policy{Capability: "scenarios:publish", PrincipalType: platformauth.PrincipalUser}, publicationHandler.CommitIteration)
	v1.POST("/tenants/:tenantId/scenarios/:scenarioId/iterations/:iterationId/deactivate", platformauth.Policy{Capability: "scenarios:publish", PrincipalType: platformauth.PrincipalUser}, publicationHandler.DeactivateIteration)
	v1.GET("/tenants/:tenantId/scenarios/:scenarioId/publications", platformauth.Policy{Capability: "scenarios:read"}, publicationHandler.ListPublications)
	v1.POST("/tenants/:tenantId/scenarios/:scenarioId/publications", platformauth.Policy{Capability: "scenarios:publish", PrincipalType: platformauth.PrincipalUser}, publicationHandler.ExecutePublicationAction)
	v1.GET("/tenants/:tenantId/scenarios/:scenarioId/publications/preparation", platformauth.Policy{Capability: "scenarios:read"}, publicationHandler.GetPreparationStatus)
	v1.POST("/tenants/:tenantId/scenarios/:scenarioId/publications/preparation", platformauth.Policy{Capability: "scenarios:publish", PrincipalType: platformauth.PrincipalUser}, publicationHandler.StartPreparation)
	v1.GET("/tenants/:tenantId/scenarios/:scenarioId/iterations/:iterationId/rules", platformauth.Policy{Capability: "scenarios:read"}, ruleHandler.ListRules)
	v1.GET("/tenants/:tenantId/scenarios/:scenarioId/rule-groups", platformauth.Policy{Capability: "scenarios:read"}, ruleHandler.ListRuleGroups)
	v1.POST("/tenants/:tenantId/scenarios/:scenarioId/iterations/:iterationId/rules", platformauth.Policy{Capability: "scenarios:write", PrincipalType: platformauth.PrincipalUser}, ruleHandler.CreateRule)
	v1.PUT("/tenants/:tenantId/scenarios/:scenarioId/iterations/:iterationId/rules/:ruleId", platformauth.Policy{Capability: "scenarios:write", PrincipalType: platformauth.PrincipalUser}, ruleHandler.UpdateRule)
	v1.DELETE("/tenants/:tenantId/scenarios/:scenarioId/iterations/:iterationId/rules/:ruleId", platformauth.Policy{Capability: "scenarios:write", PrincipalType: platformauth.PrincipalUser}, ruleHandler.DeleteRule)
	v1.GET("/tenants/:tenantId/scenarios/:scenarioId/iterations/:iterationId/rules/:ruleId/ai-description", platformauth.Policy{Capability: "scenarios:read"}, scenarioHandler.DescribeRuleWithAI)
	v1.POST("/tenants/:tenantId/scenarios/:scenarioId/iterations/:iterationId/validate", platformauth.Policy{Capability: "scenarios:write", PrincipalType: platformauth.PrincipalUser}, validationHandler.ValidateIteration)
	v1.POST("/tenants/:tenantId/scenarios/:scenarioId/evaluate", platformauth.Policy{Capability: "decisions:evaluate"}, decisionHandler.EvaluateScenario)
	v1.GET("/tenants/:tenantId/scenarios/:scenarioId/decisions", platformauth.Policy{Capability: "decisions:read"}, decisionHandler.ListDecisionsByScenario)
	v1.GET("/tenants/:tenantId/decisions/count", platformauth.Policy{Capability: "decisions:read"}, decisionHandler.CountDecisions)
	v1.GET("/tenants/:tenantId/scenarios/:scenarioId/test-runs", platformauth.Policy{Capability: "scenarios:read"}, testRunHandler.ListByScenario)
	v1.POST("/tenants/:tenantId/scenarios/:scenarioId/test-runs", platformauth.Policy{Capability: "scenarios:write", PrincipalType: platformauth.PrincipalUser}, testRunHandler.Create)
	v1.GET("/tenants/:tenantId/test-runs/:testRunId", platformauth.Policy{Capability: "scenarios:read"}, testRunHandler.Get)
	v1.POST("/tenants/:tenantId/test-runs/:testRunId/cancel", platformauth.Policy{Capability: "scenarios:write", PrincipalType: platformauth.PrincipalUser}, testRunHandler.Cancel)
	v1.GET("/tenants/:tenantId/test-runs/:testRunId/decision-data-by-score", platformauth.Policy{Capability: "scenarios:read"}, testRunHandler.DecisionSummaries)
	v1.GET("/tenants/:tenantId/test-runs/:testRunId/data-by-rule-execution", platformauth.Policy{Capability: "scenarios:read"}, testRunHandler.RuleStats)
	v1.GET("/tenants/:tenantId/scenarios/:scenarioId/workflows", platformauth.Policy{Capability: "workflows:read", PrincipalType: platformauth.PrincipalUser}, workflowHandler.ListByScenario)
	v1.POST("/tenants/:tenantId/scenarios/:scenarioId/workflows", platformauth.Policy{Capability: "workflows:write", PrincipalType: platformauth.PrincipalUser}, workflowHandler.Create)
	v1.POST("/tenants/:tenantId/scenarios/:scenarioId/workflows/reorder", platformauth.Policy{Capability: "workflows:write", PrincipalType: platformauth.PrincipalUser}, workflowHandler.Reorder)
	v1.GET("/tenants/:tenantId/scenarios/:scenarioId/workflows/:workflowId", platformauth.Policy{Capability: "workflows:read", PrincipalType: platformauth.PrincipalUser}, workflowHandler.Get)
	v1.PUT("/tenants/:tenantId/scenarios/:scenarioId/workflows/:workflowId", platformauth.Policy{Capability: "workflows:write", PrincipalType: platformauth.PrincipalUser}, workflowHandler.Update)
	v1.DELETE("/tenants/:tenantId/scenarios/:scenarioId/workflows/:workflowId", platformauth.Policy{Capability: "workflows:write", PrincipalType: platformauth.PrincipalUser}, workflowHandler.Delete)
	v1.GET("/tenants/:tenantId/scenarios/:scenarioId/workflow-rules", platformauth.Policy{Capability: "workflows:read", PrincipalType: platformauth.PrincipalUser}, workflowRuleHandler.ListByScenario)
	v1.POST("/tenants/:tenantId/scenarios/:scenarioId/workflow-rules", platformauth.Policy{Capability: "workflows:write", PrincipalType: platformauth.PrincipalUser}, workflowRuleHandler.Create)
	v1.POST("/tenants/:tenantId/scenarios/:scenarioId/workflow-rules/reorder", platformauth.Policy{Capability: "workflows:write", PrincipalType: platformauth.PrincipalUser}, workflowRuleHandler.Reorder)
	v1.GET("/tenants/:tenantId/scenarios/:scenarioId/workflow-rules/:ruleId", platformauth.Policy{Capability: "workflows:read", PrincipalType: platformauth.PrincipalUser}, workflowRuleHandler.Get)
	v1.PUT("/tenants/:tenantId/scenarios/:scenarioId/workflow-rules/:ruleId", platformauth.Policy{Capability: "workflows:write", PrincipalType: platformauth.PrincipalUser}, workflowRuleHandler.Update)
	v1.DELETE("/tenants/:tenantId/scenarios/:scenarioId/workflow-rules/:ruleId", platformauth.Policy{Capability: "workflows:write", PrincipalType: platformauth.PrincipalUser}, workflowRuleHandler.Delete)
	v1.POST("/tenants/:tenantId/scenarios/:scenarioId/workflow-rules/:ruleId/conditions", platformauth.Policy{Capability: "workflows:write", PrincipalType: platformauth.PrincipalUser}, workflowRuleHandler.CreateCondition)
	v1.PUT("/tenants/:tenantId/scenarios/:scenarioId/workflow-rules/:ruleId/conditions/:conditionId", platformauth.Policy{Capability: "workflows:write", PrincipalType: platformauth.PrincipalUser}, workflowRuleHandler.UpdateCondition)
	v1.DELETE("/tenants/:tenantId/scenarios/:scenarioId/workflow-rules/:ruleId/conditions/:conditionId", platformauth.Policy{Capability: "workflows:write", PrincipalType: platformauth.PrincipalUser}, workflowRuleHandler.DeleteCondition)
	v1.POST("/tenants/:tenantId/scenarios/:scenarioId/workflow-rules/:ruleId/actions", platformauth.Policy{Capability: "workflows:write", PrincipalType: platformauth.PrincipalUser}, workflowRuleHandler.CreateAction)
	v1.PUT("/tenants/:tenantId/scenarios/:scenarioId/workflow-rules/:ruleId/actions/:actionId", platformauth.Policy{Capability: "workflows:write", PrincipalType: platformauth.PrincipalUser}, workflowRuleHandler.UpdateAction)
	v1.DELETE("/tenants/:tenantId/scenarios/:scenarioId/workflow-rules/:ruleId/actions/:actionId", platformauth.Policy{Capability: "workflows:write", PrincipalType: platformauth.PrincipalUser}, workflowRuleHandler.DeleteAction)
	v1.GET("/tenants/:tenantId/scenarios/:scenarioId/screening-configs", platformauth.Policy{Capability: "screening:read", PrincipalType: platformauth.PrincipalUser}, screeningHandler.ListConfigsByScenario)
	v1.POST("/tenants/:tenantId/scenarios/:scenarioId/screening-configs", platformauth.Policy{Capability: "screening:write", PrincipalType: platformauth.PrincipalUser}, screeningHandler.CreateConfig)
	v1.GET("/tenants/:tenantId/scenarios/:scenarioId/screening-configs/:configId", platformauth.Policy{Capability: "screening:read", PrincipalType: platformauth.PrincipalUser}, screeningHandler.GetConfig)
	v1.PUT("/tenants/:tenantId/scenarios/:scenarioId/screening-configs/:configId", platformauth.Policy{Capability: "screening:write", PrincipalType: platformauth.PrincipalUser}, screeningHandler.UpdateConfig)
	v1.DELETE("/tenants/:tenantId/scenarios/:scenarioId/screening-configs/:configId", platformauth.Policy{Capability: "screening:write", PrincipalType: platformauth.PrincipalUser}, screeningHandler.DeleteConfig)
	v1.GET("/tenants/:tenantId/scenarios/:scenarioId/scoring-configs", platformauth.Policy{Capability: "screening:read", PrincipalType: platformauth.PrincipalUser}, scoringHandler.ListConfigsByScenario)
	v1.POST("/tenants/:tenantId/scenarios/:scenarioId/scoring-configs", platformauth.Policy{Capability: "screening:write", PrincipalType: platformauth.PrincipalUser}, scoringHandler.CreateConfig)
	v1.GET("/tenants/:tenantId/scenarios/:scenarioId/scoring-configs/:configId", platformauth.Policy{Capability: "screening:read", PrincipalType: platformauth.PrincipalUser}, scoringHandler.GetConfig)
	v1.PUT("/tenants/:tenantId/scenarios/:scenarioId/scoring-configs/:configId", platformauth.Policy{Capability: "screening:write", PrincipalType: platformauth.PrincipalUser}, scoringHandler.UpdateConfig)
	v1.DELETE("/tenants/:tenantId/scenarios/:scenarioId/scoring-configs/:configId", platformauth.Policy{Capability: "screening:write", PrincipalType: platformauth.PrincipalUser}, scoringHandler.DeleteConfig)
	v1.GET("/tenants/:tenantId/scenarios/:scenarioId/rule-snoozes", platformauth.Policy{Capability: "scenarios:read"}, snoozeHandler.ListActive)
	v1.POST("/tenants/:tenantId/scenarios/:scenarioId/rule-snoozes", platformauth.Policy{Capability: "scenarios:write", PrincipalType: platformauth.PrincipalUser}, snoozeHandler.Create)
	v1.GET("/tenants/:tenantId/scenarios/:scenarioId/recurring-schedule", platformauth.Policy{Capability: "scenarios:read"}, executionHandler.GetRecurringSchedule)
	v1.PUT("/tenants/:tenantId/scenarios/:scenarioId/recurring-schedule", platformauth.Policy{Capability: "scenarios:write", PrincipalType: platformauth.PrincipalUser}, executionHandler.UpdateRecurringSchedule)
	v1.GET("/tenants/:tenantId/scenarios/:scenarioId/scheduled-executions", platformauth.Policy{Capability: "executions:read", PrincipalType: platformauth.PrincipalUser}, executionHandler.ListScheduledExecutionsByScenario)
	v1.GET("/tenants/:tenantId/scenarios/:scenarioId/scheduled-executions/status-summary", platformauth.Policy{Capability: "executions:read", PrincipalType: platformauth.PrincipalUser}, executionHandler.GetScheduledExecutionStatusSummary)
	v1.GET("/tenants/:tenantId/scenarios/:scenarioId/scheduled-executions/:executionId", platformauth.Policy{Capability: "executions:read", PrincipalType: platformauth.PrincipalUser}, executionHandler.GetScheduledExecution)
	v1.POST("/tenants/:tenantId/scenarios/:scenarioId/scheduled-executions/:executionId/retry", platformauth.Policy{Capability: "executions:manage", PrincipalType: platformauth.PrincipalUser}, executionHandler.RetryScheduledExecution)
	v1.POST("/tenants/:tenantId/scenarios/:scenarioId/scheduled-executions", platformauth.Policy{Capability: "executions:manage", PrincipalType: platformauth.PrincipalUser}, executionHandler.CreateScheduledExecution)
	v1.GET("/tenants/:tenantId/platform/custom-lists", platformauth.Policy{Capability: "lists:read", PrincipalType: platformauth.PrincipalUser}, platformHandler.ListCustomLists)
	v1.POST("/tenants/:tenantId/platform/custom-lists", platformauth.Policy{Capability: "lists:write", PrincipalType: platformauth.PrincipalUser}, platformHandler.CreateCustomList)
	v1.GET("/tenants/:tenantId/platform/custom-lists/:listId", platformauth.Policy{Capability: "lists:read", PrincipalType: platformauth.PrincipalUser}, platformHandler.GetCustomList)
	v1.PUT("/tenants/:tenantId/platform/custom-lists/:listId", platformauth.Policy{Capability: "lists:write", PrincipalType: platformauth.PrincipalUser}, platformHandler.UpdateCustomList)
	v1.DELETE("/tenants/:tenantId/platform/custom-lists/:listId", platformauth.Policy{Capability: "lists:write", PrincipalType: platformauth.PrincipalUser}, platformHandler.DeleteCustomList)
	v1.GET("/tenants/:tenantId/platform/custom-lists/:listId/entries", platformauth.Policy{Capability: "lists:read", PrincipalType: platformauth.PrincipalUser}, platformHandler.ListCustomListEntries)
	v1.POST("/tenants/:tenantId/platform/custom-lists/:listId/entries", platformauth.Policy{Capability: "lists:write", PrincipalType: platformauth.PrincipalUser}, platformHandler.CreateCustomListEntry)
	v1.POST("/tenants/:tenantId/platform/custom-lists/:listId/entries/import", platformauth.Policy{Capability: "lists:write", PrincipalType: platformauth.PrincipalUser}, platformHandler.ImportCustomListEntries)
	v1.PUT("/tenants/:tenantId/platform/custom-lists/:listId/entries/:entryId", platformauth.Policy{Capability: "lists:write", PrincipalType: platformauth.PrincipalUser}, platformHandler.UpdateCustomListEntry)
	v1.DELETE("/tenants/:tenantId/platform/custom-lists/:listId/entries/:entryId", platformauth.Policy{Capability: "lists:write", PrincipalType: platformauth.PrincipalUser}, platformHandler.DeleteCustomListEntry)
	v1.GET("/tenants/:tenantId/platform/custom-list-entries", platformauth.Policy{Capability: "scenarios:read"}, platformHandler.ListCustomListEntries)
	v1.GET("/tenants/:tenantId/platform/record-tags", platformauth.Policy{Capability: "lists:read", PrincipalType: platformauth.PrincipalUser}, platformHandler.ListRecordTags)
	v1.POST("/tenants/:tenantId/platform/record-tags", platformauth.Policy{Capability: "lists:write", PrincipalType: platformauth.PrincipalUser}, platformHandler.CreateRecordTag)
	v1.POST("/tenants/:tenantId/platform/risk-snapshots", platformauth.Policy{Capability: "lists:write", PrincipalType: platformauth.PrincipalUser}, platformHandler.CreateRiskSnapshot)
	v1.GET("/tenants/:tenantId/platform/ip-flags", platformauth.Policy{Capability: "lists:read", PrincipalType: platformauth.PrincipalUser}, platformHandler.ListIPFlags)
	v1.POST("/tenants/:tenantId/platform/ip-flags", platformauth.Policy{Capability: "lists:write", PrincipalType: platformauth.PrincipalUser}, platformHandler.CreateIPFlag)
	v1.GET("/tenants/:tenantId/decisions/:decisionId", platformauth.Policy{Capability: "decisions:read"}, decisionHandler.GetDecision)
	v1.GET("/tenants/:tenantId/decisions", platformauth.Policy{Capability: "decisions:read"}, decisionHandler.ListDecisions)
	v1.POST("/tenants/:tenantId/decisions", platformauth.Policy{Capability: "decisions:evaluate"}, decisionHandler.CreateDecision)
	v1.POST("/tenants/:tenantId/decisions/all", platformauth.Policy{Capability: "decisions:evaluate"}, decisionHandler.CreateAllDecisions)
	v1.GET("/tenants/:tenantId/decisions/:decisionId/workflow-executions", platformauth.Policy{Capability: "executions:read", PrincipalType: platformauth.PrincipalUser}, workflowHandler.ListByDecision)
	v1.GET("/tenants/:tenantId/decisions/:decisionId/screening-executions", platformauth.Policy{Capability: "executions:read", PrincipalType: platformauth.PrincipalUser}, screeningHandler.ListExecutionsByDecision)
	v1.GET("/tenants/:tenantId/screening-executions/:executionId", platformauth.Policy{Capability: "executions:read", PrincipalType: platformauth.PrincipalUser}, screeningHandler.GetExecution)
	v1.POST("/tenants/:tenantId/screening-executions/:executionId/status", platformauth.Policy{Capability: "executions:manage", PrincipalType: platformauth.PrincipalUser}, screeningHandler.UpdateExecutionStatus)
	v1.POST("/tenants/:tenantId/screening-executions/:executionId/retry", platformauth.Policy{Capability: "executions:manage", PrincipalType: platformauth.PrincipalUser}, screeningHandler.RetryExecution)
	v1.GET("/tenants/:tenantId/decisions/:decisionId/scoring-requests", platformauth.Policy{Capability: "executions:read", PrincipalType: platformauth.PrincipalUser}, scoringHandler.ListRequestsByDecision)
	v1.GET("/tenants/:tenantId/scoring-requests/:requestId", platformauth.Policy{Capability: "executions:read", PrincipalType: platformauth.PrincipalUser}, scoringHandler.GetRequest)
	v1.POST("/tenants/:tenantId/scoring-requests/:requestId/status", platformauth.Policy{Capability: "executions:manage", PrincipalType: platformauth.PrincipalUser}, scoringHandler.UpdateRequestStatus)
	v1.POST("/tenants/:tenantId/scoring-requests/:requestId/retry", platformauth.Policy{Capability: "executions:manage", PrincipalType: platformauth.PrincipalUser}, scoringHandler.RetryRequest)
	v1.GET("/tenants/:tenantId/outbox-events", platformauth.Policy{Capability: "executions:read", PrincipalType: platformauth.PrincipalUser}, outboxHandler.ListByTenant)
	v1.GET("/tenants/:tenantId/async-decision-executions", platformauth.Policy{Capability: "executions:read", PrincipalType: platformauth.PrincipalUser}, executionHandler.ListAsyncDecisionExecutionsByTenant)
	v1.GET("/tenants/:tenantId/async-decision-executions/status-summary", platformauth.Policy{Capability: "executions:read", PrincipalType: platformauth.PrincipalUser}, executionHandler.GetAsyncDecisionExecutionStatusSummary)
	v1.GET("/tenants/:tenantId/async-decision-executions/:executionId", platformauth.Policy{Capability: "executions:read", PrincipalType: platformauth.PrincipalUser}, executionHandler.GetAsyncDecisionExecution)
	v1.POST("/tenants/:tenantId/async-decision-executions/:executionId/retry", platformauth.Policy{Capability: "executions:manage", PrincipalType: platformauth.PrincipalUser}, executionHandler.RetryAsyncDecisionExecution)
	v1.POST("/tenants/:tenantId/async-decision-executions", platformauth.Policy{Capability: "executions:manage", PrincipalType: platformauth.PrincipalUser}, executionHandler.CreateAsyncDecisionExecution)
	v1.POST("/tenants/:tenantId/test-runs/:testRunId/evaluate", platformauth.Policy{Capability: "scenarios:write", PrincipalType: platformauth.PrincipalUser}, testRunHandler.Evaluate)
	v1.POST("/tenants/:tenantId/ingestion-events/record-ingested", platformauth.Policy{Capability: "ingestion:events", PrincipalType: platformauth.PrincipalService, BodyTenant: true}, decisionHandler.HandleRecordIngested)

	internal := router.Group("/internal")
	internal.POST("/screening-status-updates", platformauth.Policy{Capability: "screening:callback", PrincipalType: platformauth.PrincipalService, BodyTenant: true}, internalScreeningHandler.UpdateStatus)

	router.GET("/v1/tenants/:tenantId/cases/:caseId/evidence/decisions/:decisionId", platformauth.Policy{Capability: "cases:read", PrincipalType: platformauth.PrincipalUser}, platformauth.CaseEvidence(db, "core", "decisionId", decisionHandler.GetDecision))

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
