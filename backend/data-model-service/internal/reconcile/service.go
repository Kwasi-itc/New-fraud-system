package reconcile

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	storepostgres "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/postgres"
)

var implicitPhysicalColumns = map[string]struct{}{
	"id":          {},
	"valid_from":  {},
	"valid_until": {},
}

type Service struct {
	db         *pgxpool.Pool
	tenants    ports.TenantRepository
	tables     ports.TableRepository
	fields     ports.FieldRepository
	introspect Introspector
}

type Introspector interface {
	SchemaExists(ctx context.Context, schemaName string) (bool, error)
	ListTables(ctx context.Context, schemaName string) ([]string, error)
	ListColumns(ctx context.Context, schemaName, tableName string) ([]string, error)
}

type Report struct {
	Healthy bool           `json:"healthy"`
	Tenants []TenantReport `json:"tenants"`
}

type TenantReport struct {
	TenantID          uuid.UUID     `json:"tenant_id"`
	TenantName        string        `json:"tenant_name"`
	SchemaName        string        `json:"schema_name"`
	SchemaExists      bool          `json:"schema_exists"`
	MissingTables     []string      `json:"missing_tables"`
	UnexpectedTables  []string      `json:"unexpected_tables"`
	TableReports      []TableReport `json:"table_reports"`
	SchemaHealthy     bool          `json:"schema_healthy"`
	MetadataOnlyState bool          `json:"metadata_only_state"`
}

type TableReport struct {
	TableName         string   `json:"table_name"`
	PhysicalExists    bool     `json:"physical_exists"`
	MissingColumns    []string `json:"missing_columns"`
	UnexpectedColumns []string `json:"unexpected_columns"`
	Healthy           bool     `json:"healthy"`
}

func NewService(db *pgxpool.Pool) Service {
	return Service{
		db:         db,
		tenants:    storepostgres.NewTenantRepository(db),
		tables:     storepostgres.NewTableRepository(db),
		fields:     storepostgres.NewFieldRepository(db),
		introspect: NewPostgresIntrospector(db),
	}
}

func (s Service) Run(ctx context.Context) (Report, error) {
	tenants, err := s.tenants.List(ctx)
	if err != nil {
		return Report{}, fmt.Errorf("list tenants: %w", err)
	}

	report := Report{
		Healthy: true,
		Tenants: make([]TenantReport, 0, len(tenants)),
	}
	for _, record := range tenants {
		tenantReport, err := s.reconcileTenant(ctx, record)
		if err != nil {
			return Report{}, err
		}
		if !tenantReport.SchemaHealthy {
			report.Healthy = false
		}
		report.Tenants = append(report.Tenants, tenantReport)
	}
	return report, nil
}

func (s Service) reconcileTenant(ctx context.Context, record tenant.Tenant) (TenantReport, error) {
	tables, err := s.tables.ListByTenant(ctx, record.ID)
	if err != nil {
		return TenantReport{}, fmt.Errorf("list tenant tables: %w", err)
	}

	schemaExists, err := s.introspect.SchemaExists(ctx, record.SchemaName)
	if err != nil {
		return TenantReport{}, fmt.Errorf("check schema existence: %w", err)
	}

	result := TenantReport{
		TenantID:          record.ID,
		TenantName:        record.Name,
		SchemaName:        record.SchemaName,
		SchemaExists:      schemaExists,
		MissingTables:     []string{},
		UnexpectedTables:  []string{},
		TableReports:      []TableReport{},
		SchemaHealthy:     true,
		MetadataOnlyState: !schemaExists && len(tables) > 0,
	}
	if !schemaExists {
		if len(tables) > 0 {
			result.SchemaHealthy = false
			for _, table := range tables {
				result.MissingTables = append(result.MissingTables, table.Name)
			}
		}
		return result, nil
	}

	physicalTables, err := s.introspect.ListTables(ctx, record.SchemaName)
	if err != nil {
		return TenantReport{}, fmt.Errorf("list physical tables: %w", err)
	}
	physicalTableSet := makeSet(physicalTables)
	metadataTables := make(map[string]datamodel.Table, len(tables))
	for _, table := range tables {
		metadataTables[table.Name] = table
		if _, ok := physicalTableSet[table.Name]; !ok {
			result.SchemaHealthy = false
			result.MissingTables = append(result.MissingTables, table.Name)
		}
	}
	for _, tableName := range physicalTables {
		if _, ok := metadataTables[tableName]; !ok {
			result.SchemaHealthy = false
			result.UnexpectedTables = append(result.UnexpectedTables, tableName)
		}
	}

	for _, table := range tables {
		tableReport, err := s.reconcileTable(ctx, record.SchemaName, table)
		if err != nil {
			return TenantReport{}, err
		}
		if !tableReport.Healthy {
			result.SchemaHealthy = false
		}
		result.TableReports = append(result.TableReports, tableReport)
	}

	slices.Sort(result.MissingTables)
	slices.Sort(result.UnexpectedTables)
	slices.SortFunc(result.TableReports, func(lhs, rhs TableReport) int {
		if lhs.TableName < rhs.TableName {
			return -1
		}
		if lhs.TableName > rhs.TableName {
			return 1
		}
		return 0
	})

	return result, nil
}

