package mysql

import (
	"errors"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
)

var ErrRecordAggregate = errors.New("supported mapped aggregate field required")

// compileAggregate accepts only this module's fixed operation names. It shares
// mapping/filter/budget handling across AVG, MIN, MAX and COUNT DISTINCT without
// admitting caller expressions, casts, GROUP BY, windows or row limits.
func (p *RecordProjection) compileAggregate(name, operation string, filter *ingestion.AggregateFilter, limits RecordQueryLimits) (string, []any, ingestion.FieldSchema, error) {
	empty := ingestion.FieldSchema{}
	if p == nil || p.table == "" {
		return "", nil, empty, ErrRecordAggregate
	}
	name = strings.TrimSpace(name)
	for _, field := range p.fields {
		if field.definition.Name != name {
			continue
		}
		if field.definition.Archived || field.column == "" {
			break
		}
		kind := field.definition.DataType
		if field.width != 1 && !(kind == "ip_address" && operation == "COUNT DISTINCT" && field.width == 3 && len(field.components) == 3) {
			break
		}
		var expression string
		switch operation {
		case "AVG":
			if kind != "int" && kind != "float" {
				break
			}
			expression = "AVG(" + field.column + "), COUNT(" + field.column + ")"
			if kind == "int" {
				expression = "SUM(" + field.column + "), COUNT(" + field.column + ")"
			}
			if kind == "float" {
				expression += ", MAX(ABS(" + field.column + "))"
			}
		case "MIN", "MAX":
			if kind != "int" && kind != "float" && kind != "timestamp" && kind != "string" {
				break
			}
			expression = operation + "(" + field.column + "), COUNT(" + field.column + ")"
			if kind == "string" {
				value := operation + "(CAST(" + field.column + " AS BINARY))"
				expression = "CASE WHEN OCTET_LENGTH(" + value + ")<=16384 THEN " + value + " ELSE NULL END, COUNT(" + field.column + "), OCTET_LENGTH(" + value + ")"
			}
		case "COUNT DISTINCT":
			if kind != "int" && kind != "float" && kind != "bool" && kind != "timestamp" && kind != "string" && kind != "ip_address" {
				break
			}
			expression = "COUNT(DISTINCT " + field.column + ")"
			if kind == "string" {
				expression = "COUNT(DISTINCT CAST(" + field.column + " AS BINARY))"
			}
			if kind == "ip_address" {
				expression = "COUNT(DISTINCT " + strings.Join(field.components, ",") + ")"
			}
		}
		if expression == "" {
			break
		}
		compiled, err := p.CompileFilter(filter)
		if err != nil {
			return "", nil, empty, err
		}
		where, args := compiled.SQL()
		query := "SELECT " + expression + " FROM " + p.table
		if where != "" {
			query += " WHERE " + where
		}
		if err := ValidateRecordQuery(query, args, limits); err != nil {
			return "", nil, empty, err
		}
		return query, args, field.definition, nil
	}
	return "", nil, empty, ErrRecordAggregate
}
