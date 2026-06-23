package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/decision"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/integration"
	scenarioDomain "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scoring"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	asteval "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/runtime/ast_eval"
)

type DecisionEvaluationRequest struct {
	ObjectID   string         `json:"object_id"`
	ObjectType string         `json:"object_type"`
	Fields     map[string]any `json:"fields"`
}

type DecisionEvaluationResult struct {
	Triggered      bool                     `json:"triggered"`
	Decision       *decision.Decision       `json:"decision,omitempty"`
	RuleExecutions []decision.RuleExecution `json:"rule_executions,omitempty"`
}

type MultiScenarioEvaluationResult struct {
	ObjectID string                     `json:"object_id"`
	Results  []DecisionEvaluationResult `json:"results"`
}

const decisionEvaluationCacheTTL = 30 * time.Second

type DBPoolStats struct {
	AcquireCount           int64
	AcquireDurationMicros  int64
	EmptyAcquireCount      int64
	EmptyAcquireWaitMicros int64
	CanceledAcquireCount   int64
	MaxConns               int32
	TotalConns             int32
	AcquiredConns          int32
	IdleConns              int32
	ConstructingConns      int32
}

type DBPoolStatsProvider func() DBPoolStats

type DecisionService struct {
	txManager                   ports.TransactionManager
	idGen                       ports.IDGenerator
	clock                       ports.Clock
	dataModelReader             ports.DataModelReader
	scenarioRepo                ports.ScenarioRepository
	iterationRepo               ports.ScenarioIterationRepository
	ruleRepo                    ports.RuleRepository
	tenantDataReader            ports.TenantDataReader
	decisionRepo                ports.DecisionRepository
	ruleExecRepo                ports.RuleExecutionRepository
	workflowRepo                ports.WorkflowRepository
	workflowRuleRepo            ports.WorkflowRuleRepository
	workflowCondRepo            ports.WorkflowConditionRepository
	workflowActionRepo          ports.WorkflowActionRepository
	workflowExecRepo            ports.WorkflowExecutionRepository
	snoozeRepo                  ports.RuleSnoozeRepository
	outboxRepo                  ports.OutboxEventRepository
	customListRepo              ports.CustomListRepository
	recordTagRepo               ports.RecordTagRepository
	riskRepo                    ports.RiskSnapshotRepository
	ipFlagRepo                  ports.IPFlagRepository
	screeningConfigRepo         ports.ScreeningConfigRepository
	screeningExecRepo           ports.ScreeningExecutionRepository
	scoringConfigRepo           ports.ScoringConfigRepository
	scoringRequestRepo          ports.ScoringRequestRepository
	aggregatePushdownMode       string
	aggregatePushdownAggregates []string
	evaluationCache             *decisionEvaluationCache
	dbPoolStatsProvider         DBPoolStatsProvider
}

func NewDecisionService(
	txManager ports.TransactionManager,
	idGen ports.IDGenerator,
	clock ports.Clock,
	dataModelReader ports.DataModelReader,
	scenarioRepo ports.ScenarioRepository,
	iterationRepo ports.ScenarioIterationRepository,
	ruleRepo ports.RuleRepository,
	tenantDataReader ports.TenantDataReader,
	decisionRepo ports.DecisionRepository,
	ruleExecRepo ports.RuleExecutionRepository,
	workflowRepo ports.WorkflowRepository,
	workflowRuleRepo ports.WorkflowRuleRepository,
	workflowCondRepo ports.WorkflowConditionRepository,
	workflowActionRepo ports.WorkflowActionRepository,
	workflowExecRepo ports.WorkflowExecutionRepository,
	snoozeRepo ports.RuleSnoozeRepository,
	outboxRepo ports.OutboxEventRepository,
	customListRepo ports.CustomListRepository,
	recordTagRepo ports.RecordTagRepository,
	riskRepo ports.RiskSnapshotRepository,
	ipFlagRepo ports.IPFlagRepository,
	screeningConfigRepo ports.ScreeningConfigRepository,
	screeningExecRepo ports.ScreeningExecutionRepository,
	scoringConfigRepo ports.ScoringConfigRepository,
	scoringRequestRepo ports.ScoringRequestRepository,
	aggregatePushdownMode string,
	aggregatePushdownAggregates []string,
	dbPoolStatsProvider DBPoolStatsProvider,
) DecisionService {
	return DecisionService{
		txManager:                   txManager,
		idGen:                       idGen,
		clock:                       clock,
		dataModelReader:             dataModelReader,
		scenarioRepo:                scenarioRepo,
		iterationRepo:               iterationRepo,
		ruleRepo:                    ruleRepo,
		tenantDataReader:            tenantDataReader,
		decisionRepo:                decisionRepo,
		ruleExecRepo:                ruleExecRepo,
		workflowRepo:                workflowRepo,
		workflowRuleRepo:            workflowRuleRepo,
		workflowCondRepo:            workflowCondRepo,
		workflowActionRepo:          workflowActionRepo,
		workflowExecRepo:            workflowExecRepo,
		snoozeRepo:                  snoozeRepo,
		outboxRepo:                  outboxRepo,
		customListRepo:              customListRepo,
		recordTagRepo:               recordTagRepo,
		riskRepo:                    riskRepo,
		ipFlagRepo:                  ipFlagRepo,
		screeningConfigRepo:         screeningConfigRepo,
		screeningExecRepo:           screeningExecRepo,
		scoringConfigRepo:           scoringConfigRepo,
		scoringRequestRepo:          scoringRequestRepo,
		aggregatePushdownMode:       aggregatePushdownMode,
		aggregatePushdownAggregates: append([]string(nil), aggregatePushdownAggregates...),
		evaluationCache:             newDecisionEvaluationCache(decisionEvaluationCacheTTL),
		dbPoolStatsProvider:         dbPoolStatsProvider,
	}
}

