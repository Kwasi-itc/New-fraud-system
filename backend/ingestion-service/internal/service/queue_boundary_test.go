package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/riverjobs"
	storepostgres "github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/store/postgres"
)

func TestIntegrationQueueAtomicityAndIsolation(t *testing.T) {
	ctx := context.Background()
	dsn := integrationDatabaseURL(t)
	pool := integrationPool(t, ctx, dsn)
	defer pool.Close()
	resetIntegrationDatabase(t, ctx, pool, dsn)
	manager := integrationQueueManager(t, pool)
	ids := &fixedIDGenerator{next: integrationUUIDSequence(20)}
	clock := fixedClock{now: time.Now().UTC()}
	uploads := NewUploadLogService(storepostgres.NewUploadLogRepository(pool), IngestService{}, manager, ids, clock, 3)
	deferred := NewDeferredIngestService(storepostgres.NewDeferredIngestRepository(pool), IngestService{}, manager, ids, clock, 3)
	for _, tenant := range []uuid.UUID{uuid.New(), uuid.New()} {
		log, err := uploads.Create(ctx, tenant, "transactions", ingestion.ModeCreate, "test.csv", "text/csv", []byte("id\n1\n"))
		if err != nil {
			t.Fatal(err)
		}
		execution, err := deferred.Create(ctx, tenant, "transactions", ingestion.ModeCreate, map[string]any{"object_id": "same-business-id"}, nil)
		if err != nil {
			t.Fatal(err)
		}
		var count int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM public.river_job j JOIN core_ingestion.upload_logs l ON l.id=(j.args->>'upload_log_id')::uuid WHERE j.kind='upload_log_execute' AND l.tenant_id=$1 AND l.id=$2`, tenant, log.ID).Scan(&count); err != nil || count != 1 {
			t.Fatalf("upload queue binding: count=%d error=%v", count, err)
		}
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM public.river_job j JOIN core_ingestion.deferred_ingests d ON d.id=(j.args->>'deferred_ingest_id')::uuid WHERE j.kind='deferred_ingest_execute' AND d.tenant_id=$1 AND d.id=$2`, tenant, execution.ID).Scan(&count); err != nil || count != 1 {
			t.Fatalf("deferred queue binding: count=%d error=%v", count, err)
		}
	}
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM public.river_job").Scan(&count); err != nil || count != 4 {
		t.Fatalf("expected four distinct jobs: %d %v", count, err)
	}
	for _, test := range []struct {
		name string
		stop func(context.CancelFunc) error
	}{
		{"callback_failure", func(context.CancelFunc) error { return errors.New("injected callback failure") }},
		{"cancelled", func(cancel context.CancelFunc) error { cancel(); return nil }},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			id := uuid.New()
			log := ingestion.UploadLog{ID: id.String(), TenantID: uuid.NewString(), ObjectType: "transactions", Mode: ingestion.ModeCreate, Filename: "test.csv", ContentType: "text/csv", Status: ingestion.UploadLogStatusUploaded, RequestedAt: time.Now().UTC()}
			err := manager.Run(ctx, func(store ports.MutationStore) error {
				if err := store.UploadLogs().Create(ctx, log); err != nil {
					return err
				}
				if err := store.EnqueueUploadLog(ctx, id, nil); err != nil {
					return err
				}
				return test.stop(cancel)
			})
			if err == nil {
				t.Fatal("mutation reported success")
			}
			if err := pool.QueryRow(context.Background(), "SELECT count(*) FROM core_ingestion.upload_logs WHERE id=$1", id).Scan(&count); err != nil || count != 0 {
				t.Fatalf("owner write survived rollback: %d %v", count, err)
			}
			if err := pool.QueryRow(context.Background(), "SELECT count(*) FROM public.river_job WHERE args->>'upload_log_id'=$1", id.String()).Scan(&count); err != nil || count != 0 {
				t.Fatalf("queue write survived rollback: %d %v", count, err)
			}
		})
	}
	// Panics are not successful mutations and must release both owner and queue
	// writes before propagating to the caller/process boundary.
	panicID := uuid.New()
	func() {
		defer func() {
			if recover() != "injected panic" {
				t.Error("transaction panic was swallowed")
			}
		}()
		_ = manager.Run(ctx, func(store ports.MutationStore) error {
			log := ingestion.UploadLog{ID: panicID.String(), TenantID: uuid.NewString(), ObjectType: "transactions", Mode: ingestion.ModeCreate, Filename: "test.csv", ContentType: "text/csv", Status: ingestion.UploadLogStatusUploaded, RequestedAt: time.Now().UTC()}
			if err := store.UploadLogs().Create(ctx, log); err != nil {
				return err
			}
			if err := store.EnqueueUploadLog(ctx, panicID, nil); err != nil {
				return err
			}
			panic("injected panic")
		})
	}()
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM core_ingestion.upload_logs WHERE id=$1", panicID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("panic persisted owner row: %d %v", count, err)
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM public.river_job WHERE args->>'upload_log_id'=$1", panicID.String()).Scan(&count); err != nil || count != 0 {
		t.Fatalf("panic persisted queue row: %d %v", count, err)
	}
	scheduledID := uuid.New()
	at := time.Now().UTC().Truncate(time.Microsecond).Add(5 * time.Minute)
	if err := manager.Run(ctx, func(store ports.MutationStore) error {
		record := ingestion.DeferredIngest{ID: scheduledID.String(), TenantID: uuid.NewString(), ObjectType: "transactions", Mode: ingestion.ModeCreate, Status: ingestion.DeferredIngestStatusQueued, Payload: ingestion.MarshalPayload(map[string]any{"object_id": "scheduled"}), RequestedAt: time.Now().UTC()}
		if err := store.DeferredIngests().Create(ctx, record); err != nil {
			return err
		}
		return store.EnqueueDeferredIngest(ctx, scheduledID, &at)
	}); err != nil {
		t.Fatal(err)
	}
	var scheduledAt time.Time
	var state string
	if err := pool.QueryRow(ctx, "SELECT scheduled_at,state FROM public.river_job WHERE args->>'deferred_ingest_id'=$1", scheduledID.String()).Scan(&scheduledAt, &state); err != nil || !scheduledAt.Equal(at) || state != "scheduled" {
		t.Fatalf("scheduled enqueue changed instant/state: %s %s %v", scheduledAt, state, err)
	}
}

