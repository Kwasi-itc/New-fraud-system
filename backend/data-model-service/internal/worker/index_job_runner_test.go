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
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
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

func (s *stubWorkerIndexJobRepository) GetByDedupeKey(context.Context, string) (*datamodel.IndexJob, error) {
	return nil, nil
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
	job := *s.claimed
	if job.Status == datamodel.IndexJobStatusApplied || job.Status == datamodel.IndexJobStatusFailed || job.Status == datamodel.IndexJobStatusCancelled {
		return job, nil
	}
	job.Status = datamodel.IndexJobStatusRunning
	if job.AttemptCount == 0 {
		job.AttemptCount = 1
	}
	return job, nil
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
	err     error
	exists  bool
	creates *int
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
func (s *stubWorkerSchemaManager) CreateManagedIndex(context.Context, tenant.Tenant, datamodel.Table, datamodel.IndexJob) error {
	if s.creates != nil {
		*s.creates++
	}
	if s.err == nil {
		s.exists = true
	}
	return s.err
}

func TestRunnerRecoveryAtAttemptLimitAndTerminalRedelivery(t *testing.T) {
	for _, mode := range []string{"existing_at_limit", "missing_at_limit", "applied", "failed", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			tenantID, tableID := uuid.New(), uuid.New()
			job := datamodel.IndexJob{ID: uuid.New(), TenantID: tenantID, TableID: &tableID, TableName: "records", AttemptCount: 4}
			if mode == "applied" || mode == "failed" || mode == "cancelled" {
				job.Status = datamodel.IndexJobStatus(mode)
			}
			jobs := &stubWorkerIndexJobRepository{claimed: &job}
			history := &stubWorkerSchemaChangeRepository{}
			creates := 0
			r := NewRunner(slog.New(slog.NewTextHandler(io.Discard, nil)), stubWorkerTenantRepository{record: tenant.Tenant{ID: tenantID}}, stubWorkerTableRepository{table: datamodel.Table{ID: tableID, TenantID: tenantID, Name: "records"}}, stubWorkerExecution{jobs: jobs, history: history}, &stubWorkerSchemaManager{exists: mode == "existing_at_limit", creates: &creates}, stubWorkerIDGenerator{uuid.New()}, fixedClock{time.Now().UTC().Truncate(time.Second).Add(123456789)}, 3)
			if err := r.RunJob(context.Background(), job.ID); err != nil {
				t.Fatal(err)
			}
			if creates != 0 {
				t.Fatal("terminal/exhausted delivery executed DDL")
			}
			switch mode {
			case "existing_at_limit":
				if len(jobs.appliedIDs) != 1 {
					t.Fatal("completed physical work not recovered")
				}
			case "missing_at_limit":
				if len(jobs.failedIDs) != 1 {
					t.Fatal("attempt exhaustion not recorded")
				}
			default:
				if len(history.changes) != 0 {
					t.Fatal("terminal replay added history")
				}
			}
			for _, change := range history.changes {
				if change.CreatedAt.Nanosecond()%1000 != 0 {
					t.Fatal("worker timestamp lost precision at adapter")
				}
			}
		})
	}
}
func (s *stubWorkerSchemaManager) GetManagedIndexState(context.Context, tenant.Tenant, datamodel.Table, datamodel.IndexJob) (datamodel.ManagedIndexState, error) {
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
		stubWorkerExecution{jobs: indexJobs, history: schemaChanges},
		&stubWorkerSchemaManager{},
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
		stubWorkerExecution{jobs: indexJobs, history: schemaChanges},
		&stubWorkerSchemaManager{err: errors.New("ddl failed")},
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
		stubWorkerExecution{jobs: indexJobs, history: schemaChanges},
		&stubWorkerSchemaManager{err: errors.New("ddl failed")},
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
		stubWorkerExecution{jobs: &stubWorkerIndexJobRepository{claimErr: errors.New("not found")}, history: &stubWorkerSchemaChangeRepository{}},
		&stubWorkerSchemaManager{},
		stubWorkerIDGenerator{value: uuid.New()},
		fixedClock{now: time.Now().UTC()},
		3,
	)

	if err := runner.RunJob(context.Background(), uuid.New()); err == nil {
		t.Fatal("expected error")
	}
}

func (s *stubWorkerIndexJobRepository) GetByDedupeKeyForUpdate(ctx context.Context, key string) (*datamodel.IndexJob, error) {
	return s.GetByDedupeKey(ctx, key)
}

type stubWorkerExecution struct {
	jobs      *stubWorkerIndexJobRepository
	history   *stubWorkerSchemaChangeRepository
	finishErr error
}

func (s stubWorkerExecution) StartAttempt(ctx context.Context, id uuid.UUID, at time.Time) (datamodel.IndexJob, error) {
	return s.jobs.StartAttempt(ctx, id, at)
}
func (s stubWorkerExecution) CompleteAttempt(ctx context.Context, o ports.IndexJobOutcome) error {
	if err := o.Validate(); err != nil {
		return err
	}
	if s.finishErr != nil {
		return s.finishErr
	}
	if err := s.history.Create(ctx, o.Change); err != nil {
		return err
	}
	switch o.Change.Status {
	case "applied":
		return s.jobs.MarkApplied(ctx, o.Job.ID, o.Change.CreatedAt)
	case "pending":
		return s.jobs.MarkPendingRetry(ctx, o.Job.ID, o.ErrorMessage)
	default:
		return s.jobs.MarkFailed(ctx, o.Job.ID, o.ErrorMessage, o.Change.CreatedAt)
	}
}

