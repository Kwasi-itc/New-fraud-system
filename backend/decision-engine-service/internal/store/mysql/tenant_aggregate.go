package mysql

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

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
func (t registryTable) aggregateExpression(q ports.AggregateQuery) (string, error) {
	field, err := t.field(q.Field)
	if err != nil {
		return "", err
	}
	aggregate := strings.ToLower(strings.TrimSpace(q.Aggregate))
	column := field.columns["value"]
	// Multi-component inet MIN/MAX and ordering need a separate reviewed ordering
	// contract; never issue a query that compares only one component.
	if field.Type == "ip_address" {
		if aggregate != "count" {
			return "", ErrAggregateCapability
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
			return "", ErrAggregateCapability
		}
		expression = strings.ToUpper(aggregate) + "(" + column + ")"
		if aggregate == "avg" {
			// MySQL AVG(integer) otherwise rounds to its default DECIMAL scale
			// (four fractional digits), changing portable rule outcomes. The
			// evaluator's numeric contract is binary64 on both engines.
			expression = "AVG(CAST(" + column + " AS DOUBLE))"
		}
		if aggregate == "sum" {
			expression = "COALESCE(" + expression + ",0)"
		}
	case "min", "max":
		if field.Type == "string" {
			column = "CAST(" + column + " AS BINARY)"
		}
		expression = strings.ToUpper(aggregate) + "(" + column + ")"
	default:
		return "", ErrAggregateCapability
	}
	return expression, nil
}

func (t registryTable) aggregatePredicate(q ports.AggregateQuery, maxBytes int) (string, []any, error) {
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
	return where, args, nil
}

func (t registryTable) compileAggregate(q ports.AggregateQuery, maxBytes int) (string, []any, error) {
	expression, err := t.aggregateExpression(q)
	if err != nil {
		return "", nil, err
	}
	where, args, err := t.aggregatePredicate(q, maxBytes)
	if err != nil {
		return "", nil, err
	}
	return "SELECT " + expression + " FROM " + t.qualified + where, args, nil
}

func decodeAggregate(field registryField, aggregate string, raw any) (result any, err error) {
	aggregate = strings.ToLower(strings.TrimSpace(aggregate))
	if raw == nil {
		return nil, nil
	}
	if aggregate == "count" || aggregate == "count_distinct" {
		switch v := raw.(type) {
		case int64:
			result = json.Number(strconv.FormatInt(v, 10))
		case []byte:
			result = json.Number(string(v))
		default:
			return nil, ErrAggregateCapability
		}
		return result, nil
	}
	if aggregate == "min" || aggregate == "max" {
		result, err = field.decode([]any{raw})
		return result, err
	}
	switch v := raw.(type) {
	case []byte:
		result = json.Number(string(v))
	case float64:
		var f mysqlstore.Float64
		if err := f.Scan(v); err != nil {
			return nil, err
		}
		// Preserve the binary64 value. Converting it to a decimal Number
		// changes exact numeric comparison against the same float constant.
		result = float64(f)
	case int64:
		result = json.Number(strconv.FormatInt(v, 10))
	default:
		return nil, ErrAggregateCapability
	}
	return result, nil
}

func (r TenantDataReader) AggregateRecords(ctx context.Context, tenant string, q ports.AggregateQuery) (any, error) {
	values, err := r.AggregateRecordsBatch(ctx, tenant, []ports.AggregateQuery{q})
	if err != nil {
		return nil, err
	}
	return values[0], nil
}

func (r TenantDataReader) AggregateRecordsBatch(ctx context.Context, tenant string, queries []ports.AggregateQuery) ([]any, error) {
	if len(queries) < 1 || len(queries) > 16 {
		return nil, ErrProjectionLimit
	}
	filter, err := json.Marshal(queries[0].Filter)
	if err != nil {
		return nil, ErrAggregateCapability
	}
	for _, query := range queries[1:] {
		candidate, err := json.Marshal(query.Filter)
		if err != nil || query.ObjectType != queries[0].ObjectType || string(candidate) != string(filter) {
			return nil, ErrAggregateCapability
		}
	}
	var result []any
	err = r.read(ctx, tenant, queries[0].ObjectType, func(repo authoringRepository, t registryTable) error {
		expressions, fields := make([]string, len(queries)), make([]registryField, len(queries))
		for i, q := range queries {
			var err error
			expressions[i], err = t.aggregateExpression(q)
			if err != nil {
				return err
			}
			fields[i], err = t.field(q.Field)
			if err != nil {
				return err
			}
		}
		where, args, err := t.aggregatePredicate(queries[0], repo.maxBytes)
		if err != nil {
			return err
		}
		raw, dest := make([]any, len(queries)), make([]any, len(queries))
		for i := range raw {
			dest[i] = &raw[i]
		}
		statement := "SELECT " + strings.Join(expressions, ",") + " FROM " + t.qualified + where
		stage := "aggregate_sql_raw"
		if summary, bound, ok, err := r.velocitySQL(ctx, repo, t, queries, expressions, where, args); err != nil {
			return err
		} else if ok {
			statement, args = summary, bound
			stage = "aggregate_sql_summary"
		}
		started := time.Now()
		if err := repo.tx.QueryRowContext(ctx, statement, args...).Scan(dest...); err != nil {
			return err
		}
		ports.RecordEvaluationDuration(ctx, stage, time.Since(started))
		result = make([]any, len(queries))
		used := 0
		for i, value := range raw {
			if b, ok := value.([]byte); ok {
				used += len(b)
			} else {
				used += 16
			}
			if used > repo.maxBytes {
				return ErrProjectionLimit
			}
			result[i], err = decodeAggregate(fields[i], queries[i].Aggregate, value)
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
