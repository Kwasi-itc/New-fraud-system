package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
)

func TestFieldServiceRejectsProjectionOnOperationalTable(t *testing.T) {
	t.Parallel()
	tableID := uuid.New()
	service := NewFieldService(
		stubTenantRepository{},
		stubTableRepository{table: datamodel.Table{ID: tableID, StorageClass: datamodel.StorageClassOperational}},
		stubFieldRepository{},
		nil,
		stubLinkRepository{},
		&stubPivotRepository{},
		&stubSchemaChangeRepository{},
		nil,
		stubTransactionManager{},
		stubIDGenerator{value: uuid.New()},
		stubClock{now: time.Now().UTC()},
	)

	_, err := service.Create(context.Background(), CreateFieldInput{
		TableID: tableID, Name: "account_ref", DataType: datamodel.DataTypeString, IsProjection: true,
	})
	if err == nil || !strings.Contains(err.Error(), "only valid for event table fields") {
		t.Fatalf("Create() error = %v, want operational projection rejection", err)
	}
}

func TestFieldServiceRequiresProjectionForAcceleratedAggregation(t *testing.T) {
	t.Parallel()
	tableID := uuid.New()
	fieldService := NewFieldService(
		stubTenantRepository{},
		stubTableRepository{table: datamodel.Table{ID: tableID, StorageClass: datamodel.StorageClassEvent}},
		stubFieldRepository{}, nil, stubLinkRepository{}, &stubPivotRepository{},
		&stubSchemaChangeRepository{}, nil, stubTransactionManager{},
		stubIDGenerator{value: uuid.New()}, stubClock{now: time.Now().UTC()},
	)

	_, err := fieldService.Create(context.Background(), CreateFieldInput{
		TableID: tableID, Name: "account_ref", DataType: datamodel.DataTypeString,
		AggregationMode: datamodel.AggregationModeAdaptiveCache,
	})
	if err == nil || !strings.Contains(err.Error(), "require is_projection=true") {
		t.Fatalf("Create() error = %v, want projection requirement", err)
	}
}

func TestFieldServiceValidatesColdDefault(t *testing.T) {
	t.Parallel()
	tableID := uuid.New()
	fieldService := NewFieldService(
		stubTenantRepository{},
		stubTableRepository{table: datamodel.Table{ID: tableID, StorageClass: datamodel.StorageClassEvent}},
		stubFieldRepository{}, nil, stubLinkRepository{}, &stubPivotRepository{},
		&stubSchemaChangeRepository{}, nil, stubTransactionManager{},
		stubIDGenerator{value: uuid.New()}, stubClock{now: time.Now().UTC()},
	)

	_, err := fieldService.Create(context.Background(), CreateFieldInput{
		TableID: tableID, Name: "account_ref", DataType: datamodel.DataTypeString, IsProjection: true,
		AggregationMode:         datamodel.AggregationModeAdaptiveCache,
		AggregationColdBehavior: datamodel.AggregationColdUseDefault,
	})
	if err == nil || !strings.Contains(err.Error(), "aggregation_default_value is required") {
		t.Fatalf("Create() error = %v, want cold default requirement", err)
	}
}

