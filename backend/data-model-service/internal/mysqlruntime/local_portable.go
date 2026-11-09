package mysqlruntime

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/service"
	store "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func (p *LocalCreation) ExportModel(ctx context.Context, tenant uuid.UUID) (service.PortableDataModelDocument, error) {
	r, err := store.NewAuthorizedPublishedModelReader(p.Target, "fraud", tenant, localMaxRows, localMaxBytes, localTenantAuthority)
	if err != nil {
		return service.PortableDataModelDocument{}, err
	}
	reader, err := service.NewSnapshotDataModelReadService(r)
	if err != nil {
		return service.PortableDataModelDocument{}, err
	}
	published, err := reader.Get(ctx, tenant)
	if err != nil {
		return service.PortableDataModelDocument{}, err
	}
	return service.ExportPublishedDataModel(published), nil
}

// Import is a recoverable sequence of owner commands, not a cross-DDL transaction.
// Immutable start/completion audit entries bind the body and original response.
// Do not prune these entries independently of the creation/publication receipts.
type portableReceipt struct {
	Hash   string                        `json:"request_hash"`
	Result *service.PortableImportResult `json:"result,omitempty"`
}

func portableStep(op uuid.UUID, step string) uuid.UUID {
	return uuid.NewSHA1(op, []byte("local-portable/v1/"+step))
}

func readPortableReceipt(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, tenant, id uuid.UUID) (portableReceipt, error) {
	var raw []byte
	err := q.QueryRowContext(ctx, "SELECT details FROM fraud.data_model_schema_change_log WHERE tenant_id=? AND id=? AND operation='portable_import' AND OCTET_LENGTH(details)<=8192", mysqlstore.UUID(tenant), mysqlstore.UUID(id)).Scan(&raw)
	var receipt portableReceipt
	if err == nil {
		err = json.Unmarshal(raw, &receipt)
	}
	return receipt, err
}
func writePortableReceipt(ctx context.Context, r ports.SchemaChangeRepository, tenant, op uuid.UUID, phase string, receipt portableReceipt) error {
	raw, err := json.Marshal(receipt)
	if err != nil {
		return err
	}
	return r.Create(ctx, datamodel.SchemaChange{ID: portableStep(op, phase), TenantID: tenant, Operation: "portable_import", ResourceType: "data_model", ResourceID: op, Status: phase, Details: raw, CreatedAt: localClock{}.Now()})
}