func (s DecisionService) EvaluateScenario(
	ctx context.Context,
	tenantID, scenarioID string,
	req DecisionEvaluationRequest,
) (result DecisionEvaluationResult, err error) {
	return s.evaluateScenario(ctx, tenantID, scenarioID, req, asteval.NewEvaluationCache(), s.clock.Now())
}

func (s DecisionService) evaluateScenario(
	ctx context.Context,
	tenantID, scenarioID string,
	req DecisionEvaluationRequest,
	evalCache *asteval.EvaluationCache,
	evaluationNow time.Time,
) (result DecisionEvaluationResult, err error) {
	timingStartedAt := time.Now()
	stageStartedAt := timingStartedAt
	timings := make(map[string]int64, 20)
	currentStage := "scenario_get"
	poolStatsBefore, hasPoolStats := s.snapshotDBPoolStats()
	markTiming := func(stage string) {
		now := time.Now()
		timings[stage+"_us"] = now.Sub(stageStartedAt).Microseconds()
		stageStartedAt = now
	}
	defer func() {
		if err == nil {
			return
		}
		timings[currentStage+"_failed_us"] = time.Since(stageStartedAt).Microseconds()
		logDecisionEvaluationTimings(
			tenantID,
			scenarioID,
			req,
			timings,
			timingStartedAt,
			false,
			0,
			0,
			0,
			0,
			0,
			0,
			append(
				[]any{
					"failed_stage", currentStage,
					"error", err.Error(),
				},
				s.dbPoolStatsAttrs(poolStatsBefore, hasPoolStats)...,
			)...,
		)
	}()

	scn, err := s.getScenario(ctx, tenantID, scenarioID)
	if err != nil {
		return DecisionEvaluationResult{}, err
	}
	markTiming("scenario_get")
	if scn.LiveIterationID == nil {
		return DecisionEvaluationResult{}, fmt.Errorf("scenario has no live iteration")
	}
	currentStage = "iteration_get"
	iteration, err := s.getIteration(ctx, tenantID, scenarioID, *scn.LiveIterationID)
	if err != nil {
		return DecisionEvaluationResult{}, err
	}
	markTiming("iteration_get")
	if req.ObjectType != scn.TriggerObjectType {
		return DecisionEvaluationResult{}, fmt.Errorf("object_type does not match scenario trigger object type")
	}
	if len(req.Fields) == 0 {
		currentStage = "record_get"
		record, err := s.tenantDataReader.GetRecord(ctx, tenantID, req.ObjectType, req.ObjectID)
		if err != nil {
			return DecisionEvaluationResult{}, err
		}
		req.Fields = record.Fields
		markTiming("record_get")
	}
	currentStage = "tenant_model_get"
	model, err := s.dataModelReader.GetTenantModel(ctx, tenantID)
	if err != nil {
		return DecisionEvaluationResult{}, err
	}
	markTiming("tenant_model_get")
	runtime := asteval.Runtime{
		TenantID:                    tenantID,
		ObjectID:                    req.ObjectID,
		ObjectType:                  req.ObjectType,
		Fields:                      req.Fields,
		Now:                         evaluationNow,
		Model:                       &model,
		TenantDataReader:            s.tenantDataReader,
		CustomListRepo:              s.customListRepo,
		RecordTagRepo:               s.recordTagRepo,
		RiskRepo:                    s.riskRepo,
		IPFlagRepo:                  s.ipFlagRepo,
		DecisionRepo:                s.decisionRepo,
		AggregatePushdownMode:       s.aggregatePushdownMode,
		AggregatePushdownAggregates: s.aggregatePushdownAggregates,
		EvalCache:                   evalCache,
	}
	currentStage = "trigger_eval"
	triggered, err := asteval.EvaluateFormula(ctx, iteration.TriggerFormula, runtime)
	if err != nil {
		return DecisionEvaluationResult{}, err
	}
	markTiming("trigger_eval")
	if !triggered {
		logDecisionEvaluationTimings(
			tenantID,
			scenarioID,
			req,
			timings,
			timingStartedAt,
			false,
			0,
			0,
			0,
			0,
			0,
			0,
			s.dbPoolStatsAttrs(poolStatsBefore, hasPoolStats)...,
		)
		return DecisionEvaluationResult{Triggered: false}, nil
	}

	currentStage = "rules_list"
	rules, err := s.getRules(ctx, tenantID, scenarioID, iteration.ID)
	if err != nil {
		return DecisionEvaluationResult{}, err
	}
	markTiming("rules_list")
	now := s.clock.Now()
	currentStage = "snoozes_list"
	activeSnoozes, err := s.snoozeRepo.ListActive(ctx, tenantID, scenarioID, req.ObjectType, req.ObjectID, now)
	if err != nil {
		return DecisionEvaluationResult{}, err
	}
	markTiming("snoozes_list")
	activeSnoozeGroups := make(map[string]struct{}, len(activeSnoozes))
	for _, item := range activeSnoozes {
		activeSnoozeGroups[item.SnoozeGroupID] = struct{}{}
	}
	decisionID := s.idGen.New().String()
	currentStage = "rules_eval"
	evaluatedRules, err := evaluateRules(ctx, rules, runtime, activeSnoozeGroups, 0)
	if err != nil {
		return DecisionEvaluationResult{}, err
	}
	markTiming("rules_eval")

	score := 0
	ruleExecs := make([]decision.RuleExecution, 0, len(evaluatedRules))
	for _, evaluatedRule := range evaluatedRules {
		exec := newRuleExecution(now, decisionID, evaluatedRule.Rule, evaluatedRule.Matched)
		exec.ID = s.idGen.New().String()
		if evaluatedRule.Snoozed {
			exec.Outcome = "snoozed"
			ruleExecs = append(ruleExecs, exec)
			continue
		}
		if evaluatedRule.Matched {
			score += evaluatedRule.Rule.ScoreModifier
		}
		ruleExecs = append(ruleExecs, exec)
	}
	currentStage = "decision_build"
	markTiming("decision_build")

	item := decision.Decision{
		ID:                  decisionID,
		TenantID:            tenantID,
		ScenarioID:          scenarioID,
		ScenarioIterationID: iteration.ID,
		ObjectID:            req.ObjectID,
		ObjectType:          req.ObjectType,
		Outcome:             outcomeFromScore(score, iteration),
		Score:               score,
		Triggered:           true,
		CreatedAt:           now,
	}

	var stored decision.Decision
	var storedExecs []decision.RuleExecution
	currentStage = "workflow_build"
	workflowBuildStartedAt := time.Now()
	workflowExecs, err := s.buildWorkflowExecutions(ctx, item, ruleExecs, runtime)
	if err != nil {
		return DecisionEvaluationResult{}, err
	}
	workflowBuiltAt := time.Now()
	timings["workflow_build_us"] = workflowBuiltAt.Sub(workflowBuildStartedAt).Microseconds()
	stageStartedAt = workflowBuiltAt
	currentStage = "screening_build"
	screeningBuildStartedAt := time.Now()
	screeningExecs, err := s.buildScreeningExecutions(ctx, item, req.Fields)
	if err != nil {
		return DecisionEvaluationResult{}, err
	}
	screeningBuiltAt := time.Now()
	timings["screening_build_us"] = screeningBuiltAt.Sub(screeningBuildStartedAt).Microseconds()
	stageStartedAt = screeningBuiltAt
	currentStage = "scoring_build"
	scoringBuildStartedAt := time.Now()
	scoringReqs, err := s.buildScoringRequests(ctx, item)
	if err != nil {
		return DecisionEvaluationResult{}, err
	}
	scoringBuiltAt := time.Now()
	timings["scoring_build_us"] = scoringBuiltAt.Sub(scoringBuildStartedAt).Microseconds()
	stageStartedAt = scoringBuiltAt

	var storedWorkflowExecs []workflow.Execution
	var storedScreeningExecs []screening.Execution
	var storedScoringReqs []scoring.Request
	var outboxEventCount int
	txTimings := &ports.TransactionTimings{}
	currentStage = "tx_begin"
	err = s.txManager.Run(ports.WithTransactionTimings(ctx, txTimings), func(store ports.MutationStore) error {
		txStartedAt := time.Now()
		txStageStartedAt := txStartedAt
		markTxTiming := func(stage string) {
			txNow := time.Now()
			timings["tx_"+stage+"_us"] = txNow.Sub(txStageStartedAt).Microseconds()
			txStageStartedAt = txNow
		}

		var err error
		currentStage = "tx_decision_insert"
		stored, err = store.Decisions().Create(ctx, item)
		if err != nil {
			return err
		}
		markTxTiming("decision_insert")
		currentStage = "tx_rule_exec_insert"
		storedExecs, err = store.RuleExecutions().CreateMany(ctx, ruleExecs)
		if err != nil {
			return err
		}
		markTxTiming("rule_exec_insert")
		currentStage = "tx_workflow_insert"
		storedWorkflowExecs, err = store.WorkflowExecutions().CreateMany(ctx, workflowExecs)
		if err != nil {
			return err
		}
		markTxTiming("workflow_insert")
		currentStage = "tx_screening_insert"
		storedScreeningExecs, err = store.ScreeningExecutions().CreateMany(ctx, screeningExecs)
		if err != nil {
			return err
		}
		markTxTiming("screening_insert")
		currentStage = "tx_scoring_insert"
		storedScoringReqs, err = store.ScoringRequests().CreateMany(ctx, scoringReqs)
		if err != nil {
			return err
		}
		markTxTiming("scoring_insert")
		currentStage = "tx_outbox_build"
		outboxEvents, err := s.buildOutboxEvents(stored, storedWorkflowExecs, storedScreeningExecs, storedScoringReqs)
		if err != nil {
			return err
		}
		outboxEventCount = len(outboxEvents)
		markTxTiming("outbox_build")
		currentStage = "tx_outbox_insert"
		_, err = store.OutboxEvents().CreateMany(ctx, outboxEvents)
		markTxTiming("outbox_insert")
		timings["tx_body_total_us"] = time.Since(txStartedAt).Microseconds()
		return err
	})
	if err != nil {
		return DecisionEvaluationResult{}, err
	}
	timings["tx_begin_us"] = txTimings.BeginMicros
	timings["tx_manager_body_us"] = txTimings.BodyMicros
	timings["tx_commit_us"] = txTimings.CommitMicros
	currentStage = "tx_total"
	markTiming("tx_total")
	logDecisionEvaluationTimings(
		tenantID,
		scenarioID,
		req,
		timings,
		timingStartedAt,
		true,
		len(rules),
		len(storedExecs),
		len(storedWorkflowExecs),
		len(storedScreeningExecs),
		len(storedScoringReqs),
		outboxEventCount,
		s.dbPoolStatsAttrs(poolStatsBefore, hasPoolStats)...,
	)

	return DecisionEvaluationResult{
		Triggered:      true,
		Decision:       &stored,
		RuleExecutions: storedExecs,
	}, nil
}

