package postgres

import (
	"context"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
)

type TestRunRepository struct{ q queryable }

func NewTestRunRepository(q queryable) TestRunRepository { return TestRunRepository{q: q} }

func (r TestRunRepository) Create(ctx context.Context, item scenario.TestRun) (scenario.TestRun, error) {
	const stmt = `
		insert into core.test_runs (
			id, tenant_id, scenario_id, live_iteration_id, phantom_iteration_id, status, created_at, expires_at, updated_at
		) values ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		returning id, tenant_id, scenario_id, live_iteration_id, phantom_iteration_id, status, created_at, expires_at, updated_at
	`
	var out scenario.TestRun
	var status string
	err := r.q.QueryRow(ctx, stmt, item.ID, item.TenantID, item.ScenarioID, item.LiveIterationID, item.PhantomIterationID, string(item.Status), item.CreatedAt, item.ExpiresAt, item.UpdatedAt).
		Scan(&out.ID, &out.TenantID, &out.ScenarioID, &out.LiveIterationID, &out.PhantomIterationID, &status, &out.CreatedAt, &out.ExpiresAt, &out.UpdatedAt)
	out.Status = scenario.TestRunStatus(status)
	return out, err
}

func (r TestRunRepository) GetByID(ctx context.Context, tenantID, testRunID string) (scenario.TestRun, error) {
	const stmt = `
		select id, tenant_id, scenario_id, live_iteration_id, phantom_iteration_id, status, created_at, expires_at, updated_at
		from core.test_runs where tenant_id = $1 and id = $2
	`
	var out scenario.TestRun
	var status string
	err := r.q.QueryRow(ctx, stmt, tenantID, testRunID).
		Scan(&out.ID, &out.TenantID, &out.ScenarioID, &out.LiveIterationID, &out.PhantomIterationID, &status, &out.CreatedAt, &out.ExpiresAt, &out.UpdatedAt)
	out.Status = scenario.TestRunStatus(status)
	return out, err
}

func (r TestRunRepository) ListByScenario(ctx context.Context, tenantID, scenarioID string) ([]scenario.TestRun, error) {
	const stmt = `
		select id, tenant_id, scenario_id, live_iteration_id, phantom_iteration_id, status, created_at, expires_at, updated_at
		from core.test_runs where tenant_id = $1 and scenario_id = $2
		order by created_at desc
	`
	rows, err := r.q.Query(ctx, stmt, tenantID, scenarioID)
	if err != nil { return nil, err }
	defer rows.Close()
	var items []scenario.TestRun
	for rows.Next() {
		var item scenario.TestRun
		var status string
		if err := rows.Scan(&item.ID, &item.TenantID, &item.ScenarioID, &item.LiveIterationID, &item.PhantomIterationID, &status, &item.CreatedAt, &item.ExpiresAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		item.Status = scenario.TestRunStatus(status)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r TestRunRepository) UpdateStatus(ctx context.Context, tenantID, testRunID string, status scenario.TestRunStatus, updatedAt time.Time) (scenario.TestRun, error) {
	const stmt = `
		update core.test_runs set status = $1, updated_at = $2
		where tenant_id = $3 and id = $4
		returning id, tenant_id, scenario_id, live_iteration_id, phantom_iteration_id, status, created_at, expires_at, updated_at
	`
	var out scenario.TestRun
	var statusStr string
	err := r.q.QueryRow(ctx, stmt, string(status), updatedAt, tenantID, testRunID).
		Scan(&out.ID, &out.TenantID, &out.ScenarioID, &out.LiveIterationID, &out.PhantomIterationID, &statusStr, &out.CreatedAt, &out.ExpiresAt, &out.UpdatedAt)
	out.Status = scenario.TestRunStatus(statusStr)
	return out, err
}
