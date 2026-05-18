package reconcile

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
	storepostgres "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/postgres"
)

func TestExpectedColumnsIncludeImplicitPhysicalColumns(t *testing.T) {
	t.Parallel()

	fields := []datamodel.Field{{Name: "object_id"}, {Name: "updated_at"}, {Name: "email"}}
	columns := expectedColumns(fields)

	expected := map[string]bool{
		"id":          true,
		"valid_from":  true,
		"valid_until": true,
		"object_id":   true,
		"updated_at":  true,
		"email":       true,
	}
	for _, column := range columns {
		delete(expected, column)
	}
	if len(expected) != 0 {
		t.Fatalf("missing expected columns: %v", expected)
	}
}

func TestReconcileTenantFlagsMissingPhysicalSchema(t *testing.T) {
	t.Parallel()

	record := tenant.Tenant{ID: uuid.New(), Name: "Tenant A", SchemaName: "tenant_a"}
	service := Service{
		tenants: stubTenantRepository{},
		tables: stubTableRepository{
			tables: []datamodel.Table{{ID: uuid.New(), TenantID: record.ID, Name: "cases"}},
		},
		fields:            stubFieldRepository{},
		navigationOptions: stubNavigationOptionRepository{},
		indexJobs:         &stubIndexJobRepository{},
		schemaChanges:     stubSchemaChangeRepository{},
		schemaManager:     stubSchemaManager{},
		idGenerator:       stubIDGenerator{value: uuid.New()},
		clock:             stubClock{},
		introspect:        stubIntrospector{schemaExists: false},
	}

	report, err := service.reconcileTenant(context.Background(), record)
	if err != nil {
		t.Fatalf("reconcile tenant: %v", err)
	}
	if report.SchemaHealthy {
		t.Fatal("expected schema to be unhealthy when metadata exists but schema is missing")
	}
	if len(report.MissingTables) != 1 || report.MissingTables[0] != "cases" {
		t.Fatalf("unexpected missing tables: %v", report.MissingTables)
	}
}

