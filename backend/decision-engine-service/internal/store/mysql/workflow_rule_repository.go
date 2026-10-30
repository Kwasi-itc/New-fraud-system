package mysql

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

type WorkflowRuleRepository struct{ authoringRepository }

var _ ports.WorkflowRuleRepository = WorkflowRuleRepository{}

func (r WorkflowRuleRepository) selectSQL() string {
	return "SELECT id,tenant_id,scenario_id,priority,fallthrough,created_at,updated_at," + r.projection("name") + " FROM " + r.schema + ".decision_engine_workflow_rules"
}
func (r WorkflowRuleRepository) scan(row rowScanner) (workflow.Rule, int64, error) {
	var id, tenant, parent mysqlstore.UUID
	var priority mysqlstore.Int32
	var continues mysqlstore.Bool
	var created, updated mysqlstore.DateTime
	var size int64
	var name mysqlstore.Text
	if err := row.Scan(&id, &tenant, &parent, &priority, &continues, &created, &updated, &size, &name); err != nil {
		return workflow.Rule{}, 0, err
	}
	return workflow.Rule{ID: uuid.UUID(id).String(), TenantID: uuid.UUID(tenant).String(), ScenarioID: uuid.UUID(parent).String(), Priority: int(priority), Fallthrough: bool(continues), CreatedAt: created.Time, UpdatedAt: updated.Time, Name: string(name)}, size, nil
}
func (r WorkflowRuleRepository) GetByID(ctx context.Context, tenant, parent, id string) (workflow.Rule, error) {
	args, err := r.ids(tenant, parent, id)
	if err != nil {
		return workflow.Rule{}, err
	}
	return platformOne(r.platformRepository, r.tx.QueryRowContext(ctx, r.selectSQL()+" WHERE tenant_id=? AND scenario_id=? AND id=? AND deleted_at IS NULL", args...), r.scan)
}
func (r WorkflowRuleRepository) ListByScenario(ctx context.Context, tenant, parent string) ([]workflow.Rule, error) {
	args, err := r.ids(tenant, parent)
	if err != nil {
		return nil, err
	}
	return platformList(ctx, r.platformRepository, r.selectSQL()+" WHERE tenant_id=? AND scenario_id=? AND deleted_at IS NULL ORDER BY priority,created_at,id", args, r.scan)
}
func (r WorkflowRuleRepository) Create(ctx context.Context, v workflow.Rule) (workflow.Rule, error) {
	if err := v.Validate(); err != nil {
		return workflow.Rule{}, err
	}
	id, err := r.creation(v.TenantID, v.ID, v.CreatedAt, v.Name)
	if err != nil {
		return workflow.Rule{}, err
	}
	args, err := r.ids(v.TenantID, v.ScenarioID)
	if err != nil {
		return workflow.Rule{}, err
	}
	if err := r.lockWorkflowAuthoring(ctx, v.TenantID, v.ScenarioID); err != nil {
		return workflow.Rule{}, err
	}
	priority, err := checkedInt(v.Priority)
	if err != nil {
		return workflow.Rule{}, err
	}
	if _, err := r.tx.ExecContext(ctx, "INSERT INTO "+r.schema+".decision_engine_workflow_rules(id,tenant_id,scenario_id,name,priority,fallthrough,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)", id, args[0], args[1], mysqlstore.Text(v.Name), priority, mysqlstore.Bool(v.Fallthrough), mysqlstore.DateTime{Time: v.CreatedAt}, mysqlstore.DateTime{Time: v.UpdatedAt}); err != nil {
		return workflow.Rule{}, err
	}
	return r.GetByID(ctx, v.TenantID, v.ScenarioID, v.ID)
}
func (r WorkflowRuleRepository) lock(ctx context.Context, tenant, parent, id string) error {
	if err := r.lockWorkflowAuthoring(ctx, tenant, parent); err != nil {
		return err
	}
	if err := r.mutating(); err != nil {
		return err
	}
	args, err := r.ids(tenant, parent, id)
	if err != nil {
		return err
	}
	var found mysqlstore.UUID
	return r.tx.QueryRowContext(ctx, "SELECT id FROM "+r.schema+".decision_engine_workflow_rules WHERE tenant_id=? AND scenario_id=? AND id=? AND deleted_at IS NULL FOR UPDATE", args...).Scan(&found)
}
func (r WorkflowRuleRepository) Update(ctx context.Context, v workflow.Rule) (workflow.Rule, error) {
	if err := v.Validate(); err != nil {
		return workflow.Rule{}, err
	}
	if err := r.scope(v.TenantID, v.Name); err != nil {
		return workflow.Rule{}, err
	}
	if err := r.lock(ctx, v.TenantID, v.ScenarioID, v.ID); err != nil {
		return workflow.Rule{}, err
	}
	args, _ := r.ids(v.TenantID, v.ScenarioID, v.ID)
	if _, err := r.tx.ExecContext(ctx, "UPDATE "+r.schema+".decision_engine_workflow_rules SET name=?,fallthrough=?,updated_at=? WHERE tenant_id=? AND scenario_id=? AND id=? AND deleted_at IS NULL", append([]any{mysqlstore.Text(v.Name), mysqlstore.Bool(v.Fallthrough), mysqlstore.DateTime{Time: v.UpdatedAt}}, args...)...); err != nil {
		return workflow.Rule{}, err
	}
	return r.GetByID(ctx, v.TenantID, v.ScenarioID, v.ID)
}
func (r WorkflowRuleRepository) Delete(ctx context.Context, tenant, parent, id string) error {
	if err := r.lock(ctx, tenant, parent, id); err != nil {
		return err
	}
	args, _ := r.ids(tenant, parent, id)
	_, err := r.tx.ExecContext(ctx, "UPDATE "+r.schema+".decision_engine_workflow_rules SET deleted_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND scenario_id=? AND id=? AND deleted_at IS NULL", args...)
	return err
}
func (r WorkflowRuleRepository) Reorder(ctx context.Context, tenant, parent string, ids []string, at time.Time) error {
	return r.reorderRows(ctx, tenant, parent, ids, at, "decision_engine_workflow_rules", "priority", " AND deleted_at IS NULL")
}

