package mysql

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/execution"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/mysqljobs"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/workers"
	"github.com/google/uuid"
)

func executionFixture(t *testing.T) (platformFixture, *ExecutionStore, uuid.UUID, uuid.UUID, uuid.UUID) {
	t.Helper()
	f := scenarioFoundationDB(t, true, true, true, true, true, true, true, true, true, true)
	tenant, parent := uuid.New(), uuid.New()
	ctx := context.Background()
	if err := scenarioInsert(ctx, f.db, f.database, tenant, parent, "Execution fixture"); err != nil {
		t.Fatal(err)
	}
	a := authoringStore(t, f, tenant, 500, 2<<20)
	iteration := createAuthoringDraft(t, a, tenant, parent)
	if err := a.Mutate(ctx, func(r AuthoringRepositories) error {
		if _, err := r.Rules.Create(ctx, authoringRule(tenant, iteration.ID, 1)); err != nil {
			return err
		}
		_, err := r.Iterations.Commit(ctx, tenant.String(), parent.String(), iteration.ID, time.Now().UTC().Truncate(time.Microsecond))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Publish(ctx, PublicationInput{parent, uuid.MustParse(iteration.ID), uuid.New(), uuid.New()}, func(context.Context, *sql.Tx, PublicationSnapshot) error { return nil }); err != nil {
		t.Fatal(err)
	}
	return f, executionTestStore(t, f, tenant), tenant, parent, uuid.MustParse(iteration.ID)
}
func executionTestStore(t *testing.T, f platformFixture, tenant uuid.UUID) *ExecutionStore {
	t.Helper()
	queues := map[ports.JobKind]DeliveryQueuePolicy{}
	for _, kind := range []ports.JobKind{ports.JobScheduled, ports.JobAsync, ports.JobCallback} {
		queues[kind] = DeliveryQueuePolicy{Name: string(kind), MaxAttempts: 3}
	}
	s, err := NewExecutionStore(f.db, f.database, tenant, 500, 2<<20, func(_ context.Context, tx *sql.Tx, actual uuid.UUID, action ExecutionAction) error {
		if tx == nil || actual != tenant || !validExecutionAction(action) {
			return ErrDecisionAuthorizationDenied
		}
		return nil // Fixture authority only; no production provider is activated.
	}, ExecutionPolicy{Queues: queues, CallbackMaxAttempts: 3})
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func scheduledInput(tenant, parent, iteration uuid.UUID) execution.ScheduledExecution {
	at := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Second)
	return execution.ScheduledExecution{ID: uuid.NewString(), TenantID: tenant.String(), ScenarioID: parent.String(), ScenarioIterationID: iteration.String(), Source: execution.SourceManual, Status: execution.StatusPending, IdempotencyKey: uuid.NewString(), MaxAttempts: 3, ScheduledFor: at, CreatedAt: at, RequestBody: []byte(`{"n":9007199254740993}`)}
}
func asyncInput(tenant, parent uuid.UUID) execution.AsyncDecisionExecution {
	return execution.AsyncDecisionExecution{ID: uuid.NewString(), TenantID: tenant.String(), ScenarioID: parent.String(), ObjectType: "wallets", Status: execution.StatusQueued, IdempotencyKey: uuid.NewString(), MaxAttempts: 3, CallbackURL: "https://callback.invalid/decision", RequestBody: []byte(`{"wallet":"exact"}`), CreatedAt: time.Now().UTC().Truncate(time.Microsecond)}
}
func executionCount(t *testing.T, f platformFixture, table string) int {
	t.Helper()
	schema, _ := mysqlstore.Identifier(f.database)
	var n int
	if err := f.db.QueryRow("SELECT COUNT(*) FROM " + schema + "." + table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// This establishes a genuine SQL lease ONLY in the disposable fixture. It does
// not claim production claim/heartbeat/rescue integration is implemented.
func executionTestLease(t *testing.T, f platformFixture, tenant, resource uuid.UUID, kind ports.JobKind, attempt, generation int) ExecutionLease {
	t.Helper()
	schema, _ := mysqlstore.Identifier(f.database)
	v := ExecutionLease{Token: uuid.New(), Identity: mysqljobs.Identity{Version: 1, Kind: kind, TenantID: tenant, ResourceID: resource, DomainAttempt: attempt, RedriveGeneration: generation}, TransportAttempt: 1}
	var id mysqlstore.UUID
	if err := f.db.QueryRow("SELECT id FROM "+schema+".decision_engine_execution_job_receipts WHERE tenant_id=? AND kind=? AND resource_id=? AND domain_attempt=? AND redrive_generation=?", mysqlstore.UUID(tenant), string(kind), mysqlstore.UUID(resource), attempt, generation).Scan(&id); err != nil {
		t.Fatal(err)
	}
	v.ID = uuid.UUID(id)
	result, err := f.db.Exec("UPDATE "+schema+".decision_engine_execution_job_deliveries SET state='running',attempt=1,lease_token=?,lease_until=UTC_TIMESTAMP(6)+INTERVAL 60 SECOND WHERE tenant_id=? AND id=? AND state='available' AND available_at<=UTC_TIMESTAMP(6)", mysqlstore.UUID(v.Token), mysqlstore.UUID(tenant), id)
	if err := executionChanged(result, err); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestMySQLExecutionReplayIsolationBoundsAndRequiredJobs(t *testing.T) {
	f, s, tenant, parent, iteration := executionFixture(t)
	ctx := context.Background()
	v := scheduledInput(tenant, parent, iteration)
	var winner ScheduledCreation
	if err := s.Run(ctx, ExecutionRequest, func(r ExecutionRepositories) error {
		var err error
		winner, err = r.Scheduled.Create(ctx, v)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if winner.Replay || winner.Record.ID != v.ID {
		t.Fatal(winner)
	}
	for _, key := range []string{v.IdempotencyKey, v.IdempotencyKey + " ", strings.ToUpper(v.IdempotencyKey), "", ""} {
		x := v
		x.ID, x.IdempotencyKey, x.RequestBody = uuid.NewString(), key, []byte(`{"changed":true}`)
		if err := s.Run(ctx, ExecutionRequest, func(r ExecutionRepositories) error {
			out, err := r.Scheduled.Create(ctx, x)
			if err == nil && key == v.IdempotencyKey && (!out.Replay || out.Record.ID != v.ID || strings.Contains(string(out.Record.RequestBody), "changed")) {
				t.Error("first request did not win", out)
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	if executionCount(t, f, "decision_engine_scheduled_executions") != 5 || executionCount(t, f, "decision_engine_execution_job_deliveries") != 5 {
		t.Fatal("wrong exact-key, empty-key or required-job semantics")
	}
	t.Run("same key concurrent first writers", func(t *testing.T) {
		key := uuid.NewString()
		var wg sync.WaitGroup
		results := make(chan ScheduledCreation, 4)
		errs := make(chan error, 4)
		for i := 0; i < 4; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				x := scheduledInput(tenant, parent, iteration)
				x.IdempotencyKey = key
				var got ScheduledCreation
				err := s.Run(ctx, ExecutionRequest, func(r ExecutionRepositories) error { var err error; got, err = r.Scheduled.Create(ctx, x); return err })
				errs <- err
				results <- got
			}()
		}
		wg.Wait()
		close(errs)
		close(results)
		for err := range errs {
			if err != nil {
				t.Fatal(err)
			}
		}
		var id string
		for got := range results {
			if id != "" && got.Record.ID != id {
				t.Fatal("duplicate winner")
			}
			id = got.Record.ID
		}
		if executionCount(t, f, "decision_engine_scheduled_executions") != 6 || executionCount(t, f, "decision_engine_execution_job_deliveries") != 6 {
			t.Fatal("concurrent duplicate jobs")
		}
	})
	t.Run("recurring slot and conflicting identities", func(t *testing.T) {
		x := scheduledInput(tenant, parent, iteration)
		x.Source = execution.SourceRecurring
		if err := s.Run(ctx, ExecutionRequest, func(r ExecutionRepositories) error { _, err := r.Scheduled.Create(ctx, x); return err }); err != nil {
			t.Fatal(err)
		}
		y := x
		y.ID, y.IdempotencyKey = uuid.NewString(), uuid.NewString()
		if err := s.Run(ctx, ExecutionRequest, func(r ExecutionRepositories) error {
			got, err := r.Scheduled.Create(ctx, y)
			if err == nil && (!got.Replay || got.Record.ID != x.ID) {
				t.Error("slot dedup")
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}
		y.IdempotencyKey = v.IdempotencyKey
		if err := s.Run(ctx, ExecutionRequest, func(r ExecutionRepositories) error { _, err := r.Scheduled.Create(ctx, y); return err }); !errors.Is(err, ErrExecutionReplayConflict) {
			t.Fatal("two keys resolved different owners", err)
		}
	})
	t.Run("authorization, foreign tenant and ignored queue error rollback", func(t *testing.T) {
		if err := s.Run(ctx, ExecutionRead, func(r ExecutionRepositories) error {
			_, err := r.Scheduled.GetByID(ctx, uuid.NewString(), parent.String(), v.ID)
			return err
		}); !errors.Is(err, ErrTenantScope) {
			t.Fatal(err)
		}
		if err := s.Run(ctx, ExecutionRun, func(r ExecutionRepositories) error { _, _ = r.Scheduled.Create(ctx, v); return nil }); !errors.Is(err, ErrDecisionAuthorizationDenied) {
			t.Fatal("ignored authority error committed", err)
		}
		x := scheduledInput(tenant, parent, iteration)
		x.ScenarioID = uuid.NewString()
		if err := s.Run(ctx, ExecutionRequest, func(r ExecutionRepositories) error { _, _ = r.Scheduled.Create(ctx, x); return nil }); err == nil {
			t.Fatal("foreign owner accepted")
		}
		schema, _ := mysqlstore.Identifier(f.database)
		before := executionCount(t, f, "decision_engine_scheduled_executions")
		if _, err := f.db.Exec("CREATE TRIGGER " + schema + ".fixture_execution_failure BEFORE INSERT ON " + schema + ".decision_engine_execution_job_deliveries FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='required queue unavailable'"); err != nil {
			t.Fatal(err)
		}
		err := s.Run(ctx, ExecutionRequest, func(r ExecutionRepositories) error {
			_, _ = r.Scheduled.Create(ctx, scheduledInput(tenant, parent, iteration))
			return nil
		})
		if _, dropErr := f.db.Exec("DROP TRIGGER " + schema + ".fixture_execution_failure"); dropErr != nil {
			t.Fatal(dropErr)
		}
		if err == nil || executionCount(t, f, "decision_engine_scheduled_executions") != before || executionCount(t, f, "decision_engine_execution_job_receipts") != before {
			t.Fatal("partial required job commit", err)
		}
	})
	t.Run("async replay and optional scenario ownership", func(t *testing.T) {
		x := asyncInput(tenant, parent)
		if err := s.Run(ctx, ExecutionRequest, func(r ExecutionRepositories) error { _, err := r.Async.Create(ctx, x); return err }); err != nil {
			t.Fatal(err)
		}
		y := x
		y.ID, y.RequestBody = uuid.NewString(), []byte(`{"changed":true}`)
		if err := s.Run(ctx, ExecutionRequest, func(r ExecutionRepositories) error {
			got, err := r.Async.Create(ctx, y)
			if err == nil && (!got.Replay || got.Record.ID != x.ID || got.Record.ResultBody != nil) {
				t.Error(got)
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}
		orphanParent := uuid.New()
		if err := scenarioInsert(ctx, f.db, f.database, tenant, orphanParent, "Optional parent"); err != nil {
			t.Fatal(err)
		}
		y = asyncInput(tenant, orphanParent)
		if err := s.Run(ctx, ExecutionRequest, func(r ExecutionRepositories) error { _, err := r.Async.Create(ctx, y); return err }); err != nil {
			t.Fatal(err)
		}
		schema, _ := mysqlstore.Identifier(f.database)
		if _, err := f.db.Exec("DELETE FROM "+schema+".decision_engine_scenarios WHERE tenant_id=? AND id=?", mysqlstore.UUID(tenant), mysqlstore.UUID(orphanParent)); err != nil {
			t.Fatal(err)
		}
		if err := s.Run(ctx, ExecutionRead, func(r ExecutionRepositories) error {
			got, err := r.Async.GetByID(ctx, tenant.String(), y.ID)
			if err == nil && (got.ScenarioID != "" || got.TenantID != tenant.String()) {
				t.Error("optional link deletion changed owner", got)
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("tenant-scoped key and bounded projection", func(t *testing.T) {
		other, otherParent := uuid.New(), uuid.New()
		if err := scenarioInsert(ctx, f.db, f.database, other, otherParent, "Other tenant"); err != nil {
			t.Fatal(err)
		}
		a := authoringStore(t, f, other, 500, 2<<20)
		draft := createAuthoringDraft(t, a, other, otherParent)
		x := scheduledInput(other, otherParent, uuid.MustParse(draft.ID))
		x.IdempotencyKey = v.IdempotencyKey
		otherStore := executionTestStore(t, f, other)
		if err := otherStore.Run(ctx, ExecutionRequest, func(r ExecutionRepositories) error {
			got, err := r.Scheduled.Create(ctx, x)
			if err == nil && (got.Replay || got.Record.ID == v.ID || got.Record.TenantID != other.String()) {
				t.Error("cross-tenant key collision", got)
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if err := s.Run(ctx, ExecutionRead, func(r ExecutionRepositories) error {
			_, err := r.Scheduled.GetByID(ctx, tenant.String(), parent.String(), x.ID)
			return err
		}); !errors.Is(err, sql.ErrNoRows) {
			t.Fatal("foreign row visible", err)
		}
		// Budget failures return an error rather than truncating a successful
		// response or returning the first arbitrary subset of history.
		bounded := *s
		config := *s.configuration
		config.maxRows = 1
		bounded.configuration = &config
		if err := bounded.Run(ctx, ExecutionRead, func(r ExecutionRepositories) error {
			_, err := r.Scheduled.ListByScenario(ctx, tenant.String(), parent.String())
			return err
		}); !errors.Is(err, ErrProjectionLimit) {
			t.Fatal("unbounded history accepted", err)
		}
		config.maxBytes = 1
		if err := bounded.Run(ctx, ExecutionRead, func(r ExecutionRepositories) error {
			_, err := r.Scheduled.GetByID(ctx, tenant.String(), parent.String(), v.ID)
			return err
		}); !errors.Is(err, ErrProjectionLimit) {
			t.Fatal("oversize projection accepted", err)
		}
	})
}

func TestMySQLExecutionLeaseRetryCallbackAndRedrive(t *testing.T) {
	f, s, tenant, parent, iteration := executionFixture(t)
	ctx := context.Background()
	schema, _ := mysqlstore.Identifier(f.database)
	v := scheduledInput(tenant, parent, iteration)
	if err := s.Run(ctx, ExecutionRequest, func(r ExecutionRepositories) error { _, err := r.Scheduled.Create(ctx, v); return err }); err != nil {
		t.Fatal(err)
	}
	lease := executionTestLease(t, f, tenant, uuid.MustParse(v.ID), ports.JobScheduled, 1, 0)
	for i := 0; i < 2; i++ { // Transport resume does not consume a domain retry.
		if err := s.Run(ctx, ExecutionRun, func(r ExecutionRepositories) error {
			got, err := r.Scheduled.Start(ctx, lease)
			if err == nil && got.AttemptCount != 1 {
				t.Error(got)
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	bad := lease
	bad.Token = uuid.New()
	if err := s.Run(ctx, ExecutionRun, func(r ExecutionRepositories) error { return r.Scheduled.Complete(ctx, bad) }); !errors.Is(err, ErrExecutionLease) {
		t.Fatal("stale token accepted", err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	if err := s.Run(ctx, ExecutionRun, func(r ExecutionRepositories) error { return r.Scheduled.Fail(ctx, lease, &now, "retry") }); err != nil {
		t.Fatal(err)
	}
	lease2 := executionTestLease(t, f, tenant, uuid.MustParse(v.ID), ports.JobScheduled, 2, 0)
	if err := s.Run(ctx, ExecutionRun, func(r ExecutionRepositories) error { _, err := r.Scheduled.Start(ctx, lease2); return err }); err != nil {
		t.Fatal(err)
	}
	if err := s.Run(ctx, ExecutionRun, func(r ExecutionRepositories) error { return r.Scheduled.Fail(ctx, lease2, nil, "terminal") }); err != nil {
		t.Fatal(err)
	}
	if err := s.Run(ctx, ExecutionRedrive, func(r ExecutionRepositories) error {
		got, err := r.Scheduled.Redrive(ctx, uuid.MustParse(v.ID), 0)
		if err == nil && (got.Generation != 1 || got.AttemptCount != 0) {
			t.Error(got)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Run(ctx, ExecutionRedrive, func(r ExecutionRepositories) error {
		_, err := r.Scheduled.Redrive(ctx, uuid.MustParse(v.ID), 0)
		return err
	}); !errors.Is(err, ErrExecutionState) {
		t.Fatal("stale redrive accepted", err)
	}
	newLease := executionTestLease(t, f, tenant, uuid.MustParse(v.ID), ports.JobScheduled, 1, 1)
	if err := s.Run(ctx, ExecutionRun, func(r ExecutionRepositories) error {
		_, err := r.Scheduled.Start(ctx, newLease)
		if err != nil {
			return err
		}
		return r.Scheduled.Complete(ctx, newLease)
	}); err != nil {
		t.Fatal(err)
	}
	x := asyncInput(tenant, parent)
	if err := s.Run(ctx, ExecutionRequest, func(r ExecutionRepositories) error { _, err := r.Async.Create(ctx, x); return err }); err != nil {
		t.Fatal(err)
	}
	al := executionTestLease(t, f, tenant, uuid.MustParse(x.ID), ports.JobAsync, 1, 0)
	if err := s.Run(ctx, ExecutionRun, func(r ExecutionRepositories) error { _, err := r.Async.Start(ctx, al); return err }); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec("CREATE TRIGGER " + schema + ".fixture_callback_failure BEFORE INSERT ON " + schema + ".decision_engine_execution_job_deliveries FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='callback queue unavailable'"); err != nil {
		t.Fatal(err)
	}
	err := s.Run(ctx, ExecutionRun, func(r ExecutionRepositories) error { _ = r.Async.Complete(ctx, al, []byte(`null`)); return nil })
	if _, dropErr := f.db.Exec("DROP TRIGGER " + schema + ".fixture_callback_failure"); dropErr != nil {
		t.Fatal(dropErr)
	}
	if err == nil {
		t.Fatal("completion without callback job committed")
	}
	if err := s.Run(ctx, ExecutionRead, func(r ExecutionRepositories) error {
		got, err := r.Async.GetByID(ctx, tenant.String(), x.ID)
		if err == nil && (got.Status != execution.StatusRunning || got.ResultBody != nil) {
			t.Error("partial completion", got)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Run(ctx, ExecutionRun, func(r ExecutionRepositories) error { return r.Async.Complete(ctx, al, []byte(`null`)) }); err != nil {
		t.Fatal(err)
	}
	cb := executionTestLease(t, f, tenant, uuid.MustParse(x.ID), ports.JobCallback, 1, 0)
	if err := s.Run(ctx, ExecutionRun, func(r ExecutionRepositories) error {
		got, err := r.Async.StartCallback(ctx, cb)
		if err == nil && (got.CallbackAttemptCount != 1 || got.AttemptCount != 1 || string(got.ResultBody) != "null") {
			t.Error(got)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Run(ctx, ExecutionRun, func(r ExecutionRepositories) error {
		return r.Async.CallbackOutcome(ctx, cb, false, "transport failed", &now)
	}); err != nil {
		t.Fatal(err)
	}
	cb2 := executionTestLease(t, f, tenant, uuid.MustParse(x.ID), ports.JobCallback, 2, 0)
	if err := s.Run(ctx, ExecutionRun, func(r ExecutionRepositories) error {
		_, err := r.Async.StartCallback(ctx, cb2)
		if err != nil {
			return err
		}
		return r.Async.CallbackOutcome(ctx, cb2, true, "", nil)
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Run(ctx, ExecutionRead, func(r ExecutionRepositories) error {
		got, err := r.Async.GetByID(ctx, tenant.String(), x.ID)
		if err == nil && (got.CallbackAttemptCount != 2 || got.AttemptCount != 1 || got.CallbackStatus != "sent") {
			t.Error("independent counters", got)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	// A lease expiring after Start cannot commit a completion.
	y := asyncInput(tenant, parent)
	if err := s.Run(ctx, ExecutionRequest, func(r ExecutionRepositories) error { _, err := r.Async.Create(ctx, y); return err }); err != nil {
		t.Fatal(err)
	}
	yl := executionTestLease(t, f, tenant, uuid.MustParse(y.ID), ports.JobAsync, 1, 0)
	if _, err := f.db.Exec("UPDATE "+schema+".decision_engine_execution_job_deliveries SET lease_until=UTC_TIMESTAMP(6)-INTERVAL 1 SECOND WHERE id=?", mysqlstore.UUID(yl.ID)); err != nil {
		t.Fatal(err)
	}
	if err := s.Run(ctx, ExecutionRun, func(r ExecutionRepositories) error { _, err := r.Async.Start(ctx, yl); return err }); !errors.Is(err, ErrExecutionLease) {
		t.Fatal("expired lease accepted", err)
	}
	t.Run("expiry during outcome rolls back owner and callback job", func(t *testing.T) {
		z := asyncInput(tenant, parent)
		if err := s.Run(ctx, ExecutionRequest, func(r ExecutionRepositories) error { _, err := r.Async.Create(ctx, z); return err }); err != nil {
			t.Fatal(err)
		}
		zl := executionTestLease(t, f, tenant, uuid.MustParse(z.ID), ports.JobAsync, 1, 0)
		if err := s.Run(ctx, ExecutionRun, func(r ExecutionRepositories) error { _, err := r.Async.Start(ctx, zl); return err }); err != nil {
			t.Fatal(err)
		}
		before := executionCount(t, f, "decision_engine_execution_job_receipts")
		if _, err := f.db.Exec("CREATE TRIGGER " + schema + ".fixture_outcome_expiry AFTER UPDATE ON " + schema + ".decision_engine_async_executions FOR EACH ROW BEGIN IF NEW.status=_binary'completed' THEN DO SLEEP(0.7); END IF; END"); err != nil {
			t.Fatal(err)
		}
		if _, err := f.db.Exec("UPDATE "+schema+".decision_engine_execution_job_deliveries SET lease_until=UTC_TIMESTAMP(6)+INTERVAL 500000 MICROSECOND WHERE id=?", mysqlstore.UUID(zl.ID)); err != nil {
			t.Fatal(err)
		}
		err := s.Run(ctx, ExecutionRun, func(r ExecutionRepositories) error { return r.Async.Complete(ctx, zl, []byte(`{"done":true}`)) })
		if _, dropErr := f.db.Exec("DROP TRIGGER " + schema + ".fixture_outcome_expiry"); dropErr != nil {
			t.Fatal(dropErr)
		}
		if !errors.Is(err, ErrExecutionLease) || executionCount(t, f, "decision_engine_execution_job_receipts") != before {
			t.Fatal("expired outcome committed callback", err)
		}
		if err := s.Run(ctx, ExecutionRead, func(r ExecutionRepositories) error {
			got, err := r.Async.GetByID(ctx, tenant.String(), z.ID)
			if err == nil && (got.Status != execution.StatusRunning || got.ResultBody != nil || got.CallbackStatus != "") {
				t.Error("expired outcome committed owner", got)
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("async redrive cancels old callback and fences old generation", func(t *testing.T) {
		z := asyncInput(tenant, parent)
		if err := s.Run(ctx, ExecutionRequest, func(r ExecutionRepositories) error { _, err := r.Async.Create(ctx, z); return err }); err != nil {
			t.Fatal(err)
		}
		zl := executionTestLease(t, f, tenant, uuid.MustParse(z.ID), ports.JobAsync, 1, 0)
		if err := s.Run(ctx, ExecutionRun, func(r ExecutionRepositories) error {
			_, err := r.Async.Start(ctx, zl)
			if err != nil {
				return err
			}
			return r.Async.Fail(ctx, zl, nil, "terminal", nil)
		}); err != nil {
			t.Fatal(err)
		}
		oldCallback := executionTestLease(t, f, tenant, uuid.MustParse(z.ID), ports.JobCallback, 1, 0)
		if err := s.Run(ctx, ExecutionRun, func(r ExecutionRepositories) error { _, err := r.Async.StartCallback(ctx, oldCallback); return err }); err != nil {
			t.Fatal(err)
		}
		if err := s.Run(ctx, ExecutionRedrive, func(r ExecutionRepositories) error {
			got, err := r.Async.Redrive(ctx, uuid.MustParse(z.ID), 0)
			if err == nil && (got.Generation != 1 || got.CallbackAttemptCount != 0 || got.AttemptCount != 0 || got.ResultBody != nil) {
				t.Error(got)
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if err := s.Run(ctx, ExecutionRun, func(r ExecutionRepositories) error { return r.Async.CallbackOutcome(ctx, oldCallback, true, "", nil) }); !errors.Is(err, ErrExecutionState) {
			t.Fatal("old callback generation committed", err)
		}
		var state string
		if err := f.db.QueryRow("SELECT state FROM "+schema+".decision_engine_execution_job_deliveries WHERE tenant_id=? AND id=?", mysqlstore.UUID(tenant), mysqlstore.UUID(oldCallback.ID)).Scan(&state); err != nil || state != "cancelled" {
			t.Fatal("old callback not cancelled", state, err)
		}
	})
}

func TestMySQLExecutionScheduleAtomicWatermarkAndPolicies(t *testing.T) {
	f, s, tenant, parent, iteration := executionFixture(t)
	ctx := context.Background()
	schema, _ := mysqlstore.Identifier(f.database)
	anchor := time.Now().UTC().Truncate(time.Microsecond).Add(-3500 * time.Millisecond)
	v := ExecutionScheduleInput{ID: uuid.New(), ScenarioID: parent, ScenarioIterationID: iteration, StartAt: anchor, Interval: time.Second, Policy: workers.CatchUp, MaxAttempts: 3, RequestBody: []byte(`{}`), Active: true}
	if err := s.Run(ctx, ExecutionScheduleAdmin, func(r ExecutionRepositories) error { _, err := r.Schedules.Create(ctx, v); return err }); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec("CREATE TRIGGER " + schema + ".fixture_schedule_failure BEFORE INSERT ON " + schema + ".decision_engine_execution_job_deliveries FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='schedule queue unavailable'"); err != nil {
		t.Fatal(err)
	}
	err := s.Run(ctx, ExecutionScheduleTick, func(r ExecutionRepositories) error { _, _ = r.Schedules.Materialize(ctx, v.ID, 2); return nil })
	if _, dropErr := f.db.Exec("DROP TRIGGER " + schema + ".fixture_schedule_failure"); dropErr != nil {
		t.Fatal(dropErr)
	}
	if err == nil || executionCount(t, f, "decision_engine_scheduled_executions") != 0 || executionCount(t, f, "decision_engine_schedule_occurrences") != 0 {
		t.Fatal("partial scheduling committed", err)
	}
	if err := s.Run(ctx, ExecutionRead, func(r ExecutionRepositories) error {
		got, err := r.Schedules.Get(ctx, v.ID)
		if err == nil && (!got.NextAt.Equal(anchor) || got.WatermarkVersion != 0) {
			t.Error("watermark advanced without jobs", got)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var first MaterializedExecutions
	if err := s.Run(ctx, ExecutionScheduleTick, func(r ExecutionRepositories) error {
		due, err := r.Schedules.Due(ctx, nil, 10)
		if err != nil {
			return err
		}
		if len(due) != 1 || due[0].ID != v.ID {
			t.Error(due)
		}
		first, err = r.Schedules.Materialize(ctx, v.ID, 2)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if len(first.Records) != 2 || !first.MoreDue || first.WatermarkVersion != 1 || !first.NextAt.Equal(anchor.Add(2*time.Second)) {
		t.Fatal(first)
	}
	if _, err := f.db.Exec("INSERT INTO "+schema+".decision_engine_schedule_occurrences(tenant_id,schedule_id,scheduled_for,execution_id,created_at) VALUES(?,?,?,?,UTC_TIMESTAMP(6))", mysqlstore.UUID(tenant), mysqlstore.UUID(v.ID), mysqlstore.DateTime{Time: first.Records[0].ScheduledFor.Add(time.Microsecond)}, mysqlstore.UUID(uuid.MustParse(first.Records[0].ID))); err == nil {
		t.Fatal("same-tenant execution accepted for the wrong occurrence instant")
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- s.Run(ctx, ExecutionScheduleTick, func(r ExecutionRepositories) error { _, err := r.Schedules.Materialize(ctx, v.ID, 10); return err })
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	n := executionCount(t, f, "decision_engine_scheduled_executions")
	if n < 4 || executionCount(t, f, "decision_engine_schedule_occurrences") != n || executionCount(t, f, "decision_engine_execution_job_deliveries") != n {
		t.Fatal("concurrent tick lost or duplicated work", n)
	}
	if err := s.Run(ctx, ExecutionScheduleAdmin, func(r ExecutionRepositories) error {
		got, err := r.Schedules.Get(ctx, v.ID)
		if err != nil {
			return err
		}
		return r.Schedules.SetActive(ctx, v.ID, got.WatermarkVersion, false)
	}); err != nil {
		t.Fatal(err)
	}
	for _, policy := range []workers.MissedRunPolicy{workers.Coalesce, workers.SkipLate} {
		x := v
		x.ID, x.Policy, x.StartAt, x.Interval = uuid.New(), policy, anchor.Add(-20*time.Second), time.Hour
		if err := s.Run(ctx, ExecutionScheduleAdmin, func(r ExecutionRepositories) error { _, err := r.Schedules.Create(ctx, x); return err }); err != nil {
			t.Fatal(err)
		}
		if err := s.Run(ctx, ExecutionScheduleTick, func(r ExecutionRepositories) error {
			got, err := r.Schedules.Materialize(ctx, x.ID, 10)
			if err == nil {
				if policy == workers.Coalesce && len(got.Records) != 1 {
					t.Error(got)
				}
				if policy == workers.SkipLate && (len(got.Records) != 0 || got.Skipped != 1) {
					t.Error(got)
				}
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if err := s.Run(ctx, ExecutionScheduleAdmin, func(r ExecutionRepositories) error {
			got, err := r.Schedules.Get(ctx, x.ID)
			if err != nil {
				return err
			}
			return r.Schedules.SetActive(ctx, x.ID, got.WatermarkVersion, false)
		}); err != nil {
			t.Fatal(err)
		}
	}
	var plan string
	if err := f.db.QueryRow("EXPLAIN ANALYZE SELECT next_at,id FROM "+schema+".decision_engine_execution_schedules WHERE tenant_id=? AND active=1 AND next_at<=UTC_TIMESTAMP(6) ORDER BY next_at,id LIMIT 10", mysqlstore.UUID(tenant)).Scan(&plan); err != nil {
		t.Fatal(err)
	}
	t.Log(plan)
	if strings.Contains(strings.ToLower(plan), "table scan") || !strings.Contains(plan, "de_execution_schedule_due") {
		t.Fatal("unbounded due scan", plan)
	}
}
