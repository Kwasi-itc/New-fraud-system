package mysql

import (
	"context"
	"errors"
	d "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
)

const configColumns = `id,tenant_id,name,object_type,provider,field_map_json,review_inbox_id,enabled,created_at,updated_at`

func scanConfig(row scanner) (d.ContinuousConfig, error) {
	var v d.ContinuousConfig
	var inbox string
	e := row.Scan(uid(&v.ID), uid(&v.TenantID), &v.Name, &v.ObjectType, &v.Provider, &v.FieldMapJSON, uid(&inbox), &v.Enabled, &v.CreatedAt, &v.UpdatedAt)
	if inbox != "" {
		v.ReviewInboxID = &inbox
	}
	return v, normalize(e)
}
func (r configs) Create(ctx context.Context, v d.ContinuousConfig) (d.ContinuousConfig, error) {
	if e := r.guardInbox(ctx, v.TenantID, v.ReviewInboxID); e != nil {
		return v, e
	}
	var inbox any
	if v.ReviewInboxID != nil {
		inbox = nullableID(*v.ReviewInboxID)
	}
	_, e := r.q.ExecContext(ctx, "INSERT INTO "+r.table("continuous_configs")+"(tenant_id,id,name,object_type,provider,field_map_json,review_inbox_id,enabled,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)", id(v.TenantID), id(v.ID), v.Name, v.ObjectType, v.Provider, jsonValue(v.FieldMapJSON), inbox, v.Enabled, v.CreatedAt, v.UpdatedAt)
	return v, normalize(e)
}
func (r configs) GetByID(ctx context.Context, t, c string) (d.ContinuousConfig, error) {
	return scanConfig(r.q.QueryRowContext(ctx, "SELECT "+configColumns+" FROM "+r.table("continuous_configs")+" WHERE tenant_id=? AND id=? AND deleted_at IS NULL", id(t), id(c)))
}
func (r configs) ListByTenant(ctx context.Context, t string) ([]d.ContinuousConfig, error) {
	return list(ctx, r.Store, "SELECT "+configColumns+" FROM "+r.table("continuous_configs")+" WHERE tenant_id=? AND deleted_at IS NULL ORDER BY created_at,id LIMIT 501", []any{id(t)}, 500, scanConfig)
}
func (r configs) Update(ctx context.Context, v d.ContinuousConfig) (d.ContinuousConfig, error) {
	if e := r.guardInbox(ctx, v.TenantID, v.ReviewInboxID); e != nil {
		return v, e
	}
	var inbox any
	if v.ReviewInboxID != nil {
		inbox = nullableID(*v.ReviewInboxID)
	}
	_, e := r.q.ExecContext(ctx, "UPDATE "+r.table("continuous_configs")+" SET name=?,object_type=?,provider=?,field_map_json=?,review_inbox_id=?,enabled=?,updated_at=? WHERE tenant_id=? AND id=? AND deleted_at IS NULL", v.Name, v.ObjectType, v.Provider, jsonValue(v.FieldMapJSON), inbox, v.Enabled, v.UpdatedAt, id(v.TenantID), id(v.ID))
	if e != nil {
		return v, normalize(e)
	}
	return r.GetByID(ctx, v.TenantID, v.ID)
}
func (r configs) Delete(ctx context.Context, t, c string) error {
	_, e := r.q.ExecContext(ctx, "UPDATE "+r.table("continuous_configs")+" SET deleted_at=UTC_TIMESTAMP(6),enabled=FALSE,updated_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND id=? AND deleted_at IS NULL", id(t), id(c))
	return normalize(e)
}

const monitoredColumns = `id,tenant_id,config_id,object_type,object_id,status,attributes_json,created_at,updated_at,last_screened_at`

