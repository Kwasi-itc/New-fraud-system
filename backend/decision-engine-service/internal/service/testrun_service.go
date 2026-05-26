package service

import (
	"context"
	"fmt"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/decision"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	asteval "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/runtime/ast_eval"
)

type TestRunEvaluationResult struct {
	Live    DecisionEvaluationResult `json:"live"`
	Phantom DecisionEvaluationResult `json:"phantom"`
}

type TestRunDecisionSummary struct {
	Outcome string `json:"outcome"`
	Score   int    `json:"score"`
	Count   int    `json:"count"`
}

type TestRunRuleStat struct {
	RuleID       string `json:"rule_id"`
	RuleName     string `json:"rule_name"`
	HitCount     int    `json:"hit_count"`
	NoHitCount   int    `json:"no_hit_count"`
	SnoozedCount int    `json:"snoozed_count"`
	TotalCount   int    `json:"total_count"`
}

type TestRunService struct {
	txManager           ports.TransactionManager
	idGen               ports.IDGenerator
	clock               ports.Clock
	scenarioRepo        ports.ScenarioRepository
	iterationRepo       ports.ScenarioIterationRepository
	ruleRepo            ports.RuleRepository
	testRunRepo         ports.TestRunRepository
	phantomDecisionRepo ports.PhantomDecisionRepository
	phantomRuleExecRepo ports.PhantomRuleExecutionRepository
}

func NewTestRunService(
	txManager ports.TransactionManager,
	idGen ports.IDGenerator,
	clock ports.Clock,
	scenarioRepo ports.ScenarioRepository,
	iterationRepo ports.ScenarioIterationRepository,
	ruleRepo ports.RuleRepository,
	testRunRepo ports.TestRunRepository,
	phantomDecisionRepo ports.PhantomDecisionRepository,
	phantomRuleExecRepo ports.PhantomRuleExecutionRepository,
) TestRunService {
	return TestRunService{
		txManager:           txManager,
		idGen:               idGen,
		clock:               clock,
		scenarioRepo:        scenarioRepo,
		iterationRepo:       iterationRepo,
		ruleRepo:            ruleRepo,
		testRunRepo:         testRunRepo,
		phantomDecisionRepo: phantomDecisionRepo,
		phantomRuleExecRepo: phantomRuleExecRepo,
	}
}

func (s TestRunService) Create(ctx context.Context, tenantID, scenarioID, phantomIterationID string, expiresAt time.Time) (scenario.TestRun, error) {
	scn, err := s.scenarioRepo.GetByID(ctx, tenantID, scenarioID)
	if err != nil {
		return scenario.TestRun{}, err
	}
	if scn.LiveIterationID == nil {
		return scenario.TestRun{}, fmt.Errorf("scenario has no live iteration")
	}
	if *scn.LiveIterationID == phantomIterationID {
		return scenario.TestRun{}, fmt.Errorf("phantom iteration must differ from live iteration")
	}
	if _, err := s.iterationRepo.GetByID(ctx, tenantID, scenarioID, phantomIterationID); err != nil {
		return scenario.TestRun{}, err
	}

	now := s.clock.Now()
	item := scenario.TestRun{
		ID:                 s.idGen.New().String(),
		TenantID:           tenantID,
		ScenarioID:         scenarioID,
		LiveIterationID:    *scn.LiveIterationID,
		PhantomIterationID: phantomIterationID,
		Status:             scenario.TestRunStatusUp,
		CreatedAt:          now,
		ExpiresAt:          expiresAt,
		UpdatedAt:          now,
	}
	var created scenario.TestRun
	err = s.txManager.Run(ctx, func(store ports.MutationStore) error {
		var err error
		created, err = store.TestRuns().Create(ctx, item)
		return err
	})
	return created, err
}

func (s TestRunService) ListByScenario(ctx context.Context, tenantID, scenarioID string) ([]scenario.TestRun, error) {
	return s.testRunRepo.ListByScenario(ctx, tenantID, scenarioID)
}

func (s TestRunService) GetByID(ctx context.Context, tenantID, testRunID string) (scenario.TestRun, error) {
	return s.testRunRepo.GetByID(ctx, tenantID, testRunID)
}

func (s TestRunService) Cancel(ctx context.Context, tenantID, testRunID string) (scenario.TestRun, error) {
	return s.testRunRepo.UpdateStatus(ctx, tenantID, testRunID, scenario.TestRunStatusDown, s.clock.Now())
}

