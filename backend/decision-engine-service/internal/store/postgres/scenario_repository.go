package postgres

import (
	"context"
	"errors"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5"
)

type queryable interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

type ScenarioRepository struct {
	q queryable
}

func NewScenarioRepository(q queryable) ScenarioRepository {
	return ScenarioRepository{q: q}
}

func (r ScenarioRepository) Create(ctx context.Context, item scenario.Scenario) (scenario.Scenario, error) {
	const stmt = `
		insert into core.scenarios (
			id, tenant_id, name, trigger_object_type, live_iteration_id, created_at, updated_at
		) values ($1, $2, $3, $4, $5, $6, $7)
		returning id, tenant_id, name, trigger_object_type, live_iteration_id, created_at, updated_at
	`
	var out scenario.Scenario
	err := r.q.QueryRow(
		ctx,
		stmt,
		item.ID,
		item.TenantID,
		item.Name,
		item.TriggerObjectType,
		item.LiveIterationID,
		item.CreatedAt,
		item.UpdatedAt,
	).Scan(
		&out.ID,
		&out.TenantID,
		&out.Name,
		&out.TriggerObjectType,
		&out.LiveIterationID,
		&out.CreatedAt,
		&out.UpdatedAt,
	)
	return out, err
}

func (r ScenarioRepository) ListByTenant(ctx context.Context, tenantID string) ([]scenario.Scenario, error) {
	const stmt = `
		select id, tenant_id, name, trigger_object_type, live_iteration_id, created_at, updated_at
		from core.scenarios
		where tenant_id = $1
		order by created_at desc
	`
	rows, err := r.q.Query(ctx, stmt, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []scenario.Scenario
	for rows.Next() {
		var item scenario.Scenario
		if err := rows.Scan(
			&item.ID,
			&item.TenantID,
			&item.Name,
			&item.TriggerObjectType,
			&item.LiveIterationID,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r ScenarioRepository) ListLiveByTriggerObject(ctx context.Context, tenantID, objectType string) ([]scenario.Scenario, error) {
	const stmt = `
		select id, tenant_id, name, trigger_object_type, live_iteration_id, created_at, updated_at
		from core.scenarios
		where tenant_id = $1 and trigger_object_type = $2 and live_iteration_id is not null
		order by created_at desc
	`
	rows, err := r.q.Query(ctx, stmt, tenantID, objectType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []scenario.Scenario
	for rows.Next() {
		var item scenario.Scenario
		if err := rows.Scan(
			&item.ID,
			&item.TenantID,
			&item.Name,
			&item.TriggerObjectType,
			&item.LiveIterationID,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r ScenarioRepository) GetByID(ctx context.Context, tenantID, scenarioID string) (scenario.Scenario, error) {
	const stmt = `
		select id, tenant_id, name, trigger_object_type, live_iteration_id, created_at, updated_at
		from core.scenarios
		where tenant_id = $1 and id = $2
	`
	var item scenario.Scenario
	err := r.q.QueryRow(ctx, stmt, tenantID, scenarioID).Scan(
		&item.ID,
		&item.TenantID,
		&item.Name,
		&item.TriggerObjectType,
		&item.LiveIterationID,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return scenario.Scenario{}, err
	}
	return item, err
}

func (r ScenarioRepository) Update(ctx context.Context, item scenario.Scenario) (scenario.Scenario, error) {
	const stmt = `
		update core.scenarios
		set name = $1,
			trigger_object_type = $2,
			live_iteration_id = $3,
			updated_at = $4
		where tenant_id = $5 and id = $6
		returning id, tenant_id, name, trigger_object_type, live_iteration_id, created_at, updated_at
	`
	var out scenario.Scenario
	err := r.q.QueryRow(
		ctx,
		stmt,
		item.Name,
		item.TriggerObjectType,
		item.LiveIterationID,
		item.UpdatedAt,
		item.TenantID,
		item.ID,
	).Scan(
		&out.ID,
		&out.TenantID,
		&out.Name,
		&out.TriggerObjectType,
		&out.LiveIterationID,
		&out.CreatedAt,
		&out.UpdatedAt,
	)
	return out, err
}

func (r ScenarioRepository) SetLiveIterationID(ctx context.Context, tenantID, scenarioID string, iterationID *string) error {
	const stmt = `
		update core.scenarios
		set live_iteration_id = $1, updated_at = now()
		where tenant_id = $2 and id = $3
	`
	_, err := r.q.Exec(ctx, stmt, iterationID, tenantID, scenarioID)
	return err
}
