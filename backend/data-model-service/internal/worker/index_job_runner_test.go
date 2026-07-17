package worker

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
)

type stubWorkerTenantRepository struct {
	record tenant.Tenant
	err    error
}

func (s stubWorkerTenantRepository) Create(context.Context, tenant.Tenant) error { return nil }
func (s stubWorkerTenantRepository) GetByID(context.Context, uuid.UUID) (tenant.Tenant, error) {
	return s.record, s.err
}
func (s stubWorkerTenantRepository) List(context.Context) ([]tenant.Tenant, error) { return nil, nil }
func (s stubWorkerTenantRepository) UpdateStatus(context.Context, uuid.UUID, tenant.Status) error {
	return nil
}

type stubWorkerTableRepository struct {
	table datamodel.Table
	err   error
}

func (s stubWorkerTableRepository) Create(context.Context, datamodel.Table) error { return nil }
func (s stubWorkerTableRepository) GetByID(context.Context, uuid.UUID) (datamodel.Table, error) {
	return s.table, s.err
}
func (s stubWorkerTableRepository) ListByTenant(context.Context, uuid.UUID) ([]datamodel.Table, error) {
	return nil, nil
}
func (s stubWorkerTableRepository) Update(context.Context, datamodel.Table) error { return nil }
func (s stubWorkerTableRepository) Delete(context.Context, uuid.UUID) error       { return nil }

type stubWorkerIndexJobRepository struct {
	claimed        *datamodel.IndexJob
	claimErr       error
	appliedIDs     []uuid.UUID
	failedIDs      []uuid.UUID
	failedMsgs     []string
	rescheduledIDs []uuid.UUID
}

func (s *stubWorkerIndexJobRepository) Create(context.Context, datamodel.IndexJob) error { return nil }
func (s *stubWorkerIndexJobRepository) GetByID(context.Context, uuid.UUID) (datamodel.IndexJob, error) {
	return datamodel.IndexJob{}, nil
}
func (s *stubWorkerIndexJobRepository) ListByTenant(context.Context, uuid.UUID) ([]datamodel.IndexJob, error) {
	return nil, nil
}
func (s *stubWorkerIndexJobRepository) StartAttempt(_ context.Context, _ uuid.UUID, _ time.Time) (datamodel.IndexJob, error) {
	if s.claimed == nil {
		return datamodel.IndexJob{}, s.claimErr
	}
	return *s.claimed, nil
}
func (s *stubWorkerIndexJobRepository) MarkApplied(_ context.Context, id uuid.UUID, _ time.Time) error {
	s.appliedIDs = append(s.appliedIDs, id)
	return nil
}
func (s *stubWorkerIndexJobRepository) MarkFailed(_ context.Context, id uuid.UUID, message string, _ time.Time) error {
	s.failedIDs = append(s.failedIDs, id)
	s.failedMsgs = append(s.failedMsgs, message)
	return nil
}
func (s *stubWorkerIndexJobRepository) MarkPendingRetry(_ context.Context, id uuid.UUID, _ string) error {
	s.rescheduledIDs = append(s.rescheduledIDs, id)
	return nil
}
func (s *stubWorkerIndexJobRepository) Retry(context.Context, uuid.UUID, time.Time) error { return nil }

type stubWorkerSchemaChangeRepository struct {
	changes []datamodel.SchemaChange
}

func (s *stubWorkerSchemaChangeRepository) Create(_ context.Context, change datamodel.SchemaChange) error {
	s.changes = append(s.changes, change)
	return nil
}
func (s *stubWorkerSchemaChangeRepository) ListByTenant(context.Context, uuid.UUID) ([]datamodel.SchemaChange, error) {
	return nil, nil
}

type stubWorkerSchemaManager struct {
	err    error
	exists bool
}

func (s stubWorkerSchemaManager) ProvisionTenantSchema(context.Context, tenant.Tenant) error {
	return nil
}
func (s stubWorkerSchemaManager) CreateTable(context.Context, tenant.Tenant, datamodel.Table) error {
	return nil
}
func (s stubWorkerSchemaManager) DropTable(context.Context, tenant.Tenant, datamodel.Table) error {
	return nil
}
func (s stubWorkerSchemaManager) AddField(context.Context, tenant.Tenant, datamodel.Table, datamodel.Field) error {
	return nil
}
func (s stubWorkerSchemaManager) DropField(context.Context, tenant.Tenant, datamodel.Table, datamodel.Field) error {
	return nil
}
func (s stubWorkerSchemaManager) ArchiveField(context.Context, tenant.Tenant, datamodel.Table, datamodel.Field) error {
	return nil
}
func (s stubWorkerSchemaManager) CreateUniqueIndex(context.Context, tenant.Tenant, datamodel.Table, []string) error {
	return nil
}
func (s stubWorkerSchemaManager) DropUniqueIndex(context.Context, tenant.Tenant, datamodel.Table, []string) error {
	return nil
}
func (s stubWorkerSchemaManager) CreateManagedIndex(context.Context, tenant.Tenant, datamodel.Table, datamodel.IndexJob) error {
	return s.err
}
func (s stubWorkerSchemaManager) GetManagedIndexState(context.Context, tenant.Tenant, datamodel.Table, datamodel.IndexJob) (datamodel.ManagedIndexState, error) {
	return datamodel.ManagedIndexState{Name: "idx_cases_deadbeef", Exists: s.exists}, nil
}

type stubWorkerIDGenerator struct {
	value uuid.UUID
}

func (s stubWorkerIDGenerator) New() uuid.UUID { return s.value }

type fixedClock struct {
	now time.Time
}

