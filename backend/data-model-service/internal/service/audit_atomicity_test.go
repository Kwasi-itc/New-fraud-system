package service

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	storepostgres "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/postgres"
	tenantdbpostgres "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/tenantdb/postgres"
	"github.com/google/uuid"
)

type stubSchemaMigrationRepository struct {
	changes []datamodel.TenantSchemaMigration
}

func (s *stubSchemaMigrationRepository) Create(_ context.Context, c datamodel.TenantSchemaMigration) error {
	s.changes = append(s.changes, c)
	return nil
}
func (s *stubSchemaMigrationRepository) ListByTenant(context.Context, uuid.UUID) ([]datamodel.TenantSchemaMigration, error) {
	return s.changes, nil
}

var errAuditInjected = errors.New("injected audit persistence failure")

type auditFailureTransactions struct {
	ports.TransactionManager
	point string
}

func (m auditFailureTransactions) Run(ctx context.Context, fn func(ports.MutationStore) error) error {
	return m.TransactionManager.Run(ctx, func(s ports.MutationStore) error { return fn(auditFailureStore{s, m.point}) })
}

type auditFailureStore struct {
	ports.MutationStore
	point string
}

func (s auditFailureStore) SchemaChanges() ports.SchemaChangeRepository {
	return auditFailureHistory{s.MutationStore.SchemaChanges(), s.point == "history"}
}
func (s auditFailureStore) TenantSchemaMigrations() ports.TenantSchemaMigrationRepository {
	return auditFailureRevision{s.MutationStore.TenantSchemaMigrations(), s.point == "revision"}
}

type auditFailureHistory struct {
	ports.SchemaChangeRepository
	fail bool
}

func (r auditFailureHistory) Create(ctx context.Context, c datamodel.SchemaChange) error {
	if err := r.SchemaChangeRepository.Create(ctx, c); err != nil {
		return err
	}
	if r.fail {
		return errAuditInjected
	}
	return nil
}

type auditFailureRevision struct {
	ports.TenantSchemaMigrationRepository
	fail bool
}

func (r auditFailureRevision) Create(ctx context.Context, c datamodel.TenantSchemaMigration) error {
	if err := r.TenantSchemaMigrationRepository.Create(ctx, c); err != nil {
		return err
	}
	if r.fail {
		return errAuditInjected
	}
	return nil
}

type auditIDs struct{}

func (auditIDs) New() uuid.UUID { return uuid.New() }

type auditServices struct {
	tenants    TenantService
	tables     TableService
	fields     FieldService
	enums      FieldEnumValueService
	links      LinkService
	pivots     PivotService
	options    OptionsService
	navigation NavigationOptionService
	indexes    IndexJobService
}

func TestAuditHelpersFailClosed(t *testing.T) {
	ctx := context.Background()
	id := uuid.New()
	at := time.Now().UTC()
	history := &stubSchemaChangeRepository{}
	if err := recordSchemaChange(ctx, history, id, id, "op", "field", id, at, map[string]any{"invalid": func() {}}); err == nil || len(history.changes) != 0 {
		t.Fatal("invalid history replaced with empty JSON")
	}
	if err := recordSchemaChange(ctx, nil, id, id, "op", "field", id, at, nil); err == nil {
		t.Fatal("missing history accepted")
	}
	if err := recordTenantSchemaMigration(ctx, nil, auditIDs{}, id, "v", at); err == nil {
		t.Fatal("missing revision store accepted")
	}
	if err := recordTenantSchemaMigration(ctx, &stubSchemaMigrationRepository{}, nil, id, "v", at); err == nil {
		t.Fatal("missing generator accepted")
	}
}