func scanMonitored(row scanner) (d.MonitoredObject, error) {
	var v d.MonitoredObject
	e := row.Scan(uid(&v.ID), uid(&v.TenantID), uid(&v.ConfigID), &v.ObjectType, &v.ObjectID, &v.Status, &v.AttributesJSON, &v.CreatedAt, &v.UpdatedAt, &v.LastScreenedAt)
	return v, normalize(e)
}
func (r monitored) Create(ctx context.Context, v d.MonitoredObject) (d.MonitoredObject, error) {
	_, e := r.q.ExecContext(ctx, "INSERT INTO "+r.table("monitored_objects")+"(id,tenant_id,config_id,object_type,object_id,status,attributes_json,created_at,updated_at,last_screened_at) VALUES(?,?,?,?,?,?,?,?,?,?)", id(v.ID), id(v.TenantID), id(v.ConfigID), v.ObjectType, []byte(v.ObjectID), v.Status, jsonValue(v.AttributesJSON), v.CreatedAt, v.UpdatedAt, v.LastScreenedAt)
	return v, normalize(e)
}
func (r monitored) GetByID(ctx context.Context, t, m string) (d.MonitoredObject, error) {
	return scanMonitored(r.q.QueryRowContext(ctx, "SELECT "+monitoredColumns+" FROM "+r.table("monitored_objects")+" WHERE tenant_id=? AND id=? AND deleted_at IS NULL", id(t), id(m)))
}
func (r monitored) Lock(ctx context.Context, t, m string) (d.MonitoredObject, error) {
	return scanMonitored(r.q.QueryRowContext(ctx, "SELECT "+monitoredColumns+" FROM "+r.table("monitored_objects")+" WHERE tenant_id=? AND id=? AND deleted_at IS NULL FOR UPDATE", id(t), id(m)))
}
func (r monitored) ListByConfig(ctx context.Context, t, c string) ([]d.MonitoredObject, error) {
	return list(ctx, r.Store, "SELECT "+monitoredColumns+" FROM "+r.table("monitored_objects")+" WHERE tenant_id=? AND config_id=? AND deleted_at IS NULL ORDER BY created_at,id LIMIT 501", []any{id(t), id(c)}, 500, scanMonitored)
}
func (r monitored) ListByStatus(ctx context.Context, status d.MonitoredObjectStatus, n int) ([]d.MonitoredObject, error) {
	return list(ctx, r.Store, "SELECT "+monitoredColumns+" FROM "+r.table("monitored_objects")+" WHERE status=? AND deleted_at IS NULL AND EXISTS(SELECT 1 FROM "+r.schema+".identity_tenants t WHERE t.id=tenant_id AND t.active) AND NOT EXISTS(SELECT 1 FROM "+r.table("work_leases")+" w WHERE w.tenant_id="+r.table("monitored_objects")+".tenant_id AND w.id="+r.table("monitored_objects")+".id AND w.kind='continuous' AND w.lease_until>UTC_TIMESTAMP(6)) ORDER BY updated_at,id LIMIT ?", []any{status, batch(n)}, 500, scanMonitored)
}
func (r monitored) ListByTenantAndStatus(ctx context.Context, t string, status d.MonitoredObjectStatus, n int) ([]d.MonitoredObject, error) {
	if n < 1 || n > 500 {
		return nil, errors.New("invalid monitored batch")
	}
	return list(ctx, r.Store, "SELECT "+monitoredColumns+" FROM "+r.table("monitored_objects")+" WHERE tenant_id=? AND status=? AND deleted_at IS NULL ORDER BY created_at,id LIMIT ?", []any{id(t), status, n + 1}, n, scanMonitored)
}
func (r monitored) Update(ctx context.Context, v d.MonitoredObject) (d.MonitoredObject, error) {
	if v.Status == "pending" {
		if _, e := r.q.ExecContext(ctx, "UPDATE "+r.table("work_leases")+" SET attempts=0,last_error='' WHERE tenant_id=? AND kind='continuous' AND id=? AND lease_until<=UTC_TIMESTAMP(6)", id(v.TenantID), id(v.ID)); e != nil {
			return v, normalize(e)
		}
	}
	_, e := r.q.ExecContext(ctx, "UPDATE "+r.table("monitored_objects")+" SET status=?,attributes_json=?,updated_at=?,last_screened_at=? WHERE tenant_id=? AND id=? AND config_id=? AND deleted_at IS NULL", v.Status, jsonValue(v.AttributesJSON), v.UpdatedAt, v.LastScreenedAt, id(v.TenantID), id(v.ID), id(v.ConfigID))
	if e != nil {
		return v, normalize(e)
	}
	return r.GetByID(ctx, v.TenantID, v.ID)
}
func (r monitored) Delete(ctx context.Context, t, m string) error {
	_, e := r.q.ExecContext(ctx, "UPDATE "+r.table("monitored_objects")+" SET deleted_at=UTC_TIMESTAMP(6),status='archived',updated_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND id=? AND deleted_at IS NULL", id(t), id(m))
	return normalize(e)
}

