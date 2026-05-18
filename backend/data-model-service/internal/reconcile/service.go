package reconcile

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	storepostgres "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/postgres"
	tenantdbpostgres "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/tenantdb/postgres"
)

var implicitPhysicalColumns = map[string]struct{}{
	"id":          {},
	"valid_from":  {},
	"valid_until": {},
}

type Service struct {
	db                *pgxpool.Pool
	tenants           ports.TenantRepository
	tables            ports.TableRepository
	fields            ports.FieldRepository
	navigationOptions ports.NavigationOptionRepository
	indexJobs         ports.IndexJobRepository
	schemaChanges     ports.SchemaChangeRepository
	schemaManager     ports.SchemaManager
	idGenerator       ports.IDGenerator
	clock             ports.Clock
	introspect        Introspector
}

type Introspector interface {
	SchemaExists(ctx context.Context, schemaName string) (bool, error)
	ListTables(ctx context.Context, schemaName string) ([]string, error)
	ListColumns(ctx context.Context, schemaName, tableName string) ([]string, error)
}

type Report struct {
	Healthy             bool           `json:"healthy"`
	RepairJobsScheduled int            `json:"repair_jobs_scheduled"`
	Tenants             []TenantReport `json:"tenants"`
}

type TenantReport struct {
	TenantID            uuid.UUID     `json:"tenant_id"`
	TenantName          string        `json:"tenant_name"`
	SchemaName          string        `json:"schema_name"`
	SchemaExists        bool          `json:"schema_exists"`
	MissingTables       []string      `json:"missing_tables"`
	UnexpectedTables    []string      `json:"unexpected_tables"`
	TableReports        []TableReport `json:"table_reports"`
	SchemaHealthy       bool          `json:"schema_healthy"`
	MetadataOnlyState   bool          `json:"metadata_only_state"`
	RepairJobsScheduled int           `json:"repair_jobs_scheduled"`
}

type TableReport struct {
	TableName             string             `json:"table_name"`
	PhysicalExists        bool               `json:"physical_exists"`
	MissingColumns        []string           `json:"missing_columns"`
	UnexpectedColumns     []string           `json:"unexpected_columns"`
	MissingManagedIndexes []ManagedIndexGap  `json:"missing_managed_indexes"`
	RepairJobIDs          []uuid.UUID        `json:"repair_job_ids"`
	Healthy               bool               `json:"healthy"`
}

type ManagedIndexGap struct {
	IndexName string   `json:"index_name"`
	IndexType string   `json:"index_type"`
	Columns   []string `json:"columns"`
}

func NewService(db *pgxpool.Pool) Service {
	return Service{
		db:                db,
		tenants:           storepostgres.NewTenantRepository(db),
		tables:            storepostgres.NewTableRepository(db),
		fields:            storepostgres.NewFieldRepository(db),
		navigationOptions: storepostgres.NewNavigationOptionRepository(db),
		indexJobs:         storepostgres.NewIndexJobRepository(db),
		schemaChanges:     storepostgres.NewSchemaChangeRepository(db),
		schemaManager:     tenantdbpostgres.NewSchemaManager(db),
		idGenerator:       reconcileUUIDGenerator{},
		clock:             reconcileSystemClock{},
		introspect:        NewPostgresIntrospector(db),
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
		report.RepairJobsScheduled += tenantReport.RepairJobsScheduled
		report.Tenants = append(report.Tenants, tenantReport)
	}
	return report, nil
}