func (s TestRunService) Evaluate(ctx context.Context, tenantID, testRunID string, req DecisionEvaluationRequest) (TestRunEvaluationResult, error) {
	tr, err := s.testRunRepo.GetByID(ctx, tenantID, testRunID)
	if err != nil {
		return TestRunEvaluationResult{}, err
	}
	if tr.Status != scenario.TestRunStatusUp {
		return TestRunEvaluationResult{}, fmt.Errorf("test run is not active")
	}
	if s.clock.Now().After(tr.ExpiresAt) {
		return TestRunEvaluationResult{}, fmt.Errorf("test run is expired")
	}

	liveResult, err := evaluateScenarioByIteration(ctx, s.idGen, s.clock, tr.TenantID, tr.ScenarioID, tr.LiveIterationID, req, s.iterationRepo, s.ruleRepo)
	if err != nil {
		return TestRunEvaluationResult{}, err
	}
	phantomEval, phantomRuleExecs, err := evaluatePhantomByIteration(ctx, s.idGen, s.clock, tr.TenantID, tr.ScenarioID, tr.PhantomIterationID, tr.ID, req, s.iterationRepo, s.ruleRepo)
	if err != nil {
		return TestRunEvaluationResult{}, err
	}

	var storedPhantom decision.PhantomDecision
	var storedExecs []decision.PhantomRuleExecution
	if phantomEval != nil {
		err = s.txManager.Run(ctx, func(store ports.MutationStore) error {
			var err error
			storedPhantom, err = store.PhantomDecisions().Create(ctx, *phantomEval)
			if err != nil {
				return err
			}
			storedExecs, err = store.PhantomRuleExecutions().CreateMany(ctx, phantomRuleExecs)
			return err
		})
		if err != nil {
			return TestRunEvaluationResult{}, err
		}
		_ = storedPhantom
		_ = storedExecs
	}

	phantomResult := DecisionEvaluationResult{Triggered: false}
	if phantomEval != nil {
		d := decision.Decision{
			ID:                  storedPhantom.ID,
			TenantID:            storedPhantom.TenantID,
			ScenarioID:          storedPhantom.ScenarioID,
			ScenarioIterationID: storedPhantom.ScenarioIterationID,
			ObjectID:            storedPhantom.ObjectID,
			ObjectType:          storedPhantom.ObjectType,
			Outcome:             storedPhantom.Outcome,
			Score:               storedPhantom.Score,
			Triggered:           storedPhantom.Triggered,
			CreatedAt:           storedPhantom.CreatedAt,
		}
		phantomRuleResults := make([]decision.RuleExecution, len(storedExecs))
		for i, item := range storedExecs {
			phantomRuleResults[i] = decision.RuleExecution{
				ID:            item.ID,
				DecisionID:    item.PhantomDecisionID,
				RuleID:        item.RuleID,
				RuleName:      item.RuleName,
				Outcome:       item.Outcome,
				ScoreModifier: item.ScoreModifier,
				CreatedAt:     item.CreatedAt,
			}
		}
		phantomResult = DecisionEvaluationResult{
			Triggered:      true,
			Decision:       &d,
			RuleExecutions: phantomRuleResults,
		}
	}

	return TestRunEvaluationResult{
		Live:    liveResult,
		Phantom: phantomResult,
	}, nil
}

func (s TestRunService) DecisionSummaries(ctx context.Context, tenantID, testRunID string) ([]TestRunDecisionSummary, error) {
	items, err := s.phantomDecisionRepo.ListByTestRun(ctx, tenantID, testRunID)
	if err != nil {
		return nil, err
	}
	byKey := make(map[string]*TestRunDecisionSummary)
	for _, item := range items {
		key := fmt.Sprintf("%s|%d", item.Outcome, item.Score)
		current := byKey[key]
		if current == nil {
			current = &TestRunDecisionSummary{
				Outcome: string(item.Outcome),
				Score:   item.Score,
			}
			byKey[key] = current
		}
		current.Count++
	}
	out := make([]TestRunDecisionSummary, 0, len(byKey))
	for _, item := range byKey {
		out = append(out, *item)
	}
	return out, nil
}

