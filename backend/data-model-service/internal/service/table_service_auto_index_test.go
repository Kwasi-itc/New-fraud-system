package service

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
)

func TestTableServiceUpdateCaptionFieldCreatesSearchIndexJob(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC)
	tenantID := uuid.New()
	tableID := uuid.New()
	jobID := uuid.New()
	changeID := uuid.New()

	indexJobs := &stubIndexJobRepository{}
	changeRepo := &stubSchemaChangeRepository{}
	enqueuer := &stubIndexJobEnqueuer{}
	service := NewTableService(
		stubTenantRepository{},
		stubTableRepository{table: datamodel.Table{ID: tableID, TenantID: tenantID, Name: "transactions"}},
		stubFieldRepository{fields: []datamodel.Field{
			{ID: uuid.New(), TableID: tableID, Name: "display_name", DataType: datamodel.DataTypeString},
		}},
		stubLinkRepository{},
		&stubPivotRepository{},
		changeRepo,
		nil,
		stubTransactionManager{store: stubMutationStore{
			tables:        stubTableRepository{table: datamodel.Table{ID: tableID, TenantID: tenantID, Name: "transactions"}},
			indexJobs:     indexJobs,
			schemaChanges: changeRepo,
		}},
		&stubSequenceIDGenerator{values: []uuid.UUID{jobID, changeID}},
		stubClock{now: now},
	).WithIndexJobEnqueuer(enqueuer)

	captionField := "display_name"
	table, err := service.Update(context.Background(), UpdateTableInput{
		TableID:      tableID,
		CaptionField: &captionField,
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if table.CaptionField != "display_name" {
		t.Fatalf("caption field = %q, want display_name", table.CaptionField)
	}
	if len(indexJobs.created) != 1 {
		t.Fatalf("created index jobs = %d, want 1", len(indexJobs.created))
	}
	if indexJobs.created[0].IndexType != datamodel.IndexJobTypeSearch {
		t.Fatalf("index type = %s, want %s", indexJobs.created[0].IndexType, datamodel.IndexJobTypeSearch)
	}
	if got := indexJobs.created[0].Columns; len(got) != 1 || got[0] != "display_name" {
		t.Fatalf("columns = %v, want [display_name]", got)
	}
	if len(enqueuer.jobIDs) != 1 || enqueuer.jobIDs[0] != jobID {
		t.Fatalf("enqueued ids = %v, want [%s]", enqueuer.jobIDs, jobID)
	}
}

func TestTableServiceUpdateCaptionFieldDedupesAppliedSearchIndexJob(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 20, 10, 15, 0, 0, time.UTC)
	tenantID := uuid.New()
	tableID := uuid.New()
	existingJobID := uuid.New()

	indexJobs := &stubIndexJobRepository{
		jobs: []datamodel.IndexJob{{
			ID:        existingJobID,
			TenantID:  tenantID,
			TableID:   &tableID,
			TableName: "transactions",
			IndexType: datamodel.IndexJobTypeSearch,
			Columns:   []string{"display_name"},
			Status:    datamodel.IndexJobStatusApplied,
			DedupeKey: datamodel.BuildIndexJobDedupeKey(tenantID, tableID, datamodel.IndexJobTypeSearch, []string{"display_name"}),
		}},
	}
	changeRepo := &stubSchemaChangeRepository{}
	enqueuer := &stubIndexJobEnqueuer{}
	service := NewTableService(
		stubTenantRepository{},
		stubTableRepository{table: datamodel.Table{ID: tableID, TenantID: tenantID, Name: "transactions"}},
		stubFieldRepository{fields: []datamodel.Field{
			{ID: uuid.New(), TableID: tableID, Name: "display_name", DataType: datamodel.DataTypeString},
		}},
		stubLinkRepository{},
		&stubPivotRepository{},
		changeRepo,
		nil,
		stubTransactionManager{store: stubMutationStore{
			tables:        stubTableRepository{table: datamodel.Table{ID: tableID, TenantID: tenantID, Name: "transactions"}},
			indexJobs:     indexJobs,
			schemaChanges: changeRepo,
		}},
		stubIDGenerator{value: uuid.New()},
		stubClock{now: now},
	).WithIndexJobEnqueuer(enqueuer)

	captionField := "display_name"
	if _, err := service.Update(context.Background(), UpdateTableInput{TableID: tableID, CaptionField: &captionField}); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if len(indexJobs.created) != 0 {
		t.Fatalf("created index jobs = %d, want 0", len(indexJobs.created))
	}
	if len(enqueuer.jobIDs) != 0 {
		t.Fatalf("enqueued ids = %v, want none", enqueuer.jobIDs)
	}
}