func (c fixedClock) Now() time.Time { return c.now }

func TestRunnerRunJobMarksAppliedAndLogsSchemaChange(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	tenantID := uuid.New()
	tableID := uuid.New()
	jobID := uuid.New()
	claim := &datamodel.IndexJob{
		ID:                   jobID,
		TenantID:             tenantID,
		TableID:              &tableID,
		TableName:            "cases",
		IndexType:            datamodel.IndexJobTypeNavigation,
		Columns:              []string{"email"},
		RequestedByOperation: "create_navigation_option",
		AttemptCount:         0,
	}
	indexJobs := &stubWorkerIndexJobRepository{claimed: claim}
	schemaChanges := &stubWorkerSchemaChangeRepository{}
	runner := NewRunner(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		stubWorkerTenantRepository{record: tenant.Tenant{ID: tenantID}},
		stubWorkerTableRepository{table: datamodel.Table{ID: tableID, TenantID: tenantID, Name: "cases"}},
		indexJobs,
		schemaChanges,
		stubWorkerSchemaManager{},
		stubWorkerIDGenerator{value: uuid.New()},
		fixedClock{now: now},
		3,
	)

	if err := runner.RunJob(context.Background(), jobID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(indexJobs.appliedIDs) != 1 || indexJobs.appliedIDs[0] != jobID {
		t.Fatalf("unexpected applied ids: %v", indexJobs.appliedIDs)
	}
	if len(schemaChanges.changes) != 1 || schemaChanges.changes[0].Operation != "apply_index_job" {
		t.Fatalf("unexpected schema changes: %+v", schemaChanges.changes)
	}
}

func TestRunnerRunJobReschedulesBeforeFinalFailure(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 18, 12, 30, 0, 0, time.UTC)
	tenantID := uuid.New()
	tableID := uuid.New()
	jobID := uuid.New()
	indexJobs := &stubWorkerIndexJobRepository{claimed: &datamodel.IndexJob{
		ID:                   jobID,
		TenantID:             tenantID,
		TableID:              &tableID,
		TableName:            "cases",
		IndexType:            datamodel.IndexJobTypeNavigation,
		Columns:              []string{"email"},
		RequestedByOperation: "create_navigation_option",
		AttemptCount:         1,
	}}
	schemaChanges := &stubWorkerSchemaChangeRepository{}
	runner := NewRunner(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		stubWorkerTenantRepository{record: tenant.Tenant{ID: tenantID}},
		stubWorkerTableRepository{table: datamodel.Table{ID: tableID, TenantID: tenantID, Name: "cases"}},
		indexJobs,
		schemaChanges,
		stubWorkerSchemaManager{err: errors.New("ddl failed")},
		stubWorkerIDGenerator{value: uuid.New()},
		fixedClock{now: now},
		3,
	)

	if err := runner.RunJob(context.Background(), jobID); err == nil {
		t.Fatal("expected error")
	}
	if len(indexJobs.rescheduledIDs) != 1 || indexJobs.rescheduledIDs[0] != jobID {
		t.Fatalf("unexpected rescheduled ids: %v", indexJobs.rescheduledIDs)
	}
	if len(schemaChanges.changes) != 1 || schemaChanges.changes[0].Operation != "reschedule_index_job" {
		t.Fatalf("unexpected schema changes: %+v", schemaChanges.changes)
	}
}

func TestRunnerRunJobMarksFailedAfterMaxAttempts(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 18, 12, 45, 0, 0, time.UTC)
	tenantID := uuid.New()
	tableID := uuid.New()
	jobID := uuid.New()
	indexJobs := &stubWorkerIndexJobRepository{claimed: &datamodel.IndexJob{
		ID:                   jobID,
		TenantID:             tenantID,
		TableID:              &tableID,
		TableName:            "cases",
		IndexType:            datamodel.IndexJobTypeNavigation,
		Columns:              []string{"email"},
		RequestedByOperation: "create_navigation_option",
		AttemptCount:         3,
	}}
	schemaChanges := &stubWorkerSchemaChangeRepository{}
	runner := NewRunner(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		stubWorkerTenantRepository{record: tenant.Tenant{ID: tenantID}},
		stubWorkerTableRepository{table: datamodel.Table{ID: tableID, TenantID: tenantID, Name: "cases"}},
		indexJobs,
		schemaChanges,
		stubWorkerSchemaManager{err: errors.New("ddl failed")},
		stubWorkerIDGenerator{value: uuid.New()},
		fixedClock{now: now},
		3,
	)

	if err := runner.RunJob(context.Background(), jobID); err == nil {
		t.Fatal("expected error")
	}
	if len(indexJobs.failedIDs) != 1 || indexJobs.failedIDs[0] != jobID {
		t.Fatalf("unexpected failed ids: %v", indexJobs.failedIDs)
	}
	if len(schemaChanges.changes) != 1 || schemaChanges.changes[0].Operation != "fail_index_job" {
		t.Fatalf("unexpected schema changes: %+v", schemaChanges.changes)
	}
}

func TestRunnerRunJobReturnsStartError(t *testing.T) {
	t.Parallel()

	runner := NewRunner(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		stubWorkerTenantRepository{},
		stubWorkerTableRepository{},
		&stubWorkerIndexJobRepository{claimErr: errors.New("not found")},
		&stubWorkerSchemaChangeRepository{},
		stubWorkerSchemaManager{},
		stubWorkerIDGenerator{value: uuid.New()},
		fixedClock{now: time.Now().UTC()},
		3,
	)

	if err := runner.RunJob(context.Background(), uuid.New()); err == nil {
		t.Fatal("expected error")
	}
}