func (s Service) reconcileTable(ctx context.Context, schemaName string, table datamodel.Table) (TableReport, error) {
	fields, err := s.fields.ListByTable(ctx, table.ID)
	if err != nil {
		return TableReport{}, fmt.Errorf("list fields for table %s: %w", table.Name, err)
	}
	columns, err := s.introspect.ListColumns(ctx, schemaName, table.Name)
	if err != nil {
		return TableReport{}, fmt.Errorf("list columns for table %s: %w", table.Name, err)
	}
	if len(columns) == 0 {
		return TableReport{
			TableName:      table.Name,
			PhysicalExists: false,
			MissingColumns: expectedColumns(fields),
			Healthy:        false,
		}, nil
	}

	physicalSet := makeSet(columns)
	expectedSet := make(map[string]struct{}, len(fields)+len(implicitPhysicalColumns))
	for _, field := range fields {
		expectedSet[field.Name] = struct{}{}
	}
	maps.Copy(expectedSet, implicitPhysicalColumns)

	missingColumns := make([]string, 0)
	for column := range expectedSet {
		if _, ok := physicalSet[column]; !ok {
			missingColumns = append(missingColumns, column)
		}
	}

	unexpectedColumns := make([]string, 0)
	for column := range physicalSet {
		if _, ok := expectedSet[column]; !ok {
			unexpectedColumns = append(unexpectedColumns, column)
		}
	}

	slices.Sort(missingColumns)
	slices.Sort(unexpectedColumns)
	return TableReport{
		TableName:         table.Name,
		PhysicalExists:    true,
		MissingColumns:    missingColumns,
		UnexpectedColumns: unexpectedColumns,
		Healthy:           len(missingColumns) == 0 && len(unexpectedColumns) == 0,
	}, nil
}

type PostgresIntrospector struct {
	db *pgxpool.Pool
}

func NewPostgresIntrospector(db *pgxpool.Pool) PostgresIntrospector {
	return PostgresIntrospector{db: db}
}

func (i PostgresIntrospector) SchemaExists(ctx context.Context, schemaName string) (bool, error) {
	var exists bool
	err := i.db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.schemata WHERE schema_name = $1
		)
	`, schemaName).Scan(&exists)
	return exists, err
}

func (i PostgresIntrospector) ListTables(ctx context.Context, schemaName string) ([]string, error) {
	rows, err := i.db.Query(ctx, `
		SELECT table_name
		FROM information_schema.tables
		WHERE table_schema = $1 AND table_type = 'BASE TABLE'
	`, schemaName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var tableName string
		if err := rows.Scan(&tableName); err != nil {
			return nil, err
		}
		tables = append(tables, tableName)
	}
	return tables, rows.Err()
}

func (i PostgresIntrospector) ListColumns(ctx context.Context, schemaName, tableName string) ([]string, error) {
	rows, err := i.db.Query(ctx, `
		SELECT column_name
		FROM information_schema.columns
		WHERE table_schema = $1 AND table_name = $2
		ORDER BY ordinal_position
	`, schemaName, tableName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var columns []string
	for rows.Next() {
		var columnName string
		if err := rows.Scan(&columnName); err != nil {
			return nil, err
		}
		columns = append(columns, columnName)
	}
	return columns, rows.Err()
}

func makeSet(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}

func expectedColumns(fields []datamodel.Field) []string {
	result := make([]string, 0, len(fields)+len(implicitPhysicalColumns))
	for _, field := range fields {
		result = append(result, field.Name)
	}
	for column := range implicitPhysicalColumns {
		result = append(result, column)
	}
	slices.Sort(result)
	return result
}