func (s Service) reconcileTenant(ctx context.Context, record tenant.Tenant) (TenantReport, error) {
	tables, err := s.tables.ListByTenant(ctx, record.ID)
	if err != nil {
		return TenantReport{}, fmt.Errorf("list tenant tables: %w", err)
	}
	navigationOptions, err := s.navigationOptions.ListByTenant(ctx, record.ID)
	if err != nil {
		return TenantReport{}, fmt.Errorf("list tenant navigation options: %w", err)
	}
	indexJobs, err := s.indexJobs.ListByTenant(ctx, record.ID)
	if err != nil {
		return TenantReport{}, fmt.Errorf("list tenant index jobs: %w", err)
	}
	expectedManagedIndexes := expectedManagedIndexesByTable(navigationOptions)

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
		tableReport, err := s.reconcileTable(ctx, record, table, expectedManagedIndexes[table.ID])
		if err != nil {
			return TenantReport{}, err
		}
		if tableReport.PhysicalExists && len(tableReport.MissingManagedIndexes) > 0 {
			repairJobIDs, err := s.scheduleRepairJobs(ctx, record, table, tableReport.MissingManagedIndexes, indexJobs)
			if err != nil {
				return TenantReport{}, err
			}
			tableReport.RepairJobIDs = repairJobIDs
			result.RepairJobsScheduled += len(repairJobIDs)
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

func (s Service) reconcileTable(
	ctx context.Context,
	record tenant.Tenant,
	table datamodel.Table,
	expectedManagedIndexes []ManagedIndexGap,
) (TableReport, error) {
	fields, err := s.fields.ListByTable(ctx, table.ID)
	if err != nil {
		return TableReport{}, fmt.Errorf("list fields for table %s: %w", table.Name, err)
	}
	columns, err := s.introspect.ListColumns(ctx, record.SchemaName, table.Name)
	if err != nil {
		return TableReport{}, fmt.Errorf("list columns for table %s: %w", table.Name, err)
	}
	if len(columns) == 0 {
		return TableReport{
			TableName:             table.Name,
			PhysicalExists:        false,
			MissingColumns:        expectedColumns(fields),
			MissingManagedIndexes: []ManagedIndexGap{},
			RepairJobIDs:          []uuid.UUID{},
			Healthy:               false,
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

	missingManagedIndexes := make([]ManagedIndexGap, 0, len(expectedManagedIndexes))
	for _, gap := range expectedManagedIndexes {
		state, err := s.schemaManager.GetManagedIndexState(ctx, record, table, datamodel.IndexJob{
			IndexType: datamodel.IndexJobTypeRepair,
			Columns:   gap.Columns,
		})
		if err != nil {
			return TableReport{}, fmt.Errorf("inspect managed index for table %s: %w", table.Name, err)
		}
		if !state.Exists {
			missingManagedIndexes = append(missingManagedIndexes, ManagedIndexGap{
				IndexName: state.Name,
				IndexType: gap.IndexType,
				Columns:   gap.Columns,
			})
		}
	}

	return TableReport{
		TableName:             table.Name,
		PhysicalExists:        true,
		MissingColumns:        missingColumns,
		UnexpectedColumns:     unexpectedColumns,
		MissingManagedIndexes: missingManagedIndexes,
		RepairJobIDs:          []uuid.UUID{},
		Healthy:               len(missingColumns) == 0 && len(unexpectedColumns) == 0 && len(missingManagedIndexes) == 0,
	}, nil
}

func (s Service) scheduleRepairJobs(
	ctx context.Context,
	record tenant.Tenant,
	table datamodel.Table,
	gaps []ManagedIndexGap,
	indexJobs []datamodel.IndexJob,
) ([]uuid.UUID, error) {
	scheduled := make([]uuid.UUID, 0, len(gaps))
	now := s.clock.Now()
	for _, gap := range gaps {
		dedupeKey := datamodel.BuildIndexJobDedupeKey(record.ID, table.ID, datamodel.IndexJobTypeRepair, gap.Columns)
		existingIndex := slices.IndexFunc(indexJobs, func(job datamodel.IndexJob) bool {
			return job.DedupeKey == dedupeKey
		})
		if existingIndex >= 0 {
			existing := indexJobs[existingIndex]
			if existing.Status == datamodel.IndexJobStatusPending || existing.Status == datamodel.IndexJobStatusRunning {
				continue
			}
			if err := s.indexJobs.Retry(ctx, existing.ID, now); err != nil {
				return nil, fmt.Errorf("requeue repair job: %w", err)
			}
			scheduled = append(scheduled, existing.ID)
			indexJobs[existingIndex].Status = datamodel.IndexJobStatusPending
			indexJobs[existingIndex].ScheduledAt = &now
			s.recordSchemaChange(ctx, record.ID, existing.ID, now, table, gap.Columns)
			continue
		}

		jobID := s.idGenerator.New()
		job := datamodel.IndexJob{
			ID:                   jobID,
			TenantID:             record.ID,
			TableID:              &table.ID,
			TableName:            table.Name,
			IndexType:            datamodel.IndexJobTypeRepair,
			Columns:              gap.Columns,
			Status:               datamodel.IndexJobStatusPending,
			RequestedByOperation: "reconcile_repair",
			RequestedAt:          now,
			ScheduledAt:          &now,
			DedupeKey:            dedupeKey,
		}
		if err := s.indexJobs.Create(ctx, job); err != nil {
			return nil, fmt.Errorf("create repair job: %w", err)
		}
		scheduled = append(scheduled, jobID)
		indexJobs = append(indexJobs, job)
		s.recordSchemaChange(ctx, record.ID, jobID, now, table, gap.Columns)
	}
	return scheduled, nil
}

func (s Service) recordSchemaChange(
	ctx context.Context,
	tenantID uuid.UUID,
	jobID uuid.UUID,
	createdAt time.Time,
	table datamodel.Table,
	columns []string,
) {
	payload, err := json.Marshal(map[string]any{
		"table_id":     table.ID,
		"table_name":   table.Name,
		"index_type":   datamodel.IndexJobTypeRepair,
		"columns":      columns,
		"reason":       "missing_managed_index",
		"scheduled_by": "reconcile",
	})
	if err != nil {
		payload = []byte(`{}`)
	}
	_ = s.schemaChanges.Create(ctx, datamodel.SchemaChange{
		ID:           s.idGenerator.New(),
		TenantID:     tenantID,
		Operation:    "request_index_job",
		ResourceType: "index_job",
		ResourceID:   jobID,
		Status:       "applied",
		Details:      payload,
		CreatedAt:    createdAt,
	})
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

func expectedManagedIndexesByTable(options []datamodel.NavigationOption) map[uuid.UUID][]ManagedIndexGap {
	grouped := make(map[uuid.UUID][]ManagedIndexGap)
	seen := make(map[string]struct{})
	for _, option := range options {
		columns := []string{
			datamodel.NormalizeName(option.FilterFieldName),
			datamodel.NormalizeName(option.OrderingFieldName),
		}
		key := option.TargetTableID.String() + ":" + strings.Join(columns, ",")
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		grouped[option.TargetTableID] = append(grouped[option.TargetTableID], ManagedIndexGap{
			IndexType: string(datamodel.IndexJobTypeNavigation),
			Columns:   columns,
		})
	}
	return grouped
}

type reconcileUUIDGenerator struct{}

func (reconcileUUIDGenerator) New() uuid.UUID {
	return uuid.New()
}

type reconcileSystemClock struct{}

func (reconcileSystemClock) Now() time.Time {
	return time.Now().UTC()
}
