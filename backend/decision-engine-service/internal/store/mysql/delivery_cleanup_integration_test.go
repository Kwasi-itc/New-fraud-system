package mysql

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/integration"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scoring"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/workers"
	"github.com/google/uuid"
)

func cleanupFixtureDelivery(t *testing.T, f effectFixture, kind ports.JobKind) (uuid.UUID, uuid.UUID, func() error) {
	t.Helper()
	ctx := context.Background()
	var resource uuid.UUID
	var replay func() error
	if executionOwner(kind) != "" {
		owner := executionTestStore(t, f.platformFixture, f.tenant)
		if kind == ports.JobScheduled {
			v := scheduledInput(f.tenant, f.parent, uuid.MustParse(f.iteration.ID))
			resource = uuid.MustParse(v.ID)
			replay = func() error {
				return owner.Run(ctx, ExecutionRequest, func(r ExecutionRepositories) error { _, err := r.Scheduled.Create(ctx, v); return err })
			}
		} else {
			v := asyncInput(f.tenant, f.parent)
			if kind == ports.JobAsync {
				v.CallbackURL = ""
			}
			resource = uuid.MustParse(v.ID)
			replay = func() error {
				return owner.Run(ctx, ExecutionRequest, func(r ExecutionRepositories) error { _, err := r.Async.Create(ctx, v); return err })
			}
		}
		if err := replay(); err != nil {
			t.Fatal(err)
		}
		if kind == ports.JobCallback {
			if err := owner.Run(ctx, ExecutionCancel, func(r ExecutionRepositories) error { _, err := r.Async.Cancel(ctx, resource, 0); return err }); err != nil {
				t.Fatal(err)
			}
		}
	} else {
		queued, err := NewQueuedEffectStore(f.db, f.database, f.tenant, 500, 2<<20, allowPlatformFixture, f.store.screening, effectQueuePolicies())
		if err != nil {
			t.Fatal(err)
		}
		d := decisionInput(f.tenant, f.parent, f.iteration.ID)
		resource = uuid.New()
		if err := queued.Mutate(ctx, func(r EffectRepositories) error {
			if _, err := r.Decisions.Create(ctx, d); err != nil {
				return err
			}
			switch kind {
			case ports.JobOutbox:
				v := jobOutboxInput(f, d)
				v.ID = resource.String()
				_, err := r.OutboxEvents.CreateMany(ctx, []integration.OutboxEvent{v})
				return err
			case ports.JobWorkflow:
				wid := f.wf.ID
				v := workflow.Execution{ID: resource.String(), TenantID: f.tenant.String(), WorkflowID: &wid, DecisionID: d.ID, ScenarioID: f.parent.String(), ActionType: f.wf.ActionType, ActionConfig: f.wf.ActionConfig, Status: workflow.ExecutionStatusPendingDispatch, CreatedAt: d.CreatedAt}
				_, err := r.WorkflowExecutions.CreateMany(ctx, []workflow.Execution{v})
				return err
			case ports.JobScreening:
				v := f.screeningInput(d)
				v.ID = resource.String()
				_, err := r.ScreeningExecutions.CreateMany(ctx, []screening.Execution{v})
				return err
			case ports.JobScoring:
				v := scoring.Request{ID: resource.String(), TenantID: f.tenant.String(), ConfigID: f.score.ID, DecisionID: d.ID, ScenarioID: f.parent.String(), Status: scoring.RequestStatusPending, RequestJSON: []byte(`{}`), CreatedAt: d.CreatedAt, UpdatedAt: d.CreatedAt}
				_, err := r.ScoringRequests.CreateMany(ctx, []scoring.Request{v})
				return err
			}
			return ErrEffectJobOwnerState
		}); err != nil {
			t.Fatal(err)
		}
		replay = func() error {
			return queued.Mutate(ctx, func(r EffectRepositories) error {
				return r.OutboxEvents.enqueueCreated(ctx, effectOwnerTable(kind), []string{resource.String()})
			})
		}
	}
	schema, _ := mysqlstore.Identifier(f.database)
	family := "effect"
	if executionOwner(kind) != "" {
		family = "execution"
	}
	var id mysqlstore.UUID
	if err := f.db.QueryRow("SELECT id FROM "+schema+".decision_engine_"+family+"_job_receipts WHERE tenant_id=? AND kind=? AND resource_id=? AND domain_attempt=1 AND redrive_generation=0", mysqlstore.UUID(f.tenant), string(kind), mysqlstore.UUID(resource)).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return uuid.UUID(id), resource, replay
}

