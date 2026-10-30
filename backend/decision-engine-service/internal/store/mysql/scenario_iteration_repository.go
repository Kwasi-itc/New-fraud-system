package mysql

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

type ScenarioIterationRepository struct{ authoringRepository }

var _ ports.ScenarioIterationRepository = ScenarioIterationRepository{}

func (r ScenarioIterationRepository) selectSQL() string {
	return "SELECT id,tenant_id,scenario_id,version,created_at,committed_at,score_review_threshold,score_block_and_review_threshold,score_decline_threshold," + r.project([]string{"status", "schedule"}, "trigger_formula") + " FROM " + r.schema + ".decision_engine_scenario_iterations"
}
func (r ScenarioIterationRepository) scan(row rowScanner) (scenario.Iteration, int64, error) {
	var id, tenant, parent mysqlstore.UUID
	var version mysqlstore.Int32
	var created mysqlstore.DateTime
	var committed mysqlstore.NullDateTime
	var a, b, c mysqlstore.NullInt32
	var status, schedule mysqlstore.Text
	var formula mysqlstore.JSON
	var size int64
	if err := row.Scan(&id, &tenant, &parent, &version, &created, &committed, &a, &b, &c, &size, &status, &schedule, &formula); err != nil {
		return scenario.Iteration{}, 0, err
	}
	v := scenario.Iteration{ID: uuid.UUID(id).String(), TenantID: uuid.UUID(tenant).String(), ScenarioID: uuid.UUID(parent).String(), Version: int(version), Status: scenario.IterationStatus(status), Schedule: string(schedule), TriggerFormula: append([]byte(nil), formula...), CreatedAt: created.Time, ScoreReviewThreshold: intPointer(a), ScoreBlockAndReviewThreshold: intPointer(b), ScoreDeclineThreshold: intPointer(c)}
	if committed.Valid {
		at := committed.DateTime.Time
		v.CommittedAt = &at
	}
	return v, size, r.scope(v.TenantID, v.Schedule, string(v.Status))
}
func intPointer(v mysqlstore.NullInt32) *int {
	if !v.Valid {
		return nil
	}
	n := int(v.Int32)
	return &n
}
func (r ScenarioIterationRepository) GetByID(ctx context.Context, tenant, parent, id string) (scenario.Iteration, error) {
	args, err := r.ids(tenant, parent, id)
	if err != nil {
		return scenario.Iteration{}, err
	}
	return platformOne(r.platformRepository, r.tx.QueryRowContext(ctx, r.selectSQL()+" WHERE tenant_id=? AND scenario_id=? AND id=?", args...), r.scan)
}
func (r ScenarioIterationRepository) ListByScenario(ctx context.Context, tenant, parent string) ([]scenario.Iteration, error) {
	args, err := r.ids(tenant, parent)
	if err != nil {
		return nil, err
	}
	return platformList(ctx, r.platformRepository, r.selectSQL()+" WHERE tenant_id=? AND scenario_id=? ORDER BY version DESC,created_at DESC,id DESC", args, r.scan)
}

// Global schedule enumeration must use a separately authorized worker adapter;
// silently returning this tenant's subset would lose other tenants' work.
func (r ScenarioIterationRepository) ListLiveScheduled(context.Context, int) ([]scenario.Iteration, error) {
	return nil, ErrSystemScopeRequired
}

func (r ScenarioIterationRepository) lockCounter(ctx context.Context, tenant, parent string) ([]any, int, error) {
	if err := r.mutating(); err != nil {
		return nil, 0, err
	}
	args, err := r.ids(tenant, parent)
	if err != nil {
		return nil, 0, err
	}
	var found mysqlstore.UUID
	// Parent SHARE precedes the dedicated counter and iteration locks. This
	// coordinates parent deletion/publication without an exclusive tenant lock.
	if err := r.tx.QueryRowContext(ctx, "SELECT id FROM "+r.schema+".decision_engine_scenarios WHERE tenant_id=? AND id=? FOR SHARE", args...).Scan(&found); err != nil {
		return nil, 0, err
	}
	if _, err := r.tx.ExecContext(ctx, "INSERT INTO "+r.schema+".decision_engine_scenario_versions(tenant_id,scenario_id,last_version) VALUES(?,?,0) ON DUPLICATE KEY UPDATE last_version=last_version", args...); err != nil {
		return nil, 0, err
	}
	var previous mysqlstore.Int32
	err = r.tx.QueryRowContext(ctx, "SELECT last_version FROM "+r.schema+".decision_engine_scenario_versions WHERE tenant_id=? AND scenario_id=? FOR UPDATE", args...).Scan(&previous)
	return args, int(previous), err
}

