package postgres

import (
	"context"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
)

type ScenarioPublicationRepository struct {
	q queryable
}

func NewScenarioPublicationRepository(q queryable) ScenarioPublicationRepository {
	return ScenarioPublicationRepository{q: q}
}

func (r ScenarioPublicationRepository) Create(ctx context.Context, item scenario.Publication) (scenario.Publication, error) {
	const stmt = `
		insert into core.scenario_publications (
			id, tenant_id, scenario_id, iteration_id, action, created_at
		) values ($1, $2, $3, $4, $5, $6)
		returning id, tenant_id, scenario_id, iteration_id, action, created_at
	`
	var out scenario.Publication
	var action string
	err := r.q.QueryRow(
		ctx,
		stmt,
		item.ID,
		item.TenantID,
		item.ScenarioID,
		item.IterationID,
		string(item.Action),
		item.CreatedAt,
	).Scan(
		&out.ID,
		&out.TenantID,
		&out.ScenarioID,
		&out.IterationID,
		&action,
		&out.CreatedAt,
	)
	out.Action = scenario.PublicationAction(action)
	return out, err
}

func (r ScenarioPublicationRepository) ListByScenario(ctx context.Context, tenantID, scenarioID string) ([]scenario.Publication, error) {
	const stmt = `
		select id, tenant_id, scenario_id, iteration_id, action, created_at
		from core.scenario_publications
		where tenant_id = $1 and scenario_id = $2
		order by created_at desc
	`
	rows, err := r.q.Query(ctx, stmt, tenantID, scenarioID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []scenario.Publication
	for rows.Next() {
		var item scenario.Publication
		var action string
		if err := rows.Scan(
			&item.ID,
			&item.TenantID,
			&item.ScenarioID,
			&item.IterationID,
			&action,
			&item.CreatedAt,
		); err != nil {
			return nil, err
		}
		item.Action = scenario.PublicationAction(action)
		items = append(items, item)
	}
	return items, rows.Err()
}
