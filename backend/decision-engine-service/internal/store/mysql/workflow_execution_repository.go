package mysql

import (
	"context"
	"encoding/json"
	"errors"
	"sort"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

type WorkflowExecutionRepository struct{ authoringRepository }

var _ ports.WorkflowExecutionRepository = WorkflowExecutionRepository{}

func (r WorkflowExecutionRepository) selectSQL() string {
	return "SELECT id,tenant_id,workflow_id,workflow_rule_id,workflow_action_id,decision_id,scenario_id,status,created_at," + r.project([]string{"action_type"}, "action_config") + " FROM " + r.schema + ".decision_engine_workflow_executions"
}
func (r WorkflowExecutionRepository) scan(row rowScanner) (workflow.Execution, int64, error) {
	var id, t, d, s mysqlstore.UUID
	var legacy, rule, action mysqlstore.NullUUID
	var status string
	var at mysqlstore.DateTime
	var size int64
	var kind mysqlstore.Text
	var config mysqlstore.JSON
	if err := row.Scan(&id, &t, &legacy, &rule, &action, &d, &s, &status, &at, &size, &kind, &config); err != nil {
		return workflow.Execution{}, 0, err
	}
	return workflow.Execution{ID: uuid.UUID(id).String(), TenantID: uuid.UUID(t).String(), WorkflowID: uuidPointer(legacy), WorkflowRuleID: uuidPointer(rule), WorkflowActionID: uuidPointer(action), DecisionID: uuid.UUID(d).String(), ScenarioID: uuid.UUID(s).String(), ActionType: workflow.ActionType(kind), Status: workflow.ExecutionStatus(status), ActionConfig: json.RawMessage(config), CreatedAt: at.Time}, size, nil
}
func (r WorkflowExecutionRepository) CreateMany(ctx context.Context, items []workflow.Execution) ([]workflow.Execution, error) {
	if err := r.mutating(); err != nil {
		return nil, err
	}
	if len(items) > r.maxRows {
		return nil, ErrProjectionLimit
	}
	values := make([][]any, len(items))
	ids := make([]string, len(items))
	parents := map[string]bool{}
	used := 0
	for i, v := range items {
		id, err := r.creation(v.TenantID, v.ID, v.CreatedAt, string(v.ActionType))
		if err != nil {
			return nil, err
		}
		args, err := r.ids(v.TenantID, v.DecisionID, v.ScenarioID)
		if err != nil {
			return nil, err
		}
		legacy, err := nullUUID(v.WorkflowID)
		if err != nil {
			return nil, err
		}
		rule, err := nullUUID(v.WorkflowRuleID)
		if err != nil {
			return nil, err
		}
		action, err := nullUUID(v.WorkflowActionID)
		if err != nil {
			return nil, err
		}
		if legacy.Valid && (rule.Valid || action.Valid) || !legacy.Valid && (!rule.Valid || !action.Valid) {
			return nil, ErrOwnerRelationshipConflict
		}
		switch v.Status {
		case workflow.ExecutionStatusPendingDispatch, workflow.ExecutionStatusDispatched, workflow.ExecutionStatusDispatchFailed:
		default:
			return nil, errors.New("invalid workflow dispatch status")
		}
		if err := workflow.ValidateActionConfig(v.ActionType, v.ActionConfig); err != nil {
			return nil, err
		}
		document := mysqlstore.JSON(v.ActionConfig)
		if _, err := document.Value(); err != nil {
			return nil, err
		}
		used += len(v.ActionConfig) + len(v.ActionType)
		if used > r.maxBytes {
			return nil, ErrProjectionLimit
		}
		ids[i] = v.ID
		parents[v.DecisionID] = true
		values[i] = []any{id, args[0], legacy, rule, action, args[1], args[2], nil, mysqlstore.Text(v.ActionType), string(v.Status), document, mysqlstore.DateTime{Time: v.CreatedAt}}
	}
	ordered := make([]string, 0, len(parents))
	for id := range parents {
		ordered = append(ordered, id)
	}
	sort.Strings(ordered)
	bindings, err := r.evidenceBindings(ctx, "decision_engine_decisions", ordered)
	if err != nil {
		return nil, err
	}
	for i, v := range items {
		values[i][7] = bindings[v.DecisionID]
	}
	return insertEffectBatch(ctx, r.authoringRepository, "decision_engine_workflow_executions", "id,tenant_id,workflow_id,workflow_rule_id,workflow_action_id,decision_id,scenario_id,decision_iteration_id,action_type,status,action_config,created_at", r.selectSQL(), values, ids, r.scan, func(v workflow.Execution) string { return v.ID })
}
func (r WorkflowExecutionRepository) GetByID(ctx context.Context, tenant, id string) (workflow.Execution, error) {
	args, err := r.ids(tenant, id)
	if err != nil {
		return workflow.Execution{}, err
	}
	return platformOne(r.platformRepository, r.tx.QueryRowContext(ctx, r.selectSQL()+" WHERE tenant_id=? AND id=?", args...), r.scan)
}
func (r WorkflowExecutionRepository) ListByDecision(ctx context.Context, tenant, id string) ([]workflow.Execution, error) {
	args, err := r.ids(tenant, id)
	if err != nil {
		return nil, err
	}
	return platformList(ctx, r.platformRepository, r.selectSQL()+" WHERE tenant_id=? AND decision_id=? ORDER BY created_at,id", args, r.scan)
}
func (r WorkflowExecutionRepository) ListByStatus(context.Context, workflow.ExecutionStatus, int) ([]workflow.Execution, error) {
	return nil, ErrSystemScopeRequired
}
func (r WorkflowExecutionRepository) UpdateStatus(ctx context.Context, id string, status workflow.ExecutionStatus) error {
	if err := r.mutating(); err != nil {
		return err
	}
	args, err := r.ids(r.tenant.String(), id)
	if err != nil {
		return err
	}
	switch status {
	case workflow.ExecutionStatusPendingDispatch, workflow.ExecutionStatusDispatched, workflow.ExecutionStatusDispatchFailed:
	default:
		return errors.New("invalid workflow dispatch status")
	}
	var locked mysqlstore.UUID
	if err := r.tx.QueryRowContext(ctx, "SELECT id FROM "+r.schema+".decision_engine_workflow_executions WHERE tenant_id=? AND id=? FOR UPDATE", args...).Scan(&locked); err != nil {
		return err
	}
	_, err = r.tx.ExecContext(ctx, "UPDATE "+r.schema+".decision_engine_workflow_executions SET status=? WHERE tenant_id=? AND id=?", string(status), args[0], args[1])
	return err
}
