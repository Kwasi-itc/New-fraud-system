package mysql

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

type workflowChildRepository struct {
	authoringRepository
	action bool
}
type workflowChildRecord struct {
	id, tenant, parent, kind string
	document                 mysqlstore.JSON
	created, updated         time.Time
}

func (r workflowChildRepository) names() (string, string, string) {
	if r.action {
		return "decision_engine_workflow_actions", "action_type", "action_config"
	}
	return "decision_engine_workflow_conditions", "`function`", "params"
}
func (r workflowChildRepository) selectSQL() string {
	table, kind, document := r.names()
	return "SELECT id,tenant_id,rule_id,created_at,updated_at," + r.project([]string{kind}, document) + " FROM " + r.schema + "." + table
}
func (r workflowChildRepository) scan(row rowScanner) (workflowChildRecord, int64, error) {
	var id, tenant, parent mysqlstore.UUID
	var created, updated mysqlstore.DateTime
	var size int64
	var kind mysqlstore.Text
	var document mysqlstore.JSON
	if err := row.Scan(&id, &tenant, &parent, &created, &updated, &size, &kind, &document); err != nil {
		return workflowChildRecord{}, 0, err
	}
	return workflowChildRecord{uuid.UUID(id).String(), uuid.UUID(tenant).String(), uuid.UUID(parent).String(), string(kind), document, created.Time, updated.Time}, size, nil
}
func (r workflowChildRepository) get(ctx context.Context, t, p, id string) (workflowChildRecord, error) {
	args, err := r.ids(t, p, id)
	if err != nil {
		return workflowChildRecord{}, err
	}
	return platformOne(r.platformRepository, r.tx.QueryRowContext(ctx, r.selectSQL()+" WHERE tenant_id=? AND rule_id=? AND id=?", args...), r.scan)
}
func (r workflowChildRepository) batch(ctx context.Context, t string, ids []string) ([]workflowChildRecord, error) {
	if _, err := r.ids(t); err != nil {
		return nil, err
	}
	if len(ids) > r.maxRows {
		return nil, ErrProjectionLimit
	}
	result := []workflowChildRecord{}
	used := 0
	for start := 0; start < len(ids); start += 100 {
		end := min(start+100, len(ids))
		args := []any{mysqlstore.UUID(r.tenant)}
		marks := []string{}
		for _, id := range ids[start:end] {
			bound, err := r.ids(t, id)
			if err != nil {
				return nil, err
			}
			args = append(args, bound[1])
			marks = append(marks, "?")
		}
		values, err := platformList(ctx, r.platformRepository, r.selectSQL()+" WHERE tenant_id=? AND rule_id IN ("+strings.Join(marks, ",")+") ORDER BY rule_id,created_at,id", args, r.scan)
		if err != nil {
			return nil, err
		}
		for _, v := range values {
			used += len(v.kind) + len(v.document)
			if used > r.maxBytes || len(result) == r.maxRows {
				return nil, ErrProjectionLimit
			}
			result = append(result, v)
		}
	}
	return result, nil
}
func (r workflowChildRepository) lockParent(ctx context.Context, t, p string) error {
	if err := r.mutating(); err != nil {
		return err
	}
	args, err := r.ids(t, p)
	if err != nil {
		return err
	}
	var scenario mysqlstore.UUID
	if err := r.tx.QueryRowContext(ctx, "SELECT scenario_id FROM "+r.schema+".decision_engine_workflow_rules WHERE tenant_id=? AND id=?", args...).Scan(&scenario); err != nil {
		return err
	}
	if err := r.lockWorkflowAuthoring(ctx, t, uuid.UUID(scenario).String()); err != nil {
		return err
	}
	var id mysqlstore.UUID
	return r.tx.QueryRowContext(ctx, "SELECT id FROM "+r.schema+".decision_engine_workflow_rules WHERE tenant_id=? AND id=? AND deleted_at IS NULL FOR SHARE", args...).Scan(&id)
}
func (r workflowChildRepository) writeRecord(ctx context.Context, v workflowChildRecord, update bool) (workflowChildRecord, error) {
	if err := r.scope(v.tenant, v.kind, string(v.document)); err != nil {
		return workflowChildRecord{}, err
	}
	if _, err := v.document.Value(); err != nil {
		return workflowChildRecord{}, err
	}
	if err := r.lockParent(ctx, v.tenant, v.parent); err != nil {
		return workflowChildRecord{}, err
	}
	args, err := r.ids(v.tenant, v.parent, v.id)
	if err != nil {
		return workflowChildRecord{}, err
	}
	table, kind, document := r.names()
	if update {
		var found mysqlstore.UUID
		if err := r.tx.QueryRowContext(ctx, "SELECT id FROM "+r.schema+"."+table+" WHERE tenant_id=? AND rule_id=? AND id=? FOR UPDATE", args...).Scan(&found); err != nil {
			return workflowChildRecord{}, err
		}
		if _, err := r.tx.ExecContext(ctx, "UPDATE "+r.schema+"."+table+" SET "+kind+"=?,"+document+"=?,updated_at=? WHERE tenant_id=? AND rule_id=? AND id=?", append([]any{mysqlstore.Text(v.kind), v.document, mysqlstore.DateTime{Time: v.updated}}, args...)...); err != nil {
			return workflowChildRecord{}, err
		}
	} else {
		id, err := r.creation(v.tenant, v.id, v.created)
		if err != nil {
			return workflowChildRecord{}, err
		}
		if _, err := r.tx.ExecContext(ctx, "INSERT INTO "+r.schema+"."+table+"(id,tenant_id,rule_id,"+kind+","+document+",created_at,updated_at) VALUES(?,?,?,?,?,?,?)", id, args[0], args[1], mysqlstore.Text(v.kind), v.document, mysqlstore.DateTime{Time: v.created}, mysqlstore.DateTime{Time: v.updated}); err != nil {
			return workflowChildRecord{}, err
		}
	}
	return r.get(ctx, v.tenant, v.parent, v.id)
}
func (r workflowChildRepository) delete(ctx context.Context, t, p, id string) error {
	if err := r.lockParent(ctx, t, p); err != nil {
		return err
	}
	args, err := r.ids(t, p, id)
	if err != nil {
		return err
	}
	table, _, _ := r.names()
	result, err := r.tx.ExecContext(ctx, "DELETE FROM "+r.schema+"."+table+" WHERE tenant_id=? AND rule_id=? AND id=?", args...)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err == nil && count == 0 {
		return sql.ErrNoRows
	}
	return err
}

