package mysql

import (
	"context"
	"database/sql"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
)

// CountDistinctRecords counts non-null full scalar values for mapped numeric,
// Boolean, timestamp, binary text and complete IP tuples. Caller owns authorization,
// originating READ COMMITTED transaction/publication barrier and deadline.
// Constant output size does not bound server sort/temp-table/scan costs.
func (p *RecordProjection) CountDistinctRecords(ctx context.Context, tx *sql.Tx, name string, filter *ingestion.AggregateFilter, limits RecordQueryLimits) (int64, error) {
	if tx == nil {
		return 0, ErrRecordAggregate
	}
	query, args, _, err := p.compileAggregate(name, "COUNT DISTINCT", filter, limits)
	if err != nil {
		return 0, err
	}
	var raw any
	if err := tx.QueryRowContext(ctx, query, args...).Scan(&raw); err != nil {
		return 0, err
	}
	return decodeRecordCount(raw)
}