func logDecisionEvaluationTimings(
	tenantID string,
	scenarioID string,
	req DecisionEvaluationRequest,
	timings map[string]int64,
	startedAt time.Time,
	triggered bool,
	ruleCount int,
	ruleExecutionCount int,
	workflowExecutionCount int,
	screeningExecutionCount int,
	scoringRequestCount int,
	outboxEventCount int,
	extraAttrs ...any,
) {
	attrs := []any{
		"tenant_id", tenantID,
		"scenario_id", scenarioID,
		"object_id", req.ObjectID,
		"object_type", req.ObjectType,
		"triggered", triggered,
		"field_count", len(req.Fields),
		"rule_count", ruleCount,
		"rule_execution_count", ruleExecutionCount,
		"workflow_execution_count", workflowExecutionCount,
		"screening_execution_count", screeningExecutionCount,
		"scoring_request_count", scoringRequestCount,
		"outbox_event_count", outboxEventCount,
		"total_us", time.Since(startedAt).Microseconds(),
	}
	attrs = append(attrs, extraAttrs...)
	for stage, duration := range timings {
		attrs = append(attrs, stage, duration)
	}
	slog.Default().Debug("decision evaluation timings", attrs...)
}

func (s DecisionService) snapshotDBPoolStats() (DBPoolStats, bool) {
	if s.dbPoolStatsProvider == nil {
		return DBPoolStats{}, false
	}
	return s.dbPoolStatsProvider(), true
}