func TestTableServiceCreatesLogicalEventTableWithoutPostgresDDL(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	tenantID := uuid.New()
	tableRepo := stubTableRepository{}
	fieldRepo := stubFieldRepository{}
	changes := &stubSchemaChangeRepository{}
	service := NewTableService(
		stubTenantRepository{record: tenant.Tenant{ID: tenantID, Status: tenant.StatusActive}},
		tableRepo,
		fieldRepo,
		stubLinkRepository{},
		&stubPivotRepository{},
		changes,
		nil,
		stubTransactionManager{store: stubMutationStore{tables: tableRepo, fields: fieldRepo, schemaChanges: changes}},
		stubIDGenerator{value: uuid.New()},
		stubClock{now: now},
	)

	table, err := service.Create(context.Background(), CreateTableInput{
		TenantID:       tenantID,
		Name:           "transactions",
		StorageClass:   "event",
		EventTimeField: "date",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if table.StorageClass != datamodel.StorageClassEvent || table.EventTimeField != "date" {
		t.Fatalf("event table metadata = %#v", table)
	}
	if table.StorageCutoverAt == nil || table.LegacyReadUntil == nil || !table.StorageCutoverAt.Equal(*table.LegacyReadUntil) {
		t.Fatalf("new event table should have a completed legacy bridge: %#v", table)
	}
}

func TestTableServiceConvertsOperationalTableToEventStorage(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 20, 11, 0, 0, 0, time.UTC)
	tenantID := uuid.New()
	tableID := uuid.New()
	tableRepo := stubTableRepository{table: datamodel.Table{
		ID:           tableID,
		TenantID:     tenantID,
		Name:         "transactions",
		StorageClass: datamodel.StorageClassOperational,
	}}
	fieldRepo := stubFieldRepository{fields: []datamodel.Field{{
		ID:       uuid.New(),
		TenantID: tenantID,
		TableID:  tableID,
		Name:     "date",
		DataType: datamodel.DataTypeTimestamp,
	}}}
	changes := &stubSchemaChangeRepository{}
	service := NewTableService(
		stubTenantRepository{},
		tableRepo,
		fieldRepo,
		stubLinkRepository{},
		&stubPivotRepository{},
		changes,
		nil,
		stubTransactionManager{store: stubMutationStore{tables: tableRepo, fields: fieldRepo, schemaChanges: changes}},
		stubIDGenerator{value: uuid.New()},
		stubClock{now: now},
	)
	storageClass := "event"
	eventTimeField := "date"
	table, err := service.Update(context.Background(), UpdateTableInput{
		TableID:        tableID,
		StorageClass:   &storageClass,
		EventTimeField: &eventTimeField,
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if table.StorageClass != datamodel.StorageClassEvent || table.EventTimeField != eventTimeField {
		t.Fatalf("event table metadata = %#v", table)
	}
	if table.StorageCutoverAt == nil || !table.StorageCutoverAt.Equal(now) {
		t.Fatalf("storage cutover = %v, want %v", table.StorageCutoverAt, now)
	}
	if table.LegacyReadUntil == nil || !table.LegacyReadUntil.Equal(now.Add(30*24*time.Hour)) {
		t.Fatalf("legacy read deadline = %v", table.LegacyReadUntil)
	}
}

func TestFieldServiceDoesNotAllowNullableEventTimeField(t *testing.T) {
	t.Parallel()
	tenantID := uuid.New()
	tableID := uuid.New()
	fieldID := uuid.New()
	fieldRepo := fieldRepositoryByTableAndID{byID: map[uuid.UUID]datamodel.Field{
		fieldID: {
			ID:       fieldID,
			TenantID: tenantID,
			TableID:  tableID,
			Name:     "date",
			DataType: datamodel.DataTypeTimestamp,
		},
	}}
	service := NewFieldService(
		stubTenantRepository{},
		tableRepositoryByID{tables: map[uuid.UUID]datamodel.Table{
			tableID: {
				ID:             tableID,
				TenantID:       tenantID,
				Name:           "transactions",
				StorageClass:   datamodel.StorageClassEvent,
				EventTimeField: "date",
			},
		}},
		fieldRepo,
		nil,
		stubLinkRepository{},
		&stubPivotRepository{},
		&stubSchemaChangeRepository{},
		nil,
		stubTransactionManager{},
		stubIDGenerator{value: uuid.New()},
		stubClock{now: time.Now().UTC()},
	)
	nullable := true
	if _, err := service.Update(context.Background(), UpdateFieldInput{FieldID: fieldID, Nullable: &nullable}); err == nil {
		t.Fatal("expected nullable event-time field to be rejected")
	}
}

func TestTableServiceDoesNotDeleteAppendOnlyEventTable(t *testing.T) {
	t.Parallel()
	tableID := uuid.New()
	service := NewTableService(
		stubTenantRepository{},
		stubTableRepository{table: datamodel.Table{ID: tableID, StorageClass: datamodel.StorageClassEvent}},
		stubFieldRepository{},
		stubLinkRepository{},
		&stubPivotRepository{},
		&stubSchemaChangeRepository{},
		nil,
		stubTransactionManager{},
		stubIDGenerator{value: uuid.New()},
		stubClock{now: time.Now().UTC()},
	)
	if _, err := service.Delete(context.Background(), tableID, false); err == nil {
		t.Fatal("expected append-only event table deletion to be rejected")
	}
}

type stubNavigationOptionRepository struct {
	created []datamodel.NavigationOption
	option  datamodel.NavigationOption
	err     error
}

func (s *stubNavigationOptionRepository) Create(_ context.Context, option datamodel.NavigationOption) error {
	s.created = append(s.created, option)
	s.option = option
	return s.err
}

func (s *stubNavigationOptionRepository) GetByID(context.Context, uuid.UUID) (datamodel.NavigationOption, error) {
	return s.option, s.err
}

func (s *stubNavigationOptionRepository) ListByTenant(context.Context, uuid.UUID) ([]datamodel.NavigationOption, error) {
	return nil, s.err
}

func (s *stubNavigationOptionRepository) ListBySourceTable(context.Context, uuid.UUID) ([]datamodel.NavigationOption, error) {
	return nil, s.err
}

func (s *stubNavigationOptionRepository) Delete(context.Context, uuid.UUID) error {
	return s.err
}

type stubLinkRepository struct {
	links []datamodel.Link
	err   error
}

func (s stubLinkRepository) Create(context.Context, datamodel.Link) error { return nil }
func (s stubLinkRepository) GetByID(context.Context, uuid.UUID) (datamodel.Link, error) {
	return datamodel.Link{}, s.err
}
func (s stubLinkRepository) ListByTenant(context.Context, uuid.UUID) ([]datamodel.Link, error) {
	return s.links, s.err
}
func (s stubLinkRepository) Delete(context.Context, uuid.UUID) error { return s.err }

func TestTableServiceUpdateRejectsMissingCaptionField(t *testing.T) {
	t.Parallel()

	tableID := uuid.New()
	service := NewTableService(
		stubTenantRepository{},
		stubTableRepository{table: datamodel.Table{ID: tableID, TenantID: uuid.New(), Name: "transactions"}},
		stubFieldRepository{fields: []datamodel.Field{
			{ID: uuid.New(), TableID: tableID, Name: "object_id", DataType: datamodel.DataTypeString},
		}},
		stubLinkRepository{},
		&stubPivotRepository{},
		&stubSchemaChangeRepository{},
		nil,
		stubTransactionManager{},
		stubIDGenerator{value: uuid.New()},
		stubClock{now: time.Now().UTC()},
	)

	captionField := "full_name"
	_, err := service.Update(context.Background(), UpdateTableInput{
		TableID:      tableID,
		CaptionField: &captionField,
	})
	if err == nil {
		t.Fatal("expected update to fail for unknown caption field")
	}
}

func TestTableServiceUpdateRejectsNonStringCaptionField(t *testing.T) {
	t.Parallel()

	tableID := uuid.New()
	service := NewTableService(
		stubTenantRepository{},
		stubTableRepository{table: datamodel.Table{ID: tableID, TenantID: uuid.New(), Name: "transactions"}},
		stubFieldRepository{fields: []datamodel.Field{
			{ID: uuid.New(), TableID: tableID, Name: "amount", DataType: datamodel.DataTypeFloat},
		}},
		stubLinkRepository{},
		&stubPivotRepository{},
		&stubSchemaChangeRepository{},
		nil,
		stubTransactionManager{},
		stubIDGenerator{value: uuid.New()},
		stubClock{now: time.Now().UTC()},
	)

	captionField := "amount"
	_, err := service.Update(context.Background(), UpdateTableInput{
		TableID:      tableID,
		CaptionField: &captionField,
	})
	if err == nil {
		t.Fatal("expected update to fail for non-string caption field")
	}
}

func TestNavigationOptionServiceCreateRejectsUnbackedNavigation(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	tenantID := uuid.New()
	sourceTableID := uuid.New()
	targetTableID := uuid.New()
	sourceFieldID := uuid.New()
	filterFieldID := uuid.New()
	orderingFieldID := uuid.New()

	navigationRepo := &stubNavigationOptionRepository{}
	indexRepo := &stubIndexJobRepository{}
	changeRepo := &stubSchemaChangeRepository{}
	service := NewNavigationOptionService(
		stubTableRepository{table: datamodel.Table{ID: sourceTableID, TenantID: tenantID, Name: "accounts"}},
		stubFieldRepository{fields: []datamodel.Field{
			{ID: filterFieldID, TableID: targetTableID, Name: "account_id", DataType: datamodel.DataTypeString},
			{ID: orderingFieldID, TableID: targetTableID, Name: "updated_at", DataType: datamodel.DataTypeTimestamp},
		}},
		stubLinkRepository{},
		&stubPivotRepository{},
		navigationRepo,
		changeRepo,
		stubTransactionManager{store: stubMutationStore{
			navigationOptions: navigationRepo,
			indexJobs:         indexRepo,
			schemaChanges:     changeRepo,
		}},
		&stubSequenceIDGenerator{values: []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}},
		stubClock{now: now},
	)
	service.tableRepository = tableRepositoryByID{
		tables: map[uuid.UUID]datamodel.Table{
			sourceTableID: {ID: sourceTableID, TenantID: tenantID, Name: "accounts"},
			targetTableID: {ID: targetTableID, TenantID: tenantID, Name: "transactions"},
		},
	}
	service.fieldRepository = fieldRepositoryByTableAndID{
		byID: map[uuid.UUID]datamodel.Field{
			sourceFieldID:   {ID: sourceFieldID, TableID: sourceTableID, Name: "object_id", DataType: datamodel.DataTypeString, IsUnique: true},
			filterFieldID:   {ID: filterFieldID, TableID: targetTableID, Name: "account_id", DataType: datamodel.DataTypeString},
			orderingFieldID: {ID: orderingFieldID, TableID: targetTableID, Name: "updated_at", DataType: datamodel.DataTypeTimestamp},
		},
		byTable: map[uuid.UUID][]datamodel.Field{
			sourceTableID: {
				{ID: sourceFieldID, TableID: sourceTableID, Name: "object_id", DataType: datamodel.DataTypeString, IsUnique: true},
			},
			targetTableID: {
				{ID: filterFieldID, TableID: targetTableID, Name: "account_id", DataType: datamodel.DataTypeString},
				{ID: orderingFieldID, TableID: targetTableID, Name: "updated_at", DataType: datamodel.DataTypeTimestamp},
			},
		},
	}

	_, err := service.Create(context.Background(), CreateNavigationOptionInput{
		TenantID:        tenantID,
		SourceTableID:   sourceTableID,
		SourceFieldID:   sourceFieldID,
		TargetTableID:   targetTableID,
		FilterFieldID:   filterFieldID,
		OrderingFieldID: orderingFieldID,
	})
	if err == nil {
		t.Fatal("expected navigation option to be rejected when no reverse link or self pivot exists")
	}
}

type tableRepositoryByID struct {
	tables map[uuid.UUID]datamodel.Table
}

func (s tableRepositoryByID) Create(context.Context, datamodel.Table) error { return nil }
func (s tableRepositoryByID) GetByID(_ context.Context, id uuid.UUID) (datamodel.Table, error) {
	return s.tables[id], nil
}
func (s tableRepositoryByID) ListByTenant(context.Context, uuid.UUID) ([]datamodel.Table, error) {
	return nil, nil
}
func (s tableRepositoryByID) Update(context.Context, datamodel.Table) error { return nil }
func (s tableRepositoryByID) Delete(context.Context, uuid.UUID) error       { return nil }

type fieldRepositoryByTableAndID struct {
	byID    map[uuid.UUID]datamodel.Field
	byTable map[uuid.UUID][]datamodel.Field
}

func (s fieldRepositoryByTableAndID) Create(context.Context, datamodel.Field) error { return nil }
func (s fieldRepositoryByTableAndID) GetByID(_ context.Context, id uuid.UUID) (datamodel.Field, error) {
	return s.byID[id], nil
}
func (s fieldRepositoryByTableAndID) ListByTable(_ context.Context, tableID uuid.UUID) ([]datamodel.Field, error) {
	return s.byTable[tableID], nil
}
func (s fieldRepositoryByTableAndID) Delete(context.Context, uuid.UUID) error       { return nil }
func (s fieldRepositoryByTableAndID) Update(context.Context, datamodel.Field) error { return nil }