func TestIntegrationMissingQueueRollsBackOwnerWrites(t *testing.T) {
	ctx := context.Background()
	dsn := integrationDatabaseURL(t)
	pool := integrationPool(t, ctx, dsn)
	defer pool.Close()
	resetIntegrationDatabase(t, ctx, pool, dsn)
	producer, err := river.NewClient(riverpgxv5.New(pool), &river.Config{Schema: "missing_queue"})
	if err != nil {
		t.Fatal(err)
	}
	missingSchema := storepostgres.NewTransactionManager(pool).WithEnqueuers(riverjobs.NewRiverUploadLogEnqueuer(producer, 3, "upload_logs"), riverjobs.NewRiverDeferredIngestEnqueuer(producer, 3, "deferred_ingests"))
	for index, manager := range []storepostgres.TransactionManager{missingSchema, storepostgres.NewTransactionManager(pool)} {
		ids := &fixedIDGenerator{next: integrationUUIDSequence(20)}
		clock := fixedClock{now: time.Now().UTC()}
		uploads := NewUploadLogService(storepostgres.NewUploadLogRepository(pool), IngestService{}, manager, ids, clock, 3)
		if _, err := uploads.Create(ctx, uuid.New(), "transactions", ingestion.ModeCreate, "test.csv", "text/csv", nil); err == nil {
			t.Fatal("missing queue/producer succeeded")
		}
		deferred := NewDeferredIngestService(storepostgres.NewDeferredIngestRepository(pool), IngestService{}, manager, ids, clock, 3)
		if _, err := deferred.Create(ctx, uuid.New(), "transactions", ingestion.ModeCreate, map[string]any{}, nil); err == nil {
			t.Fatal("missing queue/producer succeeded")
		} else if index == 0 {
			var pg *pgconn.PgError
			if !errors.As(err, &pg) || pg.Code != "42P01" {
				t.Fatalf("expected actual queue SQL failure, got %v", err)
			}
		}
		var count int
		for _, table := range []string{"core_ingestion.upload_logs", "core_ingestion.deferred_ingests"} {
			if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil || count != 0 {
				t.Fatalf("partial owner commit in %s: %d %v", table, count, err)
			}
		}
	}
}

