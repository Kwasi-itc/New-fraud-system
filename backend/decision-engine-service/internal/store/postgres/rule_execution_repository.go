package postgres

import (
	"context"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/decision"
)

type RuleExecutionRepository struct{ q queryable }

func NewRuleExecutionRepository(q queryable) RuleExecutionRepository { return RuleExecutionRepository{q: q} }

func (r RuleExecutionRepository) CreateMany(ctx context.Context, items []decision.RuleExecution) ([]decision.RuleExecution, error) {
	if len(items) == 0 {
		return []decision.RuleExecution{}, nil
	}
	out := make([]decision.RuleExecution, 0, len(items))
	const stmt = `
		insert into core.rule_executions (
			id, decision_id, rule_id, rule_name, outcome, score_modifier, created_at
		) values ($1,$2,$3,$4,$5,$6,$7)
		returning id, decision_id, rule_id, rule_name, outcome, score_modifier, created_at
	`
	for _, item := range items {
		var stored decision.RuleExecution
		err := r.q.QueryRow(ctx, stmt, item.ID, item.DecisionID, item.RuleID, item.RuleName, item.Outcome, item.ScoreModifier, item.CreatedAt).
			Scan(&stored.ID, &stored.DecisionID, &stored.RuleID, &stored.RuleName, &stored.Outcome, &stored.ScoreModifier, &stored.CreatedAt)
		if err != nil {
			return nil, err
		}
		out = append(out, stored)
	}
	return out, nil
}

func (r RuleExecutionRepository) ListByDecision(ctx context.Context, tenantID, decisionID string) ([]decision.RuleExecution, error) {
	const stmt = `
		select re.id, re.decision_id, re.rule_id, re.rule_name, re.outcome, re.score_modifier, re.created_at
		from core.rule_executions re
		join core.decisions d on d.id = re.decision_id
		where d.tenant_id = $1 and re.decision_id = $2
		order by re.created_at asc
	`
	rows, err := r.q.Query(ctx, stmt, tenantID, decisionID)
	if err != nil { return nil, err }
	defer rows.Close()
	var items []decision.RuleExecution
	for rows.Next() {
		var item decision.RuleExecution
		if err := rows.Scan(&item.ID, &item.DecisionID, &item.RuleID, &item.RuleName, &item.Outcome, &item.ScoreModifier, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