const jobColumns = `id,tenant_id,provider,job_type,status,provider_cursor,result_json,last_error,attempt_count,created_at,updated_at,started_at,completed_at`

func scanJob(row scanner) (d.DatasetUpdateJob, error) {
	var v d.DatasetUpdateJob
	e := row.Scan(uid(&v.ID), uid(&v.TenantID), &v.Provider, &v.JobType, &v.Status, &v.Cursor, &v.ResultJSON, &v.LastError, &v.AttemptCount, &v.CreatedAt, &v.UpdatedAt, &v.StartedAt, &v.CompletedAt)
	return v, normalize(e)
}
func (r jobs) Create(ctx context.Context, v d.DatasetUpdateJob) (d.DatasetUpdateJob, error) {
	_, e := r.q.ExecContext(ctx, "INSERT INTO "+r.table("dataset_jobs")+"(id,tenant_id,provider,job_type,status,provider_cursor,result_json,last_error,attempt_count,created_at,updated_at,started_at,completed_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)", id(v.ID), id(v.TenantID), v.Provider, v.JobType, v.Status, v.Cursor, jsonValue(v.ResultJSON), v.LastError, v.AttemptCount, v.CreatedAt, v.UpdatedAt, v.StartedAt, v.CompletedAt)
	return v, normalize(e)
}
func (r jobs) GetByID(ctx context.Context, t, j string) (d.DatasetUpdateJob, error) {
	return scanJob(r.q.QueryRowContext(ctx, "SELECT "+jobColumns+" FROM "+r.table("dataset_jobs")+" WHERE tenant_id=? AND id=?", id(t), id(j)))
}
func (r jobs) ListByTenant(ctx context.Context, t string) ([]d.DatasetUpdateJob, error) {
	return list(ctx, r.Store, "SELECT "+jobColumns+" FROM "+r.table("dataset_jobs")+" WHERE tenant_id=? ORDER BY created_at,id LIMIT 501", []any{id(t)}, 500, scanJob)
}
func (r jobs) ListByStatus(ctx context.Context, status d.DatasetUpdateJobStatus, n int) ([]d.DatasetUpdateJob, error) {
	return list(ctx, r.Store, "SELECT "+jobColumns+" FROM "+r.table("dataset_jobs")+" WHERE status=? AND EXISTS(SELECT 1 FROM "+r.schema+".identity_tenants t WHERE t.id=tenant_id AND t.active) AND NOT EXISTS(SELECT 1 FROM "+r.table("work_leases")+" w WHERE w.tenant_id="+r.table("dataset_jobs")+".tenant_id AND w.id="+r.table("dataset_jobs")+".id AND w.kind='dataset' AND w.lease_until>UTC_TIMESTAMP(6)) ORDER BY updated_at,id LIMIT ?", []any{status, batch(n)}, 500, scanJob)
}
func (r jobs) Update(ctx context.Context, v d.DatasetUpdateJob) (d.DatasetUpdateJob, error) {
	_, e := r.q.ExecContext(ctx, "UPDATE "+r.table("dataset_jobs")+" SET status=?,provider_cursor=?,result_json=?,last_error=?,attempt_count=?,updated_at=?,started_at=?,completed_at=? WHERE tenant_id=? AND id=?", v.Status, v.Cursor, jsonValue(v.ResultJSON), v.LastError, v.AttemptCount, v.UpdatedAt, v.StartedAt, v.CompletedAt, id(v.TenantID), id(v.ID))
	if e != nil {
		return v, normalize(e)
	}
	return r.GetByID(ctx, v.TenantID, v.ID)
}
