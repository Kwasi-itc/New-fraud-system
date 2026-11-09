package mysql

import (
	"context"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
	"strings"
)

func (r IndexJobRepository) ListByTables(ctx context.Context, tenant uuid.UUID, tables []uuid.UUID, limit int) ([]datamodel.IndexJob, error) {
	if tenant != r.tenant {
		return nil, datamodel.ErrIndexJobScopeLimit
	}
	if err := datamodel.ValidateIndexJobScope(tenant, tables, limit); err != nil {
		return nil, err
	}
	args := []any{mysqlstore.UUID(tenant)}
	for _, id := range tables {
		args = append(args, mysqlstore.UUID(id))
	}
	args = append(args, limit+1)
	rows, err := r.tx.QueryContext(ctx, r.selectSQL()+" AND table_id IN ("+strings.TrimSuffix(strings.Repeat("?,", len(tables)), ",")+") ORDER BY requested_at DESC,id DESC LIMIT ?", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	jobs := []datamodel.IndexJob{}
	var bytes int64
	for rows.Next() {
		v, size, err := r.scan(rows)
		if err != nil {
			return nil, err
		}
		bytes += size
		if bytes > int64(r.maxBytes) || len(jobs) >= limit || len(jobs) >= r.maxRows {
			return nil, datamodel.ErrIndexJobScopeLimit
		}
		jobs = append(jobs, v)
	}
	return jobs, rows.Err()
}