// All affected row locks are acquired in UUID order, not input/display order.
// Only the affected rules are locked: unrelated scenarios are not serialized.
func (r authoringRepository) reorderRows(ctx context.Context, tenant, parent string, ids []string, at time.Time, table, column, suffix string) error {
	if err := r.lockWorkflowAuthoring(ctx, tenant, parent); err != nil {
		return err
	}
	if err := r.mutating(); err != nil {
		return err
	}
	if len(ids) > r.maxRows {
		return ErrProjectionLimit
	}
	scope, err := r.ids(tenant, parent)
	if err != nil {
		return err
	}
	if _, err := (mysqlstore.DateTime{Time: at}).Value(); err != nil {
		return err
	}
	seen := map[string]bool{}
	order := map[string]int{}
	for i, id := range ids {
		if _, err := r.ids(tenant, id); err != nil {
			return err
		}
		if seen[id] {
			return errors.New("duplicate reorder identity")
		}
		seen[id] = true
		order[id] = i
	}
	sorted := append([]string(nil), ids...)
	sort.Strings(sorted)
	for start := 0; start < len(sorted); start += 100 {
		end := min(start+100, len(sorted))
		args := append([]any(nil), scope...)
		marks := []string{}
		for _, id := range sorted[start:end] {
			args = append(args, mysqlstore.UUID(uuid.MustParse(id)))
			marks = append(marks, "?")
		}
		rows, err := r.tx.QueryContext(ctx, "SELECT id FROM "+r.schema+"."+table+" WHERE tenant_id=? AND scenario_id=? AND id IN ("+strings.Join(marks, ",")+")"+suffix+" ORDER BY id FOR UPDATE", args...)
		if err != nil {
			return err
		}
		count := 0
		for rows.Next() {
			var id mysqlstore.UUID
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			count++
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if count != end-start {
			return sql.ErrNoRows
		}
	}
	for start := 0; start < len(sorted); start += 100 {
		end := min(start+100, len(sorted))
		args := []any{}
		cases := []string{}
		marks := []string{}
		for _, id := range sorted[start:end] {
			cases = append(cases, "WHEN ? THEN ?")
			args = append(args, mysqlstore.UUID(uuid.MustParse(id)), order[id])
			marks = append(marks, "?")
		}
		args = append(args, mysqlstore.DateTime{Time: at})
		args = append(args, scope...)
		for _, id := range sorted[start:end] {
			args = append(args, mysqlstore.UUID(uuid.MustParse(id)))
		}
		if _, err := r.tx.ExecContext(ctx, "UPDATE "+r.schema+"."+table+" SET "+column+"=CASE id "+strings.Join(cases, " ")+" END,updated_at=? WHERE tenant_id=? AND scenario_id=? AND id IN ("+strings.Join(marks, ",")+")"+suffix, args...); err != nil {
			return err
		}
	}
	return nil
}
func (r WorkflowRuleRepository) ListStructured(ctx context.Context, tenant, parent string) ([]workflow.StructuredRule, error) {
	if r.write {
		return nil, errors.New("structured workflow assembly requires a read-only snapshot")
	}
	bounded := r
	bounded.maxRows = min(r.maxRows, 500)
	parents, err := bounded.ListByScenario(ctx, tenant, parent)
	if err != nil {
		return nil, err
	}
	ids := make([]string, len(parents))
	used := 0
	result := make([]workflow.StructuredRule, len(parents))
	index := map[string]int{}
	for i, p := range parents {
		ids[i] = p.ID
		index[p.ID] = i
		used += len(p.Name)
		result[i] = workflow.StructuredRule{Rule: p, Conditions: []workflow.Condition{}, Actions: []workflow.Action{}}
	}
	conditions, err := (workflowChildRepository{r.authoringRepository, false}).batch(ctx, tenant, ids)
	if err != nil {
		return nil, err
	}
	actions, err := (workflowChildRepository{r.authoringRepository, true}).batch(ctx, tenant, ids)
	if err != nil {
		return nil, err
	}
	for _, v := range conditions {
		used += len(v.kind) + len(v.document)
		if used > r.maxBytes {
			return nil, ErrProjectionLimit
		}
		c := conditionValue(v)
		c.CreatedAt, c.UpdatedAt = time.Time{}, time.Time{}
		i := index[v.parent]
		result[i].Conditions = append(result[i].Conditions, c)
	}
	for _, v := range actions {
		used += len(v.kind) + len(v.document)
		if used > r.maxBytes {
			return nil, ErrProjectionLimit
		}
		a := actionValue(v)
		a.CreatedAt, a.UpdatedAt = time.Time{}, time.Time{}
		i := index[v.parent]
		result[i].Actions = append(result[i].Actions, a)
	}
	return result, nil
}
