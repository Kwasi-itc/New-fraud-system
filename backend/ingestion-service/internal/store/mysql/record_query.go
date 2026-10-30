package mysql

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
)

var ErrRecordQueryLimit = errors.New("record statement exceeds parameter or SQL budget")

// RecordQueryLimits bounds one statement, not wire framing, RSS or server cost.
// ParameterBytes counts repeated occurrences, including repeated lookup keys.
type RecordQueryLimits struct{ Parameters, ParameterBytes, SQLBytes int }

// ValidateRecordQuery accepts only known driver value shapes, never invokes an
// arbitrary driver.Valuer, and performs no copying or SQL execution. The caller
// must retain ownership of args until execution finishes; concurrent mutation is
// not supported. Limits are explicit and constrained by staged safety ceilings.
func ValidateRecordQuery(query string, args []any, limits RecordQueryLimits) error {
	if limits.Parameters < 1 || limits.Parameters > 60000 || limits.ParameterBytes < 1 || limits.ParameterBytes > 16<<20 || limits.SQLBytes < 1 || limits.SQLBytes > 1<<20 {
		return ErrRecordQueryLimit
	}
	if query == "" || len(query) > limits.SQLBytes || len(args) > limits.Parameters {
		return ErrRecordQueryLimit
	}
	remaining := limits.ParameterBytes
	for _, arg := range args {
		var size int
		switch v := arg.(type) {
		case nil:
			size = 1
		case bool:
			size = 1
		case int, int64:
			size = 8
		case float64:
			if math.IsNaN(v) || math.IsInf(v, 0) {
				return ErrRecordQueryLimit
			}
			size = 8
		case string:
			size = len(v)
		case []byte:
			size = len(v)
		case mysqlstore.UUID:
			size = 16
		case time.Time:
			if _, err := (mysqlstore.DateTime{Time: v}).Value(); err != nil {
				return ErrRecordQueryLimit
			}
			size = 32
		default:
			return ErrRecordQueryLimit
		}
		if size > remaining {
			return ErrRecordQueryLimit
		}
		remaining -= size
	}
	return nil
}

// SelectRecords composes and executes one bounded, unordered selection. It is
// not stable pagination. Caller must use the projection's authorized READ
// COMMITTED transaction and retain its publication barrier. No raw caller SQL is
// accepted. Empty filters deliberately omit WHERE, preserving the source contract.
func (p *RecordProjection) SelectRecords(ctx context.Context, tx *sql.Tx, filter *ingestion.AggregateFilter, results RecordResultLimits, limits RecordQueryLimits) ([]map[string]any, error) {
	if tx == nil {
		return nil, ErrRecordProjection
	}
	query, args, b, err := p.compileSelection(filter, results, limits)
	if err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return b.Collect(rows)
}
func (p *RecordProjection) compileSelection(filter *ingestion.AggregateFilter, results RecordResultLimits, limits RecordQueryLimits) (string, []any, *BoundedRecordProjection, error) {
	b, err := p.Bounded(results)
	if err != nil {
		return "", nil, nil, err
	}
	f, err := p.CompileFilter(filter)
	if err != nil {
		return "", nil, nil, err
	}
	columns, table, args := b.SQL()
	where, params := f.SQL()
	query := "SELECT " + columns + " FROM " + table
	if where != "" {
		query += " WHERE " + where
	}
	query += " LIMIT ?"
	args = append(args, params...)
	args = append(args, int64(results.Rows))
	if err := ValidateRecordQuery(query, args, limits); err != nil {
		return "", nil, nil, err
	}
	return query, args, b, nil
}
