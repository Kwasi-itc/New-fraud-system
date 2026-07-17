package postgres

import (
	"context"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
)

type WorkflowExecutionRepository struct{ q queryable }

func NewWorkflowExecutionRepository(q queryable) WorkflowExecutionRepository {
	return WorkflowExecutionRepository{q: q}
}

func (r WorkflowExecutionRepository) CreateMany(ctx context.Context, items []workflow.Execution) ([]workflow.Execution, error) {
	if len(items) == 0 {
		return []workflow.Execution{}, nil
	}
	const stmt = `
		insert into core.workflow_executions (
			id, tenant_id, workflow_id, workflow_rule_id, workflow_action_id, decision_id, scenario_id, action_type, status, action_config, created_at
		) values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		returning id, tenant_id, workflow_id, workflow_rule_id, workflow_action_id, decision_id, scenario_id, action_type, status, action_config, created_at
	`
	out := make([]workflow.Execution, 0, len(items))
	for _, item := range items {
		var stored workflow.Execution
		var actionType, status string
		err := r.q.QueryRow(ctx, stmt, item.ID, item.TenantID, item.WorkflowID, item.WorkflowRuleID, item.WorkflowActionID, item.DecisionID, item.ScenarioID, string(item.ActionType), string(item.Status), item.ActionConfig, item.CreatedAt).
			Scan(&stored.ID, &stored.TenantID, &stored.WorkflowID, &stored.WorkflowRuleID, &stored.WorkflowActionID, &stored.DecisionID, &stored.ScenarioID, &actionType, &status, &stored.ActionConfig, &stored.CreatedAt)
		if err != nil {
			return nil, err
		}
		stored.ActionType = workflow.ActionType(actionType)
		stored.Status = workflow.ExecutionStatus(status)
		out = append(out, stored)
	}
	return out, nil
}

func (r WorkflowExecutionRepository) GetByID(ctx context.Context, tenantID, executionID string) (workflow.Execution, error) {
	const stmt = `
		select id, tenant_id, workflow_id, workflow_rule_id, workflow_action_id, decision_id, scenario_id, action_type, status, action_config, created_at
		from core.workflow_executions
		where tenant_id = $1 and id = $2
	`
	var item workflow.Execution
	var actionType, status string
	err := r.q.QueryRow(ctx, stmt, tenantID, executionID).
		Scan(&item.ID, &item.TenantID, &item.WorkflowID, &item.WorkflowRuleID, &item.WorkflowActionID, &item.DecisionID, &item.ScenarioID, &actionType, &status, &item.ActionConfig, &item.CreatedAt)
	item.ActionType = workflow.ActionType(actionType)
	item.Status = workflow.ExecutionStatus(status)
	return item, err
}

func (r WorkflowExecutionRepository) ListByDecision(ctx context.Context, tenantID, decisionID string) ([]workflow.Execution, error) {
	const stmt = `
		select id, tenant_id, workflow_id, workflow_rule_id, workflow_action_id, decision_id, scenario_id, action_type, status, action_config, created_at
		from core.workflow_executions
		where tenant_id = $1 and decision_id = $2
		order by created_at asc
	`
	rows, err := r.q.Query(ctx, stmt, tenantID, decisionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []workflow.Execution
	for rows.Next() {
		var item workflow.Execution
		var actionType, status string
		if err := rows.Scan(&item.ID, &item.TenantID, &item.WorkflowID, &item.WorkflowRuleID, &item.WorkflowActionID, &item.DecisionID, &item.ScenarioID, &actionType, &status, &item.ActionConfig, &item.CreatedAt); err != nil {
			return nil, err
		}
		item.ActionType = workflow.ActionType(actionType)
		item.Status = workflow.ExecutionStatus(status)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r WorkflowExecutionRepository) ListByStatus(ctx context.Context, status workflow.ExecutionStatus, limit int) ([]workflow.Execution, error) {
	if limit <= 0 {
		limit = 50
	}
	const stmt = `
		select id, tenant_id, workflow_id, workflow_rule_id, workflow_action_id, decision_id, scenario_id, action_type, status, action_config, created_at
		from core.workflow_executions
		where status = $1
		order by created_at asc
		limit $2
	`
	rows, err := r.q.Query(ctx, stmt, string(status), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []workflow.Execution
	for rows.Next() {
		var item workflow.Execution
		var actionType, statusValue string
		if err := rows.Scan(&item.ID, &item.TenantID, &item.WorkflowID, &item.WorkflowRuleID, &item.WorkflowActionID, &item.DecisionID, &item.ScenarioID, &actionType, &statusValue, &item.ActionConfig, &item.CreatedAt); err != nil {
			return nil, err
		}
		item.ActionType = workflow.ActionType(actionType)
		item.Status = workflow.ExecutionStatus(statusValue)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r WorkflowExecutionRepository) UpdateStatus(ctx context.Context, id string, status workflow.ExecutionStatus) error {
	const stmt = `update core.workflow_executions set status = $1 where id = $2`
	_, err := r.q.Exec(ctx, stmt, string(status), id)
	return err
}
