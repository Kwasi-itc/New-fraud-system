package postgres

import (
	"context"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
	"time"
)

// Recovery projects identities only; RunScreening reads the current row before
// claiming. Live claims cannot hide expired work behind the batch limit.
func (r ScreeningRepository) ListRecoverable(ctx context.Context, now time.Time, limit int) ([]screening.Screening, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.q.Query(ctx, `SELECT tenant_id::text,id::text FROM screening.screenings WHERE status='processing' AND (processing_until IS NULL OR processing_until<=$1) ORDER BY processing_until NULLS FIRST,id LIMIT $2`, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []screening.Screening
	for rows.Next() {
		var item screening.Screening
		if err := rows.Scan(&item.TenantID, &item.ID); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r ScreeningRepository) Claim(ctx context.Context, tenant, id, token string, now time.Time) (bool, error) {
	tag, err := r.q.Exec(ctx, `UPDATE screening.screenings SET status='processing',processing_token=$3,processing_until=$4::timestamptz+interval '6 minutes',sent_at=$4,updated_at=$4
 WHERE tenant_id=$1 AND id=$2 AND (status='pending' OR (status='processing' AND (processing_until IS NULL OR processing_until<=$4)))`, tenant, id, token, now)
	return tag.RowsAffected() == 1, err
}

func (r ScreeningRepository) Lock(ctx context.Context, tenant, id string) (screening.Screening, error) {
	var found string
	if err := r.q.QueryRow(ctx, `SELECT id::text FROM screening.screenings WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenant, id).Scan(&found); err != nil {
		return screening.Screening{}, err
	}
	return r.GetByID(ctx, tenant, id)
}

// Hold this row lock until result/match/event writes commit. No provider IO is
// performed under the lock. An expired attempt cannot publish after takeover.
func (r ScreeningRepository) LockClaim(ctx context.Context, tenant, id, token string) error {
	var found string
	return r.q.QueryRow(ctx, `SELECT id::text FROM screening.screenings WHERE tenant_id=$1 AND id=$2 AND status='processing' AND processing_token=$3 FOR UPDATE`, tenant, id, token).Scan(&found)
}
func (r ScreeningRepository) ReleaseClaim(ctx context.Context, tenant, id, token string) error {
	_, err := r.q.Exec(ctx, `UPDATE screening.screenings SET status='pending',processing_until=NULL,processing_token=NULL WHERE tenant_id=$1 AND id=$2 AND status='processing' AND processing_token=$3`, tenant, id, token)
	return err
}
func (r ScreeningRepository) CreateContinuousRun(ctx context.Context, tenant, id, config, monitored string, now time.Time) error {
	_, err := r.q.Exec(ctx, `INSERT INTO screening.continuous_runs(tenant_id,screening_id,config_id,monitored_object_id,created_at) VALUES($1,$2,$3,$4,$5)`, tenant, id, config, monitored, now)
	return err
}
func (r ScreeningRepository) IsContinuousRun(ctx context.Context, tenant, id string) (bool, error) {
	var found bool
	err := r.q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM screening.continuous_runs WHERE tenant_id=$1 AND screening_id=$2)`, tenant, id).Scan(&found)
	return found, err
}
func (r MonitoredObjectRepository) Lock(ctx context.Context, tenant, id string) (screening.MonitoredObject, error) {
	var found string
	if err := r.q.QueryRow(ctx, `SELECT id::text FROM screening.monitored_objects WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenant, id).Scan(&found); err != nil {
		return screening.MonitoredObject{}, err
	}
	return r.GetByID(ctx, tenant, id)
}
