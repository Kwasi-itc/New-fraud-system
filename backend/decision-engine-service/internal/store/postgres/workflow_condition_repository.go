package postgres

import (
	"context"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
)

type WorkflowConditionRepository struct{ q queryable }

func NewWorkflowConditionRepository(q queryable) WorkflowConditionRepository {
	return WorkflowConditionRepository{q: q}
}

func (r WorkflowConditionRepository) Create(ctx context.Context, item workflow.Condition) (workflow.Condition, error) {
	const stmt = `
		insert into core.workflow_conditions (
			id, tenant_id, rule_id, function, params, created_at, updated_at
		) values ($1,$2,$3,$4,$5,$6,$7)
		returning id, tenant_id, rule_id, function, params, created_at, updated_at
	`
	var out workflow.Condition
	var function string
	err := r.q.QueryRow(ctx, stmt, item.ID, item.TenantID, item.RuleID, string(item.Function), item.Params, item.CreatedAt, item.UpdatedAt).
		Scan(&out.ID, &out.TenantID, &out.RuleID, &function, &out.Params, &out.CreatedAt, &out.UpdatedAt)
	out.Function = workflow.ConditionType(function)
	return out, err
}

func (r WorkflowConditionRepository) GetByID(ctx context.Context, tenantID, ruleID, conditionID string) (workflow.Condition, error) {
	const stmt = `
		select id, tenant_id, rule_id, function, params, created_at, updated_at
		from core.workflow_conditions where tenant_id = $1 and rule_id = $2 and id = $3
	`
	var out workflow.Condition
	var function string
	err := r.q.QueryRow(ctx, stmt, tenantID, ruleID, conditionID).
		Scan(&out.ID, &out.TenantID, &out.RuleID, &function, &out.Params, &out.CreatedAt, &out.UpdatedAt)
	out.Function = workflow.ConditionType(function)
	return out, err
}

func (r WorkflowConditionRepository) ListByRule(ctx context.Context, tenantID, ruleID string) ([]workflow.Condition, error) {
	rows, err := r.q.Query(ctx, `
		select id, tenant_id, rule_id, function, params, created_at, updated_at
		from core.workflow_conditions where tenant_id = $1 and rule_id = $2
		order by created_at asc
	`, tenantID, ruleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []workflow.Condition
	for rows.Next() {
		var item workflow.Condition
		var function string
		if err := rows.Scan(&item.ID, &item.TenantID, &item.RuleID, &function, &item.Params, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		item.Function = workflow.ConditionType(function)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r WorkflowConditionRepository) Update(ctx context.Context, item workflow.Condition) (workflow.Condition, error) {
	const stmt = `
		update core.workflow_conditions
		set function = $1, params = $2, updated_at = $3
		where tenant_id = $4 and rule_id = $5 and id = $6
		returning id, tenant_id, rule_id, function, params, created_at, updated_at
	`
	var out workflow.Condition
	var function string
	err := r.q.QueryRow(ctx, stmt, string(item.Function), item.Params, item.UpdatedAt, item.TenantID, item.RuleID, item.ID).
		Scan(&out.ID, &out.TenantID, &out.RuleID, &function, &out.Params, &out.CreatedAt, &out.UpdatedAt)
	out.Function = workflow.ConditionType(function)
	return out, err
}

func (r WorkflowConditionRepository) Delete(ctx context.Context, tenantID, ruleID, conditionID string) error {
	_, err := r.q.Exec(ctx, `delete from core.workflow_conditions where tenant_id = $1 and rule_id = $2 and id = $3`, tenantID, ruleID, conditionID)
	return err
}
