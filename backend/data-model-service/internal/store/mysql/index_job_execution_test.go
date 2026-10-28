package mysql

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/worker"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func testIndexJobExecution(t *testing.T, db *sql.DB) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s, err := NewIndexJobExecutionStore(db, "data_model_publication_test", 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	tenantID := uuid.New()
	seed := func() uuid.UUID {
		t.Helper()
		id := uuid.New()
		_, err := db.ExecContext(ctx, "INSERT INTO data_model_index_jobs(id,tenant_id,table_name,index_type,columns_json,status,requested_by_operation,requested_at,dedupe_key) VALUES (?,?,'records','repair',?,'pending','fixture',?,'')", mysqlstore.UUID(id), mysqlstore.UUID(tenantID), `["email","name"]`, mysqlstore.DateTime{Time: now})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	outcome := func(job datamodel.IndexJob, status string) ports.IndexJobOutcome {
		op := map[string]string{"applied": "apply_index_job", "pending": "reschedule_index_job", "failed": "fail_index_job"}[status]
		return ports.IndexJobOutcome{Job: job, Change: datamodel.SchemaChange{ID: uuid.New(), TenantID: job.TenantID, ResourceID: job.ID, ResourceType: "index_job", Operation: op, Status: status, Details: []byte(`{"fixture":true}`), CreatedAt: now}}
	}
	for _, status := range []string{"applied", "pending", "failed"} {
		t.Run(status+"_atomic_history", func(t *testing.T) {
			id := seed()
			job, err := s.StartAttempt(ctx, id, now)
			if err != nil || job.AttemptCount != 1 || job.Status != datamodel.IndexJobStatusRunning || len(job.Columns) != 2 || job.Columns[0] != "email" || job.Columns[1] != "name" || job.TableID != nil || job.StartedAt == nil || !job.StartedAt.Equal(now) {
				t.Fatal("invalid claim", job, err)
			}
			o := outcome(job, status)
			// Existing audit primary key forces history failure after status UPDATE.
			_, err = db.ExecContext(ctx, "INSERT INTO data_model_schema_change_log(id,tenant_id,operation,resource_type,resource_id,status,details,created_at) VALUES (?,?,'fixture','index_job',?,'fixture','{}',?)", mysqlstore.UUID(o.Change.ID), mysqlstore.UUID(tenantID), mysqlstore.UUID(id), mysqlstore.DateTime{Time: now})
			if err != nil {
				t.Fatal(err)
			}
			if err := s.CompleteAttempt(ctx, o); err == nil {
				t.Fatal("audit failure hidden")
			}
			var current string
			var completed sql.NullTime
			if err := db.QueryRowContext(ctx, "SELECT status,completed_at FROM data_model_index_jobs WHERE id=?", mysqlstore.UUID(id)).Scan(&current, &completed); err != nil || current != "running" || completed.Valid {
				t.Fatal("partial completion", current, err)
			}
			o.Change.ID = uuid.New()
			if status != "applied" {
				o.ErrorMessage = "retry cause"
			}
			if err := s.CompleteAttempt(ctx, o); err != nil {
				t.Fatal(err)
			}
			if err := s.CompleteAttempt(ctx, o); !errors.Is(err, ports.ErrStaleIndexJobAttempt) {
				t.Fatal("duplicate outcome accepted", err)
			}
			if err := db.QueryRowContext(ctx, "SELECT status,completed_at FROM data_model_index_jobs WHERE id=?", mysqlstore.UUID(id)).Scan(&current, &completed); err != nil || current != status || completed.Valid != (status != "pending") {
				t.Fatal("wrong outcome", current, completed, err)
			}
			again, err := s.StartAttempt(ctx, id, now)
			if err != nil {
				t.Fatal(err)
			}
			if status == "pending" {
				if again.AttemptCount != 2 || again.Status != datamodel.IndexJobStatusRunning || again.ErrorMessage != nil {
					t.Fatal("retry claim failed", again)
				}
			} else if again.AttemptCount != 1 || string(again.Status) != status {
				t.Fatal("terminal redelivery reclaimed", again)
			}
		})
	}
	t.Run("stale_wrong_tenant_and_concurrent_completion", func(t *testing.T) {
		id := seed()
		first, err := s.StartAttempt(ctx, id, now)
		if err != nil {
			t.Fatal(err)
		}
		second, err := s.StartAttempt(ctx, id, now)
		if err != nil || second.AttemptCount != 2 {
			t.Fatal(second, err)
		}
		if err := s.CompleteAttempt(ctx, outcome(first, "applied")); !errors.Is(err, ports.ErrStaleIndexJobAttempt) {
			t.Fatal("stale completion accepted", err)
		}
		wrong := outcome(second, "applied")
		wrong.Job.TenantID = uuid.New()
		wrong.Change.TenantID = wrong.Job.TenantID
		if err := s.CompleteAttempt(ctx, wrong); !errors.Is(err, ports.ErrStaleIndexJobAttempt) {
			t.Fatal("cross-tenant completion accepted", err)
		}
		var wg sync.WaitGroup
		results := make(chan error, 2)
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func() { defer wg.Done(); results <- s.CompleteAttempt(ctx, outcome(second, "applied")) }()
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
		var count int
		if err := db.QueryRowContext(ctx, "SELECT count(*) FROM data_model_schema_change_log WHERE resource_id=?", mysqlstore.UUID(id)).Scan(&count); err != nil || count != 1 || wins != 1 || stale != 1 {
			t.Fatal(count, wins, stale, err)
		}
	})
	t.Run("concurrent_claims_and_cancellation", func(t *testing.T) {
		id := seed()
		claims := make(chan datamodel.IndexJob, 2)
		failures := make(chan error, 2)
		var wg sync.WaitGroup
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func() { defer wg.Done(); j, e := s.StartAttempt(ctx, id, now); claims <- j; failures <- e }()
		}
		wg.Wait()
		close(claims)
		close(failures)
		for err := range failures {
			if err != nil {
				t.Fatal(err)
			}
		}
		seen := map[int]bool{}
		var latest datamodel.IndexJob
		for j := range claims {
			seen[j.AttemptCount] = true
			if j.AttemptCount == 2 {
				latest = j
			}
		}
		if len(seen) != 2 || !seen[1] || !seen[2] {
			t.Fatal("claims not serialized", seen)
		}
		cancelled, stop := context.WithCancel(ctx)
		stop()
		if err := s.CompleteAttempt(cancelled, outcome(latest, "applied")); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		if err := s.CompleteAttempt(ctx, outcome(latest, "applied")); err != nil {
			t.Fatal("cancelled completion leaked effect", err)
		}
	})
	t.Run("lost_claim_and_outcome_commit_acknowledgements", func(t *testing.T) {
		for _, point := range []string{"claim", "outcome"} {
			t.Run(point, func(t *testing.T) {
				id := seed()
				var observed atomic.Bool
				fault, err := NewIndexJobExecutionStore(lostCommitDB(t, &observed), "data_model_publication_test", 1<<20)
				if err != nil {
					t.Fatal(err)
				}
				var uncertain *mysqlstore.CommitError
				if point == "claim" {
					claim, err := fault.StartAttempt(ctx, id, now)
					if !errors.As(err, &uncertain) || !observed.Load() || claim.ID != uuid.Nil {
						t.Fatal("uncertain claim returned work", claim, err)
					}
					recovered, err := s.StartAttempt(ctx, id, now)
					if err != nil || recovered.AttemptCount != 2 {
						t.Fatal("claim redelivery not fenced", recovered, err)
					}
					if err := s.CompleteAttempt(ctx, outcome(recovered, "applied")); err != nil {
						t.Fatal(err)
					}
				} else {
					claim, err := s.StartAttempt(ctx, id, now)
					if err != nil {
						t.Fatal(err)
					}
					err = fault.CompleteAttempt(ctx, outcome(claim, "applied"))
					if !errors.As(err, &uncertain) || !observed.Load() {
						t.Fatal("uncertain outcome acknowledged", err)
					}
					recovered, err := s.StartAttempt(ctx, id, now)
					if err != nil || recovered.Status != datamodel.IndexJobStatusApplied || recovered.AttemptCount != 1 {
						t.Fatal("terminal outcome lost", recovered, err)
					}
				}
				var count int
				if err := db.QueryRowContext(ctx, "SELECT count(*) FROM data_model_schema_change_log WHERE resource_id=?", mysqlstore.UUID(id)).Scan(&count); err != nil || count != 1 {
					t.Fatal("history duplicated or missing", count, err)
				}
			})
		}
	})
	t.Run("runner_uses_mysql_outcomes_and_terminal_redelivery", func(t *testing.T) {
		id := seed()
		tableID := uuid.New()
		if _, err := db.ExecContext(ctx, "UPDATE data_model_index_jobs SET table_id=? WHERE id=?", mysqlstore.UUID(tableID), mysqlstore.UUID(id)); err != nil {
			t.Fatal(err)
		}
		schema := &executionTestSchema{}
		r := worker.NewRunner(slog.New(slog.NewTextHandler(io.Discard, nil)), executionTestTenant{record: tenant.Tenant{ID: tenantID}}, executionTestTable{record: datamodel.Table{ID: tableID, TenantID: tenantID, Name: "records"}}, s, schema, executionTestClock{now.Add(789 * time.Nanosecond)}, executionTestClock{now.Add(789 * time.Nanosecond)}, 3)
		if err := r.RunJob(ctx, id); err != nil {
			t.Fatal(err)
		}
		if err := r.RunJob(ctx, id); err != nil {
			t.Fatal(err)
		}
		if schema.inspections != 1 {
			t.Fatal("terminal redelivery executed inspection", schema.inspections)
		}
		var count int
		if err := db.QueryRowContext(ctx, "SELECT count(*) FROM data_model_schema_change_log WHERE resource_id=?", mysqlstore.UUID(id)).Scan(&count); err != nil || count != 1 {
			t.Fatal(count, err)
		}
	})
	t.Run("invalid_payload_precision_and_limits", func(t *testing.T) {
		id := seed()
		if _, err := s.StartAttempt(ctx, id, now.Add(time.Nanosecond)); err == nil {
			t.Fatal("precision loss accepted")
		}
		bounded, err := NewIndexJobExecutionStore(db, "data_model_publication_test", 1)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := bounded.StartAttempt(ctx, id, now); !errors.Is(err, ErrSnapshotLimit) {
			t.Fatal("oversized row accepted", err)
		}
		if _, err := db.ExecContext(ctx, "UPDATE data_model_index_jobs SET columns_json=? WHERE id=?", `[null]`, mysqlstore.UUID(id)); err != nil {
			t.Fatal(err)
		}
		if _, err := s.StartAttempt(ctx, id, now); err == nil {
			t.Fatal("null column accepted")
		}
		var attempts int
		if err := db.QueryRowContext(ctx, "SELECT attempt_count FROM data_model_index_jobs WHERE id=?", mysqlstore.UUID(id)).Scan(&attempts); err != nil || attempts != 0 {
			t.Fatal("invalid claim changed attempt", attempts, err)
		}
	})
}

type executionTestTenant struct {
	ports.TenantRepository
	record tenant.Tenant
}

func (s executionTestTenant) GetByID(context.Context, uuid.UUID) (tenant.Tenant, error) {
	return s.record, nil
}

type executionTestTable struct {
	ports.TableRepository
	record datamodel.Table
}

func (s executionTestTable) GetByID(context.Context, uuid.UUID) (datamodel.Table, error) {
	return s.record, nil
}

type executionTestSchema struct {
	ports.SchemaManager
	inspections int
}

func (s *executionTestSchema) GetManagedIndexState(context.Context, tenant.Tenant, datamodel.Table, datamodel.IndexJob) (datamodel.ManagedIndexState, error) {
	s.inspections++
	return datamodel.ManagedIndexState{Name: "fixture_index", Exists: true}, nil
}

type executionTestClock struct{ at time.Time }

func (c executionTestClock) Now() time.Time { return c.at }
func (executionTestClock) New() uuid.UUID   { return uuid.New() }
