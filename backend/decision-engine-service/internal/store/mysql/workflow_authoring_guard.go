package mysql

import (
	"context"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

// All legacy/structured workflow configuration writers participate, including
// low-level child edits. Only authoring for this scenario serializes; decision
// persistence/evaluation never takes this guard. Missing required installation
// fails, rather than silently reverting to unsafe parent locking/no locking.
// A callback must not use its transaction repositories concurrently.
func (r authoringRepository) lockWorkflowAuthoring(ctx context.Context, tenant, scenario string) error {
	if err := r.mutating(); err != nil {
		return err
	}
	args, err := r.ids(tenant, scenario)
	if err != nil {
		return err
	}
	id := uuid.UUID(args[1].(mysqlstore.UUID))
	if r.workflowGuards[id] {
		return nil
	}
	if _, err := r.tx.ExecContext(ctx, "INSERT INTO "+r.schema+".decision_engine_workflow_authoring_guards(tenant_id,scenario_id) VALUES(?,?) ON DUPLICATE KEY UPDATE scenario_id=scenario_id", args...); err != nil {
		return err
	}
	var locked mysqlstore.UUID
	if err := r.tx.QueryRowContext(ctx, "SELECT scenario_id FROM "+r.schema+".decision_engine_workflow_authoring_guards WHERE tenant_id=? AND scenario_id=? FOR UPDATE", args...).Scan(&locked); err != nil {
		return err
	}
	if r.workflowGuards != nil {
		r.workflowGuards[id] = true
	}
	return nil
}
