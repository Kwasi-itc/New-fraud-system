package mysql

import (
	"context"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

type TestRunRepository struct{ authoringRepository }

var _ ports.TestRunRepository = TestRunRepository{}

func (r TestRunRepository) selectSQL() string {
	return "SELECT id,tenant_id,scenario_id,live_iteration_id,phantom_iteration_id,created_at,expires_at,updated_at," + r.projection("status") + " FROM " + r.schema + ".decision_engine_test_runs"
}
func (r TestRunRepository) scan(row rowScanner) (scenario.TestRun, int64, error) {
	var id, tenant, parent, live, phantom mysqlstore.UUID
	var created, expires, updated mysqlstore.DateTime
	var size int64
	var status mysqlstore.Text
	if err := row.Scan(&id, &tenant, &parent, &live, &phantom, &created, &expires, &updated, &size, &status); err != nil {
		return scenario.TestRun{}, 0, err
	}
	return scenario.TestRun{ID: uuid.UUID(id).String(), TenantID: uuid.UUID(tenant).String(), ScenarioID: uuid.UUID(parent).String(), LiveIterationID: uuid.UUID(live).String(), PhantomIterationID: uuid.UUID(phantom).String(), Status: scenario.TestRunStatus(status), CreatedAt: created.Time, ExpiresAt: expires.Time, UpdatedAt: updated.Time}, size, nil
}
func (r TestRunRepository) GetByID(ctx context.Context, t, id string) (scenario.TestRun, error) {
	args, err := r.ids(t, id)
	if err != nil {
		return scenario.TestRun{}, err
	}
	return platformOne(r.platformRepository, r.tx.QueryRowContext(ctx, r.selectSQL()+" WHERE tenant_id=? AND id=?", args...), r.scan)
}
func (r TestRunRepository) ListByScenario(ctx context.Context, t, s string) ([]scenario.TestRun, error) {
	args, err := r.ids(t, s)
	if err != nil {
		return nil, err
	}
	return platformList(ctx, r.platformRepository, r.selectSQL()+" WHERE tenant_id=? AND scenario_id=? ORDER BY created_at DESC,id DESC", args, r.scan)
}
func (r TestRunRepository) Create(ctx context.Context, v scenario.TestRun) (scenario.TestRun, error) {
	id, err := r.creation(v.TenantID, v.ID, v.CreatedAt, string(v.Status))
	if err != nil {
		return scenario.TestRun{}, err
	}
	args, err := r.ids(v.TenantID, v.ScenarioID, v.LiveIterationID, v.PhantomIterationID)
	if err != nil {
		return scenario.TestRun{}, err
	}
	values := []any{id, args[0], args[1], args[2], args[3], mysqlstore.Text(v.Status), mysqlstore.DateTime{Time: v.CreatedAt}, mysqlstore.DateTime{Time: v.ExpiresAt}, mysqlstore.DateTime{Time: v.UpdatedAt}}
	if _, err := r.tx.ExecContext(ctx, "INSERT INTO "+r.schema+".decision_engine_test_runs(id,tenant_id,scenario_id,live_iteration_id,phantom_iteration_id,status,created_at,expires_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)", values...); err != nil {
		return scenario.TestRun{}, err
	}
	return r.GetByID(ctx, v.TenantID, v.ID)
}
func (r TestRunRepository) UpdateStatus(ctx context.Context, t, id string, status scenario.TestRunStatus, at time.Time) (scenario.TestRun, error) {
	if err := r.mutating(); err != nil {
		return scenario.TestRun{}, err
	}
	args, err := r.ids(t, id)
	if err != nil {
		return scenario.TestRun{}, err
	}
	if err := r.scope(t, string(status)); err != nil {
		return scenario.TestRun{}, err
	}
	var found mysqlstore.UUID
	if err := r.tx.QueryRowContext(ctx, "SELECT id FROM "+r.schema+".decision_engine_test_runs WHERE tenant_id=? AND id=? FOR UPDATE", args...).Scan(&found); err != nil {
		return scenario.TestRun{}, err
	}
	result, err := r.tx.ExecContext(ctx, "UPDATE "+r.schema+".decision_engine_test_runs SET status=?,updated_at=? WHERE tenant_id=? AND id=?", append([]any{mysqlstore.Text(status), mysqlstore.DateTime{Time: at}}, args...)...)
	if err != nil {
		return scenario.TestRun{}, err
	}
	if _, err = result.RowsAffected(); err != nil {
		return scenario.TestRun{}, err
	}
	return r.GetByID(ctx, t, id)
}
