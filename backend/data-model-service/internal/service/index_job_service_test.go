package service

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/riverjobs"
)

type stubTenantRepository struct {
	record tenant.Tenant
	err    error
}

func (s stubTenantRepository) Create(context.Context, tenant.Tenant) error { return nil }
func (s stubTenantRepository) GetByID(context.Context, uuid.UUID) (tenant.Tenant, error) {
	return s.record, s.err
}
func (s stubTenantRepository) List(context.Context) ([]tenant.Tenant, error) { return nil, nil }
func (s stubTenantRepository) UpdateStatus(context.Context, uuid.UUID, tenant.Status) error {
	return nil
}

type stubTableRepository struct {
	table datamodel.Table
	err   error
}

func (s stubTableRepository) Create(context.Context, datamodel.Table) error { return nil }
func (s stubTableRepository) GetByID(context.Context, uuid.UUID) (datamodel.Table, error) {
	return s.table, s.err
}
func (s stubTableRepository) ListByTenant(context.Context, uuid.UUID) ([]datamodel.Table, error) {
	return nil, nil
}
func (s stubTableRepository) Update(context.Context, datamodel.Table) error { return nil }
func (s stubTableRepository) Delete(context.Context, uuid.UUID) error       { return nil }

type stubFieldRepository struct {
	fields []datamodel.Field
	err    error
}

func (s stubFieldRepository) Create(context.Context, datamodel.Field) error { return nil }
func (s stubFieldRepository) GetByID(context.Context, uuid.UUID) (datamodel.Field, error) {
	return datamodel.Field{}, nil
}
func (s stubFieldRepository) ListByTable(context.Context, uuid.UUID) ([]datamodel.Field, error) {
	return s.fields, s.err
}
func (s stubFieldRepository) Delete(context.Context, uuid.UUID) error       { return nil }
func (s stubFieldRepository) Update(context.Context, datamodel.Field) error { return nil }

type stubIndexJobRepository struct {
	created []datamodel.IndexJob
	job     datamodel.IndexJob
	err     error
	retried []uuid.UUID
}

func (s *stubIndexJobRepository) Create(_ context.Context, job datamodel.IndexJob) error {
	s.created = append(s.created, job)
	s.job = job
	return s.err
}
func (s *stubIndexJobRepository) GetByID(context.Context, uuid.UUID) (datamodel.IndexJob, error) {
	return s.job, s.err
}
func (s *stubIndexJobRepository) ListByTenant(context.Context, uuid.UUID) ([]datamodel.IndexJob, error) {
	return nil, s.err
}
func (s *stubIndexJobRepository) StartAttempt(context.Context, uuid.UUID, time.Time) (datamodel.IndexJob, error) {
	return s.job, s.err
}
func (s *stubIndexJobRepository) MarkApplied(context.Context, uuid.UUID, time.Time) error {
	return nil
}
func (s *stubIndexJobRepository) MarkFailed(context.Context, uuid.UUID, string, time.Time) error {
	return nil
}
func (s *stubIndexJobRepository) MarkPendingRetry(context.Context, uuid.UUID, string) error {
	return nil
}
func (s *stubIndexJobRepository) Retry(_ context.Context, id uuid.UUID, scheduledAt time.Time) error {
	s.retried = append(s.retried, id)
	s.job.Status = datamodel.IndexJobStatusPending
	s.job.ScheduledAt = &scheduledAt
	s.job.ErrorMessage = nil
	return s.err
}

type stubIndexJobEnqueuer struct {
	jobIDs []uuid.UUID
}

func (s *stubIndexJobEnqueuer) Enqueue(_ context.Context, jobID uuid.UUID, _ *time.Time) error {
	s.jobIDs = append(s.jobIDs, jobID)
	return nil
}

func (s *stubIndexJobEnqueuer) EnqueueTx(_ context.Context, _ pgx.Tx, jobID uuid.UUID, _ *time.Time) error {
	s.jobIDs = append(s.jobIDs, jobID)
	return nil
}

