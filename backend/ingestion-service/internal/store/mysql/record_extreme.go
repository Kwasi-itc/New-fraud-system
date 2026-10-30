package mysql

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
)

var ErrRecordExtremeResult = errors.New("MIN/MAX result violates mapped field or null contract")

// MinRecords and MaxRecords accept numeric/timestamp, bounded binary strings and IPs. Each requires
// the originating projection's authorized READ COMMITTED transaction, retained
// publication barrier and suitable deadline. Empty/all-null input returns nil.
func (p *RecordProjection) MinRecords(ctx context.Context, tx *sql.Tx, name string, filter *ingestion.AggregateFilter, limits RecordQueryLimits) (any, error) {
	return p.extremeRecords(ctx, tx, name, "MIN", filter, limits)
}
func (p *RecordProjection) MaxRecords(ctx context.Context, tx *sql.Tx, name string, filter *ingestion.AggregateFilter, limits RecordQueryLimits) (any, error) {
	return p.extremeRecords(ctx, tx, name, "MAX", filter, limits)
}
func (p *RecordProjection) extremeRecords(ctx context.Context, tx *sql.Tx, name, operation string, filter *ingestion.AggregateFilter, limits RecordQueryLimits) (any, error) {
	if tx == nil {
		return nil, ErrRecordAggregate
	}
	if p != nil {
		for _, field := range p.fields {
			if field.definition.Name == strings.TrimSpace(name) && field.definition.DataType == "ip_address" {
				return p.ipExtreme(ctx, tx, field, operation, filter, limits)
			}
		}
	}
	query, args, field, err := p.compileAggregate(name, operation, filter, limits)
	if err != nil {
		return nil, err
	}
	var raw, rawCount, rawLength any
	dest := []any{&raw, &rawCount}
	if field.DataType == "string" {
		dest = append(dest, &rawLength)
	}
	if err := tx.QueryRowContext(ctx, query, args...).Scan(dest...); err != nil {
		return nil, err
	}
	if field.DataType == "string" && rawLength != nil {
		n, err := recordInteger(rawLength)
		if err != nil || n < 0 {
			return nil, ErrRecordExtremeResult
		}
		if n > 16384 {
			return nil, ErrRecordResultLimit
		}
	}
	return decodeRecordExtreme(field, raw, rawCount)
}
func decodeRecordExtreme(field ingestion.FieldSchema, raw, rawCount any) (any, error) {
	count, err := decodeRecordCount(rawCount)
	if err != nil {
		return nil, ErrRecordExtremeResult
	}
	switch field.DataType {
	case "int", "float", "timestamp", "string":
	default:
		return nil, ErrRecordExtremeResult
	}
	if field.Archived {
		return nil, ErrRecordExtremeResult
	}
	if count == 0 {
		if raw != nil {
			return nil, ErrRecordExtremeResult
		}
		return nil, nil
	}
	// A positive non-null count cannot yield a nullable result.
	field.Nullable = false
	value, err := DecodeRecordValue(field, []any{raw}, 16384)
	if err != nil {
		return nil, ErrRecordExtremeResult
	}
	return value, nil
}
