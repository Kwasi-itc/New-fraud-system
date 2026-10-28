package metadata_test

import (
	"context"
	"database/sql"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	store "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/worker"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

// The executor below performs metadata only. This proves the transport boundary,
// not physical DDL readiness or production executable wiring.
func TestMySQLDeliveryPolling(t *testing.T) {
	db, database := metadataDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	q, err := store.NewIndexDeliveryStore(db, database, "poll_test", 3, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	tenantID := uuid.New()
	at := time.Now().UTC().Truncate(time.Microsecond)
	if err := mysqlstore.Mutation(ctx, db, func(tx *sql.Tx) error {
		r, err := store.NewMetadataRepositories(tx, database, tenantID, 100, 10000)
		if err != nil {
			return err
		}
		if err := r.Tenants.Create(ctx, tenant.New(tenantID, at, "poll", nil)); err != nil {
			return err
		}
		for range 3 {
			id := uuid.New()
			if err := r.Jobs.Create(ctx, datamodel.IndexJob{ID: id, TenantID: tenantID, TableName: "records", IndexType: "search", Columns: []string{}, Status: "pending", RequestedAt: at}); err != nil {
				return err
			}
			if _, err := q.EnqueueTx(ctx, tx, tenantID, id, nil); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	supervisor, err := worker.NewLeaseSupervisor(30*time.Second, time.Second, time.Second, 15*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	poller, err := worker.NewDeliveryPoller(2, 10*time.Millisecond, time.Second, supervisor)
	if err != nil {
		t.Fatal(err)
	}
	run, stop := context.WithCancel(ctx)
	defer stop()
	var completed atomic.Int32
	err = poller.Run(run, func(ctx context.Context) (*worker.DeliveryTask, error) {
		lease, err := q.Claim(ctx)
		if err != nil || lease == nil {
			return nil, err
		}
		execution, err := store.NewLeasedIndexJobExecutionStore(q, *lease, 10000, time.Second)
		if err != nil {
			return nil, err
		}
		renewed := make(chan struct{})
		beats := 0 // Only the supervisor heartbeat goroutine uses this counter.
		return &worker.DeliveryTask{
			Heartbeat: func(ctx context.Context) error {
				if err := q.Heartbeat(ctx, *lease); err != nil {
					return err
				}
				beats++
				if beats == 2 {
					close(renewed)
				}
				return nil
			},
			Execute: func(ctx context.Context) error {
				job, err := execution.StartAttempt(ctx, lease.JobID, at)
				if err != nil {
					return err
				}
				select {
				case <-renewed:
				case <-ctx.Done():
					return ctx.Err()
				}
				outcome := ports.IndexJobOutcome{Job: job, Change: datamodel.SchemaChange{ID: uuid.New(), TenantID: tenantID, ResourceID: job.ID, ResourceType: "index_job", Operation: "apply_index_job", Status: "applied", Details: []byte(`{}`), CreatedAt: at}}
				if err := execution.CompleteAttempt(ctx, outcome); err != nil {
					return err
				}
				if completed.Add(1) == 3 {
					stop()
				}
				return nil
			},
		}, nil
	}, func(_ context.Context, err error) error { return err })
	if !errors.Is(err, context.Canceled) || completed.Load() != 3 {
		t.Fatalf("completed=%d err=%v", completed.Load(), err)
	}
	var deliveries, jobs, history int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM data_model_index_deliveries WHERE tenant_id=? AND state='completed' AND heartbeat_version>=2", mysqlstore.UUID(tenantID)).Scan(&deliveries); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM data_model_index_jobs WHERE tenant_id=? AND status='applied' AND attempt_count=1", mysqlstore.UUID(tenantID)).Scan(&jobs); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM data_model_schema_change_log WHERE tenant_id=? AND operation='apply_index_job'", mysqlstore.UUID(tenantID)).Scan(&history); err != nil {
		t.Fatal(err)
	}
	if deliveries != 3 || jobs != 3 || history != 3 {
		t.Fatalf("deliveries=%d jobs=%d history=%d", deliveries, jobs, history)
	}
}
