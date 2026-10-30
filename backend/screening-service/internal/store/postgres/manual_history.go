package postgres

import (
	"context"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
)

func (r ScreeningRepository) ListManualHistory(ctx context.Context, tenant, actor string, f screening.ManualHistoryFilter) ([]screening.ManualHistoryItem, error) {
	rows, err := r.q.Query(ctx, `SELECT id::text,coalesce(request_json->'queries'->0->>'name',''),object_type,status,partial,created_at
 FROM screening.screenings
 WHERE tenant_id=$1 AND split_part(idempotency_key,':',2)=$2
 AND is_manual AND decision_id IS NULL AND scenario_id IS NULL AND idempotency_key LIKE 'manual:%'
 AND ($3='' OR status=$3) AND ($4='' OR object_type=$4)
 AND ($5::timestamptz IS NULL OR created_at >= $5) AND ($6::timestamptz IS NULL OR created_at < $6)
 AND ($7::timestamptz IS NULL OR (created_at,id)<($7,$8::uuid))
 ORDER BY created_at DESC,id DESC LIMIT $9`, tenant, actor, f.Status, f.EntityType, f.From, f.Until, f.Before, nullIfEmpty(f.BeforeID), f.Limit+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]screening.ManualHistoryItem, 0)
	for rows.Next() {
		var item screening.ManualHistoryItem
		if err := rows.Scan(&item.ID, &item.Name, &item.EntityType, &item.Status, &item.Partial, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
