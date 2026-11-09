package mysql

import (
	"context"
	"database/sql"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
	"strings"
)

var ErrPhysicalNullValues = errors.New("existing NULL values must be filled before making the field required")

// SetFieldNullableChecked preserves older empty-table plans while adding a
// separately typed protocol for populated tables. Both preparation and execution
// check for NULLs while the table's writer gate excludes ingestion transactions.
func (p *PhysicalPlanner) SetFieldNullableChecked(id uuid.UUID, nullable bool) (PhysicalStatement, error) {
	statement, err := p.SetFieldNullable(id, nullable)
	if err != nil {
		return statement, err
	}
	statement.requireEmpty = false
	statement.native.Kind = "field_nullability_checked"
	return statement, nil
}

type PhysicalDataReader interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// CheckNativeNullability performs bounded-result existence queries on only the
// changed columns. It can scan those columns; it never loads records into memory.
func CheckNativeNullability(ctx context.Context, q PhysicalDataReader, database, name string, plan PhysicalDDLPlan) error {
	if plan.Native == nil {
		return nil
	}
	schema, err := mysqlstore.Identifier(database)
	if err != nil {
		return err
	}
	table, err := mysqlstore.Identifier(name)
	if err != nil {
		return err
	}
	for _, change := range plan.Native.Changes {
		if (change.Kind != "field_nullability_checked" && change.Kind != "text_nullability") || change.Nullable {
			continue
		}
		var parts []string
		for _, field := range plan.Native.Fields {
			if field.Field.ID == change.FieldID {
				for _, component := range field.Components {
					column, err := mysqlstore.Identifier(component.Name)
					if err != nil {
						return err
					}
					parts = append(parts, column+" IS NULL")
				}
			}
		}
		if len(parts) == 0 {
			return ErrPhysicalConflict
		}
		var found int
		err := q.QueryRowContext(ctx, "SELECT 1 FROM "+schema+"."+table+" WHERE "+strings.Join(parts, " OR ")+" LIMIT 1").Scan(&found)
		if err == nil {
			return ErrPhysicalNullValues
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	return nil
}