// NextVersion reserves only inside the caller's mutation; a later failure must
// roll back that whole mutation. No standalone autocommit reservation is exposed.
func (r ScenarioIterationRepository) NextVersion(ctx context.Context, tenant, parent string) (int, error) {
	args, previous, err := r.lockCounter(ctx, tenant, parent)
	if err != nil {
		return 0, err
	}
	if previous == math.MaxInt32 {
		return 0, ErrIterationVersionExhausted
	}
	next := previous + 1
	_, err = r.tx.ExecContext(ctx, "UPDATE "+r.schema+".decision_engine_scenario_versions SET last_version=? WHERE tenant_id=? AND scenario_id=?", append([]any{next}, args...)...)
	return next, err
}
func (r ScenarioIterationRepository) validate(v scenario.Iteration) error {
	if err := r.scope(v.TenantID, v.Schedule, string(v.Status), string(v.TriggerFormula)); err != nil {
		return err
	}
	if err := v.Validate(); err != nil {
		return err
	}
	if len(v.TriggerFormula) > r.maxBytes {
		return ErrProjectionLimit
	}
	if _, err := (mysqlstore.JSON(v.TriggerFormula)).Value(); err != nil {
		return err
	}
	if v.Version < 1 {
		return errors.New("iteration version must be positive")
	}
	if _, err := checkedInt(v.Version); err != nil {
		return err
	}
	for _, p := range []*int{v.ScoreReviewThreshold, v.ScoreBlockAndReviewThreshold, v.ScoreDeclineThreshold} {
		if p != nil {
			if _, err := checkedInt(*p); err != nil {
				return err
			}
		}
	}
	return nil
}
func (r ScenarioIterationRepository) Create(ctx context.Context, v scenario.Iteration) (scenario.Iteration, error) {
	if err := r.validate(v); err != nil {
		return scenario.Iteration{}, err
	}
	id, err := r.creation(v.TenantID, v.ID, v.CreatedAt)
	if err != nil {
		return scenario.Iteration{}, err
	}
	args, _, err := r.lockCounter(ctx, v.TenantID, v.ScenarioID)
	if err != nil {
		return scenario.Iteration{}, err
	}
	var committed mysqlstore.NullDateTime
	if v.CommittedAt != nil {
		committed = mysqlstore.NullDateTime{DateTime: mysqlstore.DateTime{Time: *v.CommittedAt}, Valid: true}
	}
	_, err = r.tx.ExecContext(ctx, "INSERT INTO "+r.schema+".decision_engine_scenario_iterations(id,tenant_id,scenario_id,version,status,trigger_formula,score_review_threshold,score_block_and_review_threshold,score_decline_threshold,schedule,created_at,committed_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)", id, args[0], args[1], v.Version, mysqlstore.Text(string(v.Status)), mysqlstore.JSON(v.TriggerFormula), threshold(v.ScoreReviewThreshold), threshold(v.ScoreBlockAndReviewThreshold), threshold(v.ScoreDeclineThreshold), mysqlstore.Text(v.Schedule), mysqlstore.DateTime{Time: v.CreatedAt}, committed)
	if err != nil {
		return scenario.Iteration{}, err
	}
	// Explicit clone/import versions may arrive out of order in one new-scenario
	// transaction; advance the high-water mark without reusing a version later.
	if _, err := r.tx.ExecContext(ctx, "UPDATE "+r.schema+".decision_engine_scenario_versions SET last_version=GREATEST(last_version,?) WHERE tenant_id=? AND scenario_id=?", append([]any{v.Version}, args...)...); err != nil {
		return scenario.Iteration{}, err
	}
	return r.GetByID(ctx, v.TenantID, v.ScenarioID, v.ID)
}
func (r ScenarioIterationRepository) Update(ctx context.Context, v scenario.Iteration) (scenario.Iteration, error) {
	if err := r.mutating(); err != nil {
		return scenario.Iteration{}, err
	}
	if err := r.validate(v); err != nil {
		return scenario.Iteration{}, err
	}
	args, err := r.ids(v.TenantID, v.ScenarioID, v.ID)
	if err != nil {
		return scenario.Iteration{}, err
	}
	var status string
	if err := r.tx.QueryRowContext(ctx, "SELECT status FROM "+r.schema+".decision_engine_scenario_iterations WHERE tenant_id=? AND scenario_id=? AND id=? FOR UPDATE", args...).Scan(&status); err != nil {
		return scenario.Iteration{}, err
	}
	if status != "draft" || v.Status != scenario.IterationStatusDraft || v.CommittedAt != nil {
		return scenario.Iteration{}, ErrCommittedIteration
	}
	values := []any{mysqlstore.JSON(v.TriggerFormula), threshold(v.ScoreReviewThreshold), threshold(v.ScoreBlockAndReviewThreshold), threshold(v.ScoreDeclineThreshold), mysqlstore.Text(v.Schedule)}
	values = append(values, args...)
	if _, err := r.tx.ExecContext(ctx, "UPDATE "+r.schema+".decision_engine_scenario_iterations SET trigger_formula=?,score_review_threshold=?,score_block_and_review_threshold=?,score_decline_threshold=?,schedule=? WHERE tenant_id=? AND scenario_id=? AND id=?", values...); err != nil {
		return scenario.Iteration{}, err
	}
	return r.GetByID(ctx, v.TenantID, v.ScenarioID, v.ID)
}
func (r ScenarioIterationRepository) Commit(ctx context.Context, tenant, parent, id string, at time.Time) (scenario.Iteration, error) {
	if err := r.mutating(); err != nil {
		return scenario.Iteration{}, err
	}
	args, err := r.ids(tenant, parent, id)
	if err != nil {
		return scenario.Iteration{}, err
	}
	var status string
	if err := r.tx.QueryRowContext(ctx, "SELECT status FROM "+r.schema+".decision_engine_scenario_iterations WHERE tenant_id=? AND scenario_id=? AND id=? FOR UPDATE", args...).Scan(&status); err != nil {
		return scenario.Iteration{}, err
	}
	if status != "draft" {
		return scenario.Iteration{}, ErrCommittedIteration
	}
	if _, err := r.tx.ExecContext(ctx, "UPDATE "+r.schema+".decision_engine_scenario_iterations SET status='committed',committed_at=? WHERE tenant_id=? AND scenario_id=? AND id=?", append([]any{mysqlstore.DateTime{Time: at}}, args...)...); err != nil {
		return scenario.Iteration{}, err
	}
	return r.GetByID(ctx, tenant, parent, id)
}
