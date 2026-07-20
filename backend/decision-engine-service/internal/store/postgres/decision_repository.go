package postgres

import (
	"context"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/decision"
)

type DecisionRepository struct{ q queryable }

func NewDecisionRepository(q queryable) DecisionRepository { return DecisionRepository{q: q} }

func (r DecisionRepository) Create(ctx context.Context, item decision.Decision) (decision.Decision, error) {
	const stmt = `
		insert into core.decisions (
			id, tenant_id, scenario_id, scenario_iteration_id, object_id, object_type, request_body, outcome, score, triggered, created_at
		) values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		returning id, tenant_id, scenario_id, scenario_iteration_id, object_id, object_type, request_body, outcome, score, triggered, created_at
	`
	var out decision.Decision
	var outcome string
	err := r.q.QueryRow(ctx, stmt, item.ID, item.TenantID, item.ScenarioID, item.ScenarioIterationID, item.ObjectID, item.ObjectType, item.RequestBody, string(item.Outcome), item.Score, item.Triggered, item.CreatedAt).
		Scan(&out.ID, &out.TenantID, &out.ScenarioID, &out.ScenarioIterationID, &out.ObjectID, &out.ObjectType, &out.RequestBody, &outcome, &out.Score, &out.Triggered, &out.CreatedAt)
	out.Outcome = decision.Outcome(outcome)
	return out, err
}

func (r DecisionRepository) GetByID(ctx context.Context, tenantID, decisionID string) (decision.Decision, error) {
	const stmt = `
		select id, tenant_id, scenario_id, scenario_iteration_id, object_id, object_type, request_body, outcome, score, triggered, created_at
		from core.decisions where tenant_id = $1 and id = $2
	`
	var out decision.Decision
	var outcome string
	err := r.q.QueryRow(ctx, stmt, tenantID, decisionID).
		Scan(&out.ID, &out.TenantID, &out.ScenarioID, &out.ScenarioIterationID, &out.ObjectID, &out.ObjectType, &out.RequestBody, &outcome, &out.Score, &out.Triggered, &out.CreatedAt)
	out.Outcome = decision.Outcome(outcome)
	return out, err
}

func (r DecisionRepository) ListByTenant(ctx context.Context, tenantID string) ([]decision.Decision, error) {
	const stmt = `
		select id, tenant_id, scenario_id, scenario_iteration_id, object_id, object_type, request_body, outcome, score, triggered, created_at
		from core.decisions where tenant_id = $1
		order by created_at desc
	`
	rows, err := r.q.Query(ctx, stmt, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []decision.Decision
	for rows.Next() {
		var item decision.Decision
		var outcome string
		if err := rows.Scan(&item.ID, &item.TenantID, &item.ScenarioID, &item.ScenarioIterationID, &item.ObjectID, &item.ObjectType, &item.RequestBody, &outcome, &item.Score, &item.Triggered, &item.CreatedAt); err != nil {
			return nil, err
		}
		item.Outcome = decision.Outcome(outcome)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r DecisionRepository) ListByScenario(ctx context.Context, tenantID, scenarioID string) ([]decision.Decision, error) {
	const stmt = `
		select id, tenant_id, scenario_id, scenario_iteration_id, object_id, object_type, request_body, outcome, score, triggered, created_at
		from core.decisions where tenant_id = $1 and scenario_id = $2
		order by created_at desc
	`
	rows, err := r.q.Query(ctx, stmt, tenantID, scenarioID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []decision.Decision
	for rows.Next() {
		var item decision.Decision
		var outcome string
		if err := rows.Scan(&item.ID, &item.TenantID, &item.ScenarioID, &item.ScenarioIterationID, &item.ObjectID, &item.ObjectType, &item.RequestBody, &outcome, &item.Score, &item.Triggered, &item.CreatedAt); err != nil {
			return nil, err
		}
		item.Outcome = decision.Outcome(outcome)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r DecisionRepository) ListByObject(ctx context.Context, tenantID, objectType, objectID string) ([]decision.Decision, error) {
	const stmt = `
		select id, tenant_id, scenario_id, scenario_iteration_id, object_id, object_type, request_body, outcome, score, triggered, created_at
		from core.decisions where tenant_id = $1 and object_type = $2 and object_id = $3
		order by created_at desc
	`
	rows, err := r.q.Query(ctx, stmt, tenantID, objectType, objectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []decision.Decision
	for rows.Next() {
		var item decision.Decision
		var outcome string
		if err := rows.Scan(&item.ID, &item.TenantID, &item.ScenarioID, &item.ScenarioIterationID, &item.ObjectID, &item.ObjectType, &item.RequestBody, &outcome, &item.Score, &item.Triggered, &item.CreatedAt); err != nil {
			return nil, err
		}
		item.Outcome = decision.Outcome(outcome)
		items = append(items, item)
	}
	return items, rows.Err()
}