func (s DecisionService) dbPoolStatsAttrs(before DBPoolStats, ok bool) []any {
	if !ok || s.dbPoolStatsProvider == nil {
		return nil
	}
	after := s.dbPoolStatsProvider()
	return []any{
		"db_pool_max_conns", after.MaxConns,
		"db_pool_total_conns", after.TotalConns,
		"db_pool_acquired_conns", after.AcquiredConns,
		"db_pool_idle_conns", after.IdleConns,
		"db_pool_constructing_conns", after.ConstructingConns,
		"db_pool_acquire_count_delta", after.AcquireCount - before.AcquireCount,
		"db_pool_acquire_duration_delta_us", after.AcquireDurationMicros - before.AcquireDurationMicros,
		"db_pool_empty_acquire_count_delta", after.EmptyAcquireCount - before.EmptyAcquireCount,
		"db_pool_empty_acquire_wait_delta_us", after.EmptyAcquireWaitMicros - before.EmptyAcquireWaitMicros,
		"db_pool_canceled_acquire_count_delta", after.CanceledAcquireCount - before.CanceledAcquireCount,
	}
}

func (s DecisionService) getScenario(ctx context.Context, tenantID, scenarioID string) (scenarioDomain.Scenario, error) {
	now := time.Now()
	if s.evaluationCache != nil {
		if item, ok := s.evaluationCache.getScenario(tenantID, scenarioID, now); ok {
			return item, nil
		}
	}
	item, err := s.scenarioRepo.GetByID(ctx, tenantID, scenarioID)
	if err != nil {
		return scenarioDomain.Scenario{}, err
	}
	if s.evaluationCache != nil {
		s.evaluationCache.setScenario(tenantID, scenarioID, item, now)
	}
	return item, nil
}

func (s DecisionService) getIteration(ctx context.Context, tenantID, scenarioID, iterationID string) (scenarioDomain.Iteration, error) {
	now := time.Now()
	if s.evaluationCache != nil {
		if item, ok := s.evaluationCache.getIteration(tenantID, scenarioID, iterationID, now); ok {
			return item, nil
		}
	}
	item, err := s.iterationRepo.GetByID(ctx, tenantID, scenarioID, iterationID)
	if err != nil {
		return scenarioDomain.Iteration{}, err
	}
	if s.evaluationCache != nil {
		s.evaluationCache.setIteration(tenantID, scenarioID, iterationID, item, now)
	}
	return item, nil
}

func (s DecisionService) getRules(ctx context.Context, tenantID, scenarioID, iterationID string) ([]scenarioDomain.Rule, error) {
	now := time.Now()
	if s.evaluationCache != nil {
		if items, ok := s.evaluationCache.getRules(tenantID, scenarioID, iterationID, now); ok {
			return items, nil
		}
	}
	items, err := s.ruleRepo.ListByIteration(ctx, tenantID, scenarioID, iterationID)
	if err != nil {
		return nil, err
	}
	if s.evaluationCache != nil {
		s.evaluationCache.setRules(tenantID, scenarioID, iterationID, items, now)
	}
	return items, nil
}

func (s DecisionService) getWorkflowRules(ctx context.Context, tenantID, scenarioID string) ([]workflow.Rule, error) {
	now := time.Now()
	if s.evaluationCache != nil {
		if items, ok := s.evaluationCache.getWorkflowRules(tenantID, scenarioID, now); ok {
			return items, nil
		}
	}
	items, err := s.workflowRuleRepo.ListByScenario(ctx, tenantID, scenarioID)
	if err != nil {
		return nil, err
	}
	if s.evaluationCache != nil {
		s.evaluationCache.setWorkflowRules(tenantID, scenarioID, items, now)
	}
	return items, nil
}

func (s DecisionService) getActiveWorkflows(ctx context.Context, tenantID, scenarioID string) ([]workflow.Definition, error) {
	now := time.Now()
	if s.evaluationCache != nil {
		if items, ok := s.evaluationCache.getActiveWorkflows(tenantID, scenarioID, now); ok {
			return items, nil
		}
	}
	items, err := s.workflowRepo.ListActiveByScenario(ctx, tenantID, scenarioID)
	if err != nil {
		return nil, err
	}
	if s.evaluationCache != nil {
		s.evaluationCache.setActiveWorkflows(tenantID, scenarioID, items, now)
	}
	return items, nil
}

func (s DecisionService) getActiveScreeningConfigs(ctx context.Context, tenantID, scenarioID string) ([]screening.Config, error) {
	now := time.Now()
	if s.evaluationCache != nil {
		if items, ok := s.evaluationCache.getActiveScreeningConfigs(tenantID, scenarioID, now); ok {
			return items, nil
		}
	}
	items, err := s.screeningConfigRepo.ListActiveByScenario(ctx, tenantID, scenarioID)
	if err != nil {
		return nil, err
	}
	if s.evaluationCache != nil {
		s.evaluationCache.setActiveScreeningConfigs(tenantID, scenarioID, items, now)
	}
	return items, nil
}

