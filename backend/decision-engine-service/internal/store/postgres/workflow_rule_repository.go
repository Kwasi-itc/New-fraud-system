package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
)

type WorkflowRuleRepository struct{ q queryable }

func NewWorkflowRuleRepository(q queryable) WorkflowRuleRepository {
	return WorkflowRuleRepository{q: q}
}

func (r WorkflowRuleRepository) Create(ctx context.Context, item workflow.Rule) (workflow.Rule, error) {
	const stmt = `
		insert into core.workflow_rules (
			id, tenant_id, scenario_id, name, priority, fallthrough, created_at, updated_at
		) values ($1,$2,$3,$4,$5,$6,$7,$8)
		returning id, tenant_id, scenario_id, name, priority, fallthrough, created_at, updated_at
	`
	var out workflow.Rule
	err := r.q.QueryRow(ctx, stmt, item.ID, item.TenantID, item.ScenarioID, item.Name, item.Priority, item.Fallthrough, item.CreatedAt, item.UpdatedAt).
		Scan(&out.ID, &out.TenantID, &out.ScenarioID, &out.Name, &out.Priority, &out.Fallthrough, &out.CreatedAt, &out.UpdatedAt)
	return out, err
}

func (r WorkflowRuleRepository) GetByID(ctx context.Context, tenantID, scenarioID, ruleID string) (workflow.Rule, error) {
	const stmt = `
		select id, tenant_id, scenario_id, name, priority, fallthrough, created_at, updated_at
		from core.workflow_rules where tenant_id = $1 and scenario_id = $2 and id = $3
	`
	var out workflow.Rule
	err := r.q.QueryRow(ctx, stmt, tenantID, scenarioID, ruleID).
		Scan(&out.ID, &out.TenantID, &out.ScenarioID, &out.Name, &out.Priority, &out.Fallthrough, &out.CreatedAt, &out.UpdatedAt)
	return out, err
}

func (r WorkflowRuleRepository) ListByScenario(ctx context.Context, tenantID, scenarioID string) ([]workflow.Rule, error) {
	rows, err := r.q.Query(ctx, `
		select id, tenant_id, scenario_id, name, priority, fallthrough, created_at, updated_at
		from core.workflow_rules
		where tenant_id = $1 and scenario_id = $2
		order by priority asc, created_at asc
	`, tenantID, scenarioID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []workflow.Rule
	for rows.Next() {
		var item workflow.Rule
		if err := rows.Scan(&item.ID, &item.TenantID, &item.ScenarioID, &item.Name, &item.Priority, &item.Fallthrough, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r WorkflowRuleRepository) Update(ctx context.Context, item workflow.Rule) (workflow.Rule, error) {
	const stmt = `
		update core.workflow_rules
		set name = $1, fallthrough = $2, updated_at = $3
		where tenant_id = $4 and scenario_id = $5 and id = $6
		returning id, tenant_id, scenario_id, name, priority, fallthrough, created_at, updated_at
	`
	var out workflow.Rule
	err := r.q.QueryRow(ctx, stmt, item.Name, item.Fallthrough, item.UpdatedAt, item.TenantID, item.ScenarioID, item.ID).
		Scan(&out.ID, &out.TenantID, &out.ScenarioID, &out.Name, &out.Priority, &out.Fallthrough, &out.CreatedAt, &out.UpdatedAt)
	return out, err
}

func (r WorkflowRuleRepository) Reorder(ctx context.Context, tenantID, scenarioID string, orderedIDs []string, updatedAt time.Time) error {
	for index, ruleID := range orderedIDs {
		tag, err := r.q.Exec(ctx, `
			update core.workflow_rules
			set priority = $1, updated_at = $2
			where tenant_id = $3 and scenario_id = $4 and id = $5
		`, index, updatedAt, tenantID, scenarioID, ruleID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return fmt.Errorf("workflow rule %q not found", ruleID)
		}
	}
	return nil
}

func (r WorkflowRuleRepository) Delete(ctx context.Context, tenantID, scenarioID, ruleID string) error {
	_, err := r.q.Exec(ctx, `delete from core.workflow_rules where tenant_id = $1 and scenario_id = $2 and id = $3`, tenantID, scenarioID, ruleID)
	return err
}