type WorkflowConditionRepository struct{ workflowChildRepository }
type WorkflowActionRepository struct{ workflowChildRepository }

var _ ports.WorkflowConditionRepository = WorkflowConditionRepository{}
var _ ports.WorkflowActionRepository = WorkflowActionRepository{}

func conditionRecord(v workflow.Condition) workflowChildRecord {
	return workflowChildRecord{v.ID, v.TenantID, v.RuleID, string(v.Function), mysqlstore.JSON(v.Params), v.CreatedAt, v.UpdatedAt}
}
func conditionValue(v workflowChildRecord) workflow.Condition {
	return workflow.Condition{ID: v.id, TenantID: v.tenant, RuleID: v.parent, Function: workflow.ConditionType(v.kind), Params: append([]byte(nil), v.document...), CreatedAt: v.created, UpdatedAt: v.updated}
}
func actionRecord(v workflow.Action) workflowChildRecord {
	return workflowChildRecord{v.ID, v.TenantID, v.RuleID, string(v.ActionType), mysqlstore.JSON(v.ActionConfig), v.CreatedAt, v.UpdatedAt}
}
func actionValue(v workflowChildRecord) workflow.Action {
	return workflow.Action{ID: v.id, TenantID: v.tenant, RuleID: v.parent, ActionType: workflow.ActionType(v.kind), ActionConfig: append([]byte(nil), v.document...), CreatedAt: v.created, UpdatedAt: v.updated}
}
func (r WorkflowConditionRepository) Create(c context.Context, v workflow.Condition) (workflow.Condition, error) {
	out, err := r.writeRecord(c, conditionRecord(v), false)
	return conditionValue(out), err
}
func (r WorkflowConditionRepository) Update(c context.Context, v workflow.Condition) (workflow.Condition, error) {
	out, err := r.writeRecord(c, conditionRecord(v), true)
	return conditionValue(out), err
}
func (r WorkflowConditionRepository) GetByID(c context.Context, t, p, id string) (workflow.Condition, error) {
	out, err := r.get(c, t, p, id)
	return conditionValue(out), err
}
func (r WorkflowConditionRepository) ListByRule(c context.Context, t, p string) ([]workflow.Condition, error) {
	out, err := r.batch(c, t, []string{p})
	if err != nil {
		return nil, err
	}
	values := make([]workflow.Condition, len(out))
	for i, v := range out {
		values[i] = conditionValue(v)
	}
	return values, nil
}
func (r WorkflowConditionRepository) Delete(c context.Context, t, p, id string) error {
	return r.delete(c, t, p, id)
}
func (r WorkflowActionRepository) Create(c context.Context, v workflow.Action) (workflow.Action, error) {
	out, err := r.writeRecord(c, actionRecord(v), false)
	return actionValue(out), err
}
func (r WorkflowActionRepository) Update(c context.Context, v workflow.Action) (workflow.Action, error) {
	out, err := r.writeRecord(c, actionRecord(v), true)
	return actionValue(out), err
}
func (r WorkflowActionRepository) GetByID(c context.Context, t, p, id string) (workflow.Action, error) {
	out, err := r.get(c, t, p, id)
	return actionValue(out), err
}
func (r WorkflowActionRepository) ListByRule(c context.Context, t, p string) ([]workflow.Action, error) {
	out, err := r.batch(c, t, []string{p})
	if err != nil {
		return nil, err
	}
	values := make([]workflow.Action, len(out))
	for i, v := range out {
		values[i] = actionValue(v)
	}
	return values, nil
}
func (r WorkflowActionRepository) Delete(c context.Context, t, p, id string) error {
	return r.delete(c, t, p, id)
}
