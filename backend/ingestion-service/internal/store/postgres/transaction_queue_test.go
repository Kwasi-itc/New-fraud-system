package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/riverjobs"
	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
)

func TestTransactionalQueueBoundary(t *testing.T) {
	pool := contractTestDB(t, true)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
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
	manager := NewTransactionManager(pool).WithJobEnqueuers(riverjobs.NewRiverUploadLogEnqueuer(client, 3, ""), riverjobs.NewRiverDeferredIngestEnqueuer(client, 3, ""))
	tenant := uuid.New()
	at := time.Now().UTC().Add(time.Hour)
	for _, kind := range []string{"upload", "deferred"} {
		for _, outcome := range []string{"commit", "rollback", "cancel", "missing_queue"} {
			t.Run(kind+"/"+outcome, func(t *testing.T) {
				id := uuid.New()
				runCtx, stop := context.WithCancel(ctx)
				defer stop()
				selected := manager
				if outcome == "missing_queue" {
					selected = NewTransactionManager(pool)
				}
				injected := errors.New("after enqueue")
				err := selected.Run(runCtx, func(store ports.MutationStore) error {
					if kind == "upload" {
						log := ingestion.UploadLog{ID: id.String(), TenantID: tenant.String(), ObjectType: "transactions", Mode: ingestion.ModeCreate, Filename: "test.csv", ContentType: "text/csv", Status: ingestion.UploadLogStatusUploaded, Payload: []byte("object_id\n1"), RequestedAt: time.Now().UTC()}
						if err := store.UploadLogs().Create(runCtx, log); err != nil {
							return err
						}
						if err := store.EnqueueUploadLog(runCtx, id, &at); err != nil {
							return err
						}
					} else {
						execution := ingestion.DeferredIngest{ID: id.String(), TenantID: tenant.String(), ObjectType: "transactions", Mode: ingestion.ModeCreate, Status: ingestion.DeferredIngestStatusQueued, Payload: []byte(`{"object_id":"1"}`), RequestedAt: time.Now().UTC()}
						if err := store.DeferredIngests().Create(runCtx, execution); err != nil {
							return err
						}
						if err := store.EnqueueDeferredIngest(runCtx, id, &at); err != nil {
							return err
						}
					}
					if outcome == "rollback" {
						return injected
					}
					if outcome == "cancel" {
						stop()
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
				if outcome == "missing_queue" && !errors.Is(err, ports.ErrJobQueueUnavailable) {
					t.Fatal(err)
				}
				table, key := "core_ingestion.upload_logs", "upload_log_id"
				if kind == "deferred" {
					table, key = "core_ingestion.deferred_ingests", "deferred_ingest_id"
				}
				var rows, jobs int
				if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+table+" WHERE id=$1", id).Scan(&rows); err != nil {
					t.Fatal(err)
				}
				if err := pool.QueryRow(ctx, "SELECT count(*) FROM river_job WHERE args->>$1=$2", key, id.String()).Scan(&jobs); err != nil {
					t.Fatal(err)
				}
				want := 0
				if outcome == "commit" {
					want = 1
				}
				if rows != want || jobs != want {
					t.Fatalf("partial outcome: rows=%d jobs=%d want=%d", rows, jobs, want)
				}
				if want == 1 {
					var scheduled time.Time
					if err := pool.QueryRow(ctx, "SELECT scheduled_at FROM river_job WHERE args->>$1=$2", key, id.String()).Scan(&scheduled); err != nil {
						t.Fatal(err)
					}
					if scheduled.Sub(at).Abs() > time.Microsecond {
						t.Fatalf("schedule changed: %v vs %v", scheduled, at)
					}
				}
			})
		}
	}
}
