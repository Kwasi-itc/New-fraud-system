package mysql

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
)

// Capability checks happen before SQL execution and bound recursive work. SQL
// comparisons retain SQL three-valued null semantics: neq excludes missing
// ownership. Explicit is_null/is_not_null express null tests. Invalid dynamic
// types return errors; they are never coerced to a successful zero count.
func (t registryTable) filter(f ports.AggregateFilter, depth int, nodes *int) (string, []any, error) {
	*nodes++
	if depth > 5 || *nodes > 50 {
		return "", nil, ErrAggregateCapability
	}
	kind := strings.ToLower(strings.TrimSpace(f.Kind))
	switch kind {
	case "", "group":
		if len(f.Children) > 20 {
			return "", nil, ErrAggregateCapability
		}
		op := strings.ToLower(strings.TrimSpace(f.Operator))
		if op == "" {
			op = "and"
		}
		if op != "and" && op != "or" && op != "not" {
			return "", nil, ErrAggregateCapability
		}
		if op == "not" && len(f.Children) != 1 {
			return "", nil, ErrAggregateCapability
		}
		if len(f.Children) == 0 {
			if op == "or" {
				return "FALSE", nil, nil
			}
			return "TRUE", nil, nil
		}
		parts := []string{}
		args := []any{}
		for _, child := range f.Children {
			expr, values, err := t.filter(child, depth+1, nodes)
			if err != nil {
				return "", nil, err
			}
			parts = append(parts, "("+expr+")")
			args = append(args, values...)
		}
		if op == "not" {
			return "NOT " + parts[0], args, nil
		}
		return strings.Join(parts, " "+strings.ToUpper(op)+" "), args, nil
	case "predicate":
		if len(f.Children) != 0 {
			return "", nil, ErrAggregateCapability
		}
		field, err := t.field(f.Field)
		if err != nil {
			return "", nil, err
		}
		return field.predicate(strings.ToLower(strings.TrimSpace(f.Op)), f.Value)
	default:
		return "", nil, ErrAggregateCapability
	}
}

// compileAggregate is shared by publication preflight and actual execution.
// It emits SQL only; neither path maintains a separate operator/type allowlist.
func (t registryTable) compileAggregate(q ports.AggregateQuery, maxBytes int) (string, []any, error) {
	field, err := t.field(q.Field)
	if err != nil {
		return "", nil, err
	}
	aggregate := strings.ToLower(strings.TrimSpace(q.Aggregate))
	column := field.columns["value"]
	// Multi-component inet MIN/MAX and ordering need a separate reviewed ordering
	// contract; never issue a query that compares only one component.
	if field.Type == "ip_address" {
		if aggregate != "count" {
			return "", nil, ErrAggregateCapability
		}
		column = field.columns["family"]
	}
	expression := ""
	switch aggregate {
	case "count":
		expression = "COUNT(" + column + ")"
	case "count_distinct":
		if field.Type == "string" {
			column = "CAST(" + column + " AS BINARY)"
		}
		expression = "COUNT(DISTINCT " + column + ")"
	case "sum", "avg":
		if field.Type != "int" && field.Type != "float" {
			return "", nil, ErrAggregateCapability
		}
		expression = strings.ToUpper(aggregate) + "(" + column + ")"
		if aggregate == "sum" {
			expression = "COALESCE(" + expression + ",0)"
		}
	case "min", "max":
		if field.Type == "string" {
			column = "CAST(" + column + " AS BINARY)"
		}
		expression = strings.ToUpper(aggregate) + "(" + column + ")"
	default:
		return "", nil, ErrAggregateCapability
	}
	where := ""
	args := []any{}
	if q.Filter != nil {
		nodes := 0
		expr, bound, err := t.filter(*q.Filter, 1, &nodes)
		if err != nil {
			return "", nil, err
		}
		where = " WHERE " + expr
		args = bound
	}
	// Bound the entire compiled predicate, including escaped LIKE values and
	// repeated digest/residual parameters, before sending the data query.
	used := 0
	for _, value := range args {
		if text, ok := value.(mysqlstore.Text); ok {
			if len(text) > maxBytes-used {
				return "", nil, ErrProjectionLimit
			}
			used += len(text)
		}
	}
	return "SELECT " + expression + " FROM " + t.qualified + where, args, nil
}

func (r TenantDataReader) AggregateRecords(ctx context.Context, tenant string, q ports.AggregateQuery) (any, error) {
	var result any
	err := r.read(ctx, tenant, q.ObjectType, func(repo authoringRepository, t registryTable) error {
		query, args, err := t.compileAggregate(q, repo.maxBytes)
		if err != nil {
			return err
		}
		field, err := t.field(q.Field)
		if err != nil {
			return err
		}
		aggregate := strings.ToLower(strings.TrimSpace(q.Aggregate))
		var raw any
		if err := repo.tx.QueryRowContext(ctx, query, args...).Scan(&raw); err != nil {
			return err
		}
		if raw == nil {
			result = nil
			return nil
		}
		if aggregate == "count" || aggregate == "count_distinct" {
			switch v := raw.(type) {
			case int64:
				result = json.Number(strconv.FormatInt(v, 10))
			case []byte:
				result = json.Number(string(v))
			default:
				return ErrAggregateCapability
			}
			return nil
		}
		if aggregate == "min" || aggregate == "max" {
			result, err = field.decode([]any{raw})
			return err
		}
		switch v := raw.(type) {
		case []byte:
			result = json.Number(string(v))
		case float64:
			var f mysqlstore.Float64
			if err := f.Scan(v); err != nil {
				return err
			}
			result = json.Number(strconv.FormatFloat(float64(f), 'g', -1, 64))
		case int64:
			result = json.Number(strconv.FormatInt(v, 10))
		default:
			return ErrAggregateCapability
		}
		return nil
	})
	return result, err
}
