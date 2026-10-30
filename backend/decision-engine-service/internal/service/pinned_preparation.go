package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"

	domainast "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/ast"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	asteval "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/runtime/ast_eval"
)

// PinnedScenarioSnapshot must originate from an authorized owner read snapshot,
// NEVER from caller JSON. Committed iteration and rules are immutable; current
// workflow routing and historical reads retain the source evaluation contract.
// An unpublish or a newer live iteration does not redirect this pinned attempt.
type PinnedScenarioSnapshot struct {
	scenario  scenario.Scenario
	iteration scenario.Iteration
	rules     []scenario.Rule
}

func NewPinnedScenarioSnapshot(parent scenario.Scenario, iteration scenario.Iteration, rules []scenario.Rule, limits ExecutionPreparationLimits) (PinnedScenarioSnapshot, error) {
	if err := limits.Validate(); err != nil {
		return PinnedScenarioSnapshot{}, err
	}
	if parent.ID == "" || parent.TenantID == "" || parent.TriggerObjectType == "" || iteration.ID == "" || iteration.TenantID != parent.TenantID || iteration.ScenarioID != parent.ID || iteration.Status != scenario.IterationStatusCommitted || iteration.CommittedAt == nil || iteration.ScoreReviewThreshold == nil || iteration.ScoreBlockAndReviewThreshold == nil || iteration.ScoreDeclineThreshold == nil {
		return PinnedScenarioSnapshot{}, ErrExecutionPreparation
	}
	if err := iteration.Validate(); err != nil {
		return PinnedScenarioSnapshot{}, errors.Join(ErrExecutionPreparation, err)
	}
	if len(rules) > limits.MaxItems {
		return PinnedScenarioSnapshot{}, ErrExecutionPreparationLimit
	}
	used := len(parent.ID) + len(parent.TenantID) + len(parent.Name) + len(parent.Description) + len(parent.TriggerObjectType) + len(iteration.TriggerFormula) + len(iteration.Schedule)
	seen := map[string]bool{}
	for _, rule := range rules {
		if rule.ID == "" || seen[rule.ID] || rule.TenantID != parent.TenantID || rule.IterationID != iteration.ID {
			return PinnedScenarioSnapshot{}, ErrExecutionPreparation
		}
		seen[rule.ID] = true
		used += len(rule.Formula) + len(rule.Name) + len(rule.Description) + len(rule.StableRuleID) + len(rule.RuleGroup) + len(rule.ID) + len(rule.IterationID) + len(rule.TenantID)
		if used > limits.MaxBytes {
			return PinnedScenarioSnapshot{}, ErrExecutionPreparationLimit
		}
	}
	if used > limits.MaxBytes {
		return PinnedScenarioSnapshot{}, ErrExecutionPreparationLimit
	}
	raw, err := json.Marshal(struct {
		Scenario  scenario.Scenario
		Iteration scenario.Iteration
		Rules     []scenario.Rule
	}{parent, iteration, rules})
	if err != nil {
		return PinnedScenarioSnapshot{}, err
	}
	if len(raw) > limits.MaxBytes {
		return PinnedScenarioSnapshot{}, ErrExecutionPreparationLimit
	}
	var detached struct {
		Scenario  scenario.Scenario
		Iteration scenario.Iteration
		Rules     []scenario.Rule
	}
	if err := json.Unmarshal(raw, &detached); err != nil {
		return PinnedScenarioSnapshot{}, err
	}
	return PinnedScenarioSnapshot{detached.Scenario, detached.Iteration, detached.Rules}, nil
}
func (p PinnedScenarioSnapshot) TenantID() string    { return p.scenario.TenantID }
func (p PinnedScenarioSnapshot) ScenarioID() string  { return p.scenario.ID }
func (p PinnedScenarioSnapshot) IterationID() string { return p.iteration.ID }
func (p PinnedScenarioSnapshot) ObjectType() string  { return p.scenario.TriggerObjectType }