func TestRunnerDoesNotAcknowledgeOutcomeFailure(t *testing.T) {
	for _, mode := range []string{"applied", "retry", "terminal", "missing_table", "wrong_tenant", "lookup"} {
		t.Run(mode, func(t *testing.T) {
			tenantID, tableID := uuid.New(), uuid.New()
			job := datamodel.IndexJob{ID: uuid.New(), TenantID: tenantID, TableID: &tableID, TableName: "records", AttemptCount: 1}
			jobs := &stubWorkerIndexJobRepository{claimed: &job}
			history := &stubWorkerSchemaChangeRepository{}
			failure := errors.New("outcome write failed")
			ddlFailure := errors.New("DDL failed")
			schema := &stubWorkerSchemaManager{exists: true}
			table := stubWorkerTableRepository{table: datamodel.Table{ID: tableID, TenantID: tenantID, Name: "records"}}
			switch mode {
			case "retry":
				schema.exists = false
				schema.err = ddlFailure
			case "terminal":
				job.AttemptCount = 3
				schema.exists = false
				schema.err = ddlFailure
			case "missing_table":
				job.TableID = nil
			case "wrong_tenant":
				table.table.TenantID = uuid.New()
			case "lookup":
				table.err = ddlFailure
			}
			r := NewRunner(slog.New(slog.NewTextHandler(io.Discard, nil)), stubWorkerTenantRepository{record: tenant.Tenant{ID: tenantID}}, table, stubWorkerExecution{jobs, history, failure}, schema, stubWorkerIDGenerator{uuid.New()}, fixedClock{time.Now().UTC()}, 3)
			err := r.RunJob(context.Background(), job.ID)
			if !errors.Is(err, failure) {
				t.Fatal("outcome failure acknowledged", err)
			}
			if (mode == "retry" || mode == "terminal" || mode == "lookup") && !errors.Is(err, ddlFailure) {
				t.Fatal("original failure lost", err)
			}
			if len(history.changes) != 0 || len(jobs.appliedIDs)+len(jobs.failedIDs)+len(jobs.rescheduledIDs) != 0 {
				t.Fatal("partial stub outcome")
			}
		})
	}
}

type recordingExecution struct {
	ports.IndexJobExecutionStore
	outcomes *[]ports.IndexJobOutcome
}

func (s recordingExecution) CompleteAttempt(ctx context.Context, o ports.IndexJobOutcome) error {
	*s.outcomes = append(*s.outcomes, o)
	return s.IndexJobExecutionStore.CompleteAttempt(ctx, o)
}

func TestRunnerTransportOutcome(t *testing.T) {
	for _, mode := range []string{"applied", "retry", "exhausted", "validation"} {
		t.Run(mode, func(t *testing.T) {
			tenantID, tableID := uuid.New(), uuid.New()
			job := datamodel.IndexJob{ID: uuid.New(), TenantID: tenantID, TableID: &tableID, TableName: "records", AttemptCount: 1}
			failure := errors.New("execution failed")
			schema := &stubWorkerSchemaManager{exists: true}
			if mode == "retry" || mode == "exhausted" {
				schema.exists, schema.err = false, failure
			}
			if mode == "exhausted" {
				job.AttemptCount = 3
			}
			if mode == "validation" {
				job.TableID = nil
			}
			jobs := &stubWorkerIndexJobRepository{claimed: &job}
			history := &stubWorkerSchemaChangeRepository{}
			var outcomes []ports.IndexJobOutcome
			execution := recordingExecution{stubWorkerExecution{jobs, history, nil}, &outcomes}
			r := NewRunner(slog.New(slog.NewTextHandler(io.Discard, nil)), stubWorkerTenantRepository{record: tenant.Tenant{ID: tenantID}}, stubWorkerTableRepository{table: datamodel.Table{ID: tableID, TenantID: tenantID, Name: "records"}}, execution, schema, stubWorkerIDGenerator{uuid.New()}, fixedClock{time.Now().UTC()}, 3)
			err := r.RunJob(context.Background(), job.ID)
			wantRetry := mode == "retry" || mode == "exhausted"
			if len(outcomes) != 1 || outcomes[0].RetryDelivery != wantRetry {
				t.Fatalf("transport outcome: %+v", outcomes)
			}
			if wantRetry && !errors.Is(err, failure) || !wantRetry && err != nil {
				t.Fatalf("source error behavior changed: %v", err)
			}
			wantStatus := map[string]string{"applied": "applied", "retry": "pending", "exhausted": "failed", "validation": "failed"}[mode]
			if outcomes[0].Change.Status != wantStatus {
				t.Fatalf("domain outcome changed: %+v", outcomes[0])
			}
		})
	}
}
