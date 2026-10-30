package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"net/netip"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
)

// NormalizeReadFilter validates before traversal and copies caller-owned inputs.
// Only explicit type conversions are accepted; IP values retain host and mask.
func (p *RecordProjection) NormalizeReadFilter(f *ingestion.AggregateFilter) (*ingestion.AggregateFilter, error) {
	if f == nil {
		return nil, nil
	}
	weighted := 0
	if e := validateRecordFilter(f, 1, &weighted); e != nil {
		return nil, e
	}
	var copyNode func(*ingestion.AggregateFilter) (*ingestion.AggregateFilter, error)
	copyNode = func(n *ingestion.AggregateFilter) (*ingestion.AggregateFilter, error) {
		out := *n
		out.Op = strings.ToLower(strings.TrimSpace(n.Op))
		out.Children = nil
		if strings.TrimSpace(strings.ToLower(n.Kind)) == ingestion.AggregateFilterKindPredicate {
			if out.Op != "is_null" && out.Op != "is_not_null" {
				if out.Op == "in" {
					values, ok := n.Value.([]any)
					if !ok || len(values) == 0 || len(values) > 100 {
						return nil, ErrRecordFilter
					}
					normalized := make([]any, len(values))
					for i, v := range values {
						var e error
						normalized[i], e = p.normalizeReadValue(n.Field, v)
						if e != nil {
							return nil, e
						}
					}
					out.Value = normalized
				} else {
					v, e := p.normalizeReadValue(n.Field, n.Value)
					if e != nil {
						return nil, e
					}
					out.Value = v
				}
			}
		}
		for i := range n.Children {
			child, e := copyNode(&n.Children[i])
			if e != nil {
				return nil, e
			}
			out.Children = append(out.Children, *child)
		}
		return &out, nil
	}
	return copyNode(f)
}
func (p *RecordProjection) normalizeReadValue(name string, v any) (any, error) {
	for _, f := range p.fields {
		if f.definition.Name != strings.TrimSpace(name) {
			continue
		}
		if v == nil {
			return nil, nil
		}
		raw := func() string {
			switch n := v.(type) {
			case string:
				return n
			case json.Number:
				return n.String()
			case int64:
				return strconv.FormatInt(n, 10)
			case float64:
				return strconv.FormatFloat(n, 'g', -1, 64)
			default:
				return ""
			}
		}()
		switch f.definition.DataType {
		case "ip_address":
			if n, ok := v.(netip.Prefix); ok && n.IsValid() {
				return n, nil
			}
			if n, ok := v.(string); ok && len(n) <= 64 {
				if prefix, e := netip.ParsePrefix(n); e == nil {
					return prefix, nil
				}
				if addr, e := netip.ParseAddr(n); e == nil && addr.Zone() == "" {
					return netip.PrefixFrom(addr, addr.BitLen()), nil
				}
			}
		case "string":
			n, ok := v.(string)
			if ok && utf8.ValidString(n) && len(n) <= 1<<20 {
				return n, nil
			}
		case "int":
			n, e := strconv.ParseInt(raw, 10, 32)
			if e == nil {
				return n, nil
			}
		case "float":
			n, e := strconv.ParseFloat(raw, 64)
			if e == nil && !math.IsNaN(n) && !math.IsInf(n, 0) {
				return n, nil
			}
		case "bool":
			if n, ok := v.(bool); ok {
				return n, nil
			}
			n, e := strconv.ParseBool(raw)
			if e == nil {
				return n, nil
			}
		case "timestamp":
			if n, ok := v.(time.Time); ok {
				return n, nil
			}
			n, e := time.Parse(time.RFC3339Nano, raw)
			if e == nil {
				return n.UTC(), nil
			}
		}
		return nil, ErrRecordPredicate
	}
	return nil, ErrRecordPredicate
}

// ReadCurrentExact retains the authorized projection's transaction and barrier.
func (p *RecordProjection) ReadCurrentExact(ctx context.Context, tx *sql.Tx, objectID string, r RecordResultLimits, q RecordQueryLimits) (map[string]any, error) {
	if objectID == "" || len(objectID) > 1<<20 || !utf8.ValidString(objectID) {
		return nil, ErrRecordLookup
	}
	key, found, e := FindRecordKey(ctx, tx, p.database, p.tenant, p.tableID, objectID)
	if e != nil {
		return nil, e
	}
	if !found {
		return nil, sql.ErrNoRows
	}
	r.Rows = 1
	statement, args, b, e := p.compileExactRead(key, objectID, r)
	if e != nil {
		return nil, e
	}
	if e = ValidateRecordQuery(statement, args, q); e != nil {
		return nil, e
	}
	rows, e := tx.QueryContext(ctx, statement, args...)
	if e != nil {
		return nil, e
	}
	records, e := b.Collect(rows)
	if e != nil {
		return nil, e
	}
	if len(records) == 0 {
		return nil, sql.ErrNoRows
	}
	return records[0], nil
}

// ReadCurrentPage follows the source updated_at DESC limit contract, adding a
// byte-stable object identity tie break. It introduces no offset/cursor contract.
func (p *RecordProjection) ReadCurrentPage(ctx context.Context, tx *sql.Tx, f *ingestion.AggregateFilter, r RecordResultLimits, q RecordQueryLimits) ([]map[string]any, error) {
	if p == nil || tx == nil {
		return nil, ErrRecordProjection
	}
	b, e := p.Bounded(r)
	if e != nil {
		return nil, e
	}
	filter, e := p.CompileFilter(f)
	if e != nil {
		return nil, e
	}
	columns, table, args := b.SQL()
	where, params := filter.SQL()
	statement := "SELECT " + columns + " FROM " + table
	if where != "" {
		statement += " WHERE " + where
	}
	args = append(args, params...)
	updated := ""
	for _, field := range p.fields {
		if field.definition.Name == "updated_at" {
			updated = field.column
		}
	}
	if updated == "" {
		return nil, errors.New("updated_at mapping required")
	}
	statement += " ORDER BY " + updated + " DESC, CAST(" + p.objectColumn + " AS BINARY) ASC LIMIT ?"
	args = append(args, int64(r.Rows))
	if e = ValidateRecordQuery(statement, args, q); e != nil {
		return nil, e
	}
	rows, e := tx.QueryContext(ctx, statement, args...)
	if e != nil {
		return nil, e
	}
	return b.Collect(rows)
}
