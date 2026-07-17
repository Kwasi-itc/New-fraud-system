package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/app"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/clients/datamodel"
	dispatchclient "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/clients/dispatch"
	ingestionclient "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/clients/ingestion"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/riverjobs"
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
	workers := river.NewWorkers()
	riverClient, err := river.NewClient(riverpgxv5.New(db), &river.Config{
		Workers: workers,
		Queues: map[string]river.QueueConfig{
			cfg.ScheduledExecutionQueueName: {
				MaxWorkers: cfg.ScheduledExecutionQueueWorkers,
			},
			cfg.AsyncExecutionQueueName: {
				MaxWorkers: cfg.AsyncExecutionQueueWorkers,
			},
			cfg.AsyncExecutionCallbackQueueName: {
				MaxWorkers: cfg.AsyncExecutionCallbackQueueWorkers,
			},
			cfg.WorkflowDispatchQueueName: {
				MaxWorkers: cfg.WorkflowDispatchQueueWorkers,
			},
			cfg.ScreeningDispatchQueueName: {
				MaxWorkers: cfg.ScreeningDispatchQueueWorkers,
			},
			cfg.ScoringDispatchQueueName: {
				MaxWorkers: cfg.ScoringDispatchQueueWorkers,
			},
			cfg.OutboxQueueName: {
				MaxWorkers: cfg.OutboxQueueWorkers,
			},
		},
	})
	if err != nil {
		logger.Error("failed to initialize river client", "error", err)
		os.Exit(1)
	}
	scheduledEnqueuer := riverjobs.NewRiverScheduledExecutionEnqueuer(riverClient, max(1, cfg.ScheduledExecutionMaxAttempts), cfg.ScheduledExecutionQueueName)
	asyncEnqueuer := riverjobs.NewRiverAsyncDecisionExecutionEnqueuer(riverClient, max(1, cfg.AsyncExecutionMaxAttempts), cfg.AsyncExecutionQueueName)
	asyncCallbackEnqueuer := riverjobs.NewRiverAsyncDecisionExecutionCallbackEnqueuer(riverClient, max(1, cfg.AsyncExecutionCallbackMaxAttempts), cfg.AsyncExecutionCallbackQueueName)
	workflowEnqueuer := riverjobs.NewRiverWorkflowExecutionEnqueuer(riverClient, 1, cfg.WorkflowDispatchQueueName)
	screeningEnqueuer := riverjobs.NewRiverScreeningExecutionEnqueuer(riverClient, 1, cfg.ScreeningDispatchQueueName)
	scoringEnqueuer := riverjobs.NewRiverScoringRequestEnqueuer(riverClient, 1, cfg.ScoringDispatchQueueName)
	outboxEnqueuer := riverjobs.NewRiverOutboxEventEnqueuer(riverClient, 1, cfg.OutboxQueueName)

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
		workflowEnqueuer,
		screeningEnqueuer,
		scoringEnqueuer,
		outboxEnqueuer,
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
	scheduledWorker := riverjobs.NewScheduledExecutionWorker(executionService)
	asyncWorker := riverjobs.NewAsyncDecisionExecutionWorker(executionService)
	asyncCallbackWorker := riverjobs.NewAsyncDecisionExecutionCallbackWorker(executionService)
	river.AddWorker(workers, &scheduledWorker)
	river.AddWorker(workers, &asyncWorker)
	river.AddWorker(workers, &asyncCallbackWorker)
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
	workflowWorker := riverjobs.NewWorkflowExecutionWorker(dispatchService)
	screeningWorker := riverjobs.NewScreeningExecutionWorker(dispatchService)
	scoringWorker := riverjobs.NewScoringRequestWorker(dispatchService)
	outboxWorker := riverjobs.NewOutboxEventWorker(dispatchService)
	river.AddWorker(workers, &workflowWorker)
	river.AddWorker(workers, &screeningWorker)
	river.AddWorker(workers, &scoringWorker)
	river.AddWorker(workers, &outboxWorker)

	runner := workerRunner{
		logger:           logger,
		executionService: executionService,
		dispatchService:  dispatchService,
		batchLimit:       cfg.WorkerBatchLimit,
		tasks:            toTaskSet(legacyWorkerTasks(cfg.WorkerTasks)),
		taskOrder:        app.SortedWorkerTasks(legacyWorkerTasks(cfg.WorkerTasks), cfg.WorkerTaskPriorities),
		metrics:          newWorkerMetrics(legacyWorkerTasks(cfg.WorkerTasks)),
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	logger.Info("starting decision engine execution workers",
		"scheduled_queue", cfg.ScheduledExecutionQueueName,
		"scheduled_workers", cfg.ScheduledExecutionQueueWorkers,
		"async_queue", cfg.AsyncExecutionQueueName,
		"async_workers", cfg.AsyncExecutionQueueWorkers,
		"async_callback_queue", cfg.AsyncExecutionCallbackQueueName,
		"async_callback_workers", cfg.AsyncExecutionCallbackQueueWorkers,
		"workflow_queue", cfg.WorkflowDispatchQueueName,
		"workflow_workers", cfg.WorkflowDispatchQueueWorkers,
		"screening_queue", cfg.ScreeningDispatchQueueName,
		"screening_workers", cfg.ScreeningDispatchQueueWorkers,
		"scoring_queue", cfg.ScoringDispatchQueueName,
		"scoring_workers", cfg.ScoringDispatchQueueWorkers,
		"outbox_queue", cfg.OutboxQueueName,
		"outbox_workers", cfg.OutboxQueueWorkers,
	)
	if err := riverClient.Start(ctx); err != nil {
		logger.Error("failed to start river client", "error", err)
		os.Exit(1)
	}
	defer func() {
		stopCtx, cancel := context.WithCancel(context.Background())
		defer cancel()
		_ = riverClient.Stop(stopCtx)
	}()

	if len(runner.taskOrder) == 0 {
		<-ctx.Done()
		if err := ctx.Err(); err != nil && !errors.Is(err, context.Canceled) {
			logger.Error("worker exited with error", "error", err)
			os.Exit(1)
		}
		return
	}

	switch cfg.WorkerMode {
	case "poll":
		logger.Info("legacy worker poll loop starting", "poll_interval", cfg.WorkerPollInterval.String(), "batch_limit", cfg.WorkerBatchLimit, "tasks", runner.taskOrder)
		if err := runner.runPollLoop(ctx, cfg.WorkerPollInterval); err != nil {
			logger.Error("legacy worker poll loop failed", "error", err)
			os.Exit(1)
		}
	default:
		logger.Info("legacy worker batch run starting", "batch_limit", cfg.WorkerBatchLimit, "tasks", runner.taskOrder)
		if err := runner.runOnce(ctx); err != nil {
			logger.Error("legacy worker batch run failed", "error", err)
			os.Exit(1)
		}
		logger.Info("legacy worker batch run completed")
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

func legacyWorkerTasks(tasks []string) []string {
	return nil
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
