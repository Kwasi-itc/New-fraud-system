package service

import (
	"context"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/execution"
)

type executionSummaryScheduledRepoStub struct {
	counts                map[execution.Status]int
	recordedStatus        execution.Status
	recordedNextAttemptAt *time.Time
	recordedFailedAt      *time.Time
}

func (s executionSummaryScheduledRepoStub) Create(context.Context, execution.ScheduledExecution) (execution.ScheduledExecution, error) {
	return execution.ScheduledExecution{}, nil
}
func (s executionSummaryScheduledRepoStub) GetByID(context.Context, string, string, string) (execution.ScheduledExecution, error) {
	return execution.ScheduledExecution{}, nil
}
func (s executionSummaryScheduledRepoStub) ListByScenario(context.Context, string, string) ([]execution.ScheduledExecution, error) {
	return nil, nil
}
func (s executionSummaryScheduledRepoStub) CountByStatus(context.Context, string, string) (map[execution.Status]int, error) {
	return s.counts, nil
}
func (s executionSummaryScheduledRepoStub) ListDue(context.Context, time.Time, int) ([]execution.ScheduledExecution, error) {
	return nil, nil
}
func (s executionSummaryScheduledRepoStub) ClaimDue(context.Context, time.Time, int) ([]execution.ScheduledExecution, error) {
	return nil, nil
}
func (s executionSummaryScheduledRepoStub) UpdateStatus(context.Context, string, execution.Status) error {
	return nil
}
func (s executionSummaryScheduledRepoStub) RecordAttemptFailure(context.Context, string, execution.Status, *time.Time, string, *time.Time) error {
	return nil
}
func (s executionSummaryScheduledRepoStub) ResetForRetry(context.Context, string, execution.Status) error {
	return nil
}

type executionSummaryAsyncRepoStub struct {
	counts                map[execution.Status]int
	recordedStatus        execution.Status
	recordedNextAttemptAt *time.Time
	recordedFailedAt      *time.Time
}

func (s executionSummaryAsyncRepoStub) Create(context.Context, execution.AsyncDecisionExecution) (execution.AsyncDecisionExecution, error) {
	return execution.AsyncDecisionExecution{}, nil
}
func (s executionSummaryAsyncRepoStub) GetByID(context.Context, string, string) (execution.AsyncDecisionExecution, error) {
	return execution.AsyncDecisionExecution{}, nil
}
func (s executionSummaryAsyncRepoStub) ListByTenant(context.Context, string) ([]execution.AsyncDecisionExecution, error) {
	return nil, nil
}
func (s executionSummaryAsyncRepoStub) CountByStatus(context.Context, string) (map[execution.Status]int, error) {
	return s.counts, nil
}
func (s executionSummaryAsyncRepoStub) ListQueued(context.Context, int) ([]execution.AsyncDecisionExecution, error) {
	return nil, nil
}
func (s executionSummaryAsyncRepoStub) ClaimQueued(context.Context, int) ([]execution.AsyncDecisionExecution, error) {
	return nil, nil
}
func (s executionSummaryAsyncRepoStub) UpdateStatus(context.Context, string, execution.Status) error {
	return nil
}
func (s executionSummaryAsyncRepoStub) RecordAttemptFailure(context.Context, string, execution.Status, *time.Time, string, *time.Time) error {
	return nil
}
func (s executionSummaryAsyncRepoStub) ResetForRetry(context.Context, string, execution.Status) error {
	return nil
}

type executionRetryScheduledRepoStub struct {
	recordedStatus        execution.Status
	recordedNextAttemptAt *time.Time
	recordedFailedAt      *time.Time
}