func (s DecisionService) getActiveScoringConfigs(ctx context.Context, tenantID, scenarioID string) ([]scoring.Config, error) {
	now := time.Now()
	if s.evaluationCache != nil {
		if items, ok := s.evaluationCache.getActiveScoringConfigs(tenantID, scenarioID, now); ok {
			return items, nil
		}
	}
	items, err := s.scoringConfigRepo.ListActiveByScenario(ctx, tenantID, scenarioID)
	if err != nil {
		return nil, err
	}
	if s.evaluationCache != nil {
		s.evaluationCache.setActiveScoringConfigs(tenantID, scenarioID, items, now)
	}
	return items, nil
}

type decisionEvaluationCache struct {
	mu                     sync.RWMutex
	ttl                    time.Duration
	scenarios              map[string]cachedScenario
	iterations             map[string]cachedIteration
	rules                  map[string]cachedRules
	workflowRules          map[string]cachedWorkflowRules
	activeWorkflows        map[string]cachedWorkflows
	activeScreeningConfigs map[string]cachedScreeningConfigs
	activeScoringConfigs   map[string]cachedScoringConfigs
}

type cachedScenario struct {
	item      scenarioDomain.Scenario
	expiresAt time.Time
}

type cachedIteration struct {
	item      scenarioDomain.Iteration
	expiresAt time.Time
}

type cachedRules struct {
	items     []scenarioDomain.Rule
	expiresAt time.Time
}

type cachedWorkflowRules struct {
	items     []workflow.Rule
	expiresAt time.Time
}

type cachedWorkflows struct {
	items     []workflow.Definition
	expiresAt time.Time
}

type cachedScreeningConfigs struct {
	items     []screening.Config
	expiresAt time.Time
}

type cachedScoringConfigs struct {
	items     []scoring.Config
	expiresAt time.Time
}

func newDecisionEvaluationCache(ttl time.Duration) *decisionEvaluationCache {
	return &decisionEvaluationCache{
		ttl:                    ttl,
		scenarios:              map[string]cachedScenario{},
		iterations:             map[string]cachedIteration{},
		rules:                  map[string]cachedRules{},
		workflowRules:          map[string]cachedWorkflowRules{},
		activeWorkflows:        map[string]cachedWorkflows{},
		activeScreeningConfigs: map[string]cachedScreeningConfigs{},
		activeScoringConfigs:   map[string]cachedScoringConfigs{},
	}
}

func (c *decisionEvaluationCache) getScenario(tenantID, scenarioID string, now time.Time) (scenarioDomain.Scenario, bool) {
	c.mu.RLock()
	entry, ok := c.scenarios[scenarioCacheKey(tenantID, scenarioID)]
	c.mu.RUnlock()
	if !ok || now.After(entry.expiresAt) {
		return scenarioDomain.Scenario{}, false
	}
	return cloneScenario(entry.item), true
}

func (c *decisionEvaluationCache) setScenario(tenantID, scenarioID string, item scenarioDomain.Scenario, now time.Time) {
	c.mu.Lock()
	c.scenarios[scenarioCacheKey(tenantID, scenarioID)] = cachedScenario{
		item:      cloneScenario(item),
		expiresAt: now.Add(c.ttl),
	}
	c.mu.Unlock()
}

func (c *decisionEvaluationCache) getIteration(tenantID, scenarioID, iterationID string, now time.Time) (scenarioDomain.Iteration, bool) {
	c.mu.RLock()
	entry, ok := c.iterations[iterationCacheKey(tenantID, scenarioID, iterationID)]
	c.mu.RUnlock()
	if !ok || now.After(entry.expiresAt) {
		return scenarioDomain.Iteration{}, false
	}
	return cloneIteration(entry.item), true
}

func (c *decisionEvaluationCache) setIteration(tenantID, scenarioID, iterationID string, item scenarioDomain.Iteration, now time.Time) {
	c.mu.Lock()
	c.iterations[iterationCacheKey(tenantID, scenarioID, iterationID)] = cachedIteration{
		item:      cloneIteration(item),
		expiresAt: now.Add(c.ttl),
	}
	c.mu.Unlock()
}

func (c *decisionEvaluationCache) getRules(tenantID, scenarioID, iterationID string, now time.Time) ([]scenarioDomain.Rule, bool) {
	c.mu.RLock()
	entry, ok := c.rules[iterationCacheKey(tenantID, scenarioID, iterationID)]
	c.mu.RUnlock()
	if !ok || now.After(entry.expiresAt) {
		return nil, false
	}
	return cloneScenarioRules(entry.items), true
}

func (c *decisionEvaluationCache) setRules(tenantID, scenarioID, iterationID string, items []scenarioDomain.Rule, now time.Time) {
	c.mu.Lock()
	c.rules[iterationCacheKey(tenantID, scenarioID, iterationID)] = cachedRules{
		items:     cloneScenarioRules(items),
		expiresAt: now.Add(c.ttl),
	}
	c.mu.Unlock()
}

func (c *decisionEvaluationCache) getWorkflowRules(tenantID, scenarioID string, now time.Time) ([]workflow.Rule, bool) {
	c.mu.RLock()
	entry, ok := c.workflowRules[scenarioCacheKey(tenantID, scenarioID)]
	c.mu.RUnlock()
	if !ok || now.After(entry.expiresAt) {
		return nil, false
	}
	return cloneWorkflowRules(entry.items), true
}

