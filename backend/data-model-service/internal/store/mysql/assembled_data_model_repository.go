package mysql

import (
	"context"
	"database/sql"
	"errors"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

// DataModelReadRepository owns a read-only consistent snapshot. Binding a tenant
// is defense in depth; the caller must authorize that tenant before construction.
// It does not certify physical readiness. GetDataModelSnapshot also reads the
// revision envelope in the same snapshot for the snapshot-aware owner service.
type DataModelReadRepository struct {
	db                *sql.DB
	schema            string
	tenant            uuid.UUID
	maxRows, maxBytes int
}

func NewDataModelReadRepository(db *sql.DB, database string, tenant uuid.UUID, maxRows, maxBytes int) (*DataModelReadRepository, error) {
	schema, err := mysqlstore.Identifier(database)
	if err != nil {
		return nil, err
	}
	if db == nil || tenant == uuid.Nil || maxRows < 1 || maxRows > 100000 || maxBytes < 1 || maxBytes > 64<<20 {
		return nil, errors.New("database, tenant and bounded row/byte limits required")
	}
	return &DataModelReadRepository{db, schema, tenant, maxRows, maxBytes}, nil
}

func (r *DataModelReadRepository) GetAssembledDataModel(ctx context.Context, tenant uuid.UUID) (datamodel.AssembledDataModel, error) {
	if tenant != r.tenant {
		return datamodel.AssembledDataModel{}, errors.New("invalid assembled model tenant scope")
	}
	var model datamodel.AssembledDataModel
	err := mysqlstore.Snapshot(ctx, r.db, func(tx *sql.Tx) error {
		var err error
		model, err = loadAssembledModel(ctx, metadataRepository{tx, r.schema, r.tenant, r.maxRows, r.maxBytes})
		return err
	})
	if err != nil {
		return datamodel.AssembledDataModel{}, err
	}
	return model, nil
}

// aggregateList shares one row/text budget across all projections. Each query
// has a remaining-budget sentinel and suppresses oversized text on the server.
func aggregateList[T any](ctx context.Context, r *metadataRepository, query string, scan func(metadataScanner) (T, int64, error)) ([]T, error) {
	var used int64
	result, err := metadataList(ctx, *r, query, []any{mysqlstore.UUID(r.tenant)}, func(row metadataScanner) (T, int64, error) { v, n, err := scan(row); used += n; return v, n, err })
	if err != nil {
		return nil, err
	}
	r.maxRows -= len(result)
	r.maxBytes -= int(used)
	return result, nil
}

func loadAssembledModel(ctx context.Context, r metadataRepository) (datamodel.AssembledDataModel, error) {
	empty := datamodel.AssembledDataModel{}
	tables, err := aggregateList(ctx, &r, (TableRepository{r}).selectSQL()+" ORDER BY created_at DESC,id DESC", scanTable)
	if err != nil {
		return empty, err
	}
	fields, err := aggregateList(ctx, &r, (FieldRepository{r}).selectSQL()+" ORDER BY table_id,created_at,id", scanField)
	if err != nil {
		return empty, err
	}
	enums, err := aggregateList(ctx, &r, (FieldEnumValueRepository{r}).selectSQL()+" ORDER BY field_id,sort_order,created_at,id", scanEnum)
	if err != nil {
		return empty, err
	}
	links, err := aggregateList(ctx, &r, (LinkRepository{r}).selectSQL()+" ORDER BY created_at DESC,id DESC", scanLink)
	if err != nil {
		return empty, err
	}
	pivots, err := (PivotRepository{r}).read(ctx, nil)
	if err != nil {
		return empty, err
	}
	for _, p := range pivots {
		r.maxRows -= max(1, len(p.PathLinkIDs))
	}
	navigation, err := aggregateList(ctx, &r, (NavigationOptionRepository{r}).selectSQL()+" ORDER BY n.created_at,n.id", scanNavigation)
	if err != nil {
		return empty, err
	}
	options, err := aggregateList(ctx, &r, "SELECT id,tenant_id,table_id,updated_at FROM "+r.schema+".data_model_table_options WHERE tenant_id=? ORDER BY table_id", func(row metadataScanner) (datamodel.TableOptions, int64, error) {
		v := datamodel.TableOptions{DisplayedFields: []uuid.UUID{}, FieldOrder: []uuid.UUID{}}
		var at mysqlstore.DateTime
		err := row.Scan((*mysqlstore.UUID)(&v.ID), (*mysqlstore.UUID)(&v.TenantID), (*mysqlstore.UUID)(&v.TableID), &at)
		v.UpdatedAt = at.Time
		return v, 0, err
	})
	if err != nil {
		return empty, err
	}
	byTable := make(map[uuid.UUID]*datamodel.TableOptions, len(options))
	for i := range options {
		byTable[options[i].TableID] = &options[i]
	}
	for _, relation := range []string{"data_model_displayed_fields", "data_model_field_order"} {
		_, err := aggregateList(ctx, &r, "SELECT table_id,ordinal,field_id FROM "+r.schema+"."+relation+" WHERE tenant_id=? ORDER BY table_id,ordinal", func(row metadataScanner) (struct{}, int64, error) {
			var table, field mysqlstore.UUID
			var ordinal int
			if err := row.Scan(&table, &ordinal, &field); err != nil {
				return struct{}{}, 0, err
			}
			option := byTable[uuid.UUID(table)]
			if option == nil {
				return struct{}{}, 0, errors.New("option parent missing")
			}
			target := &option.FieldOrder
			if relation == "data_model_displayed_fields" {
				target = &option.DisplayedFields
			}
			if ordinal != len(*target) {
				return struct{}{}, 0, errors.New("noncontiguous option ordinals")
			}
			*target = append(*target, uuid.UUID(field))
			return struct{}{}, 0, nil
		})
		if err != nil {
			return empty, err
		}
	}
	return assembleModel(tables, fields, enums, links, pivots, navigation, byTable)
}

func assembleModel(tables []datamodel.Table, fields []datamodel.Field, enums []datamodel.FieldEnumValue, links []datamodel.Link, pivots []datamodel.Pivot, navigation []datamodel.NavigationOption, options map[uuid.UUID]*datamodel.TableOptions) (datamodel.AssembledDataModel, error) {
	empty := datamodel.AssembledDataModel{}
	result := datamodel.AssembledDataModel{Tables: map[string]datamodel.AssembledTable{}, Pivots: []datamodel.AssembledPivot{}}
	tableByID := make(map[uuid.UUID]datamodel.Table, len(tables))
	fieldByID := make(map[uuid.UUID]datamodel.Field, len(fields))
	linkByID := make(map[uuid.UUID]datamodel.Link, len(links))
	for _, t := range tables {
		if _, exists := result.Tables[t.Name]; exists {
			return empty, errors.New("duplicate assembled table name")
		}
		tableByID[t.ID] = t
		result.Tables[t.Name] = datamodel.AssembledTable{ID: t.ID, Name: t.Name, Description: t.Description, Alias: t.Alias, SemanticType: t.SemanticType, CaptionField: t.CaptionField, Archived: t.Archived, Fields: map[string]datamodel.AssembledField{}, LinksToSingle: map[string]datamodel.AssembledLink{}, NavigationOptions: []datamodel.NavigationOption{}, Options: options[t.ID]}
	}
	for _, f := range fields {
		t, ok := tableByID[f.TableID]
		if !ok {
			return empty, errors.New("field table missing")
		}
		target := result.Tables[t.Name]
		if _, exists := target.Fields[f.Name]; exists {
			return empty, errors.New("duplicate assembled field name")
		}
		fieldByID[f.ID] = f
		target.Fields[f.Name] = datamodel.AssembledField{ID: f.ID, Name: f.Name, Description: f.Description, Alias: f.Alias, DataType: f.DataType, Nullable: f.Nullable, IsEnum: f.IsEnum, IsUnique: f.IsUnique, Archived: f.Archived, EnumValues: []datamodel.FieldEnumValue{}}
	}
	for _, e := range enums {
		f, ok := fieldByID[e.FieldID]
		if !ok {
			return empty, errors.New("enum field missing")
		}
		// Match the source aggregate: retained values on non-enum fields are hidden.
		if !f.IsEnum {
			continue
		}
		target := result.Tables[tableByID[f.TableID].Name]
		af := target.Fields[f.Name]
		af.EnumValues = append(af.EnumValues, e)
		target.Fields[f.Name] = af
	}
	for _, l := range links {
		parent, pok := fieldByID[l.ParentField]
		child, cok := fieldByID[l.ChildField]
		if !pok || !cok || parent.TableID != l.ParentTable || child.TableID != l.ChildTable {
			return empty, errors.New("link endpoint missing or inconsistent")
		}
		pt, ct := tableByID[l.ParentTable], tableByID[l.ChildTable]
		target := result.Tables[ct.Name]
		if _, exists := target.LinksToSingle[l.Name]; exists {
			return empty, errors.New("duplicate assembled link name")
		}
		target.LinksToSingle[l.Name] = datamodel.AssembledLink{ID: l.ID, Name: l.Name, ParentTableID: l.ParentTable, ParentFieldID: l.ParentField, ChildTableID: l.ChildTable, ChildFieldID: l.ChildField, ParentTableName: pt.Name, ParentFieldName: parent.Name, ChildTableName: ct.Name, ChildFieldName: child.Name}
		linkByID[l.ID] = l
	}
	for _, p := range pivots {
		base, ok := tableByID[p.BaseTableID]
		if !ok {
			return empty, errors.New("pivot base missing")
		}
		ap := datamodel.AssembledPivot{ID: p.ID, BaseTableID: p.BaseTableID, BaseTable: base.Name, FieldID: p.FieldID, PathLinkIDs: p.PathLinkIDs, PathLinks: []string{}}
		if p.FieldID != nil {
			f, ok := fieldByID[*p.FieldID]
			if !ok || f.TableID != p.BaseTableID {
				return empty, errors.New("pivot field missing or inconsistent")
			}
			ap.Field = f.Name
		}
		for _, id := range p.PathLinkIDs {
			l, ok := linkByID[id]
			if !ok {
				return empty, errors.New("pivot link missing")
			}
			ap.PathLinks = append(ap.PathLinks, l.Name)
			ap.Field = fieldByID[l.ParentField].Name
		}
		result.Pivots = append(result.Pivots, ap)
	}
	for _, n := range navigation {
		source, ok := tableByID[n.SourceTableID]
		if !ok {
			return empty, errors.New("navigation source missing")
		}
		target := result.Tables[source.Name]
		target.NavigationOptions = append(target.NavigationOptions, n)
		result.Tables[source.Name] = target
	}
	for table, option := range options {
		if _, ok := tableByID[table]; !ok {
			return empty, errors.New("options table missing")
		}
		for _, ids := range [][]uuid.UUID{option.DisplayedFields, option.FieldOrder} {
			for _, id := range ids {
				f, ok := fieldByID[id]
				if !ok || f.TableID != table {
					return empty, errors.New("option field missing or inconsistent")
				}
			}
		}
	}
	return result, nil
}

var _ ports.DataModelReadRepository = (*DataModelReadRepository)(nil)

func (r *DataModelReadRepository) GetDataModelSnapshot(ctx context.Context, tenant uuid.UUID) (ports.DataModelSnapshot, error) {
	if tenant != r.tenant {
		return ports.DataModelSnapshot{}, errors.New("invalid assembled model tenant scope")
	}
	var result ports.DataModelSnapshot
	err := mysqlstore.Snapshot(ctx, r.db, func(tx *sql.Tx) error {
		budget := metadataRepository{tx, r.schema, r.tenant, r.maxRows, r.maxBytes}
		tenants, err := aggregateList(ctx, &budget, tenantSelect(budget)+" WHERE id=?", scanTenant)
		if err != nil {
			return err
		}
		if len(tenants) != 1 {
			return sql.ErrNoRows
		}
		result.Tenant = tenants[0]
		result.Migrations, err = aggregateList(ctx, &budget, (TenantSchemaMigrationRepository{budget}).selectSQL()+" ORDER BY applied_at DESC,id DESC", scanRevision)
		if err != nil {
			return err
		}
		result.Model, err = loadAssembledModel(ctx, budget)
		return err
	})
	if err != nil {
		return ports.DataModelSnapshot{}, err
	}
	return result, nil
}

var _ ports.DataModelSnapshotRepository = (*DataModelReadRepository)(nil)
