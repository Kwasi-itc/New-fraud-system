package mysql

import (
	"errors"
	"strings"
	"unicode/utf8"
)

var ErrRecordPredicate = errors.New("supported mapped scalar predicate with normalized value required")

// ScalarPredicate is a transaction-local SQL fragment, bound to the projection
// that compiled it. It is not an authorization or reusable publication token.
// Use only with that projection's table in the same authorized transaction.
// SQL returns fresh arguments; no caller-owned mutable value is retained.
type ScalarPredicate struct {
	clause string
	args   []any
}

func (p ScalarPredicate) SQL() (string, []any) {
	return p.clause, cloneRecordArguments(p.args)
}

// ScalarPredicate compiles supported source operators using mapped columns.
// Inputs must be normalized int64 (INT32 range), finite float64, bool or a valid
// microsecond time.Time (converted to UTC). HTTP/raw-value coercion is upstream.
// Comparisons with nil remain SQL UNKNOWN, even for nonnullable columns.
// Single-column null checks ignore Value as the PostgreSQL source does.
// String comparisons are byte-exact; prefix values are escaped literals.
// IP comparisons retain address family, network bits, mask length and host bits.
func (p *RecordProjection) ScalarPredicate(name, operator string, value any) (ScalarPredicate, error) {
	bad := func() (ScalarPredicate, error) { return ScalarPredicate{}, ErrRecordPredicate }
	if p == nil || p.table == "" || len(p.fields) == 0 {
		return bad()
	}
	name = strings.TrimSpace(name)
	operator = strings.ToLower(strings.TrimSpace(operator))
	for _, field := range p.fields {
		if field.definition.Name != name {
			continue
		}
		if field.definition.Archived || field.column == "" {
			return bad()
		}
		if field.definition.DataType == "ip_address" {
			return ipScalarPredicate(field, operator, value)
		}
		if field.width != 1 {
			return bad()
		}
		switch field.definition.DataType {
		case "int", "float", "bool", "timestamp", "string":
		default:
			return bad()
		}
		switch operator {
		case "is_null":
			return ScalarPredicate{clause: field.column + " IS NULL"}, nil
		case "is_not_null":
			return ScalarPredicate{clause: field.column + " IS NOT NULL"}, nil
		}
		if field.definition.DataType == "string" {
			if value != nil {
				v, ok := value.(string)
				if !ok || !utf8.ValidString(v) || len(v) > 1<<20 {
					return bad()
				}
			}
			if operator == "starts_with" {
				if value == nil {
					return ScalarPredicate{clause: field.column + " LIKE ?", args: []any{nil}}, nil
				}
				escaped := strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(value.(string)) + "%"
				return ScalarPredicate{clause: "CONVERT(" + field.column + " USING utf8mb4) COLLATE utf8mb4_0900_bin LIKE CONVERT(? USING utf8mb4) COLLATE utf8mb4_0900_bin ESCAPE '!'", args: []any{escaped}}, nil
			}
			var symbol string
			switch operator {
			case "eq":
				symbol = "="
			case "neq":
				symbol = "<>"
			case "gt":
				symbol = ">"
			case "gte":
				symbol = ">="
			case "lt":
				symbol = "<"
			case "lte":
				symbol = "<="
			default:
				return bad()
			}
			var argument any
			if value != nil {
				argument = value.(string)
			}
			return ScalarPredicate{clause: "CAST(" + field.column + " AS BINARY) " + symbol + " ?", args: []any{argument}}, nil
		}
		var symbol string
		switch operator {
		case "eq":
			symbol = "="
		case "neq":
			symbol = "<>"
		case "gt":
			symbol = ">"
		case "gte":
			symbol = ">="
		case "lt":
			symbol = "<"
		case "lte":
			symbol = "<="
		default:
			return bad()
		}
		var argument any
		if value != nil {
			_, values, err := encodeWriteValue(field.definition, value)
			if err != nil || len(values) != 1 {
				return bad()
			}
			argument = values[0]
		}
		return ScalarPredicate{clause: field.column + " " + symbol + " ?", args: []any{argument}}, nil
	}
	return bad()
}
