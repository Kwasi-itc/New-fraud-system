package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/app"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/clients/datamodel"
	dispatchclient "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/clients/dispatch"
	ingestionclient "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/clients/ingestion"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/service"
	storepostgres "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/postgres"
)

type uuidGenerator struct{}

func (uuidGenerator) New() uuid.UUID { return uuid.New() }

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now().UTC() }

type workerRunner struct {
	logger           *slog.Logger
	executionService service.ExecutionService
	dispatchService  service.DispatchService
	batchLimit       int
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	cfg, err := app.LoadConfig()
	if err != nil {
		logger.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	db, err := storepostgres.NewPool(context.Background(), cfg.DatabaseURL)
	if err != nil {
		logger.Error("failed to connect database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	var txManager ports.TransactionManager = storepostgres.NewTransactionManager(db)
	var scenarioRepo ports.ScenarioRepository = storepostgres.NewScenarioRepository(db)
	var iterationRepo ports.ScenarioIterationRepository = storepostgres.NewScenarioIterationRepository(db)
	var ruleRepo ports.RuleRepository = storepostgres.NewRuleRepository(db)
	var decisionRepo ports.DecisionRepository = storepostgres.NewDecisionRepository(db)
	var ruleExecutionRepo ports.RuleExecutionRepository = storepostgres.NewRuleExecutionRepository(db)
	var workflowRepo ports.WorkflowRepository = storepostgres.NewWorkflowRepository(db)
	var workflowRuleRepo ports.WorkflowRuleRepository = storepostgres.NewWorkflowRuleRepository(db)
	var workflowConditionRepo ports.WorkflowConditionRepository = storepostgres.NewWorkflowConditionRepository(db)
	var workflowActionRepo ports.WorkflowActionRepository = storepostgres.NewWorkflowActionRepository(db)
	var workflowExecutionRepo ports.WorkflowExecutionRepository = storepostgres.NewWorkflowExecutionRepository(db)
	var snoozeRepo ports.RuleSnoozeRepository = storepostgres.NewRuleSnoozeRepository(db)
	var outboxRepo ports.OutboxEventRepository = storepostgres.NewOutboxEventRepository(db)
	var customListRepo ports.CustomListRepository = storepostgres.NewCustomListRepository(db)
	var recordTagRepo ports.RecordTagRepository = storepostgres.NewRecordTagRepository(db)
	var riskRepo ports.RiskSnapshotRepository = storepostgres.NewRiskSnapshotRepository(db)
	var ipFlagRepo ports.IPFlagRepository = storepostgres.NewIPFlagRepository(db)
	var scheduledRepo ports.ScheduledExecutionRepository = storepostgres.NewScheduledExecutionRepository(db)
	var asyncRepo ports.AsyncDecisionExecutionRepository = storepostgres.NewAsyncDecisionExecutionRepository(db)
	var screeningConfigRepo ports.ScreeningConfigRepository = storepostgres.NewScreeningConfigRepository(db)
	var screeningExecutionRepo ports.ScreeningExecutionRepository = storepostgres.NewScreeningExecutionRepository(db)
	var scoringConfigRepo ports.ScoringConfigRepository = storepostgres.NewScoringConfigRepository(db)
	var scoringRequestRepo ports.ScoringRequestRepository = storepostgres.NewScoringRequestRepository(db)

	dataModelReader := datamodel.NewHTTPClient(cfg.DataModelServiceURL, cfg.HTTPClientTimeout)
	tenantDataReader := ingestionclient.NewHTTPClient(cfg.IngestionServiceURL, cfg.HTTPClientTimeout)
	_ = service.NewValidationService(dataModelReader, scenarioRepo, iterationRepo, ruleRepo)

	decisionService := service.NewDecisionService(
		txManager,
		uuidGenerator{},
		systemClock{},
		dataModelReader,
		scenarioRepo,
		iterationRepo,
		ruleRepo,
		tenantDataReader,
		decisionRepo,
		ruleExecutionRepo,
		workflowRepo,
		workflowRuleRepo,
		workflowConditionRepo,
		workflowActionRepo,
		workflowExecutionRepo,
		snoozeRepo,
		outboxRepo,
		customListRepo,
		recordTagRepo,
		riskRepo,
		ipFlagRepo,
		screeningConfigRepo,
		screeningExecutionRepo,
		scoringConfigRepo,
		scoringRequestRepo,
		cfg.AggregatePushdownMode,
		cfg.AggregatePushdownAggregates,
	)

	executionService := service.NewExecutionService(
		txManager,
		uuidGenerator{},
		systemClock{},
		scenarioRepo,
		iterationRepo,
		tenantDataReader,
		scheduledRepo,
		asyncRepo,
		decisionService,
	)
	dispatchClient := dispatchclient.NewHTTPClient(
		cfg.HTTPClientTimeout,
		cfg.ServiceAuthMode,
		cfg.ServiceAuthToken,
		cfg.WorkflowActionURL,
		firstNonEmpty(cfg.ScreeningServiceURL, cfg.ScreeningProviderURL),
		cfg.ScoringProviderURL,
		cfg.OutboxPublisherURL,
	)
	dispatchService := service.NewDispatchService(
		workflowExecutionRepo,
		screeningExecutionRepo,
		scoringRequestRepo,
		outboxRepo,
		dispatchClient,
		dispatchClient,
		dispatchClient,
		dispatchClient,
	)

	runner := workerRunner{
		logger:           logger,
		executionService: executionService,
		dispatchService:  dispatchService,
		batchLimit:       cfg.WorkerBatchLimit,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	switch cfg.WorkerMode {
	case "poll":
		logger.Info("worker poll loop starting", "poll_interval", cfg.WorkerPollInterval.String(), "batch_limit", cfg.WorkerBatchLimit)
		if err := runner.runPollLoop(ctx, cfg.WorkerPollInterval); err != nil {
			logger.Error("worker poll loop failed", "error", err)
			os.Exit(1)
		}
	default:
		logger.Info("worker batch run starting", "batch_limit", cfg.WorkerBatchLimit)
		if err := runner.runOnce(ctx); err != nil {
			logger.Error("worker batch run failed", "error", err)
			os.Exit(1)
		}
		logger.Info("worker batch run completed")
	}
}

func (w workerRunner) runPollLoop(ctx context.Context, interval time.Duration) error {
	if err := w.runOnce(ctx); err != nil {
		return err
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			w.logger.Info("worker poll loop stopping")
			return nil
		case <-ticker.C:
			if err := w.runOnce(ctx); err != nil {
				return err
			}
		}
	}
}

func (w workerRunner) runOnce(ctx context.Context) error {
	w.logger.Info("worker cycle started", "batch_limit", w.batchLimit)

	if err := w.executionService.ProcessDueScheduledExecutions(ctx, w.batchLimit); err != nil {
		return err
	}
	if err := w.executionService.ProcessQueuedAsyncExecutions(ctx, w.batchLimit); err != nil {
		return err
	}
	if err := w.dispatchService.ProcessPendingWorkflowExecutions(ctx, w.batchLimit); err != nil {
		return err
	}
	if err := w.dispatchService.ProcessPendingScreeningExecutions(ctx, w.batchLimit); err != nil {
		return err
	}
	if err := w.dispatchService.ProcessPendingScoringRequests(ctx, w.batchLimit); err != nil {
		return err
	}
	if err := w.dispatchService.ProcessPendingOutboxEvents(ctx, w.batchLimit); err != nil {
		return err
	}

	w.logger.Info("worker cycle completed")
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
