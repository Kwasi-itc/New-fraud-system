package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/execution"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/riverjobs"
	storepostgres "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/postgres"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
)

func decisionQueueManager(t *testing.T, db *pgxpool.Pool) storepostgres.TransactionManager {
	t.Helper()
	migrator, err := rivermigrate.New(riverpgxv5.New(db), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = migrator.Migrate(context.Background(), rivermigrate.DirectionUp, nil); err != nil {
		t.Fatal(err)
	}
	client, err := river.NewClient(riverpgxv5.New(db), &river.Config{})
	if err != nil {
		t.Fatal(err)
	}
	return storepostgres.NewTransactionManager(db).WithJobEnqueuers(storepostgres.JobEnqueuers{
		Scheduled: riverjobs.NewRiverScheduledExecutionEnqueuer(client, 3, ""), Async: riverjobs.NewRiverAsyncDecisionExecutionEnqueuer(client, 3, ""), Callback: riverjobs.NewRiverAsyncDecisionExecutionCallbackEnqueuer(client, 3, ""), Workflow: riverjobs.NewRiverWorkflowExecutionEnqueuer(client, 1, ""), Screening: riverjobs.NewRiverScreeningExecutionEnqueuer(client, 1, ""), Scoring: riverjobs.NewRiverScoringRequestEnqueuer(client, 1, ""), Outbox: riverjobs.NewRiverOutboxEventEnqueuer(client, 1, ""),
	})
}

// All rows use the real owner migration lineage in a freshly generated DB.
func seedQueueResources(t *testing.T, db *pgxpool.Pool, tenant, id string) {
	t.Helper()
	statements := []string{
		`INSERT INTO core.scenarios(id,tenant_id,name,trigger_object_type,created_at,updated_at) VALUES($1,$2,'test','transactions',now(),now())`,
		`INSERT INTO core.scenario_iterations(id,scenario_id,tenant_id,version,status,created_at) VALUES($1,$1,$2,1,'committed',now())`,
		`INSERT INTO core.decisions(id,tenant_id,scenario_id,scenario_iteration_id,object_id,object_type,outcome,created_at) VALUES($1,$2,$1,$1,'txn','transactions','review',now())`,
		`INSERT INTO core.workflows(id,tenant_id,scenario_id,name,allowed_outcomes,action_type,created_at,updated_at) VALUES($1,$2,$1,'test','{}','webhook',now(),now())`,
		`INSERT INTO core.workflow_executions(id,tenant_id,workflow_id,decision_id,scenario_id,action_type,status,created_at) VALUES($1,$2,$1,$1,$1,'webhook','pending',now())`,
		`INSERT INTO core.screening_configs(id,tenant_id,scenario_id,name,allowed_outcomes,provider,created_at,updated_at) VALUES($1,$2,$1,'test','{}','test',now(),now())`,
		`INSERT INTO core.screening_executions(id,tenant_id,config_id,decision_id,scenario_id,status,created_at) VALUES($1,$2,$1,$1,$1,'pending',now())`,
		`INSERT INTO core.scoring_configs(id,tenant_id,scenario_id,name,allowed_outcomes,ruleset_ref,created_at,updated_at) VALUES($1,$2,$1,'test','{}','test',now(),now())`,
		`INSERT INTO core.scoring_requests(id,tenant_id,config_id,decision_id,scenario_id,status,created_at) VALUES($1,$2,$1,$1,$1,'pending',now())`,
		`INSERT INTO core.outbox_events(id,tenant_id,aggregate_type,aggregate_id,event_type,status,created_at) VALUES($1,$2,'decision','fixture','test','pending',now())`,
		`INSERT INTO core.scheduled_executions(id,tenant_id,scenario_id,scenario_iteration_id,status,scheduled_for,created_at) VALUES($1,$2,$1,$1,'pending',now(),now())`,
		`INSERT INTO core.async_decision_executions(id,tenant_id,object_type,status,created_at) VALUES($1,$2,'transactions','queued',now())`,
	}
	for _, sql := range statements {
		if _, err := db.Exec(context.Background(), sql, id, tenant); err != nil {
			t.Fatalf("seed queue resource (%s): %v", sql, err)
		}
	}
}

func TestIntegrationAllDecisionJobsAtomicAndTenantBound(t *testing.T) {
	ctx := context.Background()
	dsn := executionIntegrationDatabaseURL(t)
	runExecutionMetadataMigrations(t, dsn)
	db := executionIntegrationPool(t, ctx, dsn)
	defer db.Close()
	manager := decisionQueueManager(t, db)
	tenant, id := uuid.NewString(), uuid.NewString()
	seedQueueResources(t, db, tenant, id)
	at := time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)
	kinds := []ports.JobKind{ports.JobScheduled, ports.JobAsync, ports.JobCallback, ports.JobWorkflow, ports.JobScreening, ports.JobScoring, ports.JobOutbox}
	for _, kind := range kinds {
		t.Run(string(kind), func(t *testing.T) {
			job := ports.JobRequest{Kind: kind, TenantID: tenant, ResourceID: id, ScheduledAt: &at}
			// Cross-tenant lookup fails, without creating a transport job.
			wrong := job
			wrong.TenantID = uuid.NewString()
			if err := manager.Run(ctx, func(s ports.MutationStore) error { return s.Enqueue(ctx, wrong) }); err == nil {
				t.Fatal("cross-tenant enqueue succeeded")
			}
			// A later callback failure rolls back a real queue INSERT.
			injected := errors.New("after enqueue")
			if err := manager.Run(ctx, func(s ports.MutationStore) error {
				if err := s.Enqueue(ctx, job); err != nil {
					return err
				}
				return injected
			}); !errors.Is(err, injected) {
				t.Fatal(err)
			}
			var n int
			if err := db.QueryRow(ctx, "SELECT count(*) FROM public.river_job WHERE kind=$1", string(kind)).Scan(&n); err != nil || n != 0 {
				t.Fatalf("rollback jobs=%d err=%v", n, err)
			}
			if err := manager.Run(ctx, func(s ports.MutationStore) error { return s.Enqueue(ctx, job) }); err != nil {
				t.Fatal(err)
			}
			var scheduled time.Time
			var state string
			var resource string
			if err := db.QueryRow(ctx, `SELECT scheduled_at,state::text,coalesce(args->>'execution_id',args->>'request_id',args->>'event_id') FROM public.river_job WHERE kind=$1`, string(kind)).Scan(&scheduled, &state, &resource); err != nil || !scheduled.Equal(at) || state != "scheduled" || resource != id {
				t.Fatalf("job binding/schedule: %s %s %s %v", resource, state, scheduled, err)
			}
		})
	}
}

