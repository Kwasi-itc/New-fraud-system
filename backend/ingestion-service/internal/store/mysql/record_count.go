package mysql

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
)

var ErrRecordCount = errors.New("mapped single-column COUNT field required")
var ErrRecordCountResult = errors.New("COUNT result must be a nonnegative signed 64-bit integer")

// CountRecords preserves COUNT(field), not COUNT(*): NULL inputs are excluded.
// Caller must retain the originating projection's authorized READ COMMITTED
// transaction/publication barrier and provide a suitable context deadline.
// A scalar result bounds output, not database scan work. No row LIMIT is added
// because limiting the counted input would change aggregate semantics.
func (p *RecordProjection) CountRecords(ctx context.Context, tx *sql.Tx, field string, filter *ingestion.AggregateFilter, limits RecordQueryLimits) (int64, error) {
	if tx == nil {
		return 0, ErrRecordCount
	}
	query, args, err := p.compileCount(field, filter, limits)
	if err != nil {
		return 0, err
	}
	var raw any
	if err := tx.QueryRowContext(ctx, query, args...).Scan(&raw); err != nil {
		return 0, err
	}
	return decodeRecordCount(raw)
}
func (p *RecordProjection) compileCount(name string, filter *ingestion.AggregateFilter, limits RecordQueryLimits) (string, []any, error) {
	if p == nil || p.table == "" {
		return "", nil, ErrRecordCount
	}
	name = strings.TrimSpace(name)
	column := ""
	for _, field := range p.fields {
		if field.definition.Name != name {
			continue
		}
		if field.definition.Archived || field.column == "" || (field.width != 1 && !(field.definition.DataType == "ip_address" && field.width == 3 && len(field.components) == 3)) {
			return "", nil, ErrRecordCount
		}
		switch field.definition.DataType {
		case "int", "float", "bool", "timestamp", "string", "ip_address":
			column = field.column
		default:
			return "", nil, ErrRecordCount
		}
		break
	}
	if column == "" {
		return "", nil, ErrRecordCount
	}
	compiled, err := p.CompileFilter(filter)
	if err != nil {
		return "", nil, err
	}
	where, args := compiled.SQL()
	query := "SELECT COUNT(" + column + ") FROM " + p.table
	if where != "" {
		query += " WHERE " + where
	}
	if err := ValidateRecordQuery(query, args, limits); err != nil {
		return "", nil, err
	}
	return query, args, nil
}
func decodeRecordCount(raw any) (int64, error) {
	value, err := recordInteger(raw)
	if err != nil || value < 0 {
		return 0, ErrRecordCountResult
	}
	return value, nil
}
