package postgres

import (
	"context"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/decision"
)

type PhantomRuleExecutionRepository struct{ q queryable }

func NewPhantomRuleExecutionRepository(q queryable) PhantomRuleExecutionRepository {
	return PhantomRuleExecutionRepository{q: q}
}

func (r PhantomRuleExecutionRepository) CreateMany(ctx context.Context, items []decision.PhantomRuleExecution) ([]decision.PhantomRuleExecution, error) {
	if len(items) == 0 {
		return []decision.PhantomRuleExecution{}, nil
	}
	out := make([]decision.PhantomRuleExecution, 0, len(items))
	const stmt = `
		insert into core.phantom_rule_executions (
			id, phantom_decision_id, rule_id, rule_name, outcome, score_modifier, created_at
		) values ($1,$2,$3,$4,$5,$6,$7)
		returning id, phantom_decision_id, rule_id, rule_name, outcome, score_modifier, created_at
	`
	for _, item := range items {
		var stored decision.PhantomRuleExecution
		err := r.q.QueryRow(ctx, stmt, item.ID, item.PhantomDecisionID, item.RuleID, item.RuleName, item.Outcome, item.ScoreModifier, item.CreatedAt).
			Scan(&stored.ID, &stored.PhantomDecisionID, &stored.RuleID, &stored.RuleName, &stored.Outcome, &stored.ScoreModifier, &stored.CreatedAt)
		if err != nil {
			return nil, err
		}
		out = append(out, stored)
	}
	return out, nil
}

func (r PhantomRuleExecutionRepository) ListByPhantomDecision(ctx context.Context, tenantID, phantomDecisionID string) ([]decision.PhantomRuleExecution, error) {
	const stmt = `
		select pre.id, pre.phantom_decision_id, pre.rule_id, pre.rule_name, pre.outcome, pre.score_modifier, pre.created_at
		from core.phantom_rule_executions pre
		join core.phantom_decisions pd on pd.id = pre.phantom_decision_id
		where pd.tenant_id = $1 and pre.phantom_decision_id = $2
		order by pre.created_at asc
	`
	rows, err := r.q.Query(ctx, stmt, tenantID, phantomDecisionID)
	if err != nil { return nil, err }
	defer rows.Close()
	var items []decision.PhantomRuleExecution
	for rows.Next() {
		var item decision.PhantomRuleExecution
		if err := rows.Scan(&item.ID, &item.PhantomDecisionID, &item.RuleID, &item.RuleName, &item.Outcome, &item.ScoreModifier, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