func TestIntegrationRequiredQueueFailureRollsBackOwner(t *testing.T) {
	ctx := context.Background()
	dsn := executionIntegrationDatabaseURL(t)
	runExecutionMetadataMigrations(t, dsn)
	db := executionIntegrationPool(t, ctx, dsn)
	defer db.Close()
	manager := decisionQueueManager(t, db)
	tenant, id := uuid.NewString(), uuid.NewString()
	seedQueueResources(t, db, tenant, id)
	if _, err := db.Exec(ctx, `ALTER TABLE public.river_job ADD CONSTRAINT reject_fixture_job CHECK(false)`); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []ports.JobKind{ports.JobScheduled, ports.JobAsync, ports.JobCallback, ports.JobWorkflow, ports.JobScreening, ports.JobScoring, ports.JobOutbox} {
		err := manager.Run(ctx, func(s ports.MutationStore) error {
			if err := s.AsyncDecisionExecutions().UpdateStatus(ctx, id, "completed"); err != nil {
				return err
			}
			return s.Enqueue(ctx, ports.JobRequest{Kind: kind, TenantID: tenant, ResourceID: id})
		})
		if err == nil {
			t.Fatalf("%s reported queue success", kind)
		}
		var status string
		if err := db.QueryRow(ctx, "SELECT status FROM core.async_decision_executions WHERE id=$1", id).Scan(&status); err != nil || status != "queued" {
			t.Fatalf("owner survived rollback: %s %v", status, err)
		}
	}
	// A missing producer fails before reporting successful owner persistence.
	if err := storepostgres.NewTransactionManager(db).Run(ctx, func(s ports.MutationStore) error {
		return s.Enqueue(ctx, ports.JobRequest{Kind: ports.JobAsync, TenantID: tenant, ResourceID: id})
	}); err == nil {
		t.Fatal("missing required producer succeeded")
	}
}