func TestReconcileTenantSchedulesRepairJobsForMissingManagedIndexes(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	tableID := uuid.New()
	record := tenant.Tenant{ID: tenantID, Name: "Tenant A", SchemaName: "tenant_a"}
	indexJobs := &stubIndexJobRepository{}
	service := Service{
		tenants: stubTenantRepository{},
		tables: stubTableRepository{
			tables: []datamodel.Table{{ID: tableID, TenantID: record.ID, Name: "cases"}},
		},
		fields: stubFieldRepository{
			fields: []datamodel.Field{
				{ID: uuid.New(), TableID: tableID, Name: "status"},
				{ID: uuid.New(), TableID: tableID, Name: "updated_at"},
			},
		},
		navigationOptions: stubNavigationOptionRepository{
			options: []datamodel.NavigationOption{{
				ID:                uuid.New(),
				TenantID:          tenantID,
				TargetTableID:     tableID,
				FilterFieldName:   "status",
				OrderingFieldName: "updated_at",
			}},
		},
		indexJobs:         indexJobs,
		schemaChanges:     stubSchemaChangeRepository{},
		schemaManager:     stubSchemaManager{managedIndexExists: false},
		idGenerator:       stubIDGenerator{value: uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")},
		clock:             fixedTestClock{now: time.Date(2026, 5, 18, 13, 0, 0, 0, time.UTC)},
		introspect: stubIntrospector{
			schemaExists: true,
			tables:       []string{"cases"},
			columns: map[string][]string{
				"cases": {"id", "valid_from", "valid_until", "status", "updated_at"},
			},
		},
	}

	report, err := service.reconcileTenant(context.Background(), record)
	if err != nil {
		t.Fatalf("reconcile tenant: %v", err)
	}
	if report.SchemaHealthy {
		t.Fatal("expected schema to be unhealthy when managed indexes are missing")
	}
	if report.RepairJobsScheduled != 1 {
		t.Fatalf("expected 1 repair job scheduled, got %d", report.RepairJobsScheduled)
	}
	if len(indexJobs.created) != 1 || indexJobs.created[0].IndexType != datamodel.IndexJobTypeRepair {
		t.Fatalf("unexpected created jobs: %+v", indexJobs.created)
	}
}

type stubIntrospector struct {
	schemaExists bool
	tables       []string
	columns      map[string][]string
}

func (s stubIntrospector) SchemaExists(context.Context, string) (bool, error) {
	return s.schemaExists, nil
}
func (s stubIntrospector) ListTables(context.Context, string) ([]string, error) {
	return s.tables, nil
}
func (s stubIntrospector) ListColumns(_ context.Context, _ string, tableName string) ([]string, error) {
	return s.columns[tableName], nil
}

type stubTenantRepository struct{}

func (stubTenantRepository) Create(context.Context, tenant.Tenant) error { return nil }
func (stubTenantRepository) GetByID(context.Context, uuid.UUID) (tenant.Tenant, error) {
	return tenant.Tenant{}, nil
}
func (stubTenantRepository) List(context.Context) ([]tenant.Tenant, error) { return nil, nil }
func (stubTenantRepository) UpdateStatus(context.Context, uuid.UUID, tenant.Status) error {
	return nil
}

type stubTableRepository struct {
	tables []datamodel.Table
}

func (s stubTableRepository) Create(context.Context, datamodel.Table) error { return nil }
func (s stubTableRepository) GetByID(context.Context, uuid.UUID) (datamodel.Table, error) {
	return datamodel.Table{}, nil
}
func (s stubTableRepository) ListByTenant(context.Context, uuid.UUID) ([]datamodel.Table, error) {
	return s.tables, nil
}
func (s stubTableRepository) Update(context.Context, datamodel.Table) error { return nil }
func (s stubTableRepository) Delete(context.Context, uuid.UUID) error       { return nil }

type stubFieldRepository struct {
	fields []datamodel.Field
}

func (stubFieldRepository) Create(context.Context, datamodel.Field) error { return nil }
func (stubFieldRepository) GetByID(context.Context, uuid.UUID) (datamodel.Field, error) {
	return datamodel.Field{}, nil
}
func (s stubFieldRepository) ListByTable(context.Context, uuid.UUID) ([]datamodel.Field, error) {
	return s.fields, nil
}
func (stubFieldRepository) Delete(context.Context, uuid.UUID) error       { return nil }
func (stubFieldRepository) Update(context.Context, datamodel.Field) error { return nil }

type stubNavigationOptionRepository struct {
	options []datamodel.NavigationOption
}

func (stubNavigationOptionRepository) Create(context.Context, datamodel.NavigationOption) error { return nil }
func (stubNavigationOptionRepository) GetByID(context.Context, uuid.UUID) (datamodel.NavigationOption, error) {
	return datamodel.NavigationOption{}, nil
}
func (s stubNavigationOptionRepository) ListByTenant(context.Context, uuid.UUID) ([]datamodel.NavigationOption, error) {
	return s.options, nil
}
func (stubNavigationOptionRepository) ListBySourceTable(context.Context, uuid.UUID) ([]datamodel.NavigationOption, error) {
	return nil, nil
}
func (stubNavigationOptionRepository) Delete(context.Context, uuid.UUID) error { return nil }

type stubIndexJobRepository struct {
	created []datamodel.IndexJob
}

func (s *stubIndexJobRepository) Create(_ context.Context, job datamodel.IndexJob) error {
	s.created = append(s.created, job)
	return nil
}
func (*stubIndexJobRepository) GetByID(context.Context, uuid.UUID) (datamodel.IndexJob, error) {
	return datamodel.IndexJob{}, nil
}
func (*stubIndexJobRepository) ListByTenant(context.Context, uuid.UUID) ([]datamodel.IndexJob, error) {
	return nil, nil
}
func (*stubIndexJobRepository) ClaimNext(context.Context, time.Time, int) (*datamodel.IndexJob, error) {
	return nil, nil
}
func (*stubIndexJobRepository) MarkApplied(context.Context, uuid.UUID, time.Time) error { return nil }
func (*stubIndexJobRepository) MarkFailed(context.Context, uuid.UUID, string, time.Time) error {
	return nil
}
func (*stubIndexJobRepository) Reschedule(context.Context, uuid.UUID, string, time.Time) error {
	return nil
}
func (*stubIndexJobRepository) Retry(context.Context, uuid.UUID, time.Time) error { return nil }

type stubSchemaChangeRepository struct{}

func (stubSchemaChangeRepository) Create(context.Context, datamodel.SchemaChange) error { return nil }
func (stubSchemaChangeRepository) ListByTenant(context.Context, uuid.UUID) ([]datamodel.SchemaChange, error) {
	return nil, nil
}

type stubSchemaManager struct{
	managedIndexExists bool
}

func (stubSchemaManager) ProvisionTenantSchema(context.Context, tenant.Tenant) error { return nil }
func (stubSchemaManager) CreateTable(context.Context, tenant.Tenant, datamodel.Table) error {
	return nil
}
func (stubSchemaManager) DropTable(context.Context, tenant.Tenant, datamodel.Table) error { return nil }
func (stubSchemaManager) AddField(context.Context, tenant.Tenant, datamodel.Table, datamodel.Field) error {
	return nil
}
func (stubSchemaManager) DropField(context.Context, tenant.Tenant, datamodel.Table, datamodel.Field) error {
	return nil
}
func (stubSchemaManager) ArchiveField(context.Context, tenant.Tenant, datamodel.Table, datamodel.Field) error {
	return nil
}
func (stubSchemaManager) CreateUniqueIndex(context.Context, tenant.Tenant, datamodel.Table, []string) error {
	return nil
}
func (stubSchemaManager) DropUniqueIndex(context.Context, tenant.Tenant, datamodel.Table, []string) error {
	return nil
}
func (stubSchemaManager) CreateManagedIndex(context.Context, tenant.Tenant, datamodel.Table, datamodel.IndexJob) error {
	return nil
}
func (s stubSchemaManager) GetManagedIndexState(context.Context, tenant.Tenant, datamodel.Table, datamodel.IndexJob) (datamodel.ManagedIndexState, error) {
	return datamodel.ManagedIndexState{Name: "idx_cases_deadbeef", Exists: s.managedIndexExists}, nil
}

type stubIDGenerator struct {
	value uuid.UUID
}

func (s stubIDGenerator) New() uuid.UUID { return s.value }

type stubClock struct{}

func (stubClock) Now() time.Time { return time.Now().UTC() }

type fixedTestClock struct {
	now time.Time
}

func (c fixedTestClock) Now() time.Time { return c.now }

var _ = storepostgres.NewTenantRepository
