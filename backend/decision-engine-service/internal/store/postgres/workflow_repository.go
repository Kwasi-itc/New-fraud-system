package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
)

type WorkflowRepository struct{ q queryable }

func NewWorkflowRepository(q queryable) WorkflowRepository { return WorkflowRepository{q: q} }

func (r WorkflowRepository) Create(ctx context.Context, item workflow.Definition) (workflow.Definition, error) {
	const stmt = `
		insert into core.workflows (
			id, tenant_id, scenario_id, display_order, name, description, allowed_outcomes, action_type, action_config, active, created_at, updated_at
		) values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		returning id, tenant_id, scenario_id, display_order, name, description, allowed_outcomes, action_type, action_config, active, created_at, updated_at
	`
	var out workflow.Definition
	var actionType string
	err := r.q.QueryRow(ctx, stmt, item.ID, item.TenantID, item.ScenarioID, item.DisplayOrder, item.Name, item.Description, item.AllowedOutcomes, string(item.ActionType), item.ActionConfig, item.Active, item.CreatedAt, item.UpdatedAt).
		Scan(&out.ID, &out.TenantID, &out.ScenarioID, &out.DisplayOrder, &out.Name, &out.Description, &out.AllowedOutcomes, &actionType, &out.ActionConfig, &out.Active, &out.CreatedAt, &out.UpdatedAt)
	out.ActionType = workflow.ActionType(actionType)
	return out, err
}

func (r WorkflowRepository) GetByID(ctx context.Context, tenantID, scenarioID, workflowID string) (workflow.Definition, error) {
	const stmt = `
		select id, tenant_id, scenario_id, display_order, name, description, allowed_outcomes, action_type, action_config, active, created_at, updated_at
		from core.workflows where tenant_id = $1 and scenario_id = $2 and id = $3
	`
	var out workflow.Definition
	var actionType string
	err := r.q.QueryRow(ctx, stmt, tenantID, scenarioID, workflowID).
		Scan(&out.ID, &out.TenantID, &out.ScenarioID, &out.DisplayOrder, &out.Name, &out.Description, &out.AllowedOutcomes, &actionType, &out.ActionConfig, &out.Active, &out.CreatedAt, &out.UpdatedAt)
	out.ActionType = workflow.ActionType(actionType)
	return out, err
}

func (r WorkflowRepository) ListByScenario(ctx context.Context, tenantID, scenarioID string) ([]workflow.Definition, error) {
	return r.list(ctx, `
		select id, tenant_id, scenario_id, display_order, name, description, allowed_outcomes, action_type, action_config, active, created_at, updated_at
		from core.workflows where tenant_id = $1 and scenario_id = $2
		order by display_order asc, created_at asc
	`, tenantID, scenarioID)
}

func (r WorkflowRepository) ListActiveByScenario(ctx context.Context, tenantID, scenarioID string) ([]workflow.Definition, error) {
	return r.list(ctx, `
		select id, tenant_id, scenario_id, display_order, name, description, allowed_outcomes, action_type, action_config, active, created_at, updated_at
		from core.workflows where tenant_id = $1 and scenario_id = $2 and active = true
		order by display_order asc, created_at asc
	`, tenantID, scenarioID)
}

func (r WorkflowRepository) Update(ctx context.Context, item workflow.Definition) (workflow.Definition, error) {
	const stmt = `
		update core.workflows
		set name = $1, description = $2, allowed_outcomes = $3, action_type = $4, action_config = $5, active = $6, updated_at = $7
		where tenant_id = $8 and scenario_id = $9 and id = $10
		returning id, tenant_id, scenario_id, display_order, name, description, allowed_outcomes, action_type, action_config, active, created_at, updated_at
	`
	var out workflow.Definition
	var actionType string
	err := r.q.QueryRow(ctx, stmt, item.Name, item.Description, item.AllowedOutcomes, string(item.ActionType), item.ActionConfig, item.Active, item.UpdatedAt, item.TenantID, item.ScenarioID, item.ID).
		Scan(&out.ID, &out.TenantID, &out.ScenarioID, &out.DisplayOrder, &out.Name, &out.Description, &out.AllowedOutcomes, &actionType, &out.ActionConfig, &out.Active, &out.CreatedAt, &out.UpdatedAt)
	out.ActionType = workflow.ActionType(actionType)
	return out, err
}

func (r WorkflowRepository) Reorder(ctx context.Context, tenantID, scenarioID string, orderedIDs []string, updatedAt time.Time) error {
	for index, workflowID := range orderedIDs {
		tag, err := r.q.Exec(ctx, `
			update core.workflows
			set display_order = $1, updated_at = $2
			where tenant_id = $3 and scenario_id = $4 and id = $5
		`, index, updatedAt, tenantID, scenarioID, workflowID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return fmt.Errorf("workflow %q not found", workflowID)
		}
	}
	return nil
}

func (r WorkflowRepository) Delete(ctx context.Context, tenantID, scenarioID, workflowID string) error {
	const stmt = `delete from core.workflows where tenant_id = $1 and scenario_id = $2 and id = $3`
	_, err := r.q.Exec(ctx, stmt, tenantID, scenarioID, workflowID)
	return err
}

func (r WorkflowRepository) list(ctx context.Context, stmt string, tenantID, scenarioID string) ([]workflow.Definition, error) {
	rows, err := r.q.Query(ctx, stmt, tenantID, scenarioID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []workflow.Definition
	for rows.Next() {
		var item workflow.Definition
		var actionType string
		if err := rows.Scan(&item.ID, &item.TenantID, &item.ScenarioID, &item.DisplayOrder, &item.Name, &item.Description, &item.AllowedOutcomes, &actionType, &item.ActionConfig, &item.Active, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		item.ActionType = workflow.ActionType(actionType)
		items = append(items, item)
	}
	return items, rows.Err()
}