func TestIntegrationAsyncCompletionCallbackRollbackAndRecovery(t *testing.T) {
	ctx := context.Background()
	dsn := executionIntegrationDatabaseURL(t)
	runExecutionMetadataMigrations(t, dsn)
	db := executionIntegrationPool(t, ctx, dsn)
	defer db.Close()
	manager := decisionQueueManager(t, db)
	repo := storepostgres.NewAsyncDecisionExecutionRepository(db)
	item, err := repo.Create(ctx, execution.AsyncDecisionExecution{ID: uuid.NewString(), TenantID: uuid.NewString(), ObjectType: "transactions", Status: execution.StatusQueued, MaxAttempts: 3, RequestBody: []byte(`{"items":[]}`), CallbackURL: "https://example.test/callback", CreatedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `ALTER TABLE public.river_job ADD CONSTRAINT reject_fixture_callback CHECK(kind <> 'async_decision_execution_callback')`); err != nil {
		t.Fatal(err)
	}
	svc := ExecutionService{txManager: manager, asyncRepo: repo, clock: realClock{}}
	if err := svc.RunAsyncDecisionExecution(ctx, item.ID); err == nil {
		t.Fatal("completion succeeded without required callback")
	}
	state, err := repo.GetByID(ctx, item.TenantID, item.ID)
	if err != nil || state.Status != execution.StatusQueued || len(state.ResultBody) != 0 || state.CompletedAt != nil || state.CallbackStatus != "" {
		t.Fatalf("completion/callback rollback: %+v %v", state, err)
	}
	// A stale recovery request must neither cross tenants nor reset a newer claim.
	newer, err := repo.StartAttempt(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.RequeueCompletionPersistenceFailure(ctx, item.TenantID, item.ID, newer.AttemptCount-1); err != nil {
		t.Fatal(err)
	}
	if err := repo.RequeueCompletionPersistenceFailure(ctx, uuid.NewString(), item.ID, newer.AttemptCount); err != nil {
		t.Fatal(err)
	}
	state, err = repo.GetByID(ctx, item.TenantID, item.ID)
	if err != nil || state.Status != execution.StatusRunning {
		t.Fatalf("stale/cross-tenant recovery changed state: %+v %v", state, err)
	}
	if err := repo.RequeueCompletionPersistenceFailure(ctx, item.TenantID, item.ID, newer.AttemptCount); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `ALTER TABLE public.river_job DROP CONSTRAINT reject_fixture_callback`); err != nil {
		t.Fatal(err)
	}
	if err := svc.RunAsyncDecisionExecution(ctx, item.ID); err != nil {
		t.Fatal(err)
	}
	state, err = repo.GetByID(ctx, item.TenantID, item.ID)
	if err != nil || state.Status != execution.StatusCompleted || state.CompletedAt == nil || state.CallbackStatus != "pending" {
		t.Fatalf("completion missing: %+v %v", state, err)
	}
	if err := repo.RequeueCompletionPersistenceFailure(ctx, item.TenantID, item.ID, state.AttemptCount); err != nil {
		t.Fatal(err)
	}
	state, err = repo.GetByID(ctx, item.TenantID, item.ID)
	if err != nil || state.Status != execution.StatusCompleted {
		t.Fatalf("recovery overwrote terminal commit: %+v %v", state, err)
	}
	var jobs int
	if err := db.QueryRow(ctx, "SELECT count(*) FROM public.river_job WHERE kind='async_decision_execution_callback'").Scan(&jobs); err != nil || jobs != 1 {
		t.Fatalf("callback jobs=%d %v", jobs, err)
	}
}

func TestIntegrationDecisionQueueCancellationAndPanicRollback(t *testing.T) {
	dsn := executionIntegrationDatabaseURL(t)
	runExecutionMetadataMigrations(t, dsn)
	db := executionIntegrationPool(t, context.Background(), dsn)
	defer db.Close()
	manager := decisionQueueManager(t, db)
	item, err := storepostgres.NewAsyncDecisionExecutionRepository(db).Create(context.Background(), execution.AsyncDecisionExecution{ID: uuid.NewString(), TenantID: uuid.NewString(), ObjectType: "transactions", Status: execution.StatusQueued, MaxAttempts: 3, RequestBody: []byte(`{"items":[]}`), CreatedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"cancel", "panic"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			func() {
				defer func() {
					if mode == "panic" && recover() != "injected panic" {
						t.Error("transaction panic swallowed")
					}
				}()
				err := manager.Run(ctx, func(s ports.MutationStore) error {
					if err := s.AsyncDecisionExecutions().UpdateStatus(ctx, item.ID, execution.StatusCompleted); err != nil {
						return err
					}
					if err := s.Enqueue(ctx, ports.JobRequest{Kind: ports.JobAsync, TenantID: item.TenantID, ResourceID: item.ID}); err != nil {
						return err
					}
					if mode == "panic" {
						panic("injected panic")
					}
					cancel()
					return nil
				})
				if mode == "cancel" && !errors.Is(err, context.Canceled) {
					t.Fatalf("cancel returned %v", err)
				}
			}()
			var state string
			var count int
			if err := db.QueryRow(context.Background(), "SELECT status FROM core.async_decision_executions WHERE id=$1", item.ID).Scan(&state); err != nil || state != "queued" {
				t.Fatalf("owner rollback: %s %v", state, err)
			}
			if err := db.QueryRow(context.Background(), "SELECT count(*) FROM public.river_job").Scan(&count); err != nil || count != 0 {
				t.Fatalf("queue rollback: %d %v", count, err)
			}
		})
	}
}