func (s *executionRetryScheduledRepoStub) Create(context.Context, execution.ScheduledExecution) (execution.ScheduledExecution, error) {
	return execution.ScheduledExecution{}, nil
}
func (s *executionRetryScheduledRepoStub) GetByID(context.Context, string, string, string) (execution.ScheduledExecution, error) {
	return execution.ScheduledExecution{}, nil
}
func (s *executionRetryScheduledRepoStub) ListByScenario(context.Context, string, string) ([]execution.ScheduledExecution, error) {
	return nil, nil
}
func (s *executionRetryScheduledRepoStub) CountByStatus(context.Context, string, string) (map[execution.Status]int, error) {
	return nil, nil
}
func (s *executionRetryScheduledRepoStub) ListDue(context.Context, time.Time, int) ([]execution.ScheduledExecution, error) {
	return nil, nil
}
func (s *executionRetryScheduledRepoStub) ClaimDue(context.Context, time.Time, int) ([]execution.ScheduledExecution, error) {
	return nil, nil
}
func (s *executionRetryScheduledRepoStub) UpdateStatus(context.Context, string, execution.Status) error {
	return nil
}
func (s *executionRetryScheduledRepoStub) RecordAttemptFailure(_ context.Context, _ string, status execution.Status, nextAttemptAt *time.Time, _ string, failedAt *time.Time) error {
	s.recordedStatus = status
	s.recordedNextAttemptAt = nextAttemptAt
	s.recordedFailedAt = failedAt
	return nil
}
func (s *executionRetryScheduledRepoStub) ResetForRetry(context.Context, string, execution.Status) error {
	return nil
}

type executionRetryAsyncRepoStub struct {
	recordedStatus        execution.Status
	recordedNextAttemptAt *time.Time
	recordedFailedAt      *time.Time
}

func (s *executionRetryAsyncRepoStub) Create(context.Context, execution.AsyncDecisionExecution) (execution.AsyncDecisionExecution, error) {
	return execution.AsyncDecisionExecution{}, nil
}
func (s *executionRetryAsyncRepoStub) GetByID(context.Context, string, string) (execution.AsyncDecisionExecution, error) {
	return execution.AsyncDecisionExecution{}, nil
}
func (s *executionRetryAsyncRepoStub) ListByTenant(context.Context, string) ([]execution.AsyncDecisionExecution, error) {
	return nil, nil
}
func (s *executionRetryAsyncRepoStub) CountByStatus(context.Context, string) (map[execution.Status]int, error) {
	return nil, nil
}
func (s *executionRetryAsyncRepoStub) ListQueued(context.Context, int) ([]execution.AsyncDecisionExecution, error) {
	return nil, nil
}
func (s *executionRetryAsyncRepoStub) ClaimQueued(context.Context, int) ([]execution.AsyncDecisionExecution, error) {
	return nil, nil
}
func (s *executionRetryAsyncRepoStub) UpdateStatus(context.Context, string, execution.Status) error {
	return nil
}
func (s *executionRetryAsyncRepoStub) RecordAttemptFailure(_ context.Context, _ string, status execution.Status, nextAttemptAt *time.Time, _ string, failedAt *time.Time) error {
	s.recordedStatus = status
	s.recordedNextAttemptAt = nextAttemptAt
	s.recordedFailedAt = failedAt
	return nil
}
func (s *executionRetryAsyncRepoStub) ResetForRetry(context.Context, string, execution.Status) error {
	return nil
}

type retryFixedClock struct{ now time.Time }

func (c retryFixedClock) Now() time.Time { return c.now }

func TestExecutionServiceGetScheduledExecutionStatusSummary(t *testing.T) {
	svc := ExecutionService{
		scheduledRepo: executionSummaryScheduledRepoStub{
			counts: map[execution.Status]int{
				execution.StatusPending:   2,
				execution.StatusRunning:   1,
				execution.StatusCompleted: 5,
				execution.StatusFailed:    1,
			},
		},
	}

	summary, err := svc.GetScheduledExecutionStatusSummary(context.Background(), "tenant-1", "scenario-1")
	if err != nil {
		t.Fatalf("GetScheduledExecutionStatusSummary() error = %v", err)
	}
	if summary.Pending != 2 || summary.Running != 1 || summary.Completed != 5 || summary.Failed != 1 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	if summary.Queued != 0 {
		t.Fatalf("summary.Queued = %d, want 0", summary.Queued)
	}
}

