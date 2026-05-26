package postgres

import (
	"context"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
)

type WorkflowActionRepository struct{ q queryable }

func NewWorkflowActionRepository(q queryable) WorkflowActionRepository {
	return WorkflowActionRepository{q: q}
}

func (r WorkflowActionRepository) Create(ctx context.Context, item workflow.Action) (workflow.Action, error) {
	const stmt = `
		insert into core.workflow_actions (
			id, tenant_id, rule_id, action_type, action_config, created_at, updated_at
		) values ($1,$2,$3,$4,$5,$6,$7)
		returning id, tenant_id, rule_id, action_type, action_config, created_at, updated_at
	`
	var out workflow.Action
	var actionType string
	err := r.q.QueryRow(ctx, stmt, item.ID, item.TenantID, item.RuleID, string(item.ActionType), item.ActionConfig, item.CreatedAt, item.UpdatedAt).
		Scan(&out.ID, &out.TenantID, &out.RuleID, &actionType, &out.ActionConfig, &out.CreatedAt, &out.UpdatedAt)
	out.ActionType = workflow.ActionType(actionType)
	return out, err
}

func (r WorkflowActionRepository) GetByID(ctx context.Context, tenantID, ruleID, actionID string) (workflow.Action, error) {
	const stmt = `
		select id, tenant_id, rule_id, action_type, action_config, created_at, updated_at
		from core.workflow_actions where tenant_id = $1 and rule_id = $2 and id = $3
	`
	var out workflow.Action
	var actionType string
	err := r.q.QueryRow(ctx, stmt, tenantID, ruleID, actionID).
		Scan(&out.ID, &out.TenantID, &out.RuleID, &actionType, &out.ActionConfig, &out.CreatedAt, &out.UpdatedAt)
	out.ActionType = workflow.ActionType(actionType)
	return out, err
}

func (r WorkflowActionRepository) ListByRule(ctx context.Context, tenantID, ruleID string) ([]workflow.Action, error) {
	rows, err := r.q.Query(ctx, `
		select id, tenant_id, rule_id, action_type, action_config, created_at, updated_at
		from core.workflow_actions where tenant_id = $1 and rule_id = $2
		order by created_at asc
	`, tenantID, ruleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []workflow.Action
	for rows.Next() {
		var item workflow.Action
		var actionType string
		if err := rows.Scan(&item.ID, &item.TenantID, &item.RuleID, &actionType, &item.ActionConfig, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		item.ActionType = workflow.ActionType(actionType)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r WorkflowActionRepository) Update(ctx context.Context, item workflow.Action) (workflow.Action, error) {
	const stmt = `
		update core.workflow_actions
		set action_type = $1, action_config = $2, updated_at = $3
		where tenant_id = $4 and rule_id = $5 and id = $6
		returning id, tenant_id, rule_id, action_type, action_config, created_at, updated_at
	`
	var out workflow.Action
	var actionType string
	err := r.q.QueryRow(ctx, stmt, string(item.ActionType), item.ActionConfig, item.UpdatedAt, item.TenantID, item.RuleID, item.ID).
		Scan(&out.ID, &out.TenantID, &out.RuleID, &actionType, &out.ActionConfig, &out.CreatedAt, &out.UpdatedAt)
	out.ActionType = workflow.ActionType(actionType)
	return out, err
}

func (r WorkflowActionRepository) Delete(ctx context.Context, tenantID, ruleID, actionID string) error {
	_, err := r.q.Exec(ctx, `delete from core.workflow_actions where tenant_id = $1 and rule_id = $2 and id = $3`, tenantID, ruleID, actionID)
	return err
}
