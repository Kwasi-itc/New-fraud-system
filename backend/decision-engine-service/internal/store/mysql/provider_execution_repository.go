package mysql

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

type providerExecutionRepository struct {
	authoringRepository
	screening bool
}
type providerExecution struct {
	id, tenant, config, decision, scenario, status string
	request, response                              mysqlstore.JSON
	reference, lastError                           string
	created, updated                               time.Time
	sent, completed, failed                        *time.Time
}

func (r providerExecutionRepository) table() string {
	if r.screening {
		return "decision_engine_screening_executions"
	}
	return "decision_engine_scoring_requests"
}
func (r providerExecutionRepository) selectSQL() string {
	return "SELECT id,tenant_id,config_id,decision_id,scenario_id,status,created_at,updated_at,sent_at,completed_at,failed_at," + r.project([]string{"provider_reference", "last_error"}, "request_json", "response_json") + " FROM " + r.schema + "." + r.table()
}
func optionalDate(v *time.Time) (mysqlstore.NullDateTime, error) {
	if v == nil {
		return mysqlstore.NullDateTime{}, nil
	}
	d := mysqlstore.DateTime{Time: *v}
	if _, err := d.Value(); err != nil {
		return mysqlstore.NullDateTime{}, err
	}
	return mysqlstore.NullDateTime{DateTime: d, Valid: true}, nil
}
func datePointer(v mysqlstore.NullDateTime) *time.Time {
	if !v.Valid {
		return nil
	}
	at := v.DateTime.Time
	return &at
}
func validProviderStatus(s string) bool {
	switch s {
	case "pending", "queued", "sent", "completed", "failed":
		return true
	}
	return false
}
func (r providerExecutionRepository) scan(row rowScanner) (providerExecution, int64, error) {
	var id, tenant, config, decision, scenario mysqlstore.UUID
	var status string
	var created, updated mysqlstore.DateTime
	var sent, completed, failed mysqlstore.NullDateTime
	var size int64
	var ref, last mysqlstore.Text
	var request, response mysqlstore.JSON
	if err := row.Scan(&id, &tenant, &config, &decision, &scenario, &status, &created, &updated, &sent, &completed, &failed, &size, &ref, &last, &request, &response); err != nil {
		return providerExecution{}, 0, err
	}
	return providerExecution{uuid.UUID(id).String(), uuid.UUID(tenant).String(), uuid.UUID(config).String(), uuid.UUID(decision).String(), uuid.UUID(scenario).String(), status, request, response, string(ref), string(last), created.Time, updated.Time, datePointer(sent), datePointer(completed), datePointer(failed)}, size, nil
}
func (r providerExecutionRepository) get(ctx context.Context, tenant, id string) (providerExecution, error) {
	args, err := r.ids(tenant, id)
	if err != nil {
		return providerExecution{}, err
	}
	return platformOne(r.platformRepository, r.tx.QueryRowContext(ctx, r.selectSQL()+" WHERE tenant_id=? AND id=?", args...), r.scan)
}
func (r providerExecutionRepository) list(ctx context.Context, tenant, decision string) ([]providerExecution, error) {
	args, err := r.ids(tenant, decision)
	if err != nil {
		return nil, err
	}
	return platformList(ctx, r.platformRepository, r.selectSQL()+" WHERE tenant_id=? AND decision_id=? ORDER BY created_at,id", args, r.scan)
}
func (r providerExecutionRepository) validate(v providerExecution) ([]any, error) {
	id, err := r.creation(v.tenant, v.id, v.created, v.reference, v.lastError)
	if err != nil {
		return nil, err
	}
	args, err := r.ids(v.tenant, v.config, v.decision, v.scenario)
	if err != nil {
		return nil, err
	}
	if !validProviderStatus(v.status) {
		return nil, errors.New("invalid provider execution status")
	}
	if len(v.response) == 0 {
		v.response = mysqlstore.JSON(`{}`)
	}
	if _, err := v.request.Value(); err != nil {
		return nil, err
	}
	if _, err := v.response.Value(); err != nil {
		return nil, err
	}
	if len(v.request)+len(v.response)+len(v.reference)+len(v.lastError) > r.maxBytes {
		return nil, ErrProjectionLimit
	}
	updated := mysqlstore.DateTime{Time: v.updated}
	if _, err := updated.Value(); err != nil {
		return nil, err
	}
	sent, err := optionalDate(v.sent)
	if err != nil {
		return nil, err
	}
	completed, err := optionalDate(v.completed)
	if err != nil {
		return nil, err
	}
	failed, err := optionalDate(v.failed)
	if err != nil {
		return nil, err
	}
	return []any{id, args[0], args[1], args[2], args[3], nil, v.status, v.request, v.response, mysqlstore.Text(v.reference), mysqlstore.Text(v.lastError), mysqlstore.DateTime{Time: v.created}, updated, sent, completed, failed}, nil
}
func (r providerExecutionRepository) create(ctx context.Context, items []providerExecution) ([]providerExecution, error) {
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
		bound, err := r.validate(v)
		if err != nil {
			return nil, err
		}
		values[i] = bound
		ids[i] = v.id
		parents[v.decision] = true
		used += len(v.request) + max(2, len(v.response)) + len(v.reference) + len(v.lastError)
		if used > r.maxBytes {
			return nil, ErrProjectionLimit
		}
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
		values[i][5] = bindings[v.decision]
	}
	return insertEffectBatch(ctx, r.authoringRepository, r.table(), "id,tenant_id,config_id,decision_id,scenario_id,decision_iteration_id,status,request_json,response_json,provider_reference,last_error,created_at,updated_at,sent_at,completed_at,failed_at", r.selectSQL(), values, ids, r.scan, func(v providerExecution) string { return v.id })
}
func (r providerExecutionRepository) update(ctx context.Context, v providerExecution) (providerExecution, error) {
	if err := r.mutating(); err != nil {
		return providerExecution{}, err
	}
	args, err := r.validate(v)
	if err != nil {
		return providerExecution{}, err
	}
	// Lock only this projection row. Source-managed screening cannot be changed
	// by a legacy dispatcher; reconciliation owns its monotonic version.
	where := " WHERE tenant_id=? AND id=?"
	if r.screening {
		where += " AND projection_source_id IS NULL"
	}
	var id mysqlstore.UUID
	if err := r.tx.QueryRowContext(ctx, "SELECT id FROM "+r.schema+"."+r.table()+where+" FOR UPDATE", args[1], args[0]).Scan(&id); err != nil {
		return providerExecution{}, err
	}
	current, err := r.get(ctx, v.tenant, v.id)
	if err != nil {
		return providerExecution{}, err
	}
	if current.config != v.config || current.decision != v.decision || current.scenario != v.scenario {
		return providerExecution{}, ErrOwnerRelationshipConflict
	}
	_, err = r.tx.ExecContext(ctx, "UPDATE "+r.schema+"."+r.table()+" SET status=?,response_json=?,provider_reference=?,last_error=?,updated_at=?,sent_at=?,completed_at=?,failed_at=?"+where, args[6], args[8], args[9], args[10], args[12], args[13], args[14], args[15], args[1], args[0])
	if err != nil {
		return providerExecution{}, err
	}
	return r.get(ctx, v.tenant, v.id)
}
func (r providerExecutionRepository) status(ctx context.Context, id, status string) error {
	if err := r.mutating(); err != nil {
		return err
	}
	if !validProviderStatus(status) {
		return errors.New("invalid provider execution status")
	}
	args, err := r.ids(r.tenant.String(), id)
	if err != nil {
		return err
	}
	where := " WHERE tenant_id=? AND id=?"
	if r.screening {
		where += " AND projection_source_id IS NULL"
	}
	var locked mysqlstore.UUID
	if err := r.tx.QueryRowContext(ctx, "SELECT id FROM "+r.schema+"."+r.table()+where+" FOR UPDATE", args...).Scan(&locked); err != nil {
		return err
	}
	_, err = r.tx.ExecContext(ctx, "UPDATE "+r.schema+"."+r.table()+" SET status=?,updated_at=UTC_TIMESTAMP(6),sent_at=CASE WHEN ?='sent' THEN UTC_TIMESTAMP(6) ELSE sent_at END,completed_at=CASE WHEN ?='completed' THEN UTC_TIMESTAMP(6) ELSE completed_at END,failed_at=CASE WHEN ?='failed' THEN UTC_TIMESTAMP(6) ELSE failed_at END"+where, status, status, status, status, args[0], args[1])
	return err
}
