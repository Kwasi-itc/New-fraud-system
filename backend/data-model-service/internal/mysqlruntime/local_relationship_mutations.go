package mysqlruntime

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/httpapi/dto"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/service"
	store "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

// relationshipCommand is internal routing data, never accepted as a client envelope.
type relationshipCommand struct {
	Kind       string
	ID         uuid.UUID
	Dry        bool
	Link       dto.CreateLinkRequest
	Pivot      dto.CreatePivotRequest
	Options    dto.TableOptionsRequest
	Navigation dto.CreateNavigationOptionRequest
}
type relationshipReceipt struct {
	Hash     [32]byte        `json:"hash"`
	Response json.RawMessage `json:"response"`
}

// Restricted repository lists keep legacy owner validation proportional to the
// selected relationship, not the tenant's complete graph.
type selectedLinks struct {
	ports.LinkRepository
	values []datamodel.Link
}

func (s selectedLinks) ListByTenant(context.Context, uuid.UUID) ([]datamodel.Link, error) {
	return s.values, nil
}

type selectedPivots struct {
	ports.PivotRepository
	values []datamodel.Pivot
}

func (s selectedPivots) ListByTenant(context.Context, uuid.UUID) ([]datamodel.Pivot, error) {
	return s.values, nil
}

func relationshipInvalid(message string) error {
	return fmt.Errorf("%w: %s", service.ErrFieldPatchInvalid, message)
}
func (p *LocalCreation) relationship(ctx context.Context, input relationshipCommand, key uuid.UUID) (json.RawMessage, error) {
	if err := localAuthority(ctx, nil); err != nil {
		return nil, err
	}
	// A separate versioned encoding for metadata-only relationship receipts.
	encoded, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(encoded)
	receiptID := uuid.NewSHA1(input.ID, []byte("relationship/v1/"+input.Kind+"/"+key.String()))
	// Receipts survive deletion and are scoped to both route identity and body.
	read := func(q interface {
		QueryRowContext(context.Context, string, ...any) *sql.Row
	}) (json.RawMessage, error) {
		var raw []byte
		var tenant mysqlstore.UUID
		e := q.QueryRowContext(ctx, "SELECT h.tenant_id,h.details FROM fraud.data_model_schema_change_log h JOIN fraud.data_model_tenants t ON t.id=h.tenant_id AND t.status='active' WHERE h.id=? AND h.operation='local_relationship' AND OCTET_LENGTH(h.details)<=1048576", mysqlstore.UUID(receiptID)).Scan(&tenant, &raw)
		if e != nil {
			return nil, e
		}
		var r relationshipReceipt
		if e = json.Unmarshal(raw, &r); e != nil {
			return nil, e
		}
		if r.Hash != hash {
			return nil, store.ErrPublicationOperationConflict
		}
		return r.Response, nil
	}
	if !input.Dry {
		if v, e := read(p.Target); e == nil {
			return v, nil
		} else if !errors.Is(e, sql.ErrNoRows) {
			return nil, e
		}
	}
	tenant := input.ID
	resource := ""
	switch input.Kind {
	case "options", "navigation_create":
		resource = "data_model_tables"
	case "link_delete":
		resource = "data_model_links"
	case "pivot_delete":
		resource = "data_model_pivots"
	case "navigation_delete":
		resource = "data_model_navigation_options"
	}
	if resource != "" {
		var t mysqlstore.UUID
		if err = p.Target.QueryRowContext(ctx, "SELECT tenant_id FROM fraud."+resource+" WHERE id=?", mysqlstore.UUID(input.ID)).Scan(&t); err != nil {
			// A concurrent delete can commit after the initial receipt read.
			if !input.Dry && errors.Is(err, sql.ErrNoRows) {
				if v, e := read(p.Target); !errors.Is(e, sql.ErrNoRows) {
					return v, e
				}
			}
			return nil, err
		}
		tenant = uuid.UUID(t)
	}
	queue, err := localIndexQueue(p.Target)
	if err != nil {
		return nil, err
	}
	manager, err := store.NewTransactionManager(p.Target, "fraud", tenant, localMaxRows, localMaxBytes, localTenantAuthority, queue)
	if err != nil {
		return nil, err
	}
	var response json.RawMessage
	err = manager.RunWithTx(ctx, func(m ports.MutationStore, tx *sql.Tx) error {
		var status string
		if e := tx.QueryRowContext(ctx, "SELECT status FROM fraud.data_model_tenants WHERE id=? FOR SHARE", mysqlstore.UUID(tenant)).Scan(&status); e != nil {
			return e
		}
		if status != "active" {
			return store.ErrModelUnavailable
		}
		tableSet := map[uuid.UUID]bool{}
		add := func(ids ...uuid.UUID) {
			for _, id := range ids {
				tableSet[id] = true
			}
		}
		links := selectedLinks{LinkRepository: m.Links()}
		pivots := selectedPivots{PivotRepository: m.Pivots()}
		var link datamodel.Link
		var pivot datamodel.Pivot
		var nav datamodel.NavigationOption
		var e error
		switch input.Kind {
		case "link_create":
			add(input.Link.ParentTableID, input.Link.ChildTableID)
		case "link_delete":
			link, e = m.Links().GetByID(ctx, input.ID)
			add(link.ParentTable, link.ChildTable)
		case "pivot_create":
			add(input.Pivot.BaseTableID)
			if len(input.Pivot.PathLinkIDs) > 100 {
				return relationshipInvalid("at most 100 path links")
			}
			repositories, x := store.NewMetadataRepositories(tx, "fraud", tenant, localMaxRows, localMaxBytes)
			if x != nil {
				return x
			}
			links.values, e = repositories.Links.ListByIDs(ctx, input.Pivot.PathLinkIDs)
			if e != nil {
				return e
			}
			for _, l := range links.values {
				add(l.ParentTable, l.ChildTable)
			}
		case "pivot_delete":
			pivot, e = m.Pivots().GetByID(ctx, input.ID)
			add(pivot.BaseTableID)
		case "options":
			add(input.ID)
		case "navigation_create":
			add(input.ID, input.Navigation.TargetTableID)
		case "navigation_delete":
			nav, e = m.NavigationOptions().GetByID(ctx, input.ID)
			add(nav.SourceTableID, nav.TargetTableID)
		default:
			return relationshipInvalid("unknown relationship command")
		}
		if e != nil {
			return e
		}
		tables := make([]uuid.UUID, 0, len(tableSet))
		for id := range tableSet {
			if id == uuid.Nil {
				return relationshipInvalid("nonzero table IDs required")
			}
			tables = append(tables, id)
		}
		sort.Slice(tables, func(i, j int) bool { return tables[i].String() < tables[j].String() })
		// Match display/DDL owner ordering. Do not serialize unrelated tenant tables.
		for _, id := range tables {
			var got mysqlstore.UUID
			if e = tx.QueryRowContext(ctx, "SELECT id FROM fraud.data_model_tables WHERE tenant_id=? AND id=? AND NOT archived FOR UPDATE", mysqlstore.UUID(tenant), mysqlstore.UUID(id)).Scan(&got); e != nil {
				return e
			}
		}
		registry, e := store.NewPhysicalRegistry(tx, "fraud", tenant, localMaxRows)
		if e != nil {
			return e
		}
		for _, id := range tables {
			var version mysqlstore.UUID
			if e = tx.QueryRowContext(ctx, "SELECT published_version FROM fraud.data_model_physical_tables WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(tenant), mysqlstore.UUID(id)).Scan(&version); e != nil {
				return e
			}
			if _, e = registry.LockPublished(ctx, id, uuid.UUID(version)); e != nil {
				return e
			}
		}
		if !input.Dry {
			if v, e := read(tx); e == nil {
				response = v
				return nil
			} else if !errors.Is(e, sql.ErrNoRows) {
				return e
			}
		}
		// Relationship identities are immutable. Re-read selected links after locks so
		// concurrent deletion cannot validate a stale path.
		if len(links.values) > 0 {
			repositories, x := store.NewMetadataRepositories(tx, "fraud", tenant, localMaxRows, localMaxBytes)
			if x != nil {
				return x
			}
			links.values, e = repositories.Links.ListByIDs(ctx, input.Pivot.PathLinkIDs)
			if e != nil {
				return e
			}
		}
		unit := pinnedPortableTx{m}
		ids, clock := localIDs{}, localClock{}
		linkOwner := service.NewLinkService(m.Tables(), m.Fields(), links, pivots, m.SchemaChanges(), unit, ids, clock)
		pivotOwner := service.NewPivotService(m.Tables(), m.Fields(), links, pivots, m.SchemaChanges(), unit, ids, clock)
		var value any
		activeFields := func(fieldIDs ...uuid.UUID) error {
			for _, id := range fieldIDs {
				f, e := m.Fields().GetByID(ctx, id)
				if e != nil {
					return e
				}
				if f.Archived || !tableSet[f.TableID] {
					return relationshipInvalid("active field in selected tables required")
				}
			}
			return nil
		}
		exists := func(query string, args ...any) (bool, error) {
			var yes bool
			e := tx.QueryRowContext(ctx, "SELECT EXISTS("+query+")", args...).Scan(&yes)
			return yes, e
		}
		switch input.Kind {
		case "link_create":
			b := input.Link
			if e = activeFields(b.ParentFieldID, b.ChildFieldID); e != nil {
				return e
			}
			v, x := linkOwner.Create(ctx, service.CreateLinkInput{TenantID: tenant, Name: b.Name, ParentTable: b.ParentTableID, ParentField: b.ParentFieldID, ChildTable: b.ChildTableID, ChildField: b.ChildFieldID})
			e = x
			value = map[string]any{"link": dto.AdaptLink(v)}
		case "link_delete":
			// Indexed dependency queries; RESTRICT FKs are an additional safety net.
			for _, q := range []string{"SELECT 1 FROM fraud.data_model_pivot_path_links WHERE tenant_id=? AND link_id=?", "SELECT 1 FROM fraud.data_model_display_link_references WHERE tenant_id=? AND link_id=?"} {
				yes, x := exists(q, mysqlstore.UUID(tenant), mysqlstore.UUID(input.ID))
				if x != nil {
					return x
				}
				if yes {
					return store.ErrFieldDeletionDependency
				}
			}
			yes, x := exists("SELECT 1 FROM fraud.data_model_navigation_options WHERE tenant_id=? AND source_table_id=? AND source_field_id=? AND target_table_id=? AND filter_field_id=?", mysqlstore.UUID(tenant), mysqlstore.UUID(link.ParentTable), mysqlstore.UUID(link.ParentField), mysqlstore.UUID(link.ChildTable), mysqlstore.UUID(link.ChildField))
			if x != nil {
				return x
			}
			if yes {
				return store.ErrFieldDeletionDependency
			}
			v, x := linkOwner.Delete(ctx, input.ID, input.Dry)
			e = x
			value = dto.AdaptDeleteReport(v)
		case "pivot_create":
			if input.Pivot.FieldID != nil {
				if e = activeFields(*input.Pivot.FieldID); e != nil {
					return e
				}
			}
			v, x := pivotOwner.Create(ctx, service.CreatePivotInput{TenantID: tenant, BaseTableID: input.Pivot.BaseTableID, FieldID: input.Pivot.FieldID, PathLinkIDs: input.Pivot.PathLinkIDs})
			e = x
			value = map[string]any{"pivot": dto.AdaptPivot(v)}
		case "pivot_delete":
			if pivot.FieldID != nil {
				yes, x := exists("SELECT 1 FROM fraud.data_model_navigation_options WHERE tenant_id=? AND source_table_id=? AND target_table_id=? AND source_field_id=?", mysqlstore.UUID(tenant), mysqlstore.UUID(pivot.BaseTableID), mysqlstore.UUID(pivot.BaseTableID), mysqlstore.UUID(*pivot.FieldID))
				if x != nil {
					return x
				}
				if yes {
					return store.ErrFieldDeletionDependency
				}
			}
			v, x := pivotOwner.Delete(ctx, input.ID, input.Dry)
			e = x
			value = dto.AdaptDeleteReport(v)
		case "options":
			b := input.Options
			if len(b.DisplayedFields)+len(b.FieldOrder) > 1000 {
				return relationshipInvalid("too many option fields")
			}
			owner := service.NewOptionsService(m.Tables(), m.Fields(), m.TableOptions(), m.SchemaChanges(), unit, ids, clock)
			prior, x := m.TableOptions().GetByTableID(ctx, input.ID)
			if x != nil {
				return x
			}
			id := uuid.Nil
			if prior != nil {
				id = prior.ID
			}
			v, x := owner.Upsert(ctx, datamodel.TableOptions{ID: id, TableID: input.ID, DisplayedFields: b.DisplayedFields, FieldOrder: b.FieldOrder})
			if x != nil {
				return x
			}
			fields, x := m.Fields().ListByTable(ctx, input.ID)
			e = x
			value = dto.AdaptTableOptions(v, fields)
		case "navigation_create":
			b := input.Navigation
			if e = activeFields(b.SourceFieldID, b.FilterFieldID, b.OrderingFieldID); e != nil {
				return e
			}
			// Fetch only potential backing relationships, never an arbitrary tenant page.
			rows, x := tx.QueryContext(ctx, "SELECT id FROM fraud.data_model_links WHERE tenant_id=? AND parent_table_id=? AND parent_field_id=? AND child_table_id=? AND child_field_id=? LIMIT 1", mysqlstore.UUID(tenant), mysqlstore.UUID(input.ID), mysqlstore.UUID(b.SourceFieldID), mysqlstore.UUID(b.TargetTableID), mysqlstore.UUID(b.FilterFieldID))
			if x != nil {
				return x
			}
			var linkID mysqlstore.UUID
			if rows.Next() {
				x = rows.Scan(&linkID)
			}
			x = errors.Join(x, rows.Err(), rows.Close())
			if x != nil {
				return x
			}
			if uuid.UUID(linkID) != uuid.Nil {
				l, x := m.Links().GetByID(ctx, uuid.UUID(linkID))
				if x != nil {
					return x
				}
				links.values = []datamodel.Link{l}
			}
			if input.ID == b.TargetTableID {
				var id mysqlstore.UUID
				x = tx.QueryRowContext(ctx, "SELECT id FROM fraud.data_model_pivots WHERE tenant_id=? AND base_table_id=? AND field_id=? LIMIT 1", mysqlstore.UUID(tenant), mysqlstore.UUID(input.ID), mysqlstore.UUID(b.SourceFieldID)).Scan(&id)
				if x == nil {
					pv, x := m.Pivots().GetByID(ctx, uuid.UUID(id))
					if x != nil {
						return x
					}
					pivots.values = []datamodel.Pivot{pv}
				} else if !errors.Is(x, sql.ErrNoRows) {
					return x
				}
			}
			owner := service.NewNavigationOptionService(m.Tables(), m.Fields(), links, pivots, m.NavigationOptions(), m.SchemaChanges(), unit, ids, clock)
			v, x := owner.Create(ctx, service.CreateNavigationOptionInput{TenantID: tenant, SourceTableID: input.ID, SourceFieldID: b.SourceFieldID, TargetTableID: b.TargetTableID, FilterFieldID: b.FilterFieldID, OrderingFieldID: b.OrderingFieldID})
			e = x
			value = map[string]any{"navigation_option": dto.AdaptNavigationOption(v)}
		case "navigation_delete":
			owner := service.NewNavigationOptionService(m.Tables(), m.Fields(), links, pivots, m.NavigationOptions(), m.SchemaChanges(), unit, ids, clock)
			e = owner.Delete(ctx, input.ID)
			value = map[string]any{}
		}
		if e != nil {
			return e
		}
		response, e = json.Marshal(value)
		if e != nil {
			return e
		}
		if input.Dry {
			return nil
		}
		if input.Kind == "navigation_create" || input.Kind == "navigation_delete" {
			if e = m.TenantSchemaMigrations().Create(ctx, datamodel.TenantSchemaMigration{ID: ids.New(), TenantID: tenant, Version: input.Kind + ":navigation_option", AppliedAt: clock.Now()}); e != nil {
				return e
			}
		}
		raw, e := json.Marshal(relationshipReceipt{Hash: hash, Response: response})
		if e != nil {
			return e
		}
		return m.SchemaChanges().Create(ctx, datamodel.SchemaChange{ID: receiptID, TenantID: tenant, Operation: "local_relationship", ResourceType: input.Kind, ResourceID: input.ID, Status: "completed", Details: raw, CreatedAt: clock.Now()})
	})
	if !input.Dry && errors.Is(err, sql.ErrNoRows) {
		if v, e := read(p.Target); !errors.Is(e, sql.ErrNoRows) {
			return v, e
		}
	}
	return response, err
}
