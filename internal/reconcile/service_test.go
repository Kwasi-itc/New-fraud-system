package reconcile

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/Kwasi-itc/marble-datamodel-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/marble-datamodel-service/internal/domain/tenant"
	storepostgres "github.com/Kwasi-itc/marble-datamodel-service/internal/store/postgres"
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
		fields:     stubFieldRepository{},
		introspect: stubIntrospector{schemaExists: false},
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

type stubFieldRepository struct{}

func (stubFieldRepository) Create(context.Context, datamodel.Field) error { return nil }
func (stubFieldRepository) GetByID(context.Context, uuid.UUID) (datamodel.Field, error) {
	return datamodel.Field{}, nil
}
func (stubFieldRepository) ListByTable(context.Context, uuid.UUID) ([]datamodel.Field, error) {
	return nil, nil
}
func (stubFieldRepository) Delete(context.Context, uuid.UUID) error       { return nil }
func (stubFieldRepository) Update(context.Context, datamodel.Field) error { return nil }

var _ = storepostgres.NewTenantRepository
