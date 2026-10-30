package mysql

import (
	"context"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

type ScenarioPublicationRepository struct{ authoringRepository }

var _ ports.ScenarioPublicationRepository = ScenarioPublicationRepository{}

func (r ScenarioPublicationRepository) selectSQL() string {
	return "SELECT id,tenant_id,scenario_id,iteration_id,created_at," + r.project([]string{"action"}) + " FROM " + r.schema + ".decision_engine_scenario_publications"
}
func (r ScenarioPublicationRepository) scan(row rowScanner) (scenario.Publication, int64, error) {
	var id, tenant, parent, iteration mysqlstore.UUID
	var at mysqlstore.DateTime
	var size int64
	var action mysqlstore.Text
	if err := row.Scan(&id, &tenant, &parent, &iteration, &at, &size, &action); err != nil {
		return scenario.Publication{}, 0, err
	}
	v := scenario.Publication{ID: uuid.UUID(id).String(), TenantID: uuid.UUID(tenant).String(), ScenarioID: uuid.UUID(parent).String(), IterationID: uuid.UUID(iteration).String(), CreatedAt: at.Time, Action: scenario.PublicationAction(action)}
	return v, size, r.scope(v.TenantID, string(v.Action))
}
func (r ScenarioPublicationRepository) Create(ctx context.Context, v scenario.Publication) (scenario.Publication, error) {
	if !r.publication {
		return scenario.Publication{}, ErrPublicationOperationRequired
	}
	id, err := r.creation(v.TenantID, v.ID, v.CreatedAt, string(v.Action))
	if err != nil {
		return scenario.Publication{}, err
	}
	args, err := r.ids(v.TenantID, v.ScenarioID, v.IterationID)
	if err != nil {
		return scenario.Publication{}, err
	}
	if _, err := r.tx.ExecContext(ctx, "INSERT INTO "+r.schema+".decision_engine_scenario_publications(id,tenant_id,scenario_id,iteration_id,action,created_at) VALUES(?,?,?,?,?,?)", id, args[0], args[1], args[2], mysqlstore.Text(string(v.Action)), mysqlstore.DateTime{Time: v.CreatedAt}); err != nil {
		return scenario.Publication{}, err
	}
	return platformOne(r.platformRepository, r.tx.QueryRowContext(ctx, r.selectSQL()+" WHERE tenant_id=? AND id=?", args[0], id), r.scan)
}
func (r ScenarioPublicationRepository) ListByScenario(ctx context.Context, tenant, parent string) ([]scenario.Publication, error) {
	args, err := r.ids(tenant, parent)
	if err != nil {
		return nil, err
	}
	return platformList(ctx, r.platformRepository, r.selectSQL()+" WHERE tenant_id=? AND scenario_id=? ORDER BY created_at DESC,id DESC", args, r.scan)
}
