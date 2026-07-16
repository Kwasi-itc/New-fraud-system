package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

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

type workerRunner struct {
	logger           *slog.Logger
	executionService service.ExecutionService
	dispatchService  service.DispatchService
	batchLimit       int
	tasks            map[string]struct{}
	taskOrder        []string
	metrics          *workerMetrics
}

type workerMetrics struct {
	mu    sync.Mutex
	tasks map[string]workerTaskMetrics
}

type workerTaskMetrics struct {
	Runs          int64
	Failures      int64
	LastDuration  time.Duration
	LastSuccessAt time.Time
	LastFailureAt time.Time
	LastError     string
}

func main() {
	cfg, err := app.LoadConfig()
	if err != nil {
		logger := app.NewLogger(os.Stdout, "info")
		logger.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	logger := app.NewLogger(os.Stdout, cfg.LogLevel)
	slog.SetDefault(logger)

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
		cfg.RuleEvaluationConcurrency,
		cfg.ScenarioEvaluationConcurrency,
		dbPoolStatsProvider(db),
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
		outboxRepo,
		decisionService,
		service.ExecutionRetryPolicy{
			ScheduledMaxAttempts: cfg.ScheduledExecutionMaxAttempts,
			ScheduledBaseBackoff: cfg.ScheduledExecutionRetryBackoff,
			AsyncMaxAttempts:     cfg.AsyncExecutionMaxAttempts,
			AsyncBaseBackoff:     cfg.AsyncExecutionRetryBackoff,
		},
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
		tasks:            toTaskSet(cfg.WorkerTasks),
		taskOrder:        app.SortedWorkerTasks(cfg.WorkerTasks, cfg.WorkerTaskPriorities),
		metrics:          newWorkerMetrics(cfg.WorkerTasks),
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	switch cfg.WorkerMode {
	case "poll":
		logger.Info("worker poll loop starting", "poll_interval", cfg.WorkerPollInterval.String(), "batch_limit", cfg.WorkerBatchLimit, "tasks", cfg.WorkerTasks)
		if err := runner.runPollLoop(ctx, cfg.WorkerPollInterval); err != nil {
			logger.Error("worker poll loop failed", "error", err)
			os.Exit(1)
		}
	default:
		logger.Info("worker batch run starting", "batch_limit", cfg.WorkerBatchLimit, "tasks", cfg.WorkerTasks)
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
	cycleStartedAt := time.Now()
	w.logger.Info("worker cycle started", "batch_limit", w.batchLimit)

	for _, task := range w.taskOrder {
		if !w.enabled(task) {
			continue
		}
		if err := w.runNamedTask(ctx, task); err != nil {
			return err
		}
	}

	w.logger.Info("worker cycle completed", "duration_ms", time.Since(cycleStartedAt).Milliseconds(), "task_metrics", w.metrics.snapshot())
	return nil
}

func (w workerRunner) enabled(task string) bool {
	_, ok := w.tasks[task]
	return ok
}

func toTaskSet(tasks []string) map[string]struct{} {
	out := make(map[string]struct{}, len(tasks))
	for _, task := range tasks {
		out[task] = struct{}{}
	}
	return out
}

func (w workerRunner) runNamedTask(ctx context.Context, task string) error {
	switch task {
	case "scheduled":
		return w.runTask(task, func() error {
			return w.executionService.ProcessDueScheduledExecutions(ctx, w.batchLimit)
		})
	case "async":
		return w.runTask(task, func() error {
			return w.executionService.ProcessQueuedAsyncExecutions(ctx, w.batchLimit)
		})
	case "workflow_dispatch":
		return w.runTask(task, func() error {
			return w.dispatchService.ProcessPendingWorkflowExecutions(ctx, w.batchLimit)
		})
	case "screening_dispatch":
		return w.runTask(task, func() error {
			return w.dispatchService.ProcessPendingScreeningExecutions(ctx, w.batchLimit)
		})
	case "scoring_dispatch":
		return w.runTask(task, func() error {
			return w.dispatchService.ProcessPendingScoringRequests(ctx, w.batchLimit)
		})
	case "outbox":
		return w.runTask(task, func() error {
			return w.dispatchService.ProcessPendingOutboxEvents(ctx, w.batchLimit)
		})
	default:
		return nil
	}
}

func (w workerRunner) runTask(name string, fn func() error) error {
	startedAt := time.Now()
	err := fn()
	duration := time.Since(startedAt)
	w.metrics.record(name, duration, err)
	if err != nil {
		w.logger.Error("worker task failed", "task", name, "duration_ms", duration.Milliseconds(), "error", err)
		return err
	}
	w.logger.Info("worker task completed", "task", name, "duration_ms", duration.Milliseconds())
	return nil
}

func newWorkerMetrics(tasks []string) *workerMetrics {
	metrics := &workerMetrics{tasks: make(map[string]workerTaskMetrics, len(tasks))}
	for _, task := range tasks {
		metrics.tasks[task] = workerTaskMetrics{}
	}
	return metrics
}

func (m *workerMetrics) record(task string, duration time.Duration, err error) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	current := m.tasks[task]
	current.Runs++
	current.LastDuration = duration
	if err != nil {
		current.Failures++
		current.LastFailureAt = time.Now().UTC()
		current.LastError = err.Error()
	} else {
		current.LastSuccessAt = time.Now().UTC()
		current.LastError = ""
	}
	m.tasks[task] = current
}

func (m *workerMetrics) snapshot() map[string]map[string]any {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]map[string]any, len(m.tasks))
	for task, metrics := range m.tasks {
		out[task] = map[string]any{
			"runs":             metrics.Runs,
			"failures":         metrics.Failures,
			"last_duration_ms": metrics.LastDuration.Milliseconds(),
			"last_success_at":  metrics.LastSuccessAt,
			"last_failure_at":  metrics.LastFailureAt,
			"last_error":       metrics.LastError,
		}
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