func TestMySQLCleanupAllKindsRetainsIdentityOwnersAndLiveRecentWork(t *testing.T) {
	f := effectsFixture(t, true, true)
	ctx := context.Background()
	schema, _ := mysqlstore.Identifier(f.database)
	for _, kind := range []ports.JobKind{ports.JobWorkflow, ports.JobScreening, ports.JobScoring, ports.JobOutbox, ports.JobScheduled, ports.JobAsync, ports.JobCallback} {
		family, queue := "effect", effectQueuePolicies()[kind].Name
		if executionOwner(kind) != "" {
			family, queue = "execution", string(kind)
		}
		worker, err := newScopedDeliveryStore(f.db, f.database, f.tenant, kind, queue, time.Minute, func(_ context.Context, tx *sql.Tx, tenant uuid.UUID, k ports.JobKind, action EffectWorkerAction) error {
			if tx == nil || tenant != f.tenant || k != kind || action != EffectWorkerCleanup {
				return ErrDecisionAuthorizationDenied
			}
			return nil
		}, family == "execution")
		if err != nil {
			t.Fatal(err)
		}
		for _, state := range []workers.DeliveryState{workers.Completed, workers.Cancelled, workers.Discarded} {
			old, resource, replay := cleanupFixtureDelivery(t, f, kind)
			recent, _, _ := cleanupFixtureDelivery(t, f, kind)
			live, _, _ := cleanupFixtureDelivery(t, f, kind)
			retention, err := deliveryRetention(state)
			if err != nil {
				t.Fatal(err)
			}
			table := schema + ".decision_engine_" + family + "_job_deliveries"
			// Fixture-only terminal timestamps. This tests transport retention,
			// not business completion; owner mutation is deliberately forbidden.
			for _, age := range []struct {
				id      uuid.UUID
				seconds int64
			}{{old, -int64(retention/time.Second) - 2}, {recent, -int64(retention/time.Second) + 60}} {
				if _, err := f.db.Exec("UPDATE "+table+" SET state=?,attempt=1,lease_token=NULL,lease_until=NULL,finished_at=TIMESTAMPADD(SECOND,?,UTC_TIMESTAMP(6)) WHERE id=?", string(state), age.seconds, mysqlstore.UUID(age.id)); err != nil {
					t.Fatal(err)
				}
			}
			foreign := *worker
			foreign.tenant = uuid.New()
			foreign.authorize = func(context.Context, *sql.Tx, uuid.UUID, ports.JobKind, EffectWorkerAction) error { return nil }
			if n, err := foreign.CleanupTerminal(ctx, state, 100); err != nil || n != 0 {
				t.Fatal("foreign cleanup crossed tenant", n, err)
			}
			denied := *worker
			denied.authorize = func(context.Context, *sql.Tx, uuid.UUID, ports.JobKind, EffectWorkerAction) error {
				return ErrDecisionAuthorizationDenied
			}
			if n, err := denied.CleanupTerminal(ctx, state, 100); err == nil || n != 0 {
				t.Fatal("cleanup authority bypassed", n, err)
			}
			if n, err := worker.CleanupTerminal(ctx, state, 1); err != nil || n != 1 {
				t.Fatal(kind, state, n, err)
			}
			var remain int
			if err := f.db.QueryRow("SELECT COUNT(*) FROM "+table+" WHERE id IN(?,?,?)", mysqlstore.UUID(old), mysqlstore.UUID(recent), mysqlstore.UUID(live)).Scan(&remain); err != nil || remain != 2 {
				t.Fatal("cleanup removed live/recent or retained old", remain, err)
			}
			if err := replay(); err != nil {
				t.Fatal("legitimate replay after retention", err)
			}
			if err := f.db.QueryRow("SELECT COUNT(*) FROM "+table+" WHERE id=?", mysqlstore.UUID(old)).Scan(&remain); err != nil || remain != 0 {
				t.Fatal("receipt deletion/resurrection", remain, err)
			}
			if err := f.db.QueryRow("SELECT COUNT(*) FROM "+schema+".decision_engine_"+family+"_job_receipts WHERE id=? AND resource_id=?", mysqlstore.UUID(old), mysqlstore.UUID(resource)).Scan(&remain); err != nil || remain != 1 {
				t.Fatal("permanent identity removed", remain, err)
			}
		}
	}
}