func (p *LocalCreation) ImportModel(ctx context.Context, tenant uuid.UUID, document service.PortableDataModelDocument, key uuid.UUID) (service.PortableImportResult, error) {
	var result service.PortableImportResult
	if err := localAuthority(ctx, nil); err != nil {
		return result, err
	}
	if tenant == uuid.Nil || key == uuid.Nil {
		return result, store.ErrPhysicalConflict
	}
	if err := validateLocalPortable(document); err != nil {
		return result, fmt.Errorf("%w: %v", service.ErrFieldPatchInvalid, err)
	}
	raw, err := json.Marshal(document)
	if err != nil {
		return result, err
	}
	if len(raw) > 1<<20 {
		return result, store.ErrSnapshotLimit
	}
	digest := sha256.Sum256(raw)
	hash := hex.EncodeToString(digest[:])
	op := uuid.NewSHA1(tenant, []byte("portable-import/"+key.String()))
	conn, err := p.Target.Conn(ctx)
	if err != nil {
		return result, err
	}
	defer conn.Close()
	lock := "dm_import_" + op.String()
	var acquired int
	if err = conn.QueryRowContext(ctx, "SELECT GET_LOCK(?,1)", lock).Scan(&acquired); err != nil {
		return result, err
	}
	if acquired != 1 {
		return result, mysqlstore.ErrDDLLockBusy
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if e := conn.QueryRowContext(cleanup, "SELECT RELEASE_LOCK(?)", lock).Scan(&acquired); e != nil {
			conn.Raw(func(any) error { return driver.ErrBadConn })
		}
	}()
	var status string
	if err = conn.QueryRowContext(ctx, "SELECT status FROM fraud.data_model_tenants WHERE id=?", mysqlstore.UUID(tenant)).Scan(&status); err != nil {
		return result, err
	}
	if status != "active" {
		return result, store.ErrModelUnavailable
	}
	receipt, err := readPortableReceipt(ctx, conn, tenant, portableStep(op, "started"))
	if err == nil && receipt.Hash != hash {
		return result, store.ErrPublicationOperationConflict
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return result, err
	}
	if errors.Is(err, sql.ErrNoRows) {
		// No physical writes before checking every requested name against the bounded
		// tenant snapshot. Concurrent races remain protected by owner name allocation.
		current, e := p.ExportModel(ctx, tenant)
		if e != nil {
			return result, e
		}
		names := map[string]bool{}
		for _, t := range current.Tables {
			names[t.Name] = true
		}
		for _, t := range document.Tables {
			if names[datamodel.NormalizeName(t.Name)] {
				return result, store.ErrPhysicalConflict
			}
		}
		err = mysqlstore.Mutation(ctx, p.Target, func(tx *sql.Tx) error {
			r, e := store.NewMetadataRepositories(tx, "fraud", tenant, localMaxRows, localMaxBytes)
			if e != nil {
				return e
			}
			return writePortableReceipt(ctx, r.History, tenant, op, "started", portableReceipt{Hash: hash})
		})
		if err != nil {
			return result, err
		}
	}
	done, err := readPortableReceipt(ctx, conn, tenant, portableStep(op, "completed"))
	if err == nil {
		if done.Hash != hash || done.Result == nil {
			return result, store.ErrPublicationOperationConflict
		}
		return *done.Result, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return result, err
	}
	tables := map[string]uuid.UUID{}
	fields := map[string]map[string]uuid.UUID{}
	for i, t := range document.Tables {
		table, e := p.Create(ctx, service.CreateTableInput{TenantID: tenant, Name: t.Name, Description: t.Description, Alias: t.Alias, SemanticType: t.SemanticType}, portableStep(op, fmt.Sprintf("table/%d", i)))
		if e != nil {
			return result, e
		}
		name := datamodel.NormalizeName(t.Name)
		tables[name] = table.ID
		for j, f := range t.Fields {
			typ, e := datamodel.ParseDataType(f.DataType)
			if e != nil {
				return result, e
			}
			_, e = p.CreateField(ctx, service.CreateFieldInput{TableID: table.ID, Name: f.Name, Description: f.Description, Alias: f.Alias, DataType: typ, Nullable: f.Nullable, IsEnum: f.IsEnum, IsUnique: f.IsUnique, EnumValues: f.EnumValues}, portableStep(op, fmt.Sprintf("field/%d/%d", i, j)))
			if e != nil {
				return result, e
			}
			result.FieldsCreated++
		}
		_, e = readLocalMetadata(ctx, p.Target, table.ID, "data_model_tables", func(ctx context.Context, r store.MetadataRepositories, id uuid.UUID) (any, error) {
			fs, e := r.Fields.ListByTable(ctx, id)
			if e != nil {
				return nil, e
			}
			fields[name] = map[string]uuid.UUID{}
			for _, f := range fs {
				if !f.Archived {
					fields[name][f.Name] = f.ID
				}
			}
			return nil, nil
		})
		if e != nil {
			return result, e
		}
		result.TablesCreated++
	}
	// Caption changes use the existing dependency/version/index publication path.
	for i, t := range document.Tables {
		if t.CaptionField != "" {
			caption := datamodel.NormalizeName(t.CaptionField)
			_, e := p.UpdateTable(ctx, service.UpdateTableInput{TableID: tables[datamodel.NormalizeName(t.Name)], CaptionField: &caption}, portableStep(op, fmt.Sprintf("caption/%d", i)))
			if e != nil {
				return result, e
			}
		}
	}
	return p.finishPortable(ctx, tenant, op, hash, document, tables, fields, result)
}

// pinnedPortableTx lets owner services compose inside one caller-owned metadata
// transaction. It cannot commit or obtain another database connection.
type pinnedPortableTx struct{ store ports.MutationStore }

func (p pinnedPortableTx) Run(ctx context.Context, fn func(ports.MutationStore) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return fn(p.store)
}

