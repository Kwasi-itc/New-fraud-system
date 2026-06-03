package service

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/decision"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/integration"
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
	}
}

func (s DecisionService) EvaluateScenario(
	ctx context.Context,
	tenantID, scenarioID string,
	req DecisionEvaluationRequest,
) (DecisionEvaluationResult, error) {
	scn, err := s.scenarioRepo.GetByID(ctx, tenantID, scenarioID)
	if err != nil {
		return DecisionEvaluationResult{}, err
	}
	if scn.LiveIterationID == nil {
		return DecisionEvaluationResult{}, fmt.Errorf("scenario has no live iteration")
	}
	iteration, err := s.iterationRepo.GetByID(ctx, tenantID, scenarioID, *scn.LiveIterationID)
	if err != nil {
		return DecisionEvaluationResult{}, err
	}
	if req.ObjectType != scn.TriggerObjectType {
		return DecisionEvaluationResult{}, fmt.Errorf("object_type does not match scenario trigger object type")
	}
	if len(req.Fields) == 0 {
		record, err := s.tenantDataReader.GetRecord(ctx, tenantID, req.ObjectType, req.ObjectID)
		if err != nil {
			return DecisionEvaluationResult{}, err
		}
		req.Fields = record.Fields
	}
	model, err := s.dataModelReader.GetTenantModel(ctx, tenantID)
	if err != nil {
		return DecisionEvaluationResult{}, err
	}
	runtime := asteval.Runtime{
		TenantID:                    tenantID,
		ObjectID:                    req.ObjectID,
		ObjectType:                  req.ObjectType,
		Fields:                      req.Fields,
		Now:                         s.clock.Now(),
		Model:                       &model,
		TenantDataReader:            s.tenantDataReader,
		CustomListRepo:              s.customListRepo,
		RecordTagRepo:               s.recordTagRepo,
		RiskRepo:                    s.riskRepo,
		IPFlagRepo:                  s.ipFlagRepo,
		DecisionRepo:                s.decisionRepo,
		AggregatePushdownMode:       s.aggregatePushdownMode,
		AggregatePushdownAggregates: s.aggregatePushdownAggregates,
	}
	triggered, err := asteval.EvaluateFormula(ctx, iteration.TriggerFormula, runtime)
	if err != nil {
		return DecisionEvaluationResult{}, err
	}
	if !triggered {
		return DecisionEvaluationResult{Triggered: false}, nil
	}

	rules, err := s.ruleRepo.ListByIteration(ctx, tenantID, scenarioID, iteration.ID)
	if err != nil {
		return DecisionEvaluationResult{}, err
	}
	now := s.clock.Now()
	activeSnoozes, err := s.snoozeRepo.ListActive(ctx, tenantID, scenarioID, req.ObjectType, req.ObjectID, now)
	if err != nil {
		return DecisionEvaluationResult{}, err
	}
	activeSnoozeGroups := make(map[string]struct{}, len(activeSnoozes))
	for _, item := range activeSnoozes {
		activeSnoozeGroups[item.SnoozeGroupID] = struct{}{}
	}
	decisionID := s.idGen.New().String()
	evaluatedRules, err := evaluateRules(ctx, rules, runtime, activeSnoozeGroups, 0)
	if err != nil {
		return DecisionEvaluationResult{}, err
	}

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
	err = s.txManager.Run(ctx, func(store ports.MutationStore) error {
		var err error
		stored, err = store.Decisions().Create(ctx, item)
		if err != nil {
			return err
		}
		storedExecs, err = store.RuleExecutions().CreateMany(ctx, ruleExecs)
		if err != nil {
			return err
		}
		workflowExecs, err := s.buildWorkflowExecutions(ctx, stored, storedExecs, runtime)
		if err != nil {
			return err
		}
		storedWorkflowExecs, err := store.WorkflowExecutions().CreateMany(ctx, workflowExecs)
		if err != nil {
			return err
		}
		screeningExecs, err := s.buildScreeningExecutions(ctx, stored, req.Fields)
		if err != nil {
			return err
		}
		storedScreeningExecs, err := store.ScreeningExecutions().CreateMany(ctx, screeningExecs)
		if err != nil {
			return err
		}
		scoringReqs, err := s.buildScoringRequests(ctx, stored)
		if err != nil {
			return err
		}
		storedScoringReqs, err := store.ScoringRequests().CreateMany(ctx, scoringReqs)
		if err != nil {
			return err
		}
		outboxEvents, err := s.buildOutboxEvents(stored, storedWorkflowExecs, storedScreeningExecs, storedScoringReqs)
		if err != nil {
			return err
		}
		_, err = store.OutboxEvents().CreateMany(ctx, outboxEvents)
		return err
	})
	if err != nil {
		return DecisionEvaluationResult{}, err
	}

	return DecisionEvaluationResult{
		Triggered:      true,
		Decision:       &stored,
		RuleExecutions: storedExecs,
	}, nil
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
	for _, scn := range scenarios {
		result, err := s.EvaluateScenario(ctx, tenantID, scn.ID, req)
		if err != nil {
			return MultiScenarioEvaluationResult{}, err
		}
		results.Results = append(results.Results, result)
	}
	return results, nil
}

func (s DecisionService) buildWorkflowExecutions(ctx context.Context, item decision.Decision, ruleExecs []decision.RuleExecution, runtime asteval.Runtime) ([]workflow.Execution, error) {
	structuredRules, err := s.workflowRuleRepo.ListByScenario(ctx, item.TenantID, item.ScenarioID)
	if err != nil {
		return nil, err
	}
	if len(structuredRules) > 0 {
		return s.buildStructuredWorkflowExecutions(ctx, item, ruleExecs, runtime, structuredRules)
	}
	workflows, err := s.workflowRepo.ListActiveByScenario(ctx, item.TenantID, item.ScenarioID)
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
	configs, err := s.screeningConfigRepo.ListActiveByScenario(ctx, item.TenantID, item.ScenarioID)
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
	configs, err := s.scoringConfigRepo.ListActiveByScenario(ctx, item.TenantID, item.ScenarioID)
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
