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
	store "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/installation"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/worker"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func TestDeliveryRetryErrorHistory(t *testing.T) {
	db, database := metadataDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	tenantID := uuid.New()
	at := time.Now().UTC().Truncate(time.Microsecond)
	if err := mysqlstore.Mutation(ctx, db, func(tx *sql.Tx) error {
		r, err := store.NewMetadataRepositories(tx, database, tenantID, 100, 10000)
		if err != nil {
			return err
		}
		return r.Tenants.Create(ctx, tenant.New(tenantID, at, "retry", nil))
	}); err != nil {
		t.Fatal(err)
	}
	newQueue := func(databasePool *sql.DB, name string, attempts int) *store.IndexDeliveryStore {
		t.Helper()
		q, err := store.NewIndexDeliveryStoreWithRetryPolicy(databasePool, database, name, attempts, time.Minute, worker.RiverDeliveryRetryPolicy{})
		if err != nil {
			t.Fatal(err)
		}
		return q
	}
	enqueue := func(q *store.IndexDeliveryStore) (uuid.UUID, uuid.UUID, error) {
		job := uuid.New()
		var delivery uuid.UUID
		err := mysqlstore.Mutation(ctx, db, func(tx *sql.Tx) error {
			r, err := store.NewMetadataRepositories(tx, database, tenantID, 100, 10000)
			if err != nil {
				return err
			}
			if err := r.Jobs.Create(ctx, datamodel.IndexJob{ID: job, TenantID: tenantID, TableName: "records", IndexType: "search", Columns: []string{"object_id"}, Status: "pending", RequestedAt: at}); err != nil {
				return err
			}
			delivery, err = q.EnqueueTx(ctx, tx, tenantID, job, nil)
			return err
		})
		return job, delivery, err
	}
	q := newQueue(db, "before_upgrade", 4)
	job, _, err := enqueue(q)
	if err == nil {
		t.Fatal("policy accepted missing upgrade")
	}
	var rows int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM data_model_index_jobs WHERE id=?", mysqlstore.UUID(job)).Scan(&rows); err != nil || rows != 0 {
		t.Fatalf("missing-upgrade rollback: %d %v", rows, err)
	}
	upgrade, err := installation.DeliveryRetryUpgradeStatement(database)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, upgrade.SQL); err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"translated_attempts", "long_backoff", "retry_after_rescue", "rescue_unknown", "rescue_exhausted", "rescue_lost_commit", "owner_rollback", "unknown_history", "exhausted", "lost_commit", "success"} {
		t.Run(scenario, func(t *testing.T) {
			max := 4
			if scenario == "long_backoff" {
				max = 100
			}
			if scenario == "exhausted" || scenario == "rescue_exhausted" {
				max = 1
			}
			q := newQueue(db, scenario, max)
			job, delivery, err := enqueue(q)
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "translated_attempts" {
				// Reviewed source translation is represented only by fixture setup:
				// historical claims/snoozes are not necessarily execution errors.
				if _, err := db.ExecContext(ctx, "UPDATE data_model_index_deliveries SET attempt=2 WHERE id=?", mysqlstore.UUID(delivery)); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "long_backoff" {
				if _, err := db.ExecContext(ctx, "UPDATE data_model_index_deliveries SET attempt=24,error_count=24 WHERE id=?", mysqlstore.UUID(delivery)); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "unknown_history" {
				if _, err := db.ExecContext(ctx, "UPDATE data_model_index_deliveries SET error_count=NULL WHERE id=?", mysqlstore.UUID(delivery)); err != nil {
					t.Fatal(err)
				}
				if lease, err := q.Claim(ctx); err == nil || lease != nil || !strings.Contains(err.Error(), "translation") {
					t.Fatalf("unknown history claimed: %v %v", lease, err)
				}
				return
			}
			lease, err := q.Claim(ctx)
			if err != nil || lease == nil {
				t.Fatalf("claim: %v %v", lease, err)
			}
			if strings.HasPrefix(scenario, "rescue_") {
				if _, err := db.ExecContext(ctx, "UPDATE data_model_index_deliveries SET lease_until=TIMESTAMPADD(SECOND,-1,UTC_TIMESTAMP(6)) WHERE id=?", mysqlstore.UUID(delivery)); err != nil {
					t.Fatal(err)
				}
				if scenario == "rescue_unknown" {
					if _, err := db.ExecContext(ctx, "UPDATE data_model_index_deliveries SET error_count=NULL WHERE id=?", mysqlstore.UUID(delivery)); err != nil {
						t.Fatal(err)
					}
					if n, err := q.ReapExpired(ctx, 10); err == nil || n != 0 {
						t.Fatalf("unknown rescue accepted: %d %v", n, err)
					}
					return
				}
				selected := q
				var observed atomic.Bool
				if scenario == "rescue_lost_commit" {
					selected = newQueue(deliveryFaultDB(t, database, &observed), scenario, max)
				}
				n, err := selected.ReapExpired(ctx, 10)
				if scenario == "rescue_lost_commit" {
					var ambiguous *mysqlstore.CommitError
					if !errors.As(err, &ambiguous) || !observed.Load() || n != 0 {
						t.Fatalf("rescue ambiguity: %d %v", n, err)
					}
					if n, err := q.ReapExpired(ctx, 10); err != nil || n != 0 {
						t.Fatalf("rescue repeated: %d %v", n, err)
					}
				} else if err != nil || n != 1 {
					t.Fatalf("rescue: %d %v", n, err)
				}
				var count int
				var state string
				if err := db.QueryRowContext(ctx, "SELECT error_count,state FROM data_model_index_deliveries WHERE id=?", mysqlstore.UUID(delivery)).Scan(&count, &state); err != nil {
					t.Fatal(err)
				}
				wantState := "available"
				if scenario == "rescue_exhausted" {
					wantState = "discarded"
				}
				if count != 1 || state != wantState {
					t.Fatalf("rescue outcome: %d %s", count, state)
				}
				return
			}
			if scenario == "retry_after_rescue" {
				// Fixture-only clock advance. River rescue records an error too.
				if _, err := db.ExecContext(ctx, "UPDATE data_model_index_deliveries SET lease_until=TIMESTAMPADD(SECOND,-1,UTC_TIMESTAMP(6)) WHERE id=?", mysqlstore.UUID(delivery)); err != nil {
					t.Fatal(err)
				}
				if n, err := q.ReapExpired(ctx, 10); err != nil || n != 1 {
					t.Fatalf("rescue: %d %v", n, err)
				}
				var rescuedErrors int
				if err := db.QueryRowContext(ctx, "SELECT error_count FROM data_model_index_deliveries WHERE id=?", mysqlstore.UUID(delivery)).Scan(&rescuedErrors); err != nil || rescuedErrors != 1 {
					t.Fatalf("rescue error count: %d %v", rescuedErrors, err)
				}
				if claimed, err := q.Claim(ctx); err != nil || claimed != nil {
					t.Fatalf("rescue ignored retry schedule: %v %v", claimed, err)
				}
				if _, err := db.ExecContext(ctx, "UPDATE data_model_index_deliveries SET available_at=UTC_TIMESTAMP(6) WHERE id=?", mysqlstore.UUID(delivery)); err != nil {
					t.Fatal(err)
				}
				lease, err = q.Claim(ctx)
				if err != nil || lease == nil || lease.Attempt != 2 {
					t.Fatalf("reclaim: %v %v", lease, err)
				}
			}
			calls := 0
			failure := errors.New("owner failure")
			owner := func(tx *sql.Tx) error {
				calls++
				r, err := store.NewMetadataRepositories(tx, database, tenantID, 100, 10000)
				if err != nil {
					return err
				}
				if err := r.History.Create(ctx, datamodel.SchemaChange{ID: uuid.New(), TenantID: tenantID, Operation: "retry_policy", ResourceType: "index_job", ResourceID: job, Status: "pending", Details: []byte(`{}`), CreatedAt: at}); err != nil {
					return err
				}
				if scenario == "owner_rollback" {
					return failure
				}
				return nil
			}
			var observed atomic.Bool
			selected := q
			if scenario == "lost_commit" {
				selected = newQueue(deliveryFaultDB(t, database, &observed), scenario, max)
			}
			var before mysqlstore.DateTime
			if err := db.QueryRowContext(ctx, "SELECT UTC_TIMESTAMP(6)").Scan(&before); err != nil {
				t.Fatal(err)
			}
			err = selected.Finish(ctx, *lease, scenario != "success", 0, "index_attempt_retry", owner)
			if calls != 1 {
				t.Fatalf("owner calls=%d", calls)
			}
			if scenario == "owner_rollback" {
				if !errors.Is(err, failure) {
					t.Fatal(err)
				}
			} else if scenario == "lost_commit" {
				var ambiguous *mysqlstore.CommitError
				if !errors.As(err, &ambiguous) || !observed.Load() {
					t.Fatalf("commit ambiguity: %v", err)
				}
				if err := q.Finish(ctx, *lease, true, 0, "index_attempt_retry", owner); !errors.Is(err, store.ErrStaleDeliveryLease) {
					t.Fatalf("duplicate finish: %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			var count int
			var state string
			var available, after mysqlstore.DateTime
			if err := db.QueryRowContext(ctx, "SELECT error_count,state,available_at,UTC_TIMESTAMP(6) FROM data_model_index_deliveries WHERE id=?", mysqlstore.UUID(delivery)).Scan(&count, &state, &available, &after); err != nil {
				t.Fatal(err)
			}
			want := 1
			if scenario == "long_backoff" {
				want = 25
			}
			if scenario == "retry_after_rescue" {
				want = 2
			}
			if scenario == "owner_rollback" || scenario == "success" {
				want = 0
			}
			if count != want {
				t.Fatalf("errors=%d want=%d", count, want)
			}
			if scenario == "retry_after_rescue" || scenario == "lost_commit" || scenario == "translated_attempts" || scenario == "long_backoff" {
				base := time.Second
				if scenario == "long_backoff" {
					base = 390625 * time.Second
				}
				if scenario == "retry_after_rescue" {
					base = 16 * time.Second
				}
				if state != "available" || available.Time.Before(before.Time.Add(base*9/10)) || available.Time.After(after.Time.Add(base*11/10)) {
					t.Fatalf("wrong first-error schedule after claim %d: %s %s", lease.Attempt, state, available.Time)
				}
			}
			if scenario == "exhausted" && state != "discarded" {
				t.Fatal(state)
			}
			if scenario == "owner_rollback" && state != "running" {
				t.Fatal(state)
			}
			if scenario == "success" && state != "completed" {
				t.Fatal(state)
			}
			var audits int
			if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM data_model_schema_change_log WHERE tenant_id=? AND resource_id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(job)).Scan(&audits); err != nil {
				t.Fatal(err)
			}
			wantAudit := 1
			if scenario == "owner_rollback" {
				wantAudit = 0
			}
			if audits != wantAudit {
				t.Fatalf("audit=%d want=%d", audits, wantAudit)
			}
			if scenario == "exhausted" {
				r := store.DeliveryRedriveRequest{RequestID: uuid.New(), TenantID: tenantID, JobID: job, SourceID: delivery, Actor: "fixture-operator", Reason: "verified retry budget", ScheduledAt: at}
				replacement, err := q.RedriveDiscarded(ctx, r)
				if err != nil {
					t.Fatal(err)
				}
				replay, err := q.RedriveDiscarded(ctx, r)
				if err != nil || replay != replacement {
					t.Fatalf("redrive replay: %v", err)
				}
				var errors int
				if err := db.QueryRowContext(ctx, "SELECT error_count FROM data_model_index_deliveries WHERE id=?", mysqlstore.UUID(replacement)).Scan(&errors); err != nil || errors != 0 {
					t.Fatalf("redrive history: %d %v", errors, err)
				}
			}
		})
	}
}