func TestNavigationOptionServiceCreateEnqueuesNavigationIndexJob(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 20, 10, 30, 0, 0, time.UTC)
	tenantID := uuid.New()
	sourceTableID := uuid.New()
	targetTableID := uuid.New()
	sourceFieldID := uuid.New()
	filterFieldID := uuid.New()
	orderingFieldID := uuid.New()
	linkID := uuid.New()
	optionID := uuid.New()
	indexJobID := uuid.New()
	changeID := uuid.New()

	navigationRepo := &stubNavigationOptionRepository{}
	indexRepo := &stubIndexJobRepository{}
	changeRepo := &stubSchemaChangeRepository{}
	enqueuer := &stubIndexJobEnqueuer{}
	service := NewNavigationOptionService(
		tableRepositoryByID{
			tables: map[uuid.UUID]datamodel.Table{
				sourceTableID: {ID: sourceTableID, TenantID: tenantID, Name: "accounts"},
				targetTableID: {ID: targetTableID, TenantID: tenantID, Name: "transactions"},
			},
		},
		fieldRepositoryByTableAndID{
			byID: map[uuid.UUID]datamodel.Field{
				sourceFieldID:   {ID: sourceFieldID, TableID: sourceTableID, Name: "object_id", DataType: datamodel.DataTypeString},
				filterFieldID:   {ID: filterFieldID, TableID: targetTableID, Name: "account_id", DataType: datamodel.DataTypeString},
				orderingFieldID: {ID: orderingFieldID, TableID: targetTableID, Name: "updated_at", DataType: datamodel.DataTypeTimestamp},
			},
			byTable: map[uuid.UUID][]datamodel.Field{
				sourceTableID: {
					{ID: sourceFieldID, TableID: sourceTableID, Name: "object_id", DataType: datamodel.DataTypeString},
				},
				targetTableID: {
					{ID: filterFieldID, TableID: targetTableID, Name: "account_id", DataType: datamodel.DataTypeString},
					{ID: orderingFieldID, TableID: targetTableID, Name: "updated_at", DataType: datamodel.DataTypeTimestamp},
				},
			},
		},
		stubLinkRepository{links: []datamodel.Link{{
			ID:          linkID,
			TenantID:    tenantID,
			Name:        "account",
			ParentTable: sourceTableID,
			ParentField: sourceFieldID,
			ChildTable:  targetTableID,
			ChildField:  filterFieldID,
		}}},
		&stubPivotRepository{},
		navigationRepo,
		changeRepo,
		stubTransactionManager{store: stubMutationStore{
			navigationOptions: navigationRepo,
			indexJobs:         indexRepo,
			schemaChanges:     changeRepo,
		}},
		&stubSequenceIDGenerator{values: []uuid.UUID{optionID, indexJobID, changeID}},
		stubClock{now: now},
	).WithIndexJobEnqueuer(enqueuer)

	_, err := service.Create(context.Background(), CreateNavigationOptionInput{
		TenantID:        tenantID,
		SourceTableID:   sourceTableID,
		SourceFieldID:   sourceFieldID,
		TargetTableID:   targetTableID,
		FilterFieldID:   filterFieldID,
		OrderingFieldID: orderingFieldID,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if len(indexRepo.created) != 1 {
		t.Fatalf("created index jobs = %d, want 1", len(indexRepo.created))
	}
	if indexRepo.created[0].IndexType != datamodel.IndexJobTypeNavigation {
		t.Fatalf("index type = %s, want %s", indexRepo.created[0].IndexType, datamodel.IndexJobTypeNavigation)
	}
	if len(enqueuer.jobIDs) != 1 || enqueuer.jobIDs[0] != indexJobID {
		t.Fatalf("enqueued ids = %v, want [%s]", enqueuer.jobIDs, indexJobID)
	}
}
