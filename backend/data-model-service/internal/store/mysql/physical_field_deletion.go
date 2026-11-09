package mysql

import (
	"context"
	"database/sql"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
	"sort"
	"strings"
)

var ErrFieldDeletionDependency = errors.New("remove field relationships, display/navigation references and dependent index jobs before deleting")

// DropField removes all registered components. Unique text retirement must
// precede this step so no trigger can refer to the removed value column.
func (p *PhysicalPlanner) DropField(id uuid.UUID) (PhysicalStatement, error) {
	fs, err := p.selectFields([]uuid.UUID{id})
	if err != nil {
		return PhysicalStatement{}, err
	}
	f := fs[0]
	if f.field.Name == "object_id" || f.field.Name == "updated_at" {
		return PhysicalStatement{}, ErrPhysicalConflict
	}
	var clauses []string
	if f.field.DataType == datamodel.DataTypeBool || f.field.DataType == datamodel.DataTypeIPAddress {
		clauses = append(clauses, "DROP CHECK `field_shape_"+physicalUUID(id)+"`")
	}
	if f.field.IsUnique && f.field.DataType != datamodel.DataTypeString {
		clauses = append(clauses, "DROP INDEX `record_unique_"+physicalUUID(id)+"`")
	}
	var columns []string
	for _, c := range f.columns {
		columns = append(columns, c)
	}
	sort.Strings(columns)
	for _, c := range columns {
		clauses = append(clauses, "DROP COLUMN "+c)
	}
	return p.statement("delete_field", "ALTER TABLE "+p.qualified+" "+strings.Join(clauses, ", ")).withNative(PhysicalNativeChange{Kind: "delete_field", FieldID: id}), nil
}

// CheckFieldDeletion uses exact table/field indexes, never tenant-wide lists.
// Call with the table/registry locks held before preparing DDL and publishing.
func CheckFieldDeletion(ctx context.Context, tx *sql.Tx, database string, tenant, table, field uuid.UUID) (datamodel.DeleteReport, error) {
	report := datamodel.NewDeleteReport()
	schema, err := mysqlstore.Identifier(database)
	if err != nil {
		return report, err
	}
	var name, caption string
	err = tx.QueryRowContext(ctx, "SELECT f.name,t.caption_field FROM "+schema+".data_model_fields f JOIN "+schema+".data_model_tables t ON t.tenant_id=f.tenant_id AND t.id=f.table_id WHERE f.tenant_id=? AND f.table_id=? AND f.id=? FOR UPDATE", mysqlstore.UUID(tenant), mysqlstore.UUID(table), mysqlstore.UUID(field)).Scan(&name, &caption)
	if err != nil {
		return report, err
	}
	report.Conflicts.Reserved = name == "object_id" || name == "updated_at"
	for _, spec := range []struct {
		object, predicate string
		target            *[]uuid.UUID
	}{
		{"data_model_links", "parent_table_id=? AND parent_field_id=?", &report.Conflicts.Links},
		{"data_model_links", "child_table_id=? AND child_field_id=?", &report.Conflicts.Links},
		{"data_model_pivots", "base_table_id=? AND field_id=?", &report.Conflicts.Pivots},
	} {
		rows, e := tx.QueryContext(ctx, "SELECT id FROM "+schema+"."+spec.object+" WHERE tenant_id=? AND "+spec.predicate+" LIMIT 1001 FOR SHARE", mysqlstore.UUID(tenant), mysqlstore.UUID(table), mysqlstore.UUID(field))
		if e != nil {
			return report, e
		}
		for rows.Next() {
			var id mysqlstore.UUID
			if e = rows.Scan(&id); e != nil {
				break
			}
			*spec.target = append(*spec.target, uuid.UUID(id))
		}
		e = errors.Join(e, rows.Err(), rows.Close())
		if e != nil {
			return report, e
		}
		if len(*spec.target) > 1000 {
			return report, ErrFieldDeletionDependency
		}
	}
	if report.Conflicts.Reserved || len(report.Conflicts.Links) > 0 || len(report.Conflicts.Pivots) > 0 || caption == name {
		return report, ErrFieldDeletionDependency
	}
	for _, q := range []string{
		"SELECT 1 FROM " + schema + ".data_model_display_field_references WHERE tenant_id=? AND field_id=? LIMIT 1 FOR SHARE",
		"SELECT 1 FROM " + schema + ".data_model_displayed_fields WHERE tenant_id=? AND table_id=? AND field_id=? LIMIT 1 FOR SHARE",
		"SELECT 1 FROM " + schema + ".data_model_navigation_options WHERE tenant_id=? AND source_table_id=? AND source_field_id=? LIMIT 1 FOR SHARE",
		"SELECT 1 FROM " + schema + ".data_model_navigation_options WHERE tenant_id=? AND target_table_id=? AND filter_field_id=? LIMIT 1 FOR SHARE",
		"SELECT 1 FROM " + schema + ".data_model_navigation_options WHERE tenant_id=? AND target_table_id=? AND ordering_field_id=? LIMIT 1 FOR SHARE",
	} {
		args := []any{mysqlstore.UUID(tenant), mysqlstore.UUID(table), mysqlstore.UUID(field)}
		if strings.Contains(q, "display_field_references") {
			args = []any{mysqlstore.UUID(tenant), mysqlstore.UUID(field)}
		}
		var exists int
		e := tx.QueryRowContext(ctx, q, args...).Scan(&exists)
		if e == nil {
			return report, ErrFieldDeletionDependency
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return report, e
		}
	}
	var exists int
	err = tx.QueryRowContext(ctx, "SELECT 1 FROM "+schema+".data_model_index_jobs WHERE tenant_id=? AND table_id=? AND status IN ('pending','running','applied') AND JSON_CONTAINS(columns_json,JSON_QUOTE(?)) LIMIT 1 FOR SHARE", mysqlstore.UUID(tenant), mysqlstore.UUID(table), name).Scan(&exists)
	if err == nil {
		return report, ErrFieldDeletionDependency
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return report, err
	}
	return report, nil
}

func (m *PhysicalPublicationManager) PublishFieldDeletion(ctx context.Context, r ports.PhysicalPublicationRequest, field uuid.UUID, effects func(ports.MutationStore) error) error {
	_, err := m.Publish(ctx, r, func(tx ports.MutationStore) error {
		scoped, ok := tx.(metadataMutationStore)
		if !ok {
			return ErrPhysicalConflict
		}
		if _, err := CheckFieldDeletion(ctx, scoped.tx, m.metadata.database, m.metadata.tenant, r.TableID, field); err != nil {
			return err
		}
		if _, err := scoped.tx.ExecContext(ctx, "DELETE FROM "+m.publication.schema+".data_model_physical_fields WHERE tenant_id=? AND table_id=? AND field_id=?", mysqlstore.UUID(m.metadata.tenant), mysqlstore.UUID(r.TableID), mysqlstore.UUID(field)); err != nil {
			return err
		}
		return effects(tx)
	})
	return err
}