func (c *decisionEvaluationCache) setWorkflowRules(tenantID, scenarioID string, items []workflow.Rule, now time.Time) {
	c.mu.Lock()
	c.workflowRules[scenarioCacheKey(tenantID, scenarioID)] = cachedWorkflowRules{
		items:     cloneWorkflowRules(items),
		expiresAt: now.Add(c.ttl),
	}
	c.mu.Unlock()
}

func (c *decisionEvaluationCache) getActiveWorkflows(tenantID, scenarioID string, now time.Time) ([]workflow.Definition, bool) {
	c.mu.RLock()
	entry, ok := c.activeWorkflows[scenarioCacheKey(tenantID, scenarioID)]
	c.mu.RUnlock()
	if !ok || now.After(entry.expiresAt) {
		return nil, false
	}
	return cloneWorkflowDefinitions(entry.items), true
}

func (c *decisionEvaluationCache) setActiveWorkflows(tenantID, scenarioID string, items []workflow.Definition, now time.Time) {
	c.mu.Lock()
	c.activeWorkflows[scenarioCacheKey(tenantID, scenarioID)] = cachedWorkflows{
		items:     cloneWorkflowDefinitions(items),
		expiresAt: now.Add(c.ttl),
	}
	c.mu.Unlock()
}

func (c *decisionEvaluationCache) getActiveScreeningConfigs(tenantID, scenarioID string, now time.Time) ([]screening.Config, bool) {
	c.mu.RLock()
	entry, ok := c.activeScreeningConfigs[scenarioCacheKey(tenantID, scenarioID)]
	c.mu.RUnlock()
	if !ok || now.After(entry.expiresAt) {
		return nil, false
	}
	return cloneScreeningConfigs(entry.items), true
}

func (c *decisionEvaluationCache) setActiveScreeningConfigs(tenantID, scenarioID string, items []screening.Config, now time.Time) {
	c.mu.Lock()
	c.activeScreeningConfigs[scenarioCacheKey(tenantID, scenarioID)] = cachedScreeningConfigs{
		items:     cloneScreeningConfigs(items),
		expiresAt: now.Add(c.ttl),
	}
	c.mu.Unlock()
}

func (c *decisionEvaluationCache) getActiveScoringConfigs(tenantID, scenarioID string, now time.Time) ([]scoring.Config, bool) {
	c.mu.RLock()
	entry, ok := c.activeScoringConfigs[scenarioCacheKey(tenantID, scenarioID)]
	c.mu.RUnlock()
	if !ok || now.After(entry.expiresAt) {
		return nil, false
	}
	return cloneScoringConfigs(entry.items), true
}

func (c *decisionEvaluationCache) setActiveScoringConfigs(tenantID, scenarioID string, items []scoring.Config, now time.Time) {
	c.mu.Lock()
	c.activeScoringConfigs[scenarioCacheKey(tenantID, scenarioID)] = cachedScoringConfigs{
		items:     cloneScoringConfigs(items),
		expiresAt: now.Add(c.ttl),
	}
	c.mu.Unlock()
}

func scenarioCacheKey(tenantID, scenarioID string) string {
	return tenantID + "\x00" + scenarioID
}

func iterationCacheKey(tenantID, scenarioID, iterationID string) string {
	return tenantID + "\x00" + scenarioID + "\x00" + iterationID
}

func cloneScenario(item scenarioDomain.Scenario) scenarioDomain.Scenario {
	if item.LiveIterationID != nil {
		liveIterationID := *item.LiveIterationID
		item.LiveIterationID = &liveIterationID
	}
	return item
}

func cloneIteration(item scenarioDomain.Iteration) scenarioDomain.Iteration {
	item.TriggerFormula = cloneRawMessage(item.TriggerFormula)
	return item
}

func cloneScenarioRules(items []scenarioDomain.Rule) []scenarioDomain.Rule {
	out := append([]scenarioDomain.Rule(nil), items...)
	for i := range out {
		out[i].Formula = cloneRawMessage(out[i].Formula)
		if out[i].SnoozeGroupID != nil {
			snoozeGroupID := *out[i].SnoozeGroupID
			out[i].SnoozeGroupID = &snoozeGroupID
		}
	}
	return out
}

func cloneWorkflowRules(items []workflow.Rule) []workflow.Rule {
	return append([]workflow.Rule(nil), items...)
}

func cloneWorkflowDefinitions(items []workflow.Definition) []workflow.Definition {
	out := append([]workflow.Definition(nil), items...)
	for i := range out {
		out[i].AllowedOutcomes = append([]string(nil), out[i].AllowedOutcomes...)
		out[i].ActionConfig = cloneRawMessage(out[i].ActionConfig)
	}
	return out
}

func cloneScreeningConfigs(items []screening.Config) []screening.Config {
	out := append([]screening.Config(nil), items...)
	for i := range out {
		out[i].AllowedOutcomes = append([]string(nil), out[i].AllowedOutcomes...)
		out[i].ConfigJSON = cloneRawMessage(out[i].ConfigJSON)
	}
	return out
}

func cloneScoringConfigs(items []scoring.Config) []scoring.Config {
	out := append([]scoring.Config(nil), items...)
	for i := range out {
		out[i].AllowedOutcomes = append([]string(nil), out[i].AllowedOutcomes...)
		out[i].ConfigJSON = cloneRawMessage(out[i].ConfigJSON)
	}
	return out
}

func cloneRawMessage(value json.RawMessage) json.RawMessage {
	return append(json.RawMessage(nil), value...)
}

func (s DecisionService) GetDecision(ctx context.Context, tenantID, decisionID string) (decision.Decision, []decision.RuleExecution, error) {
	item, err := s.decisionRepo.GetByID(ctx, tenantID, decisionID)
	if err != nil {
		return decision.Decision{}, nil, err
	}
	rules, err := s.ruleExecRepo.ListByDecision(ctx, tenantID, decisionID)
	if err != nil {
		return decision.Decision{}, nil, err
	}
	return item, rules, nil
}

