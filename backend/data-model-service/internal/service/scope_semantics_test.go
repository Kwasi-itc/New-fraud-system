package service

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
)

type stubNavigationOptionRepository struct {
	created []datamodel.NavigationOption
	option   datamodel.NavigationOption
	err      error
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
func (s tableRepositoryByID) Delete(context.Context, uuid.UUID) error        { return nil }

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
func (s fieldRepositoryByTableAndID) Delete(context.Context, uuid.UUID) error      { return nil }
func (s fieldRepositoryByTableAndID) Update(context.Context, datamodel.Field) error { return nil }
