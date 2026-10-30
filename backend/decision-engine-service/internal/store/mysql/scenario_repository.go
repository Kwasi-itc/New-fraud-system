package mysql

import (
	"context"
	"database/sql"
	"errors"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

type ScenarioRepository struct{ authoringRepository }

var _ ports.ScenarioRepository = ScenarioRepository{}

func (r ScenarioRepository) selectSQL() string {
	return "SELECT s.id,s.tenant_id,s.created_at,s.updated_at,l.iteration_id," + r.project([]string{"s.name", "s.description", "s.trigger_object_type"}) + " FROM " + r.schema + ".decision_engine_scenarios s LEFT JOIN " + r.schema + ".decision_engine_scenario_live_iterations l ON l.tenant_id=s.tenant_id AND l.scenario_id=s.id"
}
func (r ScenarioRepository) scan(row rowScanner) (scenario.Scenario, int64, error) {
	var id, tenant mysqlstore.UUID
	var created, updated mysqlstore.DateTime
	var live mysqlstore.NullUUID
	var size int64
	var name, desc, object mysqlstore.Text
	if err := row.Scan(&id, &tenant, &created, &updated, &live, &size, &name, &desc, &object); err != nil {
		return scenario.Scenario{}, 0, err
	}
	v := scenario.Scenario{ID: uuid.UUID(id).String(), TenantID: uuid.UUID(tenant).String(), Name: string(name), Description: string(desc), TriggerObjectType: string(object), LiveIterationID: uuidPointer(live), CreatedAt: created.Time, UpdatedAt: updated.Time}
	return v, size, r.scope(v.TenantID, v.Name, v.Description, v.TriggerObjectType)
}
func (r ScenarioRepository) GetByID(ctx context.Context, tenant, id string) (scenario.Scenario, error) {
	args, err := r.ids(tenant, id)
	if err != nil {
		return scenario.Scenario{}, err
	}
	return platformOne(r.platformRepository, r.tx.QueryRowContext(ctx, r.selectSQL()+" WHERE s.tenant_id=? AND s.id=?", args...), r.scan)
}
func (r ScenarioRepository) ListByTenant(ctx context.Context, tenant string) ([]scenario.Scenario, error) {
	args, err := r.ids(tenant)
	if err != nil {
		return nil, err
	}
	return platformList(ctx, r.platformRepository, r.selectSQL()+" WHERE s.tenant_id=? ORDER BY s.created_at DESC,s.id DESC", args, r.scan)
}
func (r ScenarioRepository) ListLiveByTriggerObject(ctx context.Context, tenant, object string) ([]scenario.Scenario, error) {
	args, err := r.ids(tenant)
	if err != nil {
		return nil, err
	}
	if err := r.scope(tenant, object); err != nil {
		return nil, err
	}
	args = append(args, mysqlstore.Text(object))
	return platformList(ctx, r.platformRepository, r.selectSQL()+" WHERE s.tenant_id=? AND l.iteration_id IS NOT NULL AND CAST(s.trigger_object_type AS BINARY)=CAST(? AS BINARY) ORDER BY s.created_at DESC,s.id DESC", args, r.scan)
}
func (r ScenarioRepository) Create(ctx context.Context, v scenario.Scenario) (scenario.Scenario, error) {
	if err := v.Validate(); err != nil {
		return scenario.Scenario{}, err
	}
	id, err := r.creation(v.TenantID, v.ID, v.CreatedAt, v.Name, v.Description, v.TriggerObjectType)
	if err != nil {
		return scenario.Scenario{}, err
	}
	if _, err := (mysqlstore.DateTime{Time: v.UpdatedAt}).Value(); err != nil {
		return scenario.Scenario{}, err
	}
	// A newly created scenario cannot already own an existing iteration. The
	// separate link is installed after its child is inserted on this transaction.
	if v.LiveIterationID != nil {
		return scenario.Scenario{}, errors.New("create scenario before assigning a live iteration")
	}
	if _, err := r.tx.ExecContext(ctx, "INSERT INTO "+r.schema+".decision_engine_scenarios(id,tenant_id,name,description,trigger_object_type,created_at,updated_at) VALUES(?,?,?,?,?,?,?)", id, mysqlstore.UUID(r.tenant), mysqlstore.Text(v.Name), mysqlstore.Text(v.Description), mysqlstore.Text(v.TriggerObjectType), mysqlstore.DateTime{Time: v.CreatedAt}, mysqlstore.DateTime{Time: v.UpdatedAt}); err != nil {
		return scenario.Scenario{}, err
	}
	return r.GetByID(ctx, v.TenantID, v.ID)
}
func (r ScenarioRepository) Update(ctx context.Context, v scenario.Scenario) (scenario.Scenario, error) {
	if err := r.mutating(); err != nil {
		return scenario.Scenario{}, err
	}
	if err := v.Validate(); err != nil {
		return scenario.Scenario{}, err
	}
	args, err := r.ids(v.TenantID, v.ID)
	if err != nil {
		return scenario.Scenario{}, err
	}
	if err := r.scope(v.TenantID, v.Name, v.Description, v.TriggerObjectType); err != nil {
		return scenario.Scenario{}, err
	}
	// Protected existence read distinguishes a successful unchanged update from
	// a missing row without relying on changed-row client flags.
	var found mysqlstore.UUID
	if err := r.tx.QueryRowContext(ctx, "SELECT id FROM "+r.schema+".decision_engine_scenarios WHERE tenant_id=? AND id=? FOR UPDATE", args...).Scan(&found); err != nil {
		return scenario.Scenario{}, err
	}
	// Ordinary content update must not overwrite a concurrent publication using
	// a stale live_iteration_id captured before this transaction.
	current, err := r.GetByID(ctx, v.TenantID, v.ID)
	if err != nil {
		return scenario.Scenario{}, err
	}
	if (current.LiveIterationID == nil) != (v.LiveIterationID == nil) || (current.LiveIterationID != nil && *current.LiveIterationID != *v.LiveIterationID) {
		return scenario.Scenario{}, errors.New("live iteration changes require publication operation")
	}
	values := []any{mysqlstore.Text(v.Name), mysqlstore.Text(v.Description), mysqlstore.Text(v.TriggerObjectType), mysqlstore.DateTime{Time: v.UpdatedAt}}
	values = append(values, args...)
	if _, err := r.tx.ExecContext(ctx, "UPDATE "+r.schema+".decision_engine_scenarios SET name=?,description=?,trigger_object_type=?,updated_at=? WHERE tenant_id=? AND id=?", values...); err != nil {
		return scenario.Scenario{}, err
	}
	return r.GetByID(ctx, v.TenantID, v.ID)
}
func (r ScenarioRepository) Delete(ctx context.Context, tenant, id string) error {
	if err := r.mutating(); err != nil {
		return err
	}
	args, err := r.ids(tenant, id)
	if err != nil {
		return err
	}
	result, err := r.tx.ExecContext(ctx, "DELETE FROM "+r.schema+".decision_engine_scenarios WHERE tenant_id=? AND id=?", args...)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n == 0 {
		return sql.ErrNoRows
	}
	return err
}
func (r ScenarioRepository) SetLiveIterationID(ctx context.Context, tenant, parent string, iteration *string) error {
	if !r.publication {
		return ErrPublicationOperationRequired
	}
	if err := r.mutating(); err != nil {
		return err
	}
	args, err := r.ids(tenant, parent)
	if err != nil {
		return err
	}
	var found mysqlstore.UUID
	if err := r.tx.QueryRowContext(ctx, "SELECT id FROM "+r.schema+".decision_engine_scenarios WHERE tenant_id=? AND id=? FOR UPDATE", args...).Scan(&found); err != nil {
		return err
	}
	if iteration != nil {
		child, err := r.ids(tenant, parent, *iteration)
		if err != nil {
			return err
		}
		var status string
		if err := r.tx.QueryRowContext(ctx, "SELECT status FROM "+r.schema+".decision_engine_scenario_iterations WHERE tenant_id=? AND scenario_id=? AND id=? FOR SHARE", child...).Scan(&status); err != nil {
			return err
		}
		if status != string(scenario.IterationStatusCommitted) {
			return errors.New("live iteration must be committed")
		}
	}
	if _, err := r.tx.ExecContext(ctx, "DELETE FROM "+r.schema+".decision_engine_scenario_live_iterations WHERE tenant_id=? AND scenario_id=?", args...); err != nil {
		return err
	}
	if iteration != nil {
		child, _ := r.ids(tenant, parent, *iteration)
		if _, err := r.tx.ExecContext(ctx, "INSERT INTO "+r.schema+".decision_engine_scenario_live_iterations(tenant_id,scenario_id,iteration_id) VALUES(?,?,?)", child...); err != nil {
			return err
		}
	}
	_, err = r.tx.ExecContext(ctx, "UPDATE "+r.schema+".decision_engine_scenarios SET updated_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND id=?", args...)
	return err
}
