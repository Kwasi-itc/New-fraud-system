package postgres

import (
	"context"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/ports"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"time"
)

var errMaintenanceFence = errors.New("dataset_maintenance_lease_lost")

func (r DatasetUpdateJobRepository) ClaimMaintenance(ctx context.Context, tenant, job, token string, now time.Time) (ports.DatasetCheckpoint, bool, error) {
	var status, kind, cursor string
	if e := r.q.QueryRow(ctx, `SELECT status,job_type,cursor FROM screening.dataset_update_jobs WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenant, job).Scan(&status, &kind, &cursor); e != nil {
		return ports.DatasetCheckpoint{}, false, e
	}
	if status != "pending" && status != "processing" {
		return ports.DatasetCheckpoint{}, false, nil
	}
	var live bool
	e := r.q.QueryRow(ctx, `SELECT lease_until>$3 FROM screening.dataset_maintenance_progress WHERE tenant_id=$1 AND job_id=$2 FOR UPDATE`, tenant, job, now).Scan(&live)
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return ports.DatasetCheckpoint{}, false, e
	}
	if live {
		return ports.DatasetCheckpoint{}, false, nil
	}
	if _, e = r.q.Exec(ctx, `INSERT INTO screening.dataset_maintenance_progress(tenant_id,job_id,lease_token,lease_until,delta_cursor,delta_done,needs_rescreen) VALUES($1,$2,$3,$4::timestamptz+interval '5 minutes',$5,$6,$6) ON CONFLICT(tenant_id,job_id) DO UPDATE SET lease_token=EXCLUDED.lease_token,lease_until=EXCLUDED.lease_until`, tenant, job, token, now, cursor, kind == "rescreen_monitored_objects"); e != nil {
		return ports.DatasetCheckpoint{}, false, e
	}
	if _, e = r.q.Exec(ctx, `UPDATE screening.dataset_update_jobs SET status='processing',attempt_count=attempt_count+1,started_at=coalesce(started_at,$3),updated_at=$3,last_error='' WHERE tenant_id=$1 AND id=$2`, tenant, job, now); e != nil {
		return ports.DatasetCheckpoint{}, false, e
	}
	out, e := r.maintenanceCheckpoint(ctx, tenant, job, token, now)
	return out, e == nil, e
}
func (r DatasetUpdateJobRepository) maintenanceCheckpoint(ctx context.Context, tenant, job, token string, now time.Time) (ports.DatasetCheckpoint, error) {
	var out ports.DatasetCheckpoint
	var id string
	if e := r.q.QueryRow(ctx, `SELECT id::text FROM screening.dataset_update_jobs WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenant, job).Scan(&id); e != nil {
		return out, e
	}
	e := r.q.QueryRow(ctx, `SELECT delta_cursor,delta_done,needs_rescreen,objects_done,delta_pages,queued_objects FROM screening.dataset_maintenance_progress WHERE tenant_id=$1 AND job_id=$2 AND lease_token=$3 AND lease_until>$4 FOR UPDATE`, tenant, job, token, now).Scan(&out.Cursor, &out.DeltaDone, &out.NeedsRescreen, &out.ObjectsDone, &out.Pages, &out.Queued)
	if errors.Is(e, pgx.ErrNoRows) {
		return out, errMaintenanceFence
	}
	return out, e
}
func (r DatasetUpdateJobRepository) SaveDeltaPage(ctx context.Context, tenant, job, token string, delta screening.DatasetDelta, now time.Time) (ports.DatasetCheckpoint, error) {
	cp, e := r.maintenanceCheckpoint(ctx, tenant, job, token, now)
	if e != nil {
		return cp, e
	}
	if cp.DeltaDone {
		return cp, errMaintenanceFence
	}
	if delta.HasMore && (delta.NextCursor == "" || delta.NextCursor == cp.Cursor) {
		return cp, errors.New("non_advancing_dataset_cursor")
	}
	if delta.NextCursor != "" {
		cp.Cursor = delta.NextCursor
	}
	cp.DeltaDone = !delta.HasMore
	cp.NeedsRescreen = cp.NeedsRescreen || delta.Changed
	cp.Pages++
	_, e = r.q.Exec(ctx, `UPDATE screening.dataset_maintenance_progress SET delta_cursor=$4,delta_done=$5,needs_rescreen=$6,delta_pages=delta_pages+1,lease_until=$7::timestamptz+interval '5 minutes' WHERE tenant_id=$1 AND job_id=$2 AND lease_token=$3`, tenant, job, token, cp.Cursor, cp.DeltaDone, cp.NeedsRescreen, now)
	if e != nil {
		return cp, e
	}
	_, e = r.q.Exec(ctx, `UPDATE screening.dataset_update_jobs SET cursor=$3,updated_at=$4 WHERE tenant_id=$1 AND id=$2`, tenant, job, cp.Cursor, now)
	return cp, e
}
func (r DatasetUpdateJobRepository) RequeueMaintenancePage(ctx context.Context, tenant, job, token string, now time.Time) ([]string, bool, error) {
	cp, e := r.maintenanceCheckpoint(ctx, tenant, job, token, now)
	if e != nil {
		return nil, false, e
	}
	if !cp.DeltaDone {
		return nil, false, errMaintenanceFence
	}
	if cp.ObjectsDone {
		return nil, true, nil
	}
	rows, e := r.q.Query(ctx, `SELECT m.id::text,m.config_id::text FROM screening.dataset_update_jobs j JOIN screening.monitored_objects m ON m.tenant_id=j.tenant_id JOIN screening.continuous_screening_configs c ON(c.tenant_id,c.id)=(m.tenant_id,m.config_id) JOIN screening.dataset_maintenance_progress p ON(p.tenant_id,p.job_id)=(j.tenant_id,j.id) WHERE j.tenant_id=$1 AND j.id=$2 AND (j.job_type='rescreen_monitored_objects' OR(j.job_type='dataset_delta_rescreen' AND p.needs_rescreen)) AND m.id>p.after_object AND m.status IN ('active','pending') AND c.enabled AND c.provider=j.provider ORDER BY m.id LIMIT 200 FOR UPDATE OF m`, tenant, job)
	if e != nil {
		return nil, false, e
	}
	ids, configs := []string{}, []string{}
	for rows.Next() {
		var id, cfg string
		if e = rows.Scan(&id, &cfg); e != nil {
			rows.Close()
			return nil, false, e
		}
		ids = append(ids, id)
		configs = append(configs, cfg)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, false, e
	}
	if len(ids) > 0 {
		if _, e = r.q.Exec(ctx, `UPDATE screening.monitored_objects SET status='pending',updated_at=$3 WHERE tenant_id=$1 AND id=ANY($2::uuid[])`, tenant, ids, now); e != nil {
			return nil, false, e
		}
		if _, e = r.q.Exec(ctx, `INSERT INTO screening.dataset_maintenance_receipts(tenant_id,job_id,config_id,object_id) SELECT $1,$2,x.config::uuid,x.id::uuid FROM unnest($3::text[],$4::text[]) x(id,config) ON CONFLICT DO NOTHING`, tenant, job, ids, configs); e != nil {
			return nil, false, e
		}
	}
	after := uuid.Nil.String()
	if len(ids) > 0 {
		after = ids[len(ids)-1]
	}
	done := len(ids) < 200
	_, e = r.q.Exec(ctx, `UPDATE screening.dataset_maintenance_progress SET after_object=CASE WHEN $4::uuid='00000000-0000-0000-0000-000000000000' THEN after_object ELSE $4::uuid END,objects_done=$5,queued_objects=queued_objects+$6,lease_until=$7::timestamptz+interval '5 minutes' WHERE tenant_id=$1 AND job_id=$2 AND lease_token=$3`, tenant, job, token, after, done, len(ids), now)
	return ids, done, e
}
func (r DatasetUpdateJobRepository) FinishMaintenance(ctx context.Context, tenant, job, token, status, code string, now time.Time) error {
	cp, e := r.maintenanceCheckpoint(ctx, tenant, job, token, now)
	if e != nil {
		return e
	}
	if status == "completed" && (!cp.DeltaDone || !cp.ObjectsDone) {
		return errMaintenanceFence
	}
	if _, e = r.q.Exec(ctx, `UPDATE screening.dataset_update_jobs SET status=$4,last_error=$5,updated_at=$6,completed_at=CASE WHEN $4='completed' THEN $6::timestamptz ELSE NULL END,result_json=jsonb_build_object('delta_pages',$7::int,'requeued_objects',$8::int,'cursor',$9::text,'delta_done',$10::boolean,'objects_done',$11::boolean) WHERE tenant_id=$1 AND id=$2 AND EXISTS(SELECT 1 FROM screening.dataset_maintenance_progress p WHERE p.tenant_id=$1 AND p.job_id=$2 AND p.lease_token=$3::uuid)`, tenant, job, token, status, code, now, cp.Pages, cp.Queued, cp.Cursor, cp.DeltaDone, cp.ObjectsDone); e != nil {
		return e
	}
	_, e = r.q.Exec(ctx, `UPDATE screening.dataset_maintenance_progress SET lease_until=$4 WHERE tenant_id=$1 AND job_id=$2 AND lease_token=$3`, tenant, job, token, now)
	return e
}
func (r DatasetUpdateJobRepository) RecoverableMaintenance(ctx context.Context, limit int) ([]screening.DatasetUpdateJob, error) {
	if limit < 1 || limit > 200 {
		limit = 50
	}
	rows, e := r.q.Query(ctx, `SELECT j.id::text,j.tenant_id::text FROM screening.dataset_update_jobs j LEFT JOIN screening.dataset_maintenance_progress p ON(p.tenant_id,p.job_id)=(j.tenant_id,j.id) WHERE j.status='pending' OR(j.status='processing' AND coalesce(p.lease_until,j.started_at+interval '5 minutes')<=now()) ORDER BY j.created_at,j.id LIMIT $1`, limit)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []screening.DatasetUpdateJob{}
	for rows.Next() {
		var item screening.DatasetUpdateJob
		if e = rows.Scan(&item.ID, &item.TenantID); e != nil {
			return nil, e
		}
		out = append(out, item)
	}
	return out, rows.Err()
}