func (p *LocalCreation) finishPortable(ctx context.Context, tenant, op uuid.UUID, hash string, d service.PortableDataModelDocument, tables map[string]uuid.UUID, fields map[string]map[string]uuid.UUID, result service.PortableImportResult) (service.PortableImportResult, error) {
	queue, err := localIndexQueue(p.Target)
	if err != nil {
		return result, err
	}
	manager, err := store.NewTransactionManager(p.Target, "fraud", tenant, localMaxRows, localMaxBytes, localTenantAuthority, queue)
	if err != nil {
		return result, err
	}
	norm := datamodel.NormalizeName
	field := func(t, f string) uuid.UUID { return fields[norm(t)][norm(f)] }
	err = manager.RunWithTx(ctx, func(m ports.MutationStore, tx *sql.Tx) error {
		ids := make([]uuid.UUID, 0, len(tables))
		for _, id := range tables {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i].String() < ids[j].String() })
		// Same ordered table locks as display writers; pin all physical registries.
		registry, e := store.NewPhysicalRegistry(tx, "fraud", tenant, localMaxRows)
		if e != nil {
			return e
		}
		for _, id := range ids {
			var got mysqlstore.UUID
			if e = tx.QueryRowContext(ctx, "SELECT id FROM fraud.data_model_tables WHERE tenant_id=? AND id=? AND NOT archived FOR UPDATE", mysqlstore.UUID(tenant), mysqlstore.UUID(id)).Scan(&got); e != nil {
				return e
			}
		}
		for _, id := range ids {
			var v mysqlstore.UUID
			if e = tx.QueryRowContext(ctx, "SELECT published_version FROM fraud.data_model_physical_tables WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(tenant), mysqlstore.UUID(id)).Scan(&v); e != nil {
				return e
			}
			if _, e = registry.LockPublished(ctx, id, uuid.UUID(v)); e != nil {
				return e
			}
		}
		unit := pinnedPortableTx{m}
		idsGenerator, clock := localIDs{}, localClock{}
		links := service.NewLinkService(m.Tables(), m.Fields(), m.Links(), m.Pivots(), m.SchemaChanges(), unit, idsGenerator, clock)
		pivots := service.NewPivotService(m.Tables(), m.Fields(), m.Links(), m.Pivots(), m.SchemaChanges(), unit, idsGenerator, clock)
		options := service.NewOptionsService(m.Tables(), m.Fields(), m.TableOptions(), m.SchemaChanges(), unit, idsGenerator, clock)
		navigation := service.NewNavigationOptionService(m.Tables(), m.Fields(), m.Links(), m.Pivots(), m.NavigationOptions(), m.SchemaChanges(), unit, idsGenerator, clock)
		linkIDs := map[string]uuid.UUID{}
		for _, l := range d.Links {
			v, e := links.Create(ctx, service.CreateLinkInput{TenantID: tenant, Name: l.Name, ParentTable: tables[norm(l.ParentTable)], ParentField: field(l.ParentTable, l.ParentField), ChildTable: tables[norm(l.ChildTable)], ChildField: field(l.ChildTable, l.ChildField)})
			if e != nil {
				return e
			}
			linkIDs[norm(l.Name)] = v.ID
			result.LinksCreated++
		}
		for _, v := range d.Pivots {
			input := service.CreatePivotInput{TenantID: tenant, BaseTableID: tables[norm(v.BaseTable)]}
			if v.Field != "" {
				id := field(v.BaseTable, v.Field)
				input.FieldID = &id
			}
			for _, l := range v.PathLinks {
				input.PathLinkIDs = append(input.PathLinkIDs, linkIDs[norm(l)])
			}
			if _, e := pivots.Create(ctx, input); e != nil {
				return e
			}
			result.PivotsCreated++
		}
		for _, t := range d.Tables {
			if t.Options != nil {
				input := datamodel.TableOptions{TableID: tables[norm(t.Name)]}
				for _, f := range t.Options.DisplayedFields {
					input.DisplayedFields = append(input.DisplayedFields, field(t.Name, f))
				}
				for _, f := range t.Options.FieldOrder {
					input.FieldOrder = append(input.FieldOrder, field(t.Name, f))
				}
				if _, e := options.Upsert(ctx, input); e != nil {
					return e
				}
				result.TableOptionsUpserted++
			}
			for _, o := range t.NavigationOptions {
				if _, e := navigation.Create(ctx, service.CreateNavigationOptionInput{TenantID: tenant, SourceTableID: tables[norm(t.Name)], SourceFieldID: field(t.Name, o.SourceField), TargetTableID: tables[norm(o.TargetTable)], FilterFieldID: field(o.TargetTable, o.FilterField), OrderingFieldID: field(o.TargetTable, o.OrderingField)}); e != nil {
					return e
				}
				result.NavigationOptionsCreated++
			}
		}
		tenantRecord, e := m.Tenants().GetByID(ctx, tenant)
		if e != nil {
			return e
		}
		if string(tenantRecord.Status) != "active" {
			return store.ErrModelUnavailable
		}
		revisions, e := m.TenantSchemaMigrations().ListByTenant(ctx, tenant)
		if e != nil {
			return e
		}
		result.RevisionID = service.PublishedRevisionID(tenantRecord, revisions)
		return writePortableReceipt(ctx, m.SchemaChanges(), tenant, op, "completed", portableReceipt{Hash: hash, Result: &result})
	})
	if err != nil {
		return service.PortableImportResult{}, err
	}
	return result, nil
}
