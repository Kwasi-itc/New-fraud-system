package mysql

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/decision"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

type PhantomDecisionRepository struct{ authoringRepository }
type PhantomRuleExecutionRepository struct{ authoringRepository }

var _ ports.PhantomDecisionRepository = PhantomDecisionRepository{}
var _ ports.PhantomRuleExecutionRepository = PhantomRuleExecutionRepository{}

func (r PhantomDecisionRepository) selectSQL() string {
	return "SELECT id,test_run_id,tenant_id,scenario_id,scenario_iteration_id,score,triggered,created_at," + r.projection("object_id", "object_type", "outcome") + " FROM " + r.schema + ".decision_engine_phantom_decisions"
}
func (r PhantomDecisionRepository) scan(row rowScanner) (decision.PhantomDecision, int64, error) {
	var id, run, tenant, parent, iteration mysqlstore.UUID
	var score mysqlstore.Int32
	var triggered mysqlstore.Bool
	var at mysqlstore.DateTime
	var size int64
	var object, kind, outcome mysqlstore.Text
	if err := row.Scan(&id, &run, &tenant, &parent, &iteration, &score, &triggered, &at, &size, &object, &kind, &outcome); err != nil {
		return decision.PhantomDecision{}, 0, err
	}
	return decision.PhantomDecision{ID: uuid.UUID(id).String(), TestRunID: uuid.UUID(run).String(), TenantID: uuid.UUID(tenant).String(), ScenarioID: uuid.UUID(parent).String(), ScenarioIterationID: uuid.UUID(iteration).String(), Score: int(score), Triggered: bool(triggered), CreatedAt: at.Time, ObjectID: string(object), ObjectType: string(kind), Outcome: decision.Outcome(outcome)}, size, nil
}
func (r PhantomDecisionRepository) Create(ctx context.Context, v decision.PhantomDecision) (decision.PhantomDecision, error) {
	id, err := r.creation(v.TenantID, v.ID, v.CreatedAt, v.ObjectID, v.ObjectType, string(v.Outcome))
	if err != nil {
		return decision.PhantomDecision{}, err
	}
	args, err := r.ids(v.TenantID, v.TestRunID, v.ScenarioID, v.ScenarioIterationID)
	if err != nil {
		return decision.PhantomDecision{}, err
	}
	score, err := checkedInt(v.Score)
	if err != nil {
		return decision.PhantomDecision{}, err
	}
	values := []any{id, args[0], args[1], args[2], args[3], mysqlstore.Text(v.ObjectID), mysqlstore.Text(v.ObjectType), mysqlstore.Text(v.Outcome), score, mysqlstore.Bool(v.Triggered), mysqlstore.DateTime{Time: v.CreatedAt}}
	if _, err := r.tx.ExecContext(ctx, "INSERT INTO "+r.schema+".decision_engine_phantom_decisions(id,tenant_id,test_run_id,scenario_id,scenario_iteration_id,object_id,object_type,outcome,score,triggered,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)", values...); err != nil {
		return decision.PhantomDecision{}, err
	}
	return platformOne(r.platformRepository, r.tx.QueryRowContext(ctx, r.selectSQL()+" WHERE tenant_id=? AND id=?", args[0], id), r.scan)
}
func (r PhantomDecisionRepository) ListByTestRun(ctx context.Context, t, run string) ([]decision.PhantomDecision, error) {
	args, err := r.ids(t, run)
	if err != nil {
		return nil, err
	}
	return platformList(ctx, r.platformRepository, r.selectSQL()+" WHERE tenant_id=? AND test_run_id=? ORDER BY created_at DESC,id DESC", args, r.scan)
}
func (r PhantomRuleExecutionRepository) selectSQL() string {
	return "SELECT id,phantom_decision_id,rule_id,score_modifier,created_at," + r.projection("rule_name", "outcome") + " FROM " + r.schema + ".decision_engine_phantom_rule_executions"
}
func (r PhantomRuleExecutionRepository) scan(row rowScanner) (decision.PhantomRuleExecution, int64, error) {
	var id, parent, rule mysqlstore.UUID
	var score mysqlstore.Int32
	var at mysqlstore.DateTime
	var size int64
	var name, outcome mysqlstore.Text
	if err := row.Scan(&id, &parent, &rule, &score, &at, &size, &name, &outcome); err != nil {
		return decision.PhantomRuleExecution{}, 0, err
	}
	return decision.PhantomRuleExecution{ID: uuid.UUID(id).String(), PhantomDecisionID: uuid.UUID(parent).String(), RuleID: uuid.UUID(rule).String(), ScoreModifier: int(score), CreatedAt: at.Time, RuleName: string(name), Outcome: string(outcome)}, size, nil
}
func (r PhantomRuleExecutionRepository) ListByPhantomDecision(ctx context.Context, t, parent string) ([]decision.PhantomRuleExecution, error) {
	args, err := r.ids(t, parent)
	if err != nil {
		return nil, err
	}
	return platformList(ctx, r.platformRepository, r.selectSQL()+" WHERE tenant_id=? AND phantom_decision_id=? ORDER BY created_at,id", args, r.scan)
}
func (r PhantomRuleExecutionRepository) CreateMany(ctx context.Context, items []decision.PhantomRuleExecution) ([]decision.PhantomRuleExecution, error) {
	if err := r.mutating(); err != nil {
		return nil, err
	}
	if len(items) > r.maxRows {
		return nil, ErrProjectionLimit
	}
	values := make([][]any, len(items))
	parents := map[string]bool{}
	seen := map[string]bool{}
	used := 0
	for i, v := range items {
		id, err := r.creation(r.tenant.String(), v.ID, v.CreatedAt, v.RuleName, v.Outcome)
		if err != nil {
			return nil, err
		}
		args, err := r.ids(r.tenant.String(), v.PhantomDecisionID, v.RuleID)
		if err != nil {
			return nil, err
		}
		if seen[v.ID] {
			return nil, errors.New("duplicate phantom evidence identity")
		}
		seen[v.ID] = true
		parents[v.PhantomDecisionID] = true
		score, err := checkedInt(v.ScoreModifier)
		if err != nil {
			return nil, err
		}
		size := len(v.RuleName) + len(v.Outcome)
		if size > r.maxBytes-used {
			return nil, ErrProjectionLimit
		}
		used += size
		values[i] = []any{id, args[0], nil, args[1], args[2], mysqlstore.Text(v.RuleName), mysqlstore.Text(v.Outcome), score, mysqlstore.DateTime{Time: v.CreatedAt}}
	}
	keys := make([]string, 0, len(parents))
	for key := range parents {
		keys = append(keys, key)
	}
	bindings, err := r.evidenceBindings(ctx, "decision_engine_phantom_decisions", keys)
	if err != nil {
		return nil, err
	}
	out := make([]decision.PhantomRuleExecution, 0, len(items))
	for start := 0; start < len(items); start += 100 {
		end := min(start+100, len(items))
		args := []any{}
		marks := []string{}
		readArgs := []any{mysqlstore.UUID(r.tenant)}
		readMarks := []string{}
		for i := start; i < end; i++ {
			values[i][2] = bindings[items[i].PhantomDecisionID]
			args = append(args, values[i]...)
			marks = append(marks, "(?,?,?,?,?,?,?,?,?)")
			readArgs = append(readArgs, values[i][0])
			readMarks = append(readMarks, "?")
		}
		if _, err := r.tx.ExecContext(ctx, "INSERT INTO "+r.schema+".decision_engine_phantom_rule_executions(id,tenant_id,scenario_iteration_id,phantom_decision_id,rule_id,rule_name,outcome,score_modifier,created_at) VALUES"+strings.Join(marks, ","), args...); err != nil {
			return nil, err
		}
		stored, err := platformList(ctx, r.platformRepository, r.selectSQL()+" WHERE tenant_id=? AND id IN ("+strings.Join(readMarks, ",")+") ORDER BY id", readArgs, r.scan)
		if err != nil {
			return nil, err
		}
		byID := map[string]decision.PhantomRuleExecution{}
		for _, v := range stored {
			byID[v.ID] = v
		}
		for i := start; i < end; i++ {
			v, ok := byID[items[i].ID]
			if !ok {
				return nil, sql.ErrNoRows
			}
			out = append(out, v)
		}
	}
	return out, nil
}