func (s DecisionService) ListByScenario(ctx context.Context, tenantID, scenarioID string) ([]decision.Decision, error) {
	return s.decisionRepo.ListByScenario(ctx, tenantID, scenarioID)
}

func (s DecisionService) ListByTenant(ctx context.Context, tenantID string) ([]decision.Decision, error) {
	return s.decisionRepo.ListByTenant(ctx, tenantID)
}

func (s DecisionService) ListByObject(ctx context.Context, tenantID, objectType, objectID string) ([]decision.Decision, error) {
	return s.decisionRepo.ListByObject(ctx, tenantID, objectType, objectID)
}

func (s DecisionService) EvaluateAllLiveScenarios(
	ctx context.Context,
	tenantID string,
	req DecisionEvaluationRequest,
) (MultiScenarioEvaluationResult, error) {
	scenarios, err := s.scenarioRepo.ListLiveByTriggerObject(ctx, tenantID, req.ObjectType)
	if err != nil {
		return MultiScenarioEvaluationResult{}, err
	}

	results := MultiScenarioEvaluationResult{
		ObjectID: req.ObjectID,
		Results:  make([]DecisionEvaluationResult, 0, len(scenarios)),
	}
	evalCache := asteval.NewEvaluationCache()
	evaluationNow := s.clock.Now()
	for _, scn := range scenarios {
		result, err := s.evaluateScenario(ctx, tenantID, scn.ID, req, evalCache, evaluationNow)
		if err != nil {
			return MultiScenarioEvaluationResult{}, err
		}
		results.Results = append(results.Results, result)
	}
	return results, nil
}

func (s DecisionService) buildWorkflowExecutions(ctx context.Context, item decision.Decision, ruleExecs []decision.RuleExecution, runtime asteval.Runtime) ([]workflow.Execution, error) {
	structuredRules, err := s.getWorkflowRules(ctx, item.TenantID, item.ScenarioID)
	if err != nil {
		return nil, err
	}
	if len(structuredRules) > 0 {
		return s.buildStructuredWorkflowExecutions(ctx, item, ruleExecs, runtime, structuredRules)
	}
	workflows, err := s.getActiveWorkflows(ctx, item.TenantID, item.ScenarioID)
	if err != nil {
		return nil, err
	}
	now := s.clock.Now()
	out := make([]workflow.Execution, 0, len(workflows))
	for _, def := range workflows {
		if !workflowMatchesOutcome(def, item.Outcome) {
			continue
		}
		out = append(out, workflow.Execution{
			ID:           s.idGen.New().String(),
			TenantID:     item.TenantID,
			WorkflowID:   stringPtr(def.ID),
			DecisionID:   item.ID,
			ScenarioID:   item.ScenarioID,
			ActionType:   def.ActionType,
			Status:       workflow.ExecutionStatusPendingDispatch,
			ActionConfig: def.ActionConfig,
			CreatedAt:    now,
		})
	}
	return out, nil
}

func (s DecisionService) buildStructuredWorkflowExecutions(ctx context.Context, item decision.Decision, ruleExecs []decision.RuleExecution, runtime asteval.Runtime, rules []workflow.Rule) ([]workflow.Execution, error) {
	now := s.clock.Now()
	var out []workflow.Execution
RuleLoop:
	for _, rule := range rules {
		conditions, err := s.workflowCondRepo.ListByRule(ctx, item.TenantID, rule.ID)
		if err != nil {
			return nil, err
		}
		actions, err := s.workflowActionRepo.ListByRule(ctx, item.TenantID, rule.ID)
		if err != nil {
			return nil, err
		}
		if len(actions) == 0 {
			continue
		}
		for _, cond := range conditions {
			matched, err := evaluateWorkflowCondition(ctx, cond, item, ruleExecs, runtime)
			if err != nil {
				return nil, err
			}
			if !matched {
				continue RuleLoop
			}
		}
		for _, action := range actions {
			out = append(out, workflow.Execution{
				ID:               s.idGen.New().String(),
				TenantID:         item.TenantID,
				WorkflowRuleID:   stringPtr(rule.ID),
				WorkflowActionID: stringPtr(action.ID),
				DecisionID:       item.ID,
				ScenarioID:       item.ScenarioID,
				ActionType:       action.ActionType,
				Status:           workflow.ExecutionStatusPendingDispatch,
				ActionConfig:     action.ActionConfig,
				CreatedAt:        now,
			})
		}
		if !rule.Fallthrough {
			break
		}
	}
	return out, nil
}

func workflowMatchesOutcome(def workflow.Definition, outcome decision.Outcome) bool {
	for _, allowed := range def.AllowedOutcomes {
		if allowed == string(outcome) {
			return true
		}
	}
	return false
}

func evaluateWorkflowCondition(ctx context.Context, cond workflow.Condition, item decision.Decision, ruleExecs []decision.RuleExecution, runtime asteval.Runtime) (bool, error) {
	switch cond.Function {
	case workflow.ConditionAlways:
		return true, nil
	case workflow.ConditionNever:
		return false, nil
	case workflow.ConditionOutcomeIn:
		var outcomes []string
		if err := json.Unmarshal(cond.Params, &outcomes); err != nil {
			return false, err
		}
		return stringAllowed(outcomes, string(item.Outcome)), nil
	case workflow.ConditionRuleHit:
		var params ruleHitParams
		if err := json.Unmarshal(cond.Params, &params); err != nil {
			return false, err
		}
		ids := params.IDs()
		for _, exec := range ruleExecs {
			if exec.Outcome != "hit" {
				continue
			}
			for _, id := range ids {
				if exec.RuleID == id {
					return true, nil
				}
			}
		}
		return false, nil
	case workflow.ConditionPayloadEvaluates:
		var params evaluatesParams
		if err := json.Unmarshal(cond.Params, &params); err != nil {
			return false, err
		}
		var node any
		if err := json.Unmarshal(params.Expression, &node); err != nil {
			return false, err
		}
		raw, err := json.Marshal(node)
		if err != nil {
			return false, err
		}
		return asteval.EvaluateFormula(ctx, raw, runtime)
	default:
		return false, fmt.Errorf("unknown workflow condition %q", cond.Function)
	}
}