func (s TestRunService) RuleStats(ctx context.Context, tenantID, testRunID string) ([]TestRunRuleStat, error) {
	decisions, err := s.phantomDecisionRepo.ListByTestRun(ctx, tenantID, testRunID)
	if err != nil {
		return nil, err
	}
	byRule := make(map[string]*TestRunRuleStat)
	for _, phantomDecision := range decisions {
		executions, err := s.phantomRuleExecRepo.ListByPhantomDecision(ctx, tenantID, phantomDecision.ID)
		if err != nil {
			return nil, err
		}
		for _, exec := range executions {
			current := byRule[exec.RuleID]
			if current == nil {
				current = &TestRunRuleStat{
					RuleID:   exec.RuleID,
					RuleName: exec.RuleName,
				}
				byRule[exec.RuleID] = current
			}
			switch exec.Outcome {
			case "hit":
				current.HitCount++
			case "snoozed":
				current.SnoozedCount++
			default:
				current.NoHitCount++
			}
			current.TotalCount++
		}
	}
	out := make([]TestRunRuleStat, 0, len(byRule))
	for _, item := range byRule {
		out = append(out, *item)
	}
	return out, nil
}

func evaluateScenarioByIteration(
	ctx context.Context,
	idGen ports.IDGenerator,
	clock ports.Clock,
	tenantID, scenarioID, iterationID string,
	req DecisionEvaluationRequest,
	iterationRepo ports.ScenarioIterationRepository,
	ruleRepo ports.RuleRepository,
) (DecisionEvaluationResult, error) {
	iteration, err := iterationRepo.GetByID(ctx, tenantID, scenarioID, iterationID)
	if err != nil {
		return DecisionEvaluationResult{}, err
	}
	runtime := asteval.Runtime{
		TenantID:   tenantID,
		ObjectID:   req.ObjectID,
		ObjectType: req.ObjectType,
		Fields:     req.Fields,
	}
	triggered, err := asteval.EvaluateFormula(ctx, iteration.TriggerFormula, runtime)
	if err != nil {
		return DecisionEvaluationResult{}, err
	}
	if !triggered {
		return DecisionEvaluationResult{Triggered: false}, nil
	}
	rules, err := ruleRepo.ListByIteration(ctx, tenantID, scenarioID, iterationID)
	if err != nil {
		return DecisionEvaluationResult{}, err
	}
	now := clock.Now()
	score := 0
	ruleExecs := make([]decision.RuleExecution, 0, len(rules))
	decisionID := idGen.New().String()
	for _, rule := range rules {
		matched, err := asteval.EvaluateFormula(ctx, rule.Formula, runtime)
		if err != nil {
			return DecisionEvaluationResult{}, err
		}
		if matched {
			score += rule.ScoreModifier
		}
		exec := newRuleExecution(now, decisionID, rule, matched)
		exec.ID = idGen.New().String()
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
	return DecisionEvaluationResult{Triggered: true, Decision: &item, RuleExecutions: ruleExecs}, nil
}

func evaluatePhantomByIteration(
	ctx context.Context,
	idGen ports.IDGenerator,
	clock ports.Clock,
	tenantID, scenarioID, iterationID, testRunID string,
	req DecisionEvaluationRequest,
	iterationRepo ports.ScenarioIterationRepository,
	ruleRepo ports.RuleRepository,
) (*decision.PhantomDecision, []decision.PhantomRuleExecution, error) {
	iteration, err := iterationRepo.GetByID(ctx, tenantID, scenarioID, iterationID)
	if err != nil {
		return nil, nil, err
	}
	runtime := asteval.Runtime{
		TenantID:   tenantID,
		ObjectID:   req.ObjectID,
		ObjectType: req.ObjectType,
		Fields:     req.Fields,
	}
	triggered, err := asteval.EvaluateFormula(ctx, iteration.TriggerFormula, runtime)
	if err != nil {
		return nil, nil, err
	}
	if !triggered {
		return nil, nil, nil
	}
	rules, err := ruleRepo.ListByIteration(ctx, tenantID, scenarioID, iterationID)
	if err != nil {
		return nil, nil, err
	}
	now := clock.Now()
	score := 0
	phantomID := idGen.New().String()
	ruleExecs := make([]decision.PhantomRuleExecution, 0, len(rules))
	for _, rule := range rules {
		matched, err := asteval.EvaluateFormula(ctx, rule.Formula, runtime)
		if err != nil {
			return nil, nil, err
		}
		if matched {
			score += rule.ScoreModifier
		}
		outcome := "no_hit"
		if matched {
			outcome = "hit"
		}
		ruleExecs = append(ruleExecs, decision.PhantomRuleExecution{
			ID:                idGen.New().String(),
			PhantomDecisionID: phantomID,
			RuleID:            rule.ID,
			RuleName:          rule.Name,
			Outcome:           outcome,
			ScoreModifier:     rule.ScoreModifier,
			CreatedAt:         now,
		})
	}
	item := &decision.PhantomDecision{
		ID:                  phantomID,
		TestRunID:           testRunID,
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
	return item, ruleExecs, nil
}
