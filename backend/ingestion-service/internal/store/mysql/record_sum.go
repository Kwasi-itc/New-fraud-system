package mysql

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
)

var ErrRecordSum = errors.New("mapped numeric SUM field required")
var ErrRecordSumResult = errors.New("SUM result exceeds supported numeric representation")

// SumRecords preserves empty/all-null zero semantics. Integer fields return int64 and
// float fields return finite float64; errors return nil, never a zero success.
// Caller owns authorization, the projection's READ COMMITTED transaction and
// publication barrier, and an appropriate deadline. Output bounds do not bound
// scan work; floating-point accumulation is approximate and order-dependent.
// Extreme float magnitude/count combinations are conservatively rejected even
// when cancellation could make the mathematical total finite.
func (p *RecordProjection) SumRecords(ctx context.Context, tx *sql.Tx, field string, filter *ingestion.AggregateFilter, limits RecordQueryLimits) (any, error) {
	if tx == nil {
		return nil, ErrRecordSum
	}
	query, args, kind, err := p.compileSum(field, filter, limits)
	if err != nil {
		return nil, err
	}
	var raw, rawCount, rawMax any
	destinations := []any{&raw, &rawCount}
	if kind == "float" {
		destinations = append(destinations, &rawMax)
	}
	if err := tx.QueryRowContext(ctx, query, args...).Scan(destinations...); err != nil {
		return nil, err
	}
	count, err := decodeRecordCount(rawCount)
	if err != nil {
		return nil, ErrRecordSumResult
	}
	if count == 0 {
		if kind == "int" {
			return int64(0), nil
		}
		return float64(0), nil
	}
	if kind == "float" && !safeFloatAccumulation(count, rawMax) {
		return nil, ErrRecordSumResult
	}

	return decodeRecordSum(kind, raw)
}
func (p *RecordProjection) compileSum(name string, filter *ingestion.AggregateFilter, limits RecordQueryLimits) (string, []any, string, error) {
	if p == nil || p.table == "" {
		return "", nil, "", ErrRecordSum
	}
	name = strings.TrimSpace(name)
	column, kind := "", ""
	for _, field := range p.fields {
		if field.definition.Name != name {
			continue
		}
		if field.definition.Archived || field.width != 1 || field.column == "" || (field.definition.DataType != "int" && field.definition.DataType != "float") {
			return "", nil, "", ErrRecordSum
		}
		column, kind = field.column, field.definition.DataType
		break
	}
	if column == "" {
		return "", nil, "", ErrRecordSum
	}
	compiled, err := p.CompileFilter(filter)
	if err != nil {
		return "", nil, "", err
	}
	where, args := compiled.SQL()
	query := "SELECT SUM(" + column + "), COUNT(" + column + ")"
	if kind == "float" {
		query += ", MAX(ABS(" + column + "))"
	}
	query += " FROM " + p.table
	if where != "" {
		query += " WHERE " + where
	}
	if err := ValidateRecordQuery(query, args, limits); err != nil {
		return "", nil, "", err
	}
	return query, args, kind, nil
}
func decodeRecordSum(kind string, raw any) (any, error) {
	switch kind {
	case "int":
		// MySQL SUM(INT) returns an exact scale-zero decimal, commonly []byte.
		// Keep the source INT-to-int64 contract; never parse through float64.
		value, err := recordInteger(raw)
		if err == nil {
			return value, nil
		}
	case "float":
		value, err := DecodeRecordValue(ingestion.FieldSchema{DataType: "float"}, []any{raw}, 64)
		if err == nil {
			return value, nil
		}
	}
	return nil, ErrRecordSumResult
}
