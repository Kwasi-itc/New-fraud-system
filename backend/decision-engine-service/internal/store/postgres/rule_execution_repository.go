package postgres

import (
	"context"
	"encoding/json"

	domainast "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/ast"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/decision"
)

type RuleExecutionRepository struct{ q queryable }

func NewRuleExecutionRepository(q queryable) RuleExecutionRepository {
	return RuleExecutionRepository{q: q}
}

func (r RuleExecutionRepository) CreateMany(ctx context.Context, items []decision.RuleExecution) ([]decision.RuleExecution, error) {
	if len(items) == 0 {
		return []decision.RuleExecution{}, nil
	}
	out := make([]decision.RuleExecution, 0, len(items))
	const stmt = `
		insert into core.rule_executions (
			id, decision_id, rule_id, rule_name, outcome, score_modifier, evaluation, created_at
		) values ($1,$2,$3,$4,$5,$6,$7,$8)
		returning id, decision_id, rule_id, rule_name, outcome, score_modifier, evaluation, created_at
	`
	for _, item := range items {
		evaluationJSON, err := marshalRuleEvaluation(item.Evaluation)
		if err != nil {
			return nil, err
		}
		var stored decision.RuleExecution
		var evaluationBytes []byte
		err = r.q.QueryRow(
			ctx,
			stmt,
			item.ID,
			item.DecisionID,
			item.RuleID,
			item.RuleName,
			item.Outcome,
			item.ScoreModifier,
			evaluationJSON,
			item.CreatedAt,
		).Scan(
			&stored.ID,
			&stored.DecisionID,
			&stored.RuleID,
			&stored.RuleName,
			&stored.Outcome,
			&stored.ScoreModifier,
			&evaluationBytes,
			&stored.CreatedAt,
		)
		if err != nil {
			return nil, err
		}
		stored.Evaluation, err = unmarshalRuleEvaluation(evaluationBytes)
		if err != nil {
			return nil, err
		}
		out = append(out, stored)
	}
	return out, nil
}

func (r RuleExecutionRepository) ListByDecision(ctx context.Context, tenantID, decisionID string) ([]decision.RuleExecution, error) {
	const stmt = `
		select re.id, re.decision_id, re.rule_id, re.rule_name, re.outcome, re.score_modifier, re.evaluation, re.created_at
		from core.rule_executions re
		join core.decisions d on d.id = re.decision_id
		where d.tenant_id = $1 and re.decision_id = $2
		order by re.created_at asc
	`
	rows, err := r.q.Query(ctx, stmt, tenantID, decisionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []decision.RuleExecution
	for rows.Next() {
		var item decision.RuleExecution
		var evaluationBytes []byte
		if err := rows.Scan(
			&item.ID,
			&item.DecisionID,
			&item.RuleID,
			&item.RuleName,
			&item.Outcome,
			&item.ScoreModifier,
			&evaluationBytes,
			&item.CreatedAt,
		); err != nil {
			return nil, err
		}
		item.Evaluation, err = unmarshalRuleEvaluation(evaluationBytes)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func marshalRuleEvaluation(evaluation any) ([]byte, error) {
	if evaluation == nil {
		return nil, nil
	}
	return json.Marshal(evaluation)
}

func unmarshalRuleEvaluation(raw []byte) (*domainast.EvaluationNode, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var evaluation domainast.EvaluationNode
	if err := json.Unmarshal(raw, &evaluation); err != nil {
		return nil, err
	}
	return &evaluation, nil
}
