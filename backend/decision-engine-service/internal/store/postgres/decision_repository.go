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
	return r.list(ctx, stmt, tenantID)
}

func (r DecisionRepository) ListByTenantPage(ctx context.Context, tenantID string, limit, offset int) ([]decision.Decision, bool, error) {
	const stmt = `
		select id, tenant_id, scenario_id, scenario_iteration_id, object_id, object_type, request_body, outcome, score, triggered, created_at
		from core.decisions where tenant_id = $1
		order by created_at desc
		limit $2 offset $3
	`
	return r.listPage(ctx, stmt, tenantID, limit, offset)
}

func (r DecisionRepository) ListByScenario(ctx context.Context, tenantID, scenarioID string) ([]decision.Decision, error) {
	const stmt = `
		select id, tenant_id, scenario_id, scenario_iteration_id, object_id, object_type, request_body, outcome, score, triggered, created_at
		from core.decisions where tenant_id = $1 and scenario_id = $2
		order by created_at desc
	`
	return r.list(ctx, stmt, tenantID, scenarioID)
}

func (r DecisionRepository) ListByScenarioPage(ctx context.Context, tenantID, scenarioID string, limit, offset int) ([]decision.Decision, bool, error) {
	const stmt = `
		select id, tenant_id, scenario_id, scenario_iteration_id, object_id, object_type, request_body, outcome, score, triggered, created_at
		from core.decisions where tenant_id = $1 and scenario_id = $2
		order by created_at desc
		limit $3 offset $4
	`
	return r.listPage(ctx, stmt, tenantID, scenarioID, limit, offset)
}

func (r DecisionRepository) ListByObject(ctx context.Context, tenantID, objectType, objectID string) ([]decision.Decision, error) {
	const stmt = `
		select id, tenant_id, scenario_id, scenario_iteration_id, object_id, object_type, request_body, outcome, score, triggered, created_at
		from core.decisions where tenant_id = $1 and object_type = $2 and object_id = $3
		order by created_at desc
	`
	return r.list(ctx, stmt, tenantID, objectType, objectID)
}

func (r DecisionRepository) ListByObjectPage(ctx context.Context, tenantID, objectType, objectID string, limit, offset int) ([]decision.Decision, bool, error) {
	const stmt = `
		select id, tenant_id, scenario_id, scenario_iteration_id, object_id, object_type, request_body, outcome, score, triggered, created_at
		from core.decisions where tenant_id = $1 and object_type = $2 and object_id = $3
		order by created_at desc
		limit $4 offset $5
	`
	return r.listPage(ctx, stmt, tenantID, objectType, objectID, limit, offset)
}

func (r DecisionRepository) list(ctx context.Context, stmt string, args ...any) ([]decision.Decision, error) {
	rows, err := r.q.Query(ctx, stmt, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanDecisions(rows)
}

func (r DecisionRepository) listPage(ctx context.Context, stmt string, args ...any) ([]decision.Decision, bool, error) {
	rows, err := r.q.Query(ctx, stmt, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	items, err := scanDecisions(rows)
	if err != nil {
		return nil, false, err
	}
	if len(items) == 0 {
		return items, false, nil
	}
	limit, _ := args[len(args)-2].(int)
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	return items, hasMore, nil
}

func scanDecisions(rows rowScanner) ([]decision.Decision, error) {
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

type rowScanner interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}
