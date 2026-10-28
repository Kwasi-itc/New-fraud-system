package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/riverjobs"
)

func TestIndexJobTransactionalQueue(t *testing.T) {
	dsn := repositoryIntegrationDatabaseURL(t)
	ctx, stop := context.WithTimeout(context.Background(), 30*time.Second)
	defer stop()
	pool := repositoryIntegrationPool(t, ctx, dsn)
	defer pool.Close()
	resetRepositoryIntegrationDatabase(t, ctx, pool, dsn)
	migrator, err := rivermigrate.New(riverpgxv5.New(pool), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrator.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
		t.Fatal(err)
	}
	client, err := river.NewClient(riverpgxv5.New(pool), &river.Config{})
	if err != nil {
		t.Fatal(err)
	}
	manager := NewTransactionManager(pool).WithIndexJobEnqueuer(riverjobs.NewRiverIndexJobEnqueuer(client, 3, ""))
	tenantID := uuid.New()
	if err := NewTenantRepository(pool).Create(ctx, tenant.New(tenantID, time.Now().UTC(), "Queue fixture", nil)); err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)
	for _, outcome := range []string{"commit", "rollback", "cancel", "missing_queue", "missing_client"} {
		t.Run(outcome, func(t *testing.T) {
			id := uuid.New()
			job := datamodel.IndexJob{ID: id, TenantID: tenantID, TableName: "records", IndexType: datamodel.IndexJobTypeSearch, Columns: []string{"object_id"}, Status: datamodel.IndexJobStatusPending, RequestedAt: time.Now().UTC(), DedupeKey: id.String()}
			selected := manager
			if outcome == "missing_queue" {
				selected = NewTransactionManager(pool)
			}
			if outcome == "missing_client" {
				selected = NewTransactionManager(pool).WithIndexJobEnqueuer(riverjobs.NewRiverIndexJobEnqueuer(nil, 3, ""))
			}
			request, cancel := context.WithCancel(ctx)
			defer cancel()
			injected := errors.New("failure after enqueue")
			err := selected.Run(request, func(store ports.MutationStore) error {
				if err := store.IndexJobs().Create(request, job); err != nil {
					return err
				}
				if err := store.EnqueueIndexJob(request, id, &at); err != nil {
					return err
				}
				// Preserve ByArgs deduplication within the same transaction.
				if err := store.EnqueueIndexJob(request, id, &at); err != nil {
					return err
				}
				if outcome == "rollback" {
					return injected
				}
				if outcome == "cancel" {
					cancel()
				}
				return nil
			})
			if outcome == "commit" && err != nil {
				t.Fatal(err)
			}
			if outcome != "commit" && err == nil {
				t.Fatal("failure reported success")
			}
			if outcome == "rollback" && !errors.Is(err, injected) {
				t.Fatal(err)
			}
			if (outcome == "missing_queue" || outcome == "missing_client") && !errors.Is(err, ports.ErrJobQueueUnavailable) {
				t.Fatal(err)
			}
			var rows, jobs int
			if err := pool.QueryRow(ctx, "SELECT count(*) FROM core.index_jobs WHERE id=$1", id).Scan(&rows); err != nil {
				t.Fatal(err)
			}
			if err := pool.QueryRow(ctx, "SELECT count(*) FROM river_job WHERE args->>'index_job_id'=$1", id.String()).Scan(&jobs); err != nil {
				t.Fatal(err)
			}
			want := 0
			if outcome == "commit" {
				want = 1
			}
			if rows != want || jobs != want {
				t.Fatalf("rows=%d jobs=%d want=%d", rows, jobs, want)
			}
			if outcome == "commit" {
				var scheduled time.Time
				if err := pool.QueryRow(ctx, "SELECT scheduled_at FROM river_job WHERE args->>'index_job_id'=$1", id.String()).Scan(&scheduled); err != nil {
					t.Fatal(err)
				}
				if !scheduled.Equal(at) {
					t.Fatalf("schedule changed: %v", scheduled)
				}
			}
		})
	}
}