// ValidatePinnedModel rechecks old committed formulas against the model used by
// THIS attempt. Published once is not permission to execute an incompatible old
// rule after the model changes. Static failures are local validation errors.
func ValidatePinnedModel(pin PinnedScenarioSnapshot, model ports.TenantModel) error {
	if pin.TenantID() == "" || model.RevisionID == "" {
		return ErrExecutionPreparation
	}
	table, ok := model.Tables[pin.ObjectType()]
	if !ok || table.Archived {
		return ErrExecutionPreparation
	}
	nodes := 0
	var bound func(domainast.Node, int) error
	bound = func(node domainast.Node, depth int) error {
		nodes++
		if depth > 64 || nodes > 10000 {
			return ErrExecutionPreparationLimit
		}
		for _, child := range node.Children {
			if err := bound(child, depth+1); err != nil {
				return err
			}
		}
		for _, child := range node.NamedChildren {
			if err := bound(child, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	validate := func(raw json.RawMessage) error {
		var node domainast.Node
		if err := json.Unmarshal(raw, &node); err != nil {
			return errors.Join(ErrExecutionPreparation, err)
		}
		if err := bound(node, 1); err != nil {
			return err
		}
		kind, issues := asteval.ValidateNode(node, model, pin.ObjectType())
		if kind != domainast.ValueTypeBool || len(issues) > 0 {
			return fmt.Errorf("%w: formula must return boolean: %v", ErrExecutionPreparation, issues)
		}
		return nil
	}
	if err := validate(pin.iteration.TriggerFormula); err != nil {
		return err
	}
	for _, rule := range pin.rules {
		if err := validate(rule.Formula); err != nil {
			return fmt.Errorf("rule %q: %w", rule.Name, err)
		}
	}
	return nil
}

// PreparedDecision contains evaluated local outputs but NO open transaction or
// retained repository. Persist is single-use and must be called on the worker's
// fenced caller transaction; its returned values are provisional until commit.
// On rollback/uncertain commit, reconcile/reprepare; never blindly replay this
// closure. It never evaluates AST or performs model/provider/network reads.
type PreparedDecision struct {
	persist func(context.Context, ports.DecisionMutationStore) (DecisionEvaluationResult, error)
	used    atomic.Bool
	bytes   int
}

// EncodedOutputBytes allows a multi-scenario coordinator to enforce one total
// output budget before any persistence, rather than one budget per scenario.
func (p *PreparedDecision) EncodedOutputBytes() int {
	if p == nil {
		return 0
	}
	return p.bytes
}

func (p *PreparedDecision) Persist(ctx context.Context, store ports.DecisionMutationStore) (DecisionEvaluationResult, error) {
	if p == nil || p.persist == nil || store == nil || !p.used.CompareAndSwap(false, true) {
		return DecisionEvaluationResult{}, ErrExecutionPreparation
	}
	if err := ctx.Err(); err != nil {
		return DecisionEvaluationResult{}, err
	}
	return p.persist(ctx, store)
}

type pinnedEvaluationOptions struct {
	snapshot *PinnedScenarioSnapshot
	capture  *PreparedDecision
}

func (s DecisionService) ValidatePreparationDependencies() error {
	if s.clock == nil || s.idGen == nil || s.snoozeRepo == nil || s.workflowRepo == nil || s.workflowRuleRepo == nil || s.screeningConfigRepo == nil || s.scoringConfigRepo == nil {
		return ErrExecutionPreparation
	}
	return nil
}

// PreparePinnedBatch uses the SAME evaluator and output-building path as live
// evaluation, but never starts its persistence transaction. Pinned metadata
// bypasses live scenario/iteration/rule caches. All items must succeed before
// any output plans are returned; not-triggered items still remain in the batch.
func (s DecisionService) PreparePinnedBatch(ctx context.Context, pin PinnedScenarioSnapshot, batch PreparedObjectBatch) ([]*PreparedDecision, error) {
	if err := s.ValidatePreparationDependencies(); err != nil {
		return nil, err
	}
	if pin.TenantID() == "" || pin.TenantID() != batch.tenant || pin.ObjectType() != batch.objectType || len(batch.items) == 0 || s.clock == nil || s.idGen == nil {
		return nil, ErrExecutionPreparation
	}
	if err := batch.limits.Validate(); err != nil {
		return nil, err
	}
	if err := ValidatePinnedModel(pin, batch.model); err != nil {
		return nil, err
	}
	plans := make([]*PreparedDecision, len(batch.items))
	used := 0
	now := s.clock.Now()
	for i, item := range batch.items {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		plan := &PreparedDecision{}
		// Source items are independent evaluations, not a global historical
		// snapshot. Never reuse payload/aggregate cache entries across items,
		// including repeated object IDs with different incoming field values.
		evalCache, aggregateCache := asteval.NewEvaluationCache(), asteval.NewAggregateResultCache()
		_, err := s.evaluateScenarioWithOptions(ctx, batch.tenant, pin.ScenarioID(), item, evalCache, aggregateCache, now, pinnedEvaluationOptions{&pin, plan})
		if err != nil {
			return nil, err
		}
		used += plan.bytes
		if plan.persist == nil || used > batch.limits.MaxBytes {
			return nil, ErrExecutionPreparationLimit
		}
		plans[i] = plan
	}
	return plans, nil
}