func TestAllServiceAuditFailuresRollback(t *testing.T) {
	dsn := integrationDatabaseURL(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	pool := integrationPool(t, ctx, dsn)
	defer pool.Close()
	resetIntegrationDatabase(t, ctx, pool, dsn)
	tr := storepostgres.NewTenantRepository(pool)
	tb := storepostgres.NewTableRepository(pool)
	fr := storepostgres.NewFieldRepository(pool)
	er := storepostgres.NewFieldEnumValueRepository(pool)
	lr := storepostgres.NewLinkRepository(pool)
	pr := storepostgres.NewPivotRepository(pool)
	or := storepostgres.NewTableOptionsRepository(pool)
	nr := storepostgres.NewNavigationOptionRepository(pool)
	ir := storepostgres.NewIndexJobRepository(pool)
	hr := storepostgres.NewSchemaChangeRepository(pool)
	sm := tenantdbpostgres.NewSchemaManager(pool)
	tx := integrationTransactionManager(t, pool)
	ids := auditIDs{}
	clock := fixedIntegrationClock{now: time.Now().UTC().Truncate(time.Microsecond)}
	services := func(tm ports.TransactionManager) auditServices {
		return auditServices{
			NewTenantService(tr, hr, sm, tm, ids, clock), NewTableService(tr, tb, fr, lr, pr, hr, sm, tm, ids, clock), NewFieldService(tr, tb, fr, er, lr, pr, hr, sm, tm, ids, clock),
			NewFieldEnumValueService(fr, er, hr, tm, ids, clock), NewLinkService(tb, fr, lr, pr, hr, tm, ids, clock), NewPivotService(tb, fr, lr, pr, hr, tm, ids, clock),
			NewOptionsService(tb, fr, or, hr, tm, ids, clock), NewNavigationOptionService(tb, fr, lr, pr, nr, hr, tm, ids, clock), NewIndexJobService(tr, tb, fr, ir, hr, tm, ids, clock),
		}
	}
	good := services(tx)
	record, err := good.tenants.Create(ctx, tenant.CreateInput{Name: "audit fixture"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = good.tenants.Provision(ctx, record.ID); err != nil {
		t.Fatal(err)
	}
	pending, err := good.tenants.Create(ctx, tenant.CreateInput{Name: "pending fixture"})
	if err != nil {
		t.Fatal(err)
	}
	parent, err := good.tables.Create(ctx, CreateTableInput{TenantID: record.ID, Name: "parents"})
	if err != nil {
		t.Fatal(err)
	}
	child, err := good.tables.Create(ctx, CreateTableInput{TenantID: record.ID, Name: "children"})
	if err != nil {
		t.Fatal(err)
	}
	empty, err := good.tables.Create(ctx, CreateTableInput{TenantID: record.ID, Name: "removable"})
	if err != nil {
		t.Fatal(err)
	}
	lookup, err := good.fields.Create(ctx, CreateFieldInput{TableID: child.ID, Name: "parent_id", DataType: datamodel.DataTypeString})
	if err != nil {
		t.Fatal(err)
	}
	note, err := good.fields.Create(ctx, CreateFieldInput{TableID: child.ID, Name: "note", DataType: datamodel.DataTypeString})
	if err != nil {
		t.Fatal(err)
	}
	state, err := good.fields.Create(ctx, CreateFieldInput{TableID: child.ID, Name: "state", DataType: datamodel.DataTypeString, IsEnum: true})
	if err != nil {
		t.Fatal(err)
	}
	value, err := good.enums.Create(ctx, CreateFieldEnumValueInput{FieldID: state.ID, Value: "a", Label: "A"})
	if err != nil {
		t.Fatal(err)
	}
	parentFields, err := fr.ListByTable(ctx, parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	childFields, err := fr.ListByTable(ctx, child.ID)
	if err != nil {
		t.Fatal(err)
	}
	parentID := findFieldByName(t, parentFields, "object_id")
	updated := findFieldByName(t, childFields, "updated_at")
	linkInput := CreateLinkInput{TenantID: record.ID, Name: "parent", ParentTable: parent.ID, ParentField: parentID.ID, ChildTable: child.ID, ChildField: lookup.ID}
	link, err := good.links.Create(ctx, linkInput)
	if err != nil {
		t.Fatal(err)
	}
	pivotInput := CreatePivotInput{TenantID: record.ID, BaseTableID: parent.ID, FieldID: &parentID.ID}
	pivot, err := good.pivots.Create(ctx, pivotInput)
	if err != nil {
		t.Fatal(err)
	}
	navInput := CreateNavigationOptionInput{TenantID: record.ID, SourceTableID: parent.ID, SourceFieldID: parentID.ID, TargetTableID: child.ID, FilterFieldID: lookup.ID, OrderingFieldID: updated.ID}
	nav, err := good.navigation.Create(ctx, navInput)
	if err != nil {
		t.Fatal(err)
	}
	// A different ordering column forces a new queue row in the failed create.
	navInput.OrderingFieldID = note.ID
	changed := "changed"
	caption := "note"
	yes := true
	cases := []struct {
		name     string
		revision bool
		run      func(auditServices) error
	}{
		{"tenant_create", true, func(s auditServices) error {
			_, e := s.tenants.Create(ctx, tenant.CreateInput{Name: "new tenant"})
			return e
		}},
		{"tenant_provision", true, func(s auditServices) error { _, e := s.tenants.Provision(ctx, pending.ID); return e }},
		{"table_create", true, func(s auditServices) error {
			_, e := s.tables.Create(ctx, CreateTableInput{TenantID: record.ID, Name: "new_table"})
			return e
		}},
		{"table_update_and_queue", false, func(s auditServices) error {
			_, e := s.tables.Update(ctx, UpdateTableInput{TableID: child.ID, CaptionField: &caption})
			return e
		}},
		{"table_delete", true, func(s auditServices) error { _, e := s.tables.Delete(ctx, empty.ID, false); return e }},
		{"field_create", true, func(s auditServices) error {
			_, e := s.fields.Create(ctx, CreateFieldInput{TableID: child.ID, Name: "new_field", DataType: datamodel.DataTypeString})
			return e
		}},
		{"inline_enum_create", true, func(s auditServices) error {
			_, e := s.fields.Create(ctx, CreateFieldInput{TableID: child.ID, Name: "new_enum", DataType: datamodel.DataTypeString, IsEnum: true, EnumValues: []CreateFieldEnumValueSeed{{Value: "a", Label: "A"}}})
			return e
		}},
		{"field_update", true, func(s auditServices) error {
			_, e := s.fields.Update(ctx, UpdateFieldInput{FieldID: note.ID, IsUnique: &yes})
			return e
		}},
		{"field_delete", true, func(s auditServices) error { _, e := s.fields.Delete(ctx, note.ID, false); return e }},
		{"enum_create", true, func(s auditServices) error {
			_, e := s.enums.Create(ctx, CreateFieldEnumValueInput{FieldID: state.ID, Value: "b", Label: "B"})
			return e
		}},
		{"enum_update", true, func(s auditServices) error {
			_, e := s.enums.Update(ctx, UpdateFieldEnumValueInput{EnumValueID: value.ID, Label: &changed})
			return e
		}},
		{"enum_delete", true, func(s auditServices) error { return s.enums.Delete(ctx, value.ID) }},
		{"link_create", true, func(s auditServices) error {
			in := linkInput
			in.Name = "other_parent"
			_, e := s.links.Create(ctx, in)
			return e
		}},
		{"link_delete", true, func(s auditServices) error { _, e := s.links.Delete(ctx, link.ID, false); return e }},
		{"pivot_create", true, func(s auditServices) error { _, e := s.pivots.Create(ctx, pivotInput); return e }},
		{"pivot_delete", true, func(s auditServices) error { _, e := s.pivots.Delete(ctx, pivot.ID, false); return e }},
		{"options_upsert", true, func(s auditServices) error {
			_, e := s.options.Upsert(ctx, datamodel.TableOptions{TableID: child.ID, DisplayedFields: []uuid.UUID{note.ID}, FieldOrder: []uuid.UUID{note.ID}})
			return e
		}},
		{"navigation_create_and_queue", false, func(s auditServices) error { _, e := s.navigation.Create(ctx, navInput); return e }},
		{"navigation_delete", false, func(s auditServices) error { return s.navigation.Delete(ctx, nav.ID) }},
		{"index_create", false, func(s auditServices) error {
			_, e := s.indexes.Create(ctx, CreateIndexJobInput{TenantID: record.ID, TableID: child.ID, IndexType: datamodel.IndexJobTypeSearch, Columns: []string{"note"}})
			return e
		}},
	}
	snapshot := func() map[string]string {
		t.Helper()
		result := map[string]string{}
		for _, name := range []string{"tenants", "model_tables", "model_fields", "field_enum_values", "model_links", "model_pivots", "table_options", "navigation_options", "index_jobs", "schema_change_log", "tenant_schema_migrations", "ingestion_contracts"} {
			var data string
			if err := pool.QueryRow(ctx, "SELECT COALESCE(jsonb_agg(to_jsonb(r) ORDER BY to_jsonb(r)::text),'[]'::jsonb)::text FROM core."+name+" r").Scan(&data); err != nil {
				t.Fatal(err)
			}
			result[name] = data
		}
		for name, query := range map[string]string{
			"physical_columns": `SELECT COALESCE(jsonb_agg(to_jsonb(r) ORDER BY to_jsonb(r)::text),'[]'::jsonb)::text FROM information_schema.columns r WHERE table_schema LIKE 'tenant_%'`,
			"physical_indexes": `SELECT COALESCE(jsonb_agg(to_jsonb(r) ORDER BY to_jsonb(r)::text),'[]'::jsonb)::text FROM pg_indexes r WHERE schemaname LIKE 'tenant_%'`,
			"schemas":          `SELECT COALESCE(jsonb_agg(nspname ORDER BY nspname),'[]'::jsonb)::text FROM pg_namespace WHERE nspname LIKE 'tenant_%'`,
			"queue":            `SELECT COALESCE(jsonb_agg(to_jsonb(r) ORDER BY id),'[]'::jsonb)::text FROM public.river_job r`,
		} {
			var data string
			if err := pool.QueryRow(ctx, query).Scan(&data); err != nil {
				t.Fatal(err)
			}
			result[name] = data
		}
		return result
	}
	before := snapshot()
	for _, tc := range cases {
		for _, point := range []string{"history", "revision"} {
			if point == "revision" && !tc.revision {
				continue
			}
			t.Run(tc.name+"/"+point, func(t *testing.T) {
				if err := tc.run(services(auditFailureTransactions{tx, point})); !errors.Is(err, errAuditInjected) {
					t.Fatalf("audit failure not propagated: %v", err)
				}
				after := snapshot()
				if !reflect.DeepEqual(before, after) {
					for key, want := range before {
						if after[key] != want {
							t.Errorf("failed mutation changed %s", key)
						}
					}
				}
			})
		}
	}
}
