package mysql

import (
	"context"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/decision"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/service"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
	"reflect"
	"sort"
)

type ComparisonSnapshot struct {
	Run           scenario.TestRun
	Live, Phantom ExecutionPreparationSnapshot
}

func comparisonRead(ctx context.Context, r authoringRepository, id uuid.UUID) (ComparisonSnapshot, error) {
	t := r.tenant.String()
	run, err := (TestRunRepository{r}).GetByID(ctx, t, id.String())
	if err != nil {
		return ComparisonSnapshot{}, err
	}
	parent, err := (ScenarioRepository{r}).GetByID(ctx, t, run.ScenarioID)
	if err != nil {
		return ComparisonSnapshot{}, err
	}
	snapshot := func(iteration string) (ExecutionPreparationSnapshot, error) {
		item, err := (ScenarioIterationRepository{r}).GetByID(ctx, t, parent.ID, iteration)
		if err != nil {
			return ExecutionPreparationSnapshot{}, err
		}
		rules, err := (RuleRepository{r}).ListByIteration(ctx, t, parent.ID, iteration)
		if err != nil {
			return ExecutionPreparationSnapshot{}, err
		}
		return ExecutionPreparationSnapshot{parent: parent, iteration: item, rules: rules}, nil
	}
	live, err := snapshot(run.LiveIterationID)
	if err != nil {
		return ComparisonSnapshot{}, err
	}
	phantom, err := snapshot(run.PhantomIterationID)
	return ComparisonSnapshot{run, live, phantom}, err
}
func (s *AdministrationStore) Comparison(ctx context.Context, id uuid.UUID) (ComparisonSnapshot, error) {
	return adminRun(ctx, s, false, AuthoringRead, func(r authoringRepository) (ComparisonSnapshot, error) {
		value, err := comparisonRead(ctx, r, id)
		if err != nil {
			return ComparisonSnapshot{}, err
		}
		if value.Run.Status != scenario.TestRunStatusUp || !value.Run.ExpiresAt.After(s.now()) {
			return ComparisonSnapshot{}, ErrExecutionState
		}
		return value, nil
	})
}
func (s *AdministrationStore) PersistComparison(ctx context.Context, reader TenantDataReader, before ComparisonSnapshot, model ports.TenantModel, phantom *decision.Decision, executions []decision.RuleExecution) error {
	if !sameEvaluationScope(s.configuration, reader.configuration) {
		return ErrTenantScope
	}
	queries := []ports.AggregateQuery{}
	for _, snapshot := range []ExecutionPreparationSnapshot{before.Live, before.Phantom} {
		q, err := aggregatePublicationQueries(ctx, PublicationSnapshot{snapshot.parent, snapshot.iteration, snapshot.rules}, model, s.configuration.maxRows, s.configuration.maxBytes)
		if err != nil {
			return err
		}
		queries = append(queries, q...)
	}
	guard, err := reader.executionRegistryGuard(map[string]bool{before.Live.parent.TriggerObjectType: true}, queries, model)
	if err != nil {
		return err
	}
	_, err = adminRun(ctx, s, true, AuthoringWrite, func(r authoringRepository) (struct{}, error) {
		var locked mysqlstore.UUID
		if err := r.tx.QueryRowContext(ctx, "SELECT id FROM "+r.schema+".decision_engine_test_runs WHERE tenant_id=? AND id=? FOR SHARE", mysqlstore.UUID(r.tenant), mysqlstore.UUID(uuid.MustParse(before.Run.ID))).Scan(&locked); err != nil {
			return struct{}{}, err
		}
		ids := []string{before.Run.LiveIterationID, before.Run.PhantomIterationID}
		sort.Strings(ids)
		for _, id := range ids {
			if err := r.tx.QueryRowContext(ctx, "SELECT id FROM "+r.schema+".decision_engine_scenario_iterations WHERE tenant_id=? AND id=? FOR UPDATE", mysqlstore.UUID(r.tenant), mysqlstore.UUID(uuid.MustParse(id))).Scan(&locked); err != nil {
				return struct{}{}, err
			}
		}
		after, err := comparisonRead(ctx, r, uuid.MustParse(before.Run.ID))
		if err != nil {
			return struct{}{}, err
		}
		if !reflect.DeepEqual(before, after) || after.Run.Status != scenario.TestRunStatusUp || !after.Run.ExpiresAt.After(s.now()) {
			return struct{}{}, ErrExecutionBindingChanged
		}
		if err := reader.configuration.authorize(ctx, r.tx, r.tenant, false); err != nil {
			return struct{}{}, err
		}
		if err := guard(ctx, r.tx); err != nil {
			return struct{}{}, err
		}
		if phantom == nil {
			return struct{}{}, nil
		}
		if phantom.TenantID != r.tenant.String() || phantom.ScenarioID != after.Run.ScenarioID || phantom.ScenarioIterationID != after.Run.PhantomIterationID {
			return struct{}{}, ErrTenantScope
		}
		value := decision.PhantomDecision{ID: phantom.ID, TestRunID: after.Run.ID, TenantID: phantom.TenantID, ScenarioID: phantom.ScenarioID, ScenarioIterationID: phantom.ScenarioIterationID, ObjectID: phantom.ObjectID, ObjectType: phantom.ObjectType, Outcome: phantom.Outcome, Score: phantom.Score, Triggered: phantom.Triggered, CreatedAt: phantom.CreatedAt}
		if _, err := (PhantomDecisionRepository{r}).Create(ctx, value); err != nil {
			return struct{}{}, err
		}
		rules := make([]decision.PhantomRuleExecution, len(executions))
		for i, item := range executions {
			rules[i] = decision.PhantomRuleExecution{ID: item.ID, PhantomDecisionID: value.ID, RuleID: item.RuleID, RuleName: item.RuleName, Outcome: item.Outcome, ScoreModifier: item.ScoreModifier, CreatedAt: item.CreatedAt}
		}
		_, err = (PhantomRuleExecutionRepository{r}).CreateMany(ctx, rules)
		return struct{}{}, err
	})
	return err
}

