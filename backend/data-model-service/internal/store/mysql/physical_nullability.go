package mysql

import (
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/google/uuid"
)

// SetFieldNullable preserves the native type and existing indexes/checks. The
// complete before/after reference must still be bound and executed under P-01.
// Tightening is intentionally restricted to empty tables until populated-table
// validation/backfill has a durable protocol. No default value is synthesized.
func (p *PhysicalPlanner) SetFieldNullable(fieldID uuid.UUID, nullable bool) (PhysicalStatement, error) {
	fields, err := p.selectFields([]uuid.UUID{fieldID})
	if err != nil {
		return PhysicalStatement{}, err
	}
	f := fields[0]
	if f.field.IsEnum || (f.field.DataType == datamodel.DataTypeString && f.field.IsUnique) {
		return PhysicalStatement{}, ErrPhysicalPlanningUnsupported
	}
	type componentType struct{ component, sqlType string }
	var components []componentType
	switch f.field.DataType {
	case datamodel.DataTypeBool:
		components = []componentType{{"value", "TINYINT"}}
	case datamodel.DataTypeInt:
		components = []componentType{{"value", "INT"}}
	case datamodel.DataTypeFloat:
		components = []componentType{{"value", "DOUBLE"}}
	case datamodel.DataTypeString:
		components = []componentType{{"value", "LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin"}}
	case datamodel.DataTypeTimestamp:
		components = []componentType{{"value", "DATETIME(6)"}}
	case datamodel.DataTypeIPAddress:
		components = []componentType{{"address", "VARBINARY(16)"}, {"family", "TINYINT UNSIGNED"}, {"prefix", "TINYINT UNSIGNED"}}
	default:
		return PhysicalStatement{}, ErrPhysicalPlanningUnsupported
	}
	nullSQL := " NOT NULL"
	if nullable {
		nullSQL = " NULL"
	}
	clauses := make([]string, 0, len(components))
	for _, component := range components {
		clauses = append(clauses, "MODIFY COLUMN "+f.columns[component.component]+" "+component.sqlType+nullSQL)
	}
	statement := p.statement("field_nullability", "ALTER TABLE "+p.qualified+" "+strings.Join(clauses, ", "))
	statement.native = PhysicalNativeChange{Kind: "field_nullability", FieldID: fieldID, Nullable: nullable}
	statement.requireEmpty = !nullable
	return statement, nil
}
