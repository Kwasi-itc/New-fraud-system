package metadata_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	store "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func TestLeasedIndexExecution(t *testing.T) {
	db, database := metadataDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	at := time.Now().UTC().Truncate(time.Microsecond)
	tenantID := uuid.New()
	if err := mysqlstore.Mutation(ctx, db, func(tx *sql.Tx) error {
		r, err := store.NewMetadataRepositories(tx, database, tenantID, 100, 10000)
		if err != nil {
			return err
		}
		return r.Tenants.Create(ctx, tenant.New(tenantID, at, "execution", nil))
	}); err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"applied", "applied_last_attempt", "pending", "failed", "failed_retry", "audit_failure", "expired", "stale_attempt", "terminal_redelivery", "exhausted", "exhausted_failed_retry", "exhausted_audit_failure", "exhausted_commit"} {
		t.Run(scenario, func(t *testing.T) {
			exhausted := strings.HasPrefix(scenario, "exhausted")
			maxAttempts := 3
			if exhausted || scenario == "applied_last_attempt" {
				maxAttempts = 1
			}
			q, err := store.NewIndexDeliveryStore(db, database, scenario, maxAttempts, time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			jobID := uuid.New()
			var deliveryID uuid.UUID
			initial := datamodel.IndexJobStatusPending
			if scenario == "terminal_redelivery" {
				initial = datamodel.IndexJobStatusApplied
			}
			if err := mysqlstore.Mutation(ctx, db, func(tx *sql.Tx) error {
				r, err := store.NewMetadataRepositories(tx, database, tenantID, 100, 10000)
				if err != nil {
					return err
				}
				if err := r.Jobs.Create(ctx, datamodel.IndexJob{ID: jobID, TenantID: tenantID, TableName: "records", IndexType: "search", Columns: []string{}, Status: initial, RequestedAt: at}); err != nil {
					return err
				}
				deliveryID, err = q.EnqueueTx(ctx, tx, tenantID, jobID, nil)
				return err
			}); err != nil {
				t.Fatal(err)
			}
			// Restarted worker configuration must not change a retained delivery's
			// immutable attempt budget.
			q, err = store.NewIndexDeliveryStore(db, database, scenario, 99, time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			lease, err := q.Claim(ctx)
			if err != nil || lease == nil {
				t.Fatalf("claim %+v %v", lease, err)
			}
			execution, err := store.NewLeasedIndexJobExecutionStore(q, *lease, 10000, 0)
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "expired" {
				if _, err := db.ExecContext(ctx, "UPDATE data_model_index_deliveries SET lease_until=TIMESTAMPADD(SECOND,-1,UTC_TIMESTAMP(6)) WHERE id=?", mysqlstore.UUID(deliveryID)); err != nil {
					t.Fatal(err)
				}
				if got, err := execution.StartAttempt(ctx, jobID, at); !errors.Is(err, store.ErrStaleDeliveryLease) || got.ID != uuid.Nil {
					t.Fatalf("expired attempt %+v %v", got, err)
				}
				return
			}
			job, err := execution.StartAttempt(ctx, jobID, at)
			if err != nil {
				t.Fatal(err)
			}
			var state string
			if scenario == "terminal_redelivery" {
				if job.Status != initial || job.AttemptCount != 0 {
					t.Fatalf("terminal restarted %+v", job)
				}
				if err := db.QueryRowContext(ctx, "SELECT state FROM data_model_index_deliveries WHERE id=?", mysqlstore.UUID(deliveryID)).Scan(&state); err != nil || state != "completed" {
					t.Fatalf("terminal not acknowledged %s %v", state, err)
				}
				return
			}
			if job.Status != datamodel.IndexJobStatusRunning || job.AttemptCount != 1 {
				t.Fatalf("attempt %+v", job)
			}
			status, operation := "applied", "apply_index_job"
			if scenario == "pending" || exhausted {
				status, operation = "pending", "reschedule_index_job"
			}
			if scenario == "failed" || scenario == "failed_retry" || scenario == "exhausted_failed_retry" {
				status, operation = "failed", "fail_index_job"
			}
			outcome := ports.IndexJobOutcome{Job: job, Change: datamodel.SchemaChange{ID: uuid.New(), TenantID: tenantID, ResourceID: jobID, ResourceType: "index_job", Operation: operation, Status: status, Details: []byte(`{}`), CreatedAt: at}}
			outcome.RetryDelivery = scenario == "failed_retry" || scenario == "exhausted_failed_retry"
			if scenario == "audit_failure" || scenario == "exhausted_audit_failure" {
				if _, err := db.ExecContext(ctx, "INSERT INTO data_model_schema_change_log(id,tenant_id,resource_id,resource_type,operation,status,details,created_at) VALUES (?,?,?,'index_job','apply_index_job','applied','{}',?)", mysqlstore.UUID(outcome.Change.ID), mysqlstore.UUID(tenantID), mysqlstore.UUID(jobID), mysqlstore.DateTime{Time: at}); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "stale_attempt" {
				outcome.Job.AttemptCount++
			}
			var observed atomic.Bool
			if scenario == "exhausted_commit" {
				faultDB := deliveryFaultDB(t, database, &observed)
				faultQueue, err := store.NewIndexDeliveryStore(faultDB, database, scenario, 99, time.Minute)
				if err != nil {
					t.Fatal(err)
				}
				execution, err = store.NewLeasedIndexJobExecutionStore(faultQueue, *lease, 10000, 0)
				if err != nil {
					t.Fatal(err)
				}
			}
			err = execution.CompleteAttempt(ctx, outcome)
			var domainStatus string
			var attempts int
			if readErr := db.QueryRowContext(ctx, "SELECT status,attempt_count FROM data_model_index_jobs WHERE id=?", mysqlstore.UUID(jobID)).Scan(&domainStatus, &attempts); readErr != nil {
				t.Fatal(readErr)
			}
			if readErr := db.QueryRowContext(ctx, "SELECT state FROM data_model_index_deliveries WHERE id=?", mysqlstore.UUID(deliveryID)).Scan(&state); readErr != nil {
				t.Fatal(readErr)
			}
			if scenario == "audit_failure" || scenario == "stale_attempt" || scenario == "exhausted_audit_failure" {
				if err == nil || domainStatus != "running" || state != "running" || attempts != 1 {
					t.Fatalf("failed outcome leaked %s %s %d %v", domainStatus, state, attempts, err)
				}
				var reserved int
				if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM data_model_index_delivery_reservations WHERE tenant_id=? AND job_id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(jobID)).Scan(&reserved); err != nil || reserved != 1 {
					t.Fatalf("failed outcome released reservation: %d %v", reserved, err)
				}
				return
			}
			wantState := "completed"
			if scenario == "pending" || scenario == "failed_retry" {
				wantState = "available"
			}
			if exhausted {
				wantState = "discarded"
				if outcome.Change.Status != status || string(outcome.Change.Details) != "{}" {
					t.Fatal("caller outcome was mutated")
				}
				var auditStatus, auditOperation, code string
				var details []byte
				if err := db.QueryRowContext(ctx, "SELECT status,operation,details FROM data_model_schema_change_log WHERE id=?", mysqlstore.UUID(outcome.Change.ID)).Scan(&auditStatus, &auditOperation, &details); err != nil {
					t.Fatal(err)
				}
				if string(details) != "{}" || auditStatus != status || auditOperation != operation {
					t.Fatalf("domain audit changed by transport exhaustion: %s %s %s", auditStatus, auditOperation, details)
				}
				if err := db.QueryRowContext(ctx, "SELECT last_error_code FROM data_model_index_deliveries WHERE id=?", mysqlstore.UUID(deliveryID)).Scan(&code); err != nil || code != "delivery_attempts_exhausted" {
					t.Fatalf("exhaustion code %s %v", code, err)
				}
				var reserved int
				if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM data_model_index_delivery_reservations WHERE tenant_id=? AND job_id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(jobID)).Scan(&reserved); err != nil || reserved != 0 {
					t.Fatalf("exhausted reservation retained: %d %v", reserved, err)
				}
			}
			if scenario == "exhausted_commit" {
				var ambiguous *mysqlstore.CommitError
				if !observed.Load() || !errors.As(err, &ambiguous) {
					t.Fatalf("commit fault absent: %v observed=%v", err, observed.Load())
				}
				execution, err = store.NewLeasedIndexJobExecutionStore(q, *lease, 10000, 0)
				if err != nil {
					t.Fatal(err)
				}
			}
			if err != nil || domainStatus != status || state != wantState {
				t.Fatalf("outcome %s %s %v", domainStatus, state, err)
			}
			var count int
			if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM data_model_schema_change_log WHERE id=?", mysqlstore.UUID(outcome.Change.ID)).Scan(&count); err != nil || count != 1 {
				t.Fatalf("audit %d %v", count, err)
			}
			if err := execution.CompleteAttempt(ctx, outcome); !errors.Is(err, store.ErrStaleDeliveryLease) {
				t.Fatalf("old outcome accepted: %v", err)
			}
			if scenario == "failed_retry" {
				// River retries the execution error, then the runner sees the
				// terminal domain job and acknowledges without another attempt.
				nextLease, err := q.Claim(ctx)
				if err != nil || nextLease == nil {
					t.Fatalf("terminal retry claim: %+v %v", nextLease, err)
				}
				next, err := store.NewLeasedIndexJobExecutionStore(q, *nextLease, 10000, 0)
				if err != nil {
					t.Fatal(err)
				}
				terminal, err := next.StartAttempt(ctx, jobID, at)
				if err != nil || terminal.Status != datamodel.IndexJobStatusFailed || terminal.AttemptCount != 1 {
					t.Fatalf("terminal redelivery: %+v %v", terminal, err)
				}
				if err := db.QueryRowContext(ctx, "SELECT state FROM data_model_index_deliveries WHERE id=?", mysqlstore.UUID(deliveryID)).Scan(&state); err != nil || state != "completed" {
					t.Fatalf("terminal acknowledgement: %s %v", state, err)
				}
			}
		})
	}
}