func TestMySQLCleanupLockedReceiptAdvancesAndDeleteErrorRollsBack(t *testing.T) {
	f := effectsFixture(t, true, true)
	ctx := context.Background()
	schema, _ := mysqlstore.Identifier(f.database)
	a, _, _ := cleanupFixtureDelivery(t, f, ports.JobOutbox)
	b, _, _ := cleanupFixtureDelivery(t, f, ports.JobOutbox)
	if _, err := f.db.Exec("UPDATE "+schema+".decision_engine_effect_job_deliveries SET state='completed',finished_at=UTC_TIMESTAMP(6)-INTERVAL 2 DAY WHERE id IN(?,?)", mysqlstore.UUID(a), mysqlstore.UUID(b)); err != nil {
		t.Fatal(err)
	}
	var first mysqlstore.UUID
	if err := f.db.QueryRow("SELECT id FROM " + schema + ".decision_engine_effect_job_deliveries ORDER BY finished_at,id LIMIT 1").Scan(&first); err != nil {
		t.Fatal(err)
	}
	lock, err := f.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Rollback()
	if err := lock.QueryRow("SELECT id FROM "+schema+".decision_engine_effect_job_receipts WHERE id=? FOR UPDATE", first).Scan(&first); err != nil {
		t.Fatal(err)
	}
	worker, err := NewEffectDeliveryStore(f.db, f.database, f.tenant, ports.JobOutbox, effectQueuePolicies()[ports.JobOutbox].Name, time.Minute, func(context.Context, *sql.Tx, uuid.UUID, ports.JobKind, EffectWorkerAction) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	n, next, err := worker.CleanupTerminalPage(ctx, workers.Completed, nil, 1)
	if err != nil || n != 0 || next == nil {
		t.Fatal("locked first page did not advance", n, next, err)
	}
	if n, _, err := worker.CleanupTerminalPage(ctx, workers.Completed, next, 1); err != nil || n != 1 {
		t.Fatal("busy receipt starved unrelated row", n, err)
	}
	if err := lock.Rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec("CREATE TRIGGER " + schema + ".fixture_cleanup_failure BEFORE DELETE ON " + schema + ".decision_engine_effect_job_deliveries FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='cleanup injected failure'"); err != nil {
		t.Fatal(err)
	}
	if n, err := worker.CleanupTerminal(ctx, workers.Completed, 10); err == nil || n != 0 {
		t.Fatal("failed delete reported acknowledged removal", n, err)
	}
	if _, err := f.db.Exec("DROP TRIGGER " + schema + ".fixture_cleanup_failure"); err != nil {
		t.Fatal(err)
	}
	if n, err := worker.CleanupTerminal(ctx, workers.Completed, 10); err != nil || n != 1 {
		t.Fatal("locked row not recoverable after rollback", n, err)
	}
}