func TestIntegrationRetryHasDistinctIdempotentDelivery(t *testing.T) {
	ctx := context.Background()
	dsn := integrationDatabaseURL(t)
	pool := integrationPool(t, ctx, dsn)
	defer pool.Close()
	resetIntegrationDatabase(t, ctx, pool, dsn)
	manager := integrationQueueManager(t, pool)
	ids := &fixedIDGenerator{next: integrationUUIDSequence(20)}
	clock := fixedClock{now: time.Now().UTC()}
	uploads := NewUploadLogService(storepostgres.NewUploadLogRepository(pool), IngestService{}, manager, ids, clock, 3)
	log, err := uploads.Create(ctx, uuid.New(), "transactions", ingestion.ModeCreate, "broken.csv", "text/csv", []byte("\"unterminated"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE public.river_job SET state='running',attempt=1,attempted_at=now() WHERE args->>'upload_log_id'=$1`, log.ID); err != nil {
		t.Fatal(err)
	}
	if err := uploads.RunLog(ctx, uuid.MustParse(log.ID)); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM public.river_job WHERE args->>'upload_log_id'=$1`, log.ID).Scan(&count); err != nil || count != 2 {
		t.Fatalf("running delivery swallowed retry: %d %v", count, err)
	}
	for range 3 {
		if err := manager.Run(ctx, func(store ports.MutationStore) error { return store.RetryUploadLog(ctx, uuid.MustParse(log.ID), nil) }); err != nil {
			t.Fatal(err)
		}
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM public.river_job WHERE args->>'upload_log_id'=$1`, log.ID).Scan(&count); err != nil || count != 2 {
		t.Fatalf("same-attempt retry was not idempotent: %d %v", count, err)
	}
}

func TestMissingTransactionCannotCreateUnscheduledWork(t *testing.T) {
	if _, err := (UploadLogService{}).Create(context.Background(), uuid.New(), "transactions", ingestion.ModeCreate, "", "", nil); err == nil {
		t.Fatal("missing transaction accepted")
	}
	if _, err := (DeferredIngestService{}).Create(context.Background(), uuid.New(), "transactions", ingestion.ModeCreate, nil, nil); err == nil {
		t.Fatal("missing transaction accepted")
	}
}

func TestIntegrationQueueReadinessAndAccess(t *testing.T) {
	ctx := context.Background()
	dsn := integrationDatabaseURL(t)
	pool := integrationPool(t, ctx, dsn)
	defer pool.Close()
	resetIntegrationDatabase(t, ctx, pool, dsn)
	if err := storepostgres.ValidateQueueReadiness(ctx, pool, false); err == nil {
		t.Fatal("missing queue was ready")
	}
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM pg_catalog.pg_tables WHERE schemaname='public' AND tablename LIKE 'river_%'").Scan(&count); err != nil || count != 0 {
		t.Fatalf("readiness installed objects: %d %v", count, err)
	}
	_ = integrationQueueManager(t, pool)
	for _, worker := range []bool{false, true} {
		if err := storepostgres.ValidateQueueReadiness(ctx, pool, worker); err != nil {
			t.Fatalf("migrated queue not ready: worker=%t %v", worker, err)
		}
	}
	if _, err := pool.Exec(ctx, "INSERT INTO public.river_migration(line,version) VALUES('main',999)"); err != nil {
		t.Fatal(err)
	}
	if err := storepostgres.ValidateQueueReadiness(ctx, pool, false); err == nil {
		t.Fatal("unknown future queue lineage accepted")
	}
	if _, err := pool.Exec(ctx, "DELETE FROM public.river_migration WHERE line='main' AND version=999"); err != nil {
		t.Fatal(err)
	}
	role := "ingestion_test_role_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	quoted := pgx.Identifier{role}.Sanitize()
	if _, err := pool.Exec(ctx, "CREATE ROLE "+quoted); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		admin, err := pgxpool.New(context.Background(), dsn)
		if err != nil {
			t.Error(err)
			return
		}
		defer admin.Close()
		for _, statement := range []string{"REVOKE ALL ON ALL TABLES IN SCHEMA public FROM " + quoted, "REVOKE ALL ON ALL SEQUENCES IN SCHEMA public FROM " + quoted, "REVOKE ALL ON SCHEMA public FROM " + quoted, "DROP ROLE " + quoted} {
			if _, err := admin.Exec(context.Background(), statement); err != nil {
				t.Error(err)
			}
		}
	})
	for _, statement := range []string{"GRANT USAGE ON SCHEMA public TO " + quoted, "GRANT SELECT,INSERT,UPDATE ON public.river_job TO " + quoted, "GRANT SELECT ON public.river_migration TO " + quoted, "GRANT USAGE ON SEQUENCE public.river_job_id_seq TO " + quoted} {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	config.AfterConnect = func(ctx context.Context, connection *pgx.Conn) error {
		_, err := connection.Exec(ctx, "SET ROLE "+quoted)
		return err
	}
	limited, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer limited.Close()
	if err := storepostgres.ValidateQueueReadiness(ctx, limited, false); err != nil {
		t.Fatalf("least-privilege API role rejected: %v", err)
	}
	if err := storepostgres.ValidateQueueReadiness(ctx, limited, true); err == nil {
		t.Fatal("API role incorrectly accepted as queue worker")
	}
	if _, err := pool.Exec(ctx, "REVOKE INSERT ON public.river_job FROM "+quoted); err != nil {
		t.Fatal(err)
	}
	if err := storepostgres.ValidateQueueReadiness(ctx, limited, false); err == nil {
		t.Fatal("readiness accepted missing INSERT because SELECT was present")
	}
	if _, err := pool.Exec(ctx, "DROP TABLE public.river_queue"); err != nil {
		t.Fatal(err)
	}
	if err := storepostgres.ValidateQueueReadiness(ctx, pool, false); err == nil {
		t.Fatal("complete ledger hid a missing queue object")
	}
}
