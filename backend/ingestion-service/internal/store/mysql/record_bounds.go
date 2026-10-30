package mysql

import (
	"database/sql"
	"errors"
	"strings"
)

var ErrRecordResultLimit = errors.New("record result exceeds configured bounds")

type RecordResultLimits struct{ FieldBytes, TransferBytes, RowBytes, PageBytes, Rows int }
type BoundedRecordProjection struct {
	source  *RecordProjection
	limits  RecordResultLimits
	list    string
	args    []any
	columns int
}

// Bounded compiles server-side string guards. TransferBytes bounds the worst-case
// selected value bytes per row (not wire framing or driver/Go overhead). RowBytes
// and PageBytes bound conservative JSON-size accounting for decoded records.
func (p *RecordProjection) Bounded(limits RecordResultLimits) (*BoundedRecordProjection, error) {
	if p == nil || p.columns < 1 || len(p.fields) < 1 {
		return nil, ErrRecordProjection
	}
	if limits.FieldBytes < 1 || limits.FieldBytes > 1<<20 || limits.TransferBytes < 1 || limits.TransferBytes > 16<<20 || limits.RowBytes < 1 || limits.RowBytes > 16<<20 || limits.PageBytes < limits.RowBytes || limits.PageBytes > 64<<20 || limits.Rows < 1 || limits.Rows > 1000 {
		return nil, ErrRecordResultLimit
	}
	columns := strings.Split(p.list, ",")
	if len(columns) != p.columns {
		return nil, ErrRecordProjection
	}
	b := &BoundedRecordProjection{source: p, limits: limits}
	var expressions []string
	offset := 0
	transfer := 0
	minimum := 2
	for _, field := range p.fields {
		// Reject pathological logical names before multiplying their byte length.
		if len(field.definition.Name) > limits.RowBytes/6 {
			return nil, ErrRecordResultLimit
		}
		minimum += 6*len(field.definition.Name) + 4
		if field.definition.DataType == "string" {
			column := columns[offset]
			expressions = append(expressions, "OCTET_LENGTH("+column+")", "CASE WHEN OCTET_LENGTH("+column+")<=? THEN "+column+" ELSE NULL END")
			b.args = append(b.args, limits.FieldBytes)
			transfer += limits.FieldBytes + 64
			b.columns += 2
		} else {
			expressions = append(expressions, columns[offset:offset+field.width]...)
			transfer += 64 * field.width
			b.columns += field.width
		}
		offset += field.width
	}
	if transfer > limits.TransferBytes || minimum > limits.RowBytes {
		return nil, ErrRecordResultLimit
	}
	b.list = strings.Join(expressions, ",")
	return b, nil
}

// SQL returns explicit guarded columns, table and fresh parameter storage. Append
// only trusted parameterized predicates and a bounded LIMIT before execution.
func (b *BoundedRecordProjection) SQL() (columns, table string, args []any) {
	return b.list, b.source.table, append([]any(nil), b.args...)
}
func (b *BoundedRecordProjection) ColumnCount() int { return b.columns }

// decode checks the entire row budget before copying strings into typed results.
// String lengths are independently checked against the server's length markers.
func (b *BoundedRecordProjection) decode(values []any) (map[string]any, int, error) {
	if b == nil || b.source == nil || len(values) != b.columns {
		return nil, 0, ErrRecordProjection
	}
	raw := make([]any, 0, b.source.columns)
	offset := 0
	cost := 2
	for _, field := range b.source.fields {
		cost += 6*len(field.definition.Name) + 4
		if field.definition.DataType == "string" {
			marker, value := values[offset], values[offset+1]
			offset += 2
			if marker == nil {
				if value != nil {
					return nil, 0, ErrRecordDecode
				}
				cost += 4
			} else {
				n, err := recordInteger(marker)
				if err != nil || n < 0 {
					return nil, 0, ErrRecordDecode
				}
				if n > int64(b.limits.FieldBytes) {
					return nil, 0, ErrRecordResultLimit
				}
				var actual int
				switch v := value.(type) {
				case []byte:
					actual = len(v)
				case string:
					actual = len(v)
				default:
					return nil, 0, ErrRecordDecode
				}
				if int64(actual) != n {
					return nil, 0, ErrRecordDecode
				}
				cost += 6*actual + 2
			}
			raw = append(raw, value)
		} else {
			raw = append(raw, values[offset:offset+field.width]...)
			offset += field.width
			cost += 64
		}
		if cost > b.limits.RowBytes {
			return nil, 0, ErrRecordResultLimit
		}
	}
	result, err := b.source.Decode(raw, b.limits.FieldBytes)
	if err != nil {
		return nil, 0, err
	}
	return result, cost, nil
}

// Collect owns and closes rows. The caller retains authorization and publication
// locks through execution/collection. No partial page escapes any failure. Each
// result page is a JSON array budget (not an HTTP envelope or process RSS limit).
func (b *BoundedRecordProjection) Collect(rows *sql.Rows) (items []map[string]any, err error) {
	if rows == nil {
		return nil, ErrRecordProjection
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			items = nil
			err = errors.Join(err, closeErr)
		}
	}()
	if b == nil || b.source == nil {
		return nil, ErrRecordProjection
	}
	page := make([]map[string]any, 0, min(b.limits.Rows, 16))
	used := 2
	for rows.Next() {
		if len(page) == b.limits.Rows {
			return nil, ErrRecordResultLimit
		}
		values := make([]any, b.columns)
		dest := make([]any, b.columns)
		for i := range values {
			dest[i] = &values[i]
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}
		row, cost, err := b.decode(values)
		if err != nil {
			return nil, err
		}
		separator := 0
		if len(page) > 0 {
			separator = 1
		}
		if cost+separator > b.limits.PageBytes-used {
			return nil, ErrRecordResultLimit
		}
		used += cost + separator
		page = append(page, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return page, nil
}
