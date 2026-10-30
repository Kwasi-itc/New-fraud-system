package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/execution"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/workers"
	"github.com/google/uuid"
)

// Fixture-only time travel; production rescue always checks database time and
// the observed expiry under lock. No application database or lease is touched.
func expireExecutionFixture(t *testing.T, f platformFixture, lease ExecutionLease) {
	t.Helper()
	schema, _ := mysqlstore.Identifier(f.database)
	result, err := f.db.Exec("UPDATE "+schema+".decision_engine_execution_job_deliveries SET lease_until=UTC_TIMESTAMP(6)-INTERVAL 1 SECOND WHERE tenant_id=? AND id=? AND state='running' AND lease_token=?", mysqlstore.UUID(lease.Identity.TenantID), mysqlstore.UUID(lease.ID), mysqlstore.UUID(lease.Token))
	if err := executionChanged(result, err); err != nil {
		t.Fatal(err)
	}
}

func TestMySQLExecutionRescueRetryExhaustionAndCallbackAtomicity(t *testing.T) {
	ctx := context.Background()
	for _, kind := range []ports.JobKind{ports.JobScheduled, ports.JobAsync, ports.JobCallback} {
		t.Run(string(kind), func(t *testing.T) {
			f, owner, tenant, parent, iteration := executionFixture(t)
			schema, _ := mysqlstore.Identifier(f.database)
			scheduled, async := scheduledInput(tenant, parent, iteration), asyncInput(tenant, parent)
			if err := owner.Run(ctx, ExecutionRequest, func(r ExecutionRepositories) error {
				if kind == ports.JobScheduled {
					_, err := r.Scheduled.Create(ctx, scheduled)
					return err
				}
				_, err := r.Async.Create(ctx, async)
				return err
			}); err != nil {
				t.Fatal(err)
			}
			if kind == ports.JobCallback {
				api := executionTransportFixture(t, f, tenant, ports.JobAsync)
				lease, err := api.Claim(ctx)
				if err != nil || lease == nil {
					t.Fatal(lease, err)
				}
				if err := owner.Run(ctx, ExecutionRun, func(r ExecutionRepositories) error {
					if _, err := r.Async.Start(ctx, *lease); err != nil {
						return err
					}
					return r.Async.Complete(ctx, *lease, []byte(`{"completed":true}`))
				}); err != nil {
					t.Fatal(err)
				}
			}
			worker := executionTransportFixture(t, f, tenant, kind)
			for attempt := 1; attempt <= 3; attempt++ {
				lease, err := worker.Claim(ctx)
				if err != nil || lease == nil || lease.TransportAttempt != attempt {
					t.Fatal(lease, err)
				}
				if err := owner.Run(ctx, ExecutionRun, func(r ExecutionRepositories) error {
					if kind == ports.JobScheduled {
						got, err := r.Scheduled.Start(ctx, *lease)
						if err == nil && got.AttemptCount != 1 {
							t.Error("rescue consumed domain retry", got)
						}
						return err
					}
					if kind == ports.JobAsync {
						got, err := r.Async.Start(ctx, *lease)
						if err == nil && got.AttemptCount != 1 {
							t.Error("rescue consumed domain retry", got)
						}
						return err
					}
					got, err := r.Async.StartCallback(ctx, *lease)
					if err == nil && got.CallbackAttemptCount != 1 {
						t.Error("rescue consumed callback retry", got)
					}
					return err
				}); err != nil {
					t.Fatal(err)
				}
				if pending, err := worker.Expired(ctx, nil, 10); err != nil || len(pending) != 0 {
					t.Fatal("live lease listed as expired", pending, err)
				}
				if err := worker.Heartbeat(ctx, *lease); err != nil {
					t.Fatal(err)
				}
				expireExecutionFixture(t, f, *lease)
				candidates, err := worker.Expired(ctx, nil, 1)
				if err != nil || len(candidates) != 1 || candidates[0].ID != lease.ID {
					t.Fatal(candidates, err)
				}
				candidate := candidates[0]
				if next, err := worker.Expired(ctx, &ExpiredDeliveryCursor{candidate.LeaseUntil, candidate.ID}, 1); err != nil || len(next) != 0 {
					t.Fatal("keyset page repeated expired candidate", next, err)
				}
				for _, forge := range []func(*ExpiredDelivery){func(v *ExpiredDelivery) { v.Token = uuid.New() }, func(v *ExpiredDelivery) { v.Identity.TenantID = uuid.New() }, func(v *ExpiredDelivery) { v.LeaseUntil = v.LeaseUntil.Add(time.Microsecond) }, func(v *ExpiredDelivery) { v.TransportAttempt++ }} {
					bad := candidate
					forge(&bad)
					if _, err := worker.Rescue(ctx, owner, bad); err == nil {
						t.Fatal("forged candidate rescued")
					}
				}
				denied := *owner
				denied.authorize = func(ctx context.Context, tx *sql.Tx, actual uuid.UUID, action ExecutionAction) error {
					if action == ExecutionRescue {
						return ErrDecisionAuthorizationDenied
					}
					return owner.authorize(ctx, tx, actual, action)
				}
				if _, err := worker.Rescue(ctx, &denied, candidate); !errors.Is(err, ErrDecisionAuthorizationDenied) {
					t.Fatal("owner rescue authority bypassed", err)
				}
				deniedWorker, raw := *worker, *worker.transport
				raw.authorize = func(ctx context.Context, tx *sql.Tx, actual uuid.UUID, k ports.JobKind, action EffectWorkerAction) error {
					if action == EffectWorkerRescue {
						return ErrDecisionAuthorizationDenied
					}
					return worker.transport.authorize(ctx, tx, actual, k, action)
				}
				deniedWorker.transport = &raw
				if _, err := deniedWorker.Expired(ctx, nil, 1); !errors.Is(err, ErrDecisionAuthorizationDenied) {
					t.Fatal("worker candidate authority bypassed", err)
				}
				if _, err := deniedWorker.Rescue(ctx, owner, candidate); !errors.Is(err, ErrDecisionAuthorizationDenied) {
					t.Fatal("worker rescue authority bypassed", err)
				}
				if _, err := worker.Rescue(ctx, executionTestStore(t, f, uuid.New()), candidate); !errors.Is(err, ErrExecutionLease) {
					t.Fatal("foreign owner composed with rescue", err)
				}
				if attempt == 3 && kind == ports.JobAsync {
					if _, err := f.db.Exec("CREATE TRIGGER " + schema + ".fixture_rescue_callback_failure BEFORE INSERT ON " + schema + ".decision_engine_execution_job_deliveries FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='required callback unavailable'"); err != nil {
						t.Fatal(err)
					}
					if _, err := worker.Rescue(ctx, owner, candidate); err == nil {
						t.Fatal("exhaustion without required callback committed")
					}
					if _, err := f.db.Exec("DROP TRIGGER " + schema + ".fixture_rescue_callback_failure"); err != nil {
						t.Fatal(err)
					}
					if err := owner.Run(ctx, ExecutionRead, func(r ExecutionRepositories) error {
						got, err := r.Async.GetByID(ctx, tenant.String(), async.ID)
						if err == nil && (got.Status != execution.StatusRunning || got.CallbackStatus != "") {
							t.Error("failed callback insert did not roll back owner", got)
						}
						return err
					}); err != nil {
						t.Fatal(err)
					}
				}
				state, err := worker.Rescue(ctx, owner, candidate)
				if err != nil {
					t.Fatal(err)
				}
				if attempt < 3 && state != workers.Available || attempt == 3 && state != workers.Discarded {
					t.Fatal(state)
				}
				if _, err := worker.Rescue(ctx, owner, candidate); err == nil {
					t.Fatal("same expiry counted twice")
				}
				if err := worker.Heartbeat(ctx, *lease); !errors.Is(err, ErrExecutionLease) {
					t.Fatal("old worker heartbeat survived rescue", err)
				}
				var count int
				if err := f.db.QueryRow("SELECT error_count FROM "+schema+".decision_engine_execution_job_deliveries WHERE id=?", mysqlstore.UUID(lease.ID)).Scan(&count); err != nil || count != attempt {
					t.Fatal(count, err)
				}
				if attempt < 3 {
					executionForceDue(t, f, *lease)
				}
			}
			if err := owner.Run(ctx, ExecutionRead, func(r ExecutionRepositories) error {
				if kind == ports.JobScheduled {
					got, err := r.Scheduled.GetByID(ctx, tenant.String(), parent.String(), scheduled.ID)
					if err == nil && (got.Status != execution.StatusFailed || got.AttemptCount != 1) {
						t.Error(got)
					}
					return err
				}
				got, err := r.Async.GetByID(ctx, tenant.String(), async.ID)
				if err == nil {
					if kind == ports.JobAsync && (got.Status != execution.StatusFailed || got.CallbackStatus != "pending" || got.AttemptCount != 1) {
						t.Error(got)
					}
					if kind == ports.JobCallback {
						var result map[string]bool
						if err := json.Unmarshal(got.ResultBody, &result); err != nil || len(result) != 1 || !result["completed"] || got.Status != execution.StatusCompleted || got.CallbackStatus != "failed" || got.CallbackAttemptCount != 1 {
							t.Error("callback exhaustion damaged completed evaluation", got, err)
						}
					}
				}
				return err
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestMySQLExecutionConcurrentRescueAndLateWorkerAreFenced(t *testing.T) {
	f, owner, tenant, parent, iteration := executionFixture(t)
	ctx := context.Background()
	if err := owner.Run(ctx, ExecutionRequest, func(r ExecutionRepositories) error {
		_, err := r.Scheduled.Create(ctx, scheduledInput(tenant, parent, iteration))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	worker := executionTransportFixture(t, f, tenant, ports.JobScheduled)
	lease, err := worker.Claim(ctx)
	if err != nil || lease == nil {
		t.Fatal(lease, err)
	}
	expireExecutionFixture(t, f, *lease)
	candidates, err := worker.Expired(ctx, nil, 1)
	if err != nil || len(candidates) != 1 {
		t.Fatal(candidates, err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 3)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := worker.Rescue(ctx, owner, candidates[0]); results <- err }()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		err := worker.Heartbeat(ctx, *lease)
		if !errors.Is(err, ErrExecutionLease) {
			results <- errors.New("late heartbeat did not fail closed")
		} else {
			results <- ErrExecutionLease
		}
	}()
	wg.Wait()
	close(results)
	wins := 0
	for err := range results {
		if err == nil {
			wins++
		} else if !errors.Is(err, ErrExecutionLease) {
			t.Fatal(err)
		}
	}
	if wins != 1 {
		t.Fatal("not exactly one rescue winner", wins)
	}
	if err := owner.Run(ctx, ExecutionRun, func(r ExecutionRepositories) error { return r.Scheduled.Complete(ctx, *lease) }); err == nil {
		t.Fatal("expired worker completed owner")
	}
	// Use the actual bounded scheduler producer for representative transport
	// cardinality; no 5,000 per-row request/job insert loop in fixture setup.
	schedule := ExecutionScheduleInput{ID: uuid.New(), ScenarioID: parent, ScenarioIterationID: iteration, StartAt: time.Now().UTC().Truncate(time.Microsecond).Add(-10000 * time.Second), Interval: time.Second, Policy: workers.CatchUp, MaxAttempts: 3, RequestBody: []byte(`{}`), Active: true}
	if err := owner.Run(ctx, ExecutionScheduleAdmin, func(r ExecutionRepositories) error { _, err := r.Schedules.Create(ctx, schedule); return err }); err != nil {
		t.Fatal(err)
	}
	for batch := 0; batch < 50; batch++ {
		if err := owner.Run(ctx, ExecutionScheduleTick, func(r ExecutionRepositories) error {
			out, err := r.Schedules.Materialize(ctx, schedule.ID, 100)
			if err == nil && len(out.Records) != 100 {
				t.Error("short plan fixture batch", len(out.Records))
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	next, err := worker.Claim(ctx)
	if err != nil || next == nil {
		t.Fatal(next, err)
	}
	expireExecutionFixture(t, f, *next)
	assertExpiredDeliveryPlan(t, f.db, worker.transport)
}