func TestIndexJobServiceCreateEnqueuesAndLogs(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC)
	tenantID := uuid.New()
	tableID := uuid.New()
	jobID := uuid.New()
	changeID := uuid.New()
	repo := &stubIndexJobRepository{}
	changeRepo := &stubSchemaChangeRepository{}
	enqueuer := &stubIndexJobEnqueuer{}
	service := NewIndexJobService(
		stubTenantRepository{record: tenant.Tenant{ID: tenantID, Status: tenant.StatusActive}},
		stubTableRepository{table: datamodel.Table{ID: tableID, TenantID: tenantID, Name: "cases"}},
		stubFieldRepository{fields: []datamodel.Field{
			{ID: uuid.New(), TableID: tableID, Name: "email"},
			{ID: uuid.New(), TableID: tableID, Name: "last_seen"},
		}},
		repo,
		changeRepo,
		stubTransactionManager{store: stubMutationStore{indexJobs: repo, schemaChanges: changeRepo}},
		&stubSequenceIDGenerator{values: []uuid.UUID{jobID, changeID}},
		stubClock{now: now},
		enqueuer,
	)

	job, err := service.Create(context.Background(), CreateIndexJobInput{
		TenantID:             tenantID,
		TableID:              tableID,
		IndexType:            datamodel.IndexJobTypeNavigation,
		Columns:              []string{"Email", "last_seen"},
		RequestedByOperation: "test_request",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(repo.created) != 1 {
		t.Fatalf("expected 1 created job, got %d", len(repo.created))
	}
	if job.ID != jobID {
		t.Fatalf("expected job id %s, got %s", jobID, job.ID)
	}
	if got, want := repo.created[0].Columns, []string{"email", "last_seen"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("unexpected columns: %v", got)
	}
	if len(changeRepo.changes) != 1 || changeRepo.changes[0].Operation != "request_index_job" {
		t.Fatalf("unexpected schema changes: %+v", changeRepo.changes)
	}
	if len(enqueuer.jobIDs) != 1 || enqueuer.jobIDs[0] != jobID {
		t.Fatalf("unexpected enqueued ids: %v", enqueuer.jobIDs)
	}
}

func TestIndexJobServiceRetryResetsFailedJob(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 18, 11, 0, 0, 0, time.UTC)
	jobID := uuid.New()
	repo := &stubIndexJobRepository{
		job: datamodel.IndexJob{
			ID:           jobID,
			Status:       datamodel.IndexJobStatusFailed,
			ErrorMessage: stringPtr("boom"),
		},
	}
	enqueuer := &stubIndexJobEnqueuer{}
	service := NewIndexJobService(
		stubTenantRepository{},
		stubTableRepository{},
		stubFieldRepository{},
		repo,
		&stubSchemaChangeRepository{},
		stubTransactionManager{store: stubMutationStore{indexJobs: repo}},
		stubIDGenerator{value: uuid.New()},
		stubClock{now: now},
		enqueuer,
	)

	job, err := service.Retry(context.Background(), jobID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(repo.retried) != 1 || repo.retried[0] != jobID {
		t.Fatalf("unexpected retried ids: %v", repo.retried)
	}
	if job.Status != datamodel.IndexJobStatusPending {
		t.Fatalf("expected pending status, got %s", job.Status)
	}
	if job.ScheduledAt == nil || !job.ScheduledAt.Equal(now) {
		t.Fatalf("unexpected scheduled_at: %v", job.ScheduledAt)
	}
	if len(enqueuer.jobIDs) != 1 || enqueuer.jobIDs[0] != jobID {
		t.Fatalf("unexpected enqueued ids: %v", enqueuer.jobIDs)
	}
}

type stubSequenceIDGenerator struct {
	values []uuid.UUID
	index  int
}

func (s *stubSequenceIDGenerator) New() uuid.UUID {
	if s.index >= len(s.values) {
		return uuid.Nil
	}
	value := s.values[s.index]
	s.index++
	return value
}

func stringPtr(value string) *string {
	return &value
}

var _ riverjobs.IndexJobEnqueuer = (*stubIndexJobEnqueuer)(nil)
