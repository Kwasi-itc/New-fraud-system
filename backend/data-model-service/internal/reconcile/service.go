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
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/riverjobs"
	storepostgres "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/postgres"
	tenantdbpostgres "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/tenantdb/postgres"
)

var implicitPhysicalColumns = map[string]struct{}{
	"id":          {},
	"valid_from":  {},
	"valid_until": {},
}

type Service struct {
	tenants           ports.TenantRepository
	tables            ports.TableRepository
	fields            ports.FieldRepository
	navigationOptions ports.NavigationOptionRepository
	schemaManager     ports.SchemaManager
	idGenerator       ports.IDGenerator
	clock             ports.Clock
	introspect        Introspector
	transactions      ports.TransactionManager
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
	TableName             string            `json:"table_name"`
	PhysicalExists        bool              `json:"physical_exists"`
	MissingColumns        []string          `json:"missing_columns"`
	UnexpectedColumns     []string          `json:"unexpected_columns"`
	MissingManagedIndexes []ManagedIndexGap `json:"missing_managed_indexes"`
	RepairJobIDs          []uuid.UUID       `json:"repair_job_ids"`
	Healthy               bool              `json:"healthy"`
}

type ManagedIndexGap struct {
	IndexName string   `json:"index_name"`
	IndexType string   `json:"index_type"`
	Columns   []string `json:"columns"`
}

func NewService(db *pgxpool.Pool, enqueuer riverjobs.IndexJobEnqueuer) (Service, error) {
	if db == nil {
		return Service{}, fmt.Errorf("database is required for reconciliation")
	}
	if enqueuer == nil {
		riverClient, err := river.NewClient(riverpgxv5.New(db), &river.Config{})
		if err != nil {
			return Service{}, fmt.Errorf("configure repair queue: %w", err)
		}
		enqueuer = riverjobs.NewRiverIndexJobEnqueuer(riverClient, 10, riverjobs.IndexJobQueueName)
	}
	return Service{
		tenants:           storepostgres.NewTenantRepository(db),
		tables:            storepostgres.NewTableRepository(db),
		fields:            storepostgres.NewFieldRepository(db),
		navigationOptions: storepostgres.NewNavigationOptionRepository(db),
		schemaManager:     tenantdbpostgres.NewSchemaManager(db),
		idGenerator:       reconcileUUIDGenerator{},
		clock:             reconcileSystemClock{},
		introspect:        NewPostgresIntrospector(db),
		transactions:      storepostgres.NewTransactionManager(db).WithIndexJobEnqueuer(enqueuer),
	}, nil
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
	expectedManagedIndexes := expectedManagedIndexesByTable(tables, navigationOptions)

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
			repairJobIDs, err := s.scheduleRepairJobs(ctx, record, table, tableReport.MissingManagedIndexes)
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
			IndexType: datamodel.IndexJobType(gap.IndexType),
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

// Each gap is an independent, short transaction. Inspection/DDL stays outside.
func (s Service) scheduleRepairJobs(ctx context.Context, record tenant.Tenant, table datamodel.Table, gaps []ManagedIndexGap) ([]uuid.UUID, error) {
	if len(gaps) > 0 && s.transactions == nil {
		return nil, fmt.Errorf("repair transaction manager is required")
	}
	scheduled := make([]uuid.UUID, 0, len(gaps))
	for _, gap := range gaps {
		var scheduledID uuid.UUID
		err := s.transactions.Run(ctx, func(store ports.MutationStore) error {
			now := s.clock.Now()
			key := datamodel.BuildIndexJobDedupeKey(record.ID, table.ID, datamodel.IndexJobTypeRepair, gap.Columns)
			existing, err := store.IndexJobs().GetByDedupeKeyForUpdate(ctx, key)
			if err != nil {
				return fmt.Errorf("lock repair job: %w", err)
			}
			if existing != nil {
				if existing.Status == datamodel.IndexJobStatusPending || existing.Status == datamodel.IndexJobStatusRunning {
					return nil
				}
				scheduledID = existing.ID
				if err := store.IndexJobs().Retry(ctx, existing.ID, now); err != nil {
					return fmt.Errorf("requeue repair job: %w", err)
				}
			} else {
				scheduledID = s.idGenerator.New()
				job := datamodel.IndexJob{ID: scheduledID, TenantID: record.ID, TableID: &table.ID, TableName: table.Name, IndexType: datamodel.IndexJobTypeRepair, Columns: gap.Columns, Status: datamodel.IndexJobStatusPending, RequestedByOperation: "reconcile_repair", RequestedAt: now, ScheduledAt: &now, DedupeKey: key}
				// Concurrent creation is resolved by the existing exact unique key. A
				// losing transaction fails explicitly; a later reconciliation reads it.
				if err := store.IndexJobs().Create(ctx, job); err != nil {
					return fmt.Errorf("create repair job: %w", err)
				}
			}
			if err := s.recordSchemaChange(ctx, store.SchemaChanges(), record.ID, scheduledID, now, table, gap.Columns); err != nil {
				return err
			}
			if err := store.EnqueueIndexJob(ctx, scheduledID, &now); err != nil {
				return fmt.Errorf("enqueue repair job: %w", err)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		if scheduledID != uuid.Nil {
			scheduled = append(scheduled, scheduledID)
		}
	}
	return scheduled, nil
}

func (s Service) recordSchemaChange(
	ctx context.Context,
	history ports.SchemaChangeRepository,
	tenantID uuid.UUID,
	jobID uuid.UUID,
	createdAt time.Time,
	table datamodel.Table,
	columns []string,
) error {
	payload, err := json.Marshal(map[string]any{
		"table_id":     table.ID,
		"table_name":   table.Name,
		"index_type":   datamodel.IndexJobTypeRepair,
		"columns":      columns,
		"reason":       "missing_managed_index",
		"scheduled_by": "reconcile",
	})
	if err != nil {
		return fmt.Errorf("encode repair history: %w", err)
	}
	return history.Create(ctx, datamodel.SchemaChange{
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

func expectedManagedIndexesByTable(tables []datamodel.Table, options []datamodel.NavigationOption) map[uuid.UUID][]ManagedIndexGap {
	grouped := make(map[uuid.UUID][]ManagedIndexGap)
	seen := make(map[string]struct{})
	for _, table := range tables {
		if table.CaptionField == "" {
			continue
		}
		columns := []string{datamodel.NormalizeName(table.CaptionField)}
		key := table.ID.String() + ":" + string(datamodel.IndexJobTypeSearch) + ":" + strings.Join(columns, ",")
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		grouped[table.ID] = append(grouped[table.ID], ManagedIndexGap{
			IndexType: string(datamodel.IndexJobTypeSearch),
			Columns:   columns,
		})
	}
	for _, option := range options {
		columns := []string{
			datamodel.NormalizeName(option.FilterFieldName),
			datamodel.NormalizeName(option.OrderingFieldName),
		}
		key := option.TargetTableID.String() + ":" + string(datamodel.IndexJobTypeNavigation) + ":" + strings.Join(columns, ",")
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