func stringPtr(value string) *string {
	return &value
}

func (s DecisionService) buildScreeningExecutions(ctx context.Context, item decision.Decision, objectFields map[string]any) ([]screening.Execution, error) {
	configs, err := s.getActiveScreeningConfigs(ctx, item.TenantID, item.ScenarioID)
	if err != nil {
		return nil, err
	}
	now := s.clock.Now()
	out := make([]screening.Execution, 0, len(configs))
	for _, cfg := range configs {
		if !stringAllowed(cfg.AllowedOutcomes, string(item.Outcome)) {
			continue
		}
		exec := screening.Execution{
			ID:         s.idGen.New().String(),
			TenantID:   item.TenantID,
			ConfigID:   cfg.ID,
			DecisionID: item.ID,
			ScenarioID: item.ScenarioID,
			Status:     screening.ExecutionStatusPending,
			CreatedAt:  now,
			UpdatedAt:  now,
		}
		req, err := buildScreeningDispatchRequest(exec.ID, cfg, exec, item.ObjectType, item.ObjectID, objectFields)
		if err != nil {
			return nil, err
		}
		exec.RequestJSON = req
		out = append(out, exec)
	}
	return out, nil
}

func (s DecisionService) buildScoringRequests(ctx context.Context, item decision.Decision) ([]scoring.Request, error) {
	configs, err := s.getActiveScoringConfigs(ctx, item.TenantID, item.ScenarioID)
	if err != nil {
		return nil, err
	}
	now := s.clock.Now()
	out := make([]scoring.Request, 0, len(configs))
	for _, cfg := range configs {
		if !stringAllowed(cfg.AllowedOutcomes, string(item.Outcome)) {
			continue
		}
		req, err := json.Marshal(map[string]any{
			"decision_id": item.ID,
			"ruleset_ref": cfg.RulesetRef,
			"object_id":   item.ObjectID,
			"object_type": item.ObjectType,
			"config_id":   cfg.ID,
		})
		if err != nil {
			return nil, err
		}
		out = append(out, scoring.Request{
			ID:          s.idGen.New().String(),
			TenantID:    item.TenantID,
			ConfigID:    cfg.ID,
			DecisionID:  item.ID,
			ScenarioID:  item.ScenarioID,
			Status:      scoring.RequestStatusPending,
			RequestJSON: req,
			CreatedAt:   now,
			UpdatedAt:   now,
		})
	}
	return out, nil
}

func (s DecisionService) buildOutboxEvents(item decision.Decision, workflowExecs []workflow.Execution, screeningExecs []screening.Execution, scoringReqs []scoring.Request) ([]integration.OutboxEvent, error) {
	now := s.clock.Now()
	decisionPayload, err := json.Marshal(map[string]any{
		"decision_id":           item.ID,
		"scenario_id":           item.ScenarioID,
		"scenario_iteration_id": item.ScenarioIterationID,
		"object_id":             item.ObjectID,
		"object_type":           item.ObjectType,
		"outcome":               item.Outcome,
		"score":                 item.Score,
		"triggered":             item.Triggered,
		"created_at":            item.CreatedAt,
	})
	if err != nil {
		return nil, err
	}

	out := []integration.OutboxEvent{
		{
			ID:            s.idGen.New().String(),
			TenantID:      item.TenantID,
			AggregateType: "decision",
			AggregateID:   item.ID,
			EventType:     "decision.created",
			Payload:       decisionPayload,
			Status:        integration.OutboxStatusPending,
			CreatedAt:     now,
		},
	}
	for _, exec := range workflowExecs {
		payload, err := json.Marshal(map[string]any{
			"workflow_execution_id": exec.ID,
			"workflow_id":           exec.WorkflowID,
			"decision_id":           exec.DecisionID,
			"scenario_id":           exec.ScenarioID,
			"action_type":           exec.ActionType,
			"status":                exec.Status,
			"created_at":            exec.CreatedAt,
		})
		if err != nil {
			return nil, err
		}
		out = append(out, integration.OutboxEvent{
			ID:            s.idGen.New().String(),
			TenantID:      exec.TenantID,
			AggregateType: "workflow_execution",
			AggregateID:   exec.ID,
			EventType:     "workflow.execution.created",
			Payload:       payload,
			Status:        integration.OutboxStatusPending,
			CreatedAt:     now,
		})
	}
	for _, exec := range screeningExecs {
		out = append(out, integration.OutboxEvent{
			ID:            s.idGen.New().String(),
			TenantID:      exec.TenantID,
			AggregateType: "screening_execution",
			AggregateID:   exec.ID,
			EventType:     "screening.execution.created",
			Payload:       exec.RequestJSON,
			Status:        integration.OutboxStatusPending,
			CreatedAt:     now,
		})
	}
	for _, req := range scoringReqs {
		out = append(out, integration.OutboxEvent{
			ID:            s.idGen.New().String(),
			TenantID:      req.TenantID,
			AggregateType: "scoring_request",
			AggregateID:   req.ID,
			EventType:     "scoring.request.created",
			Payload:       req.RequestJSON,
			Status:        integration.OutboxStatusPending,
			CreatedAt:     now,
		})
	}
	return out, nil
}

func stringAllowed(values []string, want string) bool {
	for _, item := range values {
		if item == want {
			return true
		}
	}
	return false
}
