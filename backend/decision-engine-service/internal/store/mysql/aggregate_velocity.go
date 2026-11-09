package mysql

import (
	"context"
	"strings"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/velocity"
)

// Recognize only a complete AND of account equality and a half-open timestamp
// window. Extra predicates, OR/NOT, wider windows and non-exact aggregates use
// the ordinary SQL compiler. A summary is never an approximate rule result.
func velocityWindow(filter *ports.AggregateFilter) (groupField, timeField string, group []byte, start, end time.Time, ok bool) {
	if filter == nil {
		return
	}
	var predicates []ports.AggregateFilter
	var walk func(ports.AggregateFilter, int) bool
	walk = func(f ports.AggregateFilter, depth int) bool {
		if depth > 5 {
			return false
		}
		if f.Kind == "predicate" {
			predicates = append(predicates, f)
			return len(predicates) <= 3
		}
		if f.Kind != "group" || !strings.EqualFold(f.Operator, "and") {
			return false
		}
		for _, child := range f.Children {
			if !walk(child, depth+1) {
				return false
			}
		}
		return true
	}
	if !walk(*filter, 1) || len(predicates) != 3 {
		return
	}
	parse := func(value any) (time.Time, bool) {
		switch v := value.(type) {
		case time.Time:
			return v, true
		case string:
			t, e := time.Parse(time.RFC3339Nano, v)
			return t, e == nil
		}
		return time.Time{}, false
	}
	var haveGroup, haveStart, haveEnd bool
	for _, p := range predicates {
		switch p.Op {
		case "eq":
			value, valid := p.Value.(string)
			if !valid || haveGroup || len(value) > velocity.MaxGroupBytes {
				return
			}
			groupField, group, haveGroup = p.Field, []byte(value), true
		case "gte":
			if haveStart {
				return
			}
			var valid bool
			start, valid = parse(p.Value)
			if !valid {
				return
			}
			timeField, haveStart = p.Field, true
		case "lt":
			if haveEnd {
				return
			}
			var valid bool
			end, valid = parse(p.Value)
			if !valid {
				return
			}
			haveEnd = true
		default:
			return
		}
	}
	if !haveGroup || !haveStart || !haveEnd {
		return
	}
	for _, p := range predicates {
		if p.Op == "lt" && p.Field != timeField {
			return
		}
	}
	_, _, ok = velocity.WholeMinutes(start, end)
	return
}

func (r TenantDataReader) velocitySQL(ctx context.Context, repo authoringRepository, t registryTable, queries []ports.AggregateQuery, expressions []string, where string, args []any) (string, []any, bool, error) {
	if !r.velocityReads {
		return "", nil, false, nil
	}
	groupField, timeField, group, start, end, eligible := velocityWindow(queries[0].Filter)
	if !eligible {
		return "", nil, false, nil
	}
	for _, q := range queries {
		if q.Aggregate != "count" && q.Aggregate != "sum" {
			return "", nil, false, nil
		}
	}
	database := strings.Trim(repo.schema, "`")
	definition, err := velocity.Load(ctx, repo.tx, database, repo.tenant, t.id, t.version)
	if err != nil || definition == nil {
		return "", nil, false, err
	}
	if definition.GroupField != groupField || definition.TimeField != timeField {
		return "", nil, false, nil
	}
	groupMapping, err := t.field(groupField)
	if err != nil {
		return "", nil, false, err
	}
	timeMapping, err := t.field(timeField)
	if err != nil {
		return "", nil, false, err
	}
	if groupMapping.Type != "string" || timeMapping.Type != "timestamp" {
		return "", nil, false, ErrPublishedModelUnavailable
	}
	columns, parameters := make([]string, len(queries)), []any{}
	for i, q := range queries {
		expr, bound, ok, err := velocity.AggregateExpression(database, t.qualified, expressions[i], where, timeMapping.columns["value"], *definition, group, start, end, args, q.Aggregate, q.Field)
		if err != nil || !ok {
			return "", nil, false, err
		}
		columns[i] = expr
		parameters = append(parameters, bound...)
	}
	return "SELECT " + strings.Join(columns, ","), parameters, true, nil
}
