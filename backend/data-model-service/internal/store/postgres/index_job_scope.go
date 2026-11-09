package postgres

import (
	"context"
	"encoding/json"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/google/uuid"
)

func (r IndexJobRepository) ListByTables(ctx context.Context, tenant uuid.UUID, tables []uuid.UUID, limit int) ([]datamodel.IndexJob, error) {
	if err := datamodel.ValidateIndexJobScope(tenant, tables, limit); err != nil {
		return nil, err
	}
	// Bound variable projections before transferring them from PostgreSQL. An
	// oversized row produces a NULL required name and fails scanning; it is never
	// returned truncated. Total encoded results are bounded independently below.
	rows, err := r.db.Query(ctx, `SELECT id,tenant_id,table_id,
	CASE WHEN octet_length(table_name)+octet_length(requested_by_operation)+COALESCE(octet_length(error_message),0)+octet_length(dedupe_key)+octet_length(columns::text)<=65536 THEN table_name END,
	index_type,CASE WHEN octet_length(columns::text)<=65536 THEN columns END,status,
	LEFT(requested_by_operation,65536),LEFT(error_message,65536),attempt_count,requested_at,started_at,completed_at,scheduled_at,LEFT(dedupe_key,65536)
	FROM core.index_jobs WHERE tenant_id=$1 AND table_id=ANY($2::uuid[])
	ORDER BY requested_at DESC,id DESC LIMIT $3`, tenant, tables, limit+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	jobs := []datamodel.IndexJob{}
	bytes := 0
	for rows.Next() {
		v, err := scanIndexJob(rows)
		if err != nil {
			return nil, err
		}
		raw, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		bytes += len(raw)
		if v.Columns == nil || bytes > 8<<20 {
			return nil, datamodel.ErrIndexJobScopeLimit
		}
		jobs = append(jobs, v)
		if len(jobs) > limit {
			return nil, datamodel.ErrIndexJobScopeLimit
		}
	}
	return jobs, rows.Err()
}