func TestExecutionServiceGetAsyncDecisionExecutionStatusSummary(t *testing.T) {
	svc := ExecutionService{
		asyncRepo: executionSummaryAsyncRepoStub{
			counts: map[execution.Status]int{
				execution.StatusQueued:    3,
				execution.StatusRunning:   2,
				execution.StatusCompleted: 4,
				execution.StatusFailed:    1,
			},
		},
	}

	summary, err := svc.GetAsyncDecisionExecutionStatusSummary(context.Background(), "tenant-1")
	if err != nil {
		t.Fatalf("GetAsyncDecisionExecutionStatusSummary() error = %v", err)
	}
	if summary.Queued != 3 || summary.Running != 2 || summary.Completed != 4 || summary.Failed != 1 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	if summary.Pending != 0 {
		t.Fatalf("summary.Pending = %d, want 0", summary.Pending)
	}
}

func TestExecutionServiceHandleScheduledExecutionFailureRequeuesBeforeMaxAttempts(t *testing.T) {
	now := time.Date(2026, 7, 9, 10, 0, 0, 0, time.UTC)
	repo := &executionRetryScheduledRepoStub{}
	svc := ExecutionService{
		clock:         retryFixedClock{now: now},
		scheduledRepo: repo,
		retryPolicy: ExecutionRetryPolicy{
			ScheduledBaseBackoff: 30 * time.Second,
		},
	}

	err := svc.handleScheduledExecutionFailure(context.Background(), execution.ScheduledExecution{
		ID:           "sched-1",
		AttemptCount: 1,
		MaxAttempts:  3,
	}, assertErr("boom"))
	if err != nil {
		t.Fatalf("handleScheduledExecutionFailure() error = %v", err)
	}
	if repo.recordedStatus != execution.StatusPending {
		t.Fatalf("recordedStatus = %s, want %s", repo.recordedStatus, execution.StatusPending)
	}
	if repo.recordedNextAttemptAt == nil || !repo.recordedNextAttemptAt.Equal(now.Add(30*time.Second)) {
		t.Fatalf("recordedNextAttemptAt = %v, want %v", repo.recordedNextAttemptAt, now.Add(30*time.Second))
	}
	if repo.recordedFailedAt != nil {
		t.Fatalf("recordedFailedAt = %v, want nil", repo.recordedFailedAt)
	}
}

func TestExecutionServiceHandleAsyncExecutionFailureMarksFailedAtMaxAttempts(t *testing.T) {
	now := time.Date(2026, 7, 9, 10, 0, 0, 0, time.UTC)
	repo := &executionRetryAsyncRepoStub{}
	svc := ExecutionService{
		clock:     retryFixedClock{now: now},
		asyncRepo: repo,
		retryPolicy: ExecutionRetryPolicy{
			AsyncBaseBackoff: 30 * time.Second,
		},
	}

	err := svc.handleAsyncExecutionFailure(context.Background(), execution.AsyncDecisionExecution{
		ID:           "async-1",
		AttemptCount: 3,
		MaxAttempts:  3,
	}, assertErr("boom"))
	if err != nil {
		t.Fatalf("handleAsyncExecutionFailure() error = %v", err)
	}
	if repo.recordedStatus != execution.StatusFailed {
		t.Fatalf("recordedStatus = %s, want %s", repo.recordedStatus, execution.StatusFailed)
	}
	if repo.recordedNextAttemptAt != nil {
		t.Fatalf("recordedNextAttemptAt = %v, want nil", repo.recordedNextAttemptAt)
	}
	if repo.recordedFailedAt == nil || !repo.recordedFailedAt.Equal(now) {
		t.Fatalf("recordedFailedAt = %v, want %v", repo.recordedFailedAt, now)
	}
}

type assertErr string

func (e assertErr) Error() string { return string(e) }
