package postgres

import (
	"context"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/decision"
)

type PhantomDecisionRepository struct{ q queryable }

func NewPhantomDecisionRepository(q queryable) PhantomDecisionRepository { return PhantomDecisionRepository{q: q} }

func (r PhantomDecisionRepository) Create(ctx context.Context, item decision.PhantomDecision) (decision.PhantomDecision, error) {
	const stmt = `
		insert into core.phantom_decisions (
			id, test_run_id, tenant_id, scenario_id, scenario_iteration_id, object_id, object_type, outcome, score, triggered, created_at
		) values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		returning id, test_run_id, tenant_id, scenario_id, scenario_iteration_id, object_id, object_type, outcome, score, triggered, created_at
	`
	var out decision.PhantomDecision
	var outcome string
	err := r.q.QueryRow(ctx, stmt, item.ID, item.TestRunID, item.TenantID, item.ScenarioID, item.ScenarioIterationID, item.ObjectID, item.ObjectType, string(item.Outcome), item.Score, item.Triggered, item.CreatedAt).
		Scan(&out.ID, &out.TestRunID, &out.TenantID, &out.ScenarioID, &out.ScenarioIterationID, &out.ObjectID, &out.ObjectType, &outcome, &out.Score, &out.Triggered, &out.CreatedAt)
	out.Outcome = decision.Outcome(outcome)
	return out, err
}

func (r PhantomDecisionRepository) ListByTestRun(ctx context.Context, tenantID, testRunID string) ([]decision.PhantomDecision, error) {
	const stmt = `
		select id, test_run_id, tenant_id, scenario_id, scenario_iteration_id, object_id, object_type, outcome, score, triggered, created_at
		from core.phantom_decisions where tenant_id = $1 and test_run_id = $2
		order by created_at desc
	`
	rows, err := r.q.Query(ctx, stmt, tenantID, testRunID)
	if err != nil { return nil, err }
	defer rows.Close()
	var items []decision.PhantomDecision
	for rows.Next() {
		var item decision.PhantomDecision
		var outcome string
		if err := rows.Scan(&item.ID, &item.TestRunID, &item.TenantID, &item.ScenarioID, &item.ScenarioIterationID, &item.ObjectID, &item.ObjectType, &outcome, &item.Score, &item.Triggered, &item.CreatedAt); err != nil {
			return nil, err
		}
		item.Outcome = decision.Outcome(outcome)
		items = append(items, item)
	}
	return items, rows.Err()
}