func (s *AdministrationStore) ComparisonScores(ctx context.Context, id uuid.UUID) ([]service.TestRunDecisionSummary, error) {
	return adminRun(ctx, s, false, AuthoringRead, func(r authoringRepository) ([]service.TestRunDecisionSummary, error) {
		if _, err := (TestRunRepository{r}).GetByID(ctx, r.tenant.String(), id.String()); err != nil {
			return nil, err
		}
		rows, err := r.tx.QueryContext(ctx, "SELECT outcome,score,COUNT(*) FROM "+r.schema+".decision_engine_phantom_decisions WHERE tenant_id=? AND test_run_id=? GROUP BY outcome,score ORDER BY score,outcome LIMIT ?", mysqlstore.UUID(r.tenant), mysqlstore.UUID(id), r.maxRows+1)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		out := []service.TestRunDecisionSummary{}
		used := 0
		for rows.Next() {
			var v service.TestRunDecisionSummary
			if err := rows.Scan(&v.Outcome, &v.Score, &v.Count); err != nil {
				return nil, err
			}
			used += len(v.Outcome)
			if len(out) == r.maxRows || used > r.maxBytes {
				return nil, ErrProjectionLimit
			}
			out = append(out, v)
		}
		return out, rows.Err()
	})
}
func (s *AdministrationStore) ComparisonRules(ctx context.Context, id uuid.UUID) ([]service.TestRunRuleStat, error) {
	return adminRun(ctx, s, false, AuthoringRead, func(r authoringRepository) ([]service.TestRunRuleStat, error) {
		if _, err := (TestRunRepository{r}).GetByID(ctx, r.tenant.String(), id.String()); err != nil {
			return nil, err
		}
		rows, err := r.tx.QueryContext(ctx, "SELECT e.rule_id,e.rule_name,SUM(e.outcome='hit'),SUM(e.outcome='no_hit'),SUM(e.outcome='snoozed'),COUNT(*) FROM "+r.schema+".decision_engine_phantom_rule_executions e JOIN "+r.schema+".decision_engine_phantom_decisions p ON p.id=e.phantom_decision_id WHERE p.tenant_id=? AND p.test_run_id=? GROUP BY e.rule_id,e.rule_name ORDER BY e.rule_id LIMIT ?", mysqlstore.UUID(r.tenant), mysqlstore.UUID(id), r.maxRows+1)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		out := []service.TestRunRuleStat{}
		used := 0
		for rows.Next() {
			var v service.TestRunRuleStat
			var rule mysqlstore.UUID
			if err := rows.Scan(&rule, &v.RuleName, &v.HitCount, &v.NoHitCount, &v.SnoozedCount, &v.TotalCount); err != nil {
				return nil, err
			}
			v.RuleID = uuid.UUID(rule).String()
			used += len(v.RuleName) + len(v.RuleID)
			if len(out) == r.maxRows || used > r.maxBytes {
				return nil, ErrProjectionLimit
			}
			out = append(out, v)
		}
		return out, rows.Err()
	})
}
