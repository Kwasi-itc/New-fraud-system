package mysql

import (
	"context"
	"database/sql"
	"errors"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

// Verify the complete desired field/mapping snapshot after native owner effects.
// A valid SQL plan alone must not publish an unrelated metadata shape. Queries
// remain bounded (1,000 fields, at most three IP components) and indexed by table.
func (e *PhysicalExecutor) verifyNativeMetadataTx(ctx context.Context, tx *sql.Tx, table uuid.UUID, plan PhysicalDDLPlan) error {
	// Recheck retirement dependencies in the final owner transaction as well
	// as preparation. A concurrent relationship cannot publish an invalid model.
	for _, change := range plan.Native.Changes {
		if change.Kind != "disable_text_unique" {
			continue
		}
		var id mysqlstore.UUID
		err := tx.QueryRowContext(ctx, "SELECT id FROM "+e.schema+".data_model_links WHERE tenant_id=? AND parent_table_id=? AND parent_field_id=? LIMIT 1 FOR SHARE", mysqlstore.UUID(e.tenant), mysqlstore.UUID(table), mysqlstore.UUID(change.FieldID)).Scan(&id)
		if err == nil {
			return ErrPhysicalConflict
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	fields := map[uuid.UUID]datamodel.Field{}
	for _, mapped := range plan.Native.Fields {
		fields[mapped.Field.ID] = mapped.Field
	}
	for _, change := range plan.Native.Changes {
		f := fields[change.FieldID]
		switch change.Kind {
		case "delete_field":
			delete(fields, change.FieldID)
		case "field_enum":
			f.IsEnum = change.Enum
			f.Nullable = change.Nullable
			fields[f.ID] = f
		case "field_nullability", "field_nullability_checked", "text_nullability":
			f.Nullable = change.Nullable
			fields[f.ID] = f
		case "create_field_unique", "enable_text_unique":
			f.IsUnique = true
			fields[f.ID] = f
		case "drop_field_unique", "disable_text_unique":
			f.IsUnique = false
			fields[f.ID] = f
		}
	}
	columns := map[string]string{}
	for _, mapped := range plan.Native.Fields {
		if _, exists := fields[mapped.Field.ID]; !exists {
			continue
		}
		for _, column := range mapped.Components {
			columns[column.FieldID.String()+"/"+column.Component] = column.Name
		}
	}
	rows, err := tx.QueryContext(ctx, "SELECT f.id,CASE WHEN OCTET_LENGTH(f.name)<=1048576 THEN f.name END,f.data_type,f.nullable,f.is_unique,f.is_enum,f.archived,p.component,p.physical_name FROM "+e.schema+".data_model_fields f LEFT JOIN "+e.schema+".data_model_physical_fields p ON p.tenant_id=f.tenant_id AND p.table_id=f.table_id AND p.field_id=f.id WHERE f.tenant_id=? AND f.table_id=? ORDER BY f.id,p.component LIMIT 3001 FOR SHARE", mysqlstore.UUID(e.tenant), mysqlstore.UUID(table))
	if err != nil {
		return err
	}
	for rows.Next() {
		var id mysqlstore.UUID
		var name, component, column sql.NullString
		var kind string
		var nullable, unique, enum, archived bool
		if err = rows.Scan(&id, &name, &kind, &nullable, &unique, &enum, &archived, &component, &column); err != nil {
			break
		}
		expected, found := fields[uuid.UUID(id)]
		key := uuid.UUID(id).String() + "/" + component.String
		physical, exists := columns[key]
		if !found || !exists || !name.Valid || !component.Valid || !column.Valid || physical != column.String || expected.Name != name.String || string(expected.DataType) != kind || expected.Nullable != nullable || expected.IsUnique != unique || expected.IsEnum != enum || expected.Archived != archived {
			err = ErrPhysicalConflict
			break
		}
		delete(columns, key)
	}
	err = errors.Join(err, rows.Err(), rows.Close())
	if err != nil {
		return err
	}
	if len(columns) != 0 {
		return ErrPhysicalConflict
	}
	return nil
}
