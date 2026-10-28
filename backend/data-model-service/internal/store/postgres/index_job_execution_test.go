package postgres

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	"github.com/google/uuid"
)

func TestIndexJobExecutionAtomicity(t *testing.T) {
	dsn := repositoryIntegrationDatabaseURL(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool := repositoryIntegrationPool(t, ctx, dsn)
	defer pool.Close()
	resetRepositoryIntegrationDatabase(t, ctx, pool, dsn)
	s := NewIndexJobExecutionStore(pool)
	now := time.Now().UTC().Truncate(time.Microsecond)
	record := tenant.New(uuid.New(), now, "worker fixture", nil)
	if err := NewTenantRepository(pool).Create(ctx, record); err != nil {
		t.Fatal(err)
	}
	seed := func() datamodel.IndexJob {
		t.Helper()
		job := datamodel.IndexJob{ID: uuid.New(), TenantID: record.ID, TableName: "records", IndexType: datamodel.IndexJobTypeRepair, Columns: []string{"name"}, Status: datamodel.IndexJobStatusPending, RequestedAt: now}
		if err := NewIndexJobRepository(pool).Create(ctx, job); err != nil {
			t.Fatal(err)
		}
		return job
	}
	outcome := func(job datamodel.IndexJob, status string) ports.IndexJobOutcome {
		op := map[string]string{"applied": "apply_index_job", "pending": "reschedule_index_job", "failed": "fail_index_job"}[status]
		return ports.IndexJobOutcome{Job: job, Change: datamodel.SchemaChange{ID: uuid.New(), TenantID: record.ID, Operation: op, ResourceType: "index_job", ResourceID: job.ID, Status: status, Details: []byte(`{"verified":true}`), CreatedAt: now}}
	}
	for _, status := range []string{"applied", "pending", "failed"} {
		t.Run(status, func(t *testing.T) {
			job := seed()
			claim, err := s.StartAttempt(ctx, job.ID, now)
			if err != nil || claim.AttemptCount != 1 || claim.Status != datamodel.IndexJobStatusRunning {
				t.Fatal(claim, err)
			}
			o := outcome(claim, status)
			// Duplicate audit identity fails after the conditional UPDATE. Both
			// must roll back; the claimed attempt remains eligible to complete.
			if err := NewSchemaChangeRepository(pool).Create(ctx, o.Change); err != nil {
				t.Fatal(err)
			}
			if err := s.CompleteAttempt(ctx, o); err == nil {
				t.Fatal("history failure hidden")
			}
			current, err := NewIndexJobRepository(pool).GetByID(ctx, job.ID)
			if err != nil || current.Status != datamodel.IndexJobStatusRunning || current.CompletedAt != nil {
				t.Fatal("partial outcome committed", current, err)
			}
			o.Change.ID = uuid.New()
			if err := s.CompleteAttempt(ctx, o); err != nil {
				t.Fatal(err)
			}
			if err := s.CompleteAttempt(ctx, o); !errors.Is(err, ports.ErrStaleIndexJobAttempt) {
				t.Fatal("repeated outcome accepted", err)
			}
			current, err = s.StartAttempt(ctx, job.ID, now)
			if err != nil {
				t.Fatal(err)
			}
			if status == "pending" {
				if current.Status != datamodel.IndexJobStatusRunning || current.AttemptCount != 2 {
					t.Fatal("retry not claimed", current)
				}
			} else if string(current.Status) != status || current.AttemptCount != 1 {
				t.Fatal("terminal job reclaimed", current)
			}
		})
	}
	t.Run("stale_attempt_and_wrong_tenant", func(t *testing.T) {
		job := seed()
		first, err := s.StartAttempt(ctx, job.ID, now)
		if err != nil {
			t.Fatal(err)
		}
		second, err := s.StartAttempt(ctx, job.ID, now)
		if err != nil || second.AttemptCount != 2 {
			t.Fatal(second, err)
		}
		if err := s.CompleteAttempt(ctx, outcome(first, "applied")); !errors.Is(err, ports.ErrStaleIndexJobAttempt) {
			t.Fatal("stale attempt accepted", err)
		}
		wrong := outcome(second, "applied")
		wrong.Job.TenantID = uuid.New()
		wrong.Change.TenantID = wrong.Job.TenantID
		if err := s.CompleteAttempt(ctx, wrong); !errors.Is(err, ports.ErrStaleIndexJobAttempt) {
			t.Fatal("wrong tenant accepted", err)
		}
		if err := s.CompleteAttempt(ctx, outcome(second, "applied")); err != nil {
			t.Fatal(err)
		}
		var count int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM core.schema_change_log WHERE resource_id=$1", job.ID).Scan(&count); err != nil || count != 1 {
			t.Fatal("stale outcome leaked history", count, err)
		}
	})
	t.Run("concurrent_completions_one_winner", func(t *testing.T) {
		job := seed()
		claim, err := s.StartAttempt(ctx, job.ID, now)
		if err != nil {
			t.Fatal(err)
		}
		results := make(chan error, 2)
		var wg sync.WaitGroup
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func() { defer wg.Done(); results <- s.CompleteAttempt(ctx, outcome(claim, "applied")) }()
		}
		wg.Wait()
		close(results)
		wins, stale := 0, 0
		for err := range results {
			if err == nil {
				wins++
			} else if errors.Is(err, ports.ErrStaleIndexJobAttempt) {
				stale++
			} else {
				t.Fatal(err)
			}
		}
		if wins != 1 || stale != 1 {
			t.Fatal(wins, stale)
		}
	})
}
