package mysql

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/integration"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scoring"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func TestMySQLEffectDeliveryFencesAndTransactionalOutcomes(t *testing.T) {
	f := effectsFixture(t, true)
	ctx := context.Background()
	policies := effectQueuePolicies()
	queued, err := NewQueuedEffectStore(f.db, f.database, f.tenant, 500, 2<<20, allowPlatformFixture, f.store.screening, policies)
	if err != nil {
		t.Fatal(err)
	}
	schema, _ := mysqlstore.Identifier(f.database)
	d := decisionInput(f.tenant, f.parent, f.iteration.ID)
	screen := f.screeningInput(d)
	score := scoring.Request{ID: uuid.NewString(), TenantID: screen.TenantID, ConfigID: f.score.ID, DecisionID: d.ID, ScenarioID: f.parent.String(), Status: scoring.RequestStatusPending, RequestJSON: screen.RequestJSON, CreatedAt: d.CreatedAt, UpdatedAt: d.CreatedAt}
	wfID := f.wf.ID
	wf := workflow.Execution{ID: uuid.NewString(), TenantID: screen.TenantID, WorkflowID: &wfID, DecisionID: d.ID, ScenarioID: screen.ScenarioID, ActionType: f.wf.ActionType, ActionConfig: f.wf.ActionConfig, Status: workflow.ExecutionStatusPendingDispatch, CreatedAt: d.CreatedAt}
	event := jobOutboxInput(f, d)
	if err := queued.Mutate(ctx, func(r EffectRepositories) error {
		if _, err := r.Decisions.Create(ctx, d); err != nil {
			return err
		}
		if _, err := r.ScreeningExecutions.CreateMany(ctx, []screening.Execution{screen}); err != nil {
			return err
		}
		if _, err := r.ScoringRequests.CreateMany(ctx, []scoring.Request{score}); err != nil {
			return err
		}
		if _, err := r.WorkflowExecutions.CreateMany(ctx, []workflow.Execution{wf}); err != nil {
			return err
		}
		_, err := r.OutboxEvents.CreateMany(ctx, []integration.OutboxEvent{event})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	allowWorker := func(_ context.Context, _ *sql.Tx, tenant uuid.UUID, kind ports.JobKind, action EffectWorkerAction) error {
		if tenant != f.tenant || effectOwnerTable(kind) == "" || action != EffectWorkerClaim && action != EffectWorkerHeartbeat && action != EffectWorkerFinish {
			return errors.New("fixture worker scope denied")
		}
		return nil
	}
	for _, pair := range []struct {
		kind ports.JobKind
		id   string
	}{{ports.JobWorkflow, wf.ID}, {ports.JobScreening, screen.ID}, {ports.JobScoring, score.ID}, {ports.JobOutbox, event.ID}} {
		t.Run(string(pair.kind), func(t *testing.T) {
			store, err := NewEffectDeliveryStore(f.db, f.database, f.tenant, pair.kind, policies[pair.kind].Name, time.Minute, allowWorker)
			if err != nil {
				t.Fatal(err)
			}
			lease, err := store.Claim(ctx)
			if err != nil || lease == nil || lease.Identity.ResourceID.String() != pair.id || lease.Attempt != 1 || lease.Identity.DomainAttempt != 1 {
				t.Fatal("first claim", lease, err)
			}
			if other, err := store.Claim(ctx); err != nil || other != nil {
				t.Fatal("duplicate concurrent claim", other, err)
			}
			if err := store.Heartbeat(ctx, *lease); err != nil {
				t.Fatal(err)
			}
			for _, forge := range []func(*EffectDeliveryLease){
				func(v *EffectDeliveryLease) { v.Identity.TenantID = uuid.New() },
				func(v *EffectDeliveryLease) { v.Identity.ResourceID = uuid.New() },
				func(v *EffectDeliveryLease) { v.Identity.DomainAttempt++ },
				func(v *EffectDeliveryLease) { v.Token = uuid.New() },
				func(v *EffectDeliveryLease) { v.Attempt++ },
			} {
				bad := *lease
				forge(&bad)
				if err := store.Heartbeat(ctx, bad); err == nil {
					t.Fatal("forged lease renewed")
				}
			}
			if err := store.Finish(ctx, *lease, true, "provider_unavailable", func(*sql.Tx) error { return nil }); err != nil {
				t.Fatal(err)
			}
			if err := store.Heartbeat(ctx, *lease); !errors.Is(err, ErrStaleEffectLease) {
				t.Fatal("old retry lease still valid", err)
			}
			var errorsRecorded int
			var delayMicros int64
			if err := f.db.QueryRowContext(ctx, "SELECT error_count,TIMESTAMPDIFF(MICROSECOND,UTC_TIMESTAMP(6),available_at) FROM "+schema+".decision_engine_effect_job_deliveries WHERE id=?", mysqlstore.UUID(lease.ID)).Scan(&errorsRecorded, &delayMicros); err != nil || errorsRecorded != 1 || delayMicros > 1100000 {
				t.Fatal("retry policy or error history", errorsRecorded, delayMicros, err)
			}
			if _, err := f.db.ExecContext(ctx, "UPDATE "+schema+".decision_engine_effect_job_deliveries SET available_at=UTC_TIMESTAMP(6) WHERE id=?", mysqlstore.UUID(lease.ID)); err != nil {
				t.Fatal(err)
			}
			next, err := store.Claim(ctx)
			if err != nil || next == nil || next.ID != lease.ID || next.Identity != lease.Identity || next.Token == lease.Token || next.Attempt != 2 {
				t.Fatal("transport retry changed domain identity", next, err)
			}
			status := "sent"
			if pair.kind == ports.JobWorkflow {
				status = "dispatched"
			}
			updateOwner := func(tx *sql.Tx) error {
				_, err := tx.ExecContext(ctx, "UPDATE "+schema+"."+effectOwnerTable(pair.kind)+" SET status=? WHERE tenant_id=? AND id=?", status, mysqlstore.UUID(f.tenant), mysqlstore.UUID(uuid.MustParse(pair.id)))
				return err
			}
			abort := errors.New("abort local outcome")
			if err := store.Finish(ctx, *next, false, "", func(tx *sql.Tx) error {
				if err := updateOwner(tx); err != nil {
					return err
				}
				return abort
			}); !errors.Is(err, abort) {
				t.Fatal(err)
			}
			var state string
			if err := f.db.QueryRowContext(ctx, "SELECT status FROM "+schema+"."+effectOwnerTable(pair.kind)+" WHERE id=?", mysqlstore.UUID(uuid.MustParse(pair.id))).Scan(&state); err != nil || state == status {
				t.Fatal("local outcome did not roll back", state, err)
			}
			if err := store.Finish(ctx, *next, false, "", updateOwner); err != nil {
				t.Fatal(err)
			}
			called := false
			if err := store.Finish(ctx, *next, false, "", func(*sql.Tx) error { called = true; return nil }); !errors.Is(err, ErrStaleEffectLease) || called {
				t.Fatal("completed lease replayed effects", err)
			}
		})
	}
	t.Run("parallel first claim has one winner", func(t *testing.T) {
		v := jobOutboxInput(f, d)
		if err := queued.Mutate(ctx, func(r EffectRepositories) error {
			_, err := r.OutboxEvents.CreateMany(ctx, []integration.OutboxEvent{v})
			return err
		}); err != nil {
			t.Fatal(err)
		}
		store, err := NewEffectDeliveryStore(f.db, f.database, f.tenant, ports.JobOutbox, policies[ports.JobOutbox].Name, time.Minute, allowWorker)
		if err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		leases := make(chan *EffectDeliveryLease, 2)
		errs := make(chan error, 2)
		start := make(chan struct{})
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func() { defer wg.Done(); <-start; v, err := store.Claim(ctx); leases <- v; errs <- err }()
		}
		close(start)
		wg.Wait()
		close(leases)
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatal(err)
			}
		}
		var claimed *EffectDeliveryLease
		for v := range leases {
			if v != nil {
				if claimed != nil {
					t.Fatal("two workers received the same delivery")
				}
				claimed = v
			}
		}
		if claimed == nil {
			t.Fatal("no claim winner")
		}
		// Expire after the callback writes, proving the FINAL lease predicate
		// rolls back local owner effects as well as the queue acknowledgement.
		err = store.Finish(ctx, *claimed, false, "", func(tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, "UPDATE "+schema+".decision_engine_outbox_events SET status='sent' WHERE id=?", mysqlstore.UUID(uuid.MustParse(v.ID))); err != nil {
				return err
			}
			_, err := tx.ExecContext(ctx, "UPDATE "+schema+".decision_engine_effect_job_deliveries SET lease_until=TIMESTAMPADD(SECOND,-1,UTC_TIMESTAMP(6)) WHERE id=?", mysqlstore.UUID(claimed.ID))
			return err
		})
		if !errors.Is(err, ErrStaleEffectLease) {
			t.Fatal("expired worker committed effects", err)
		}
		var status string
		if err := f.db.QueryRowContext(ctx, "SELECT status FROM "+schema+".decision_engine_outbox_events WHERE id=?", mysqlstore.UUID(uuid.MustParse(v.ID))).Scan(&status); err != nil || status != "pending" {
			t.Fatal("expired local write survived", status, err)
		}
	})
	t.Run("missing or revoked worker authority", func(t *testing.T) {
		if _, err := NewEffectDeliveryStore(f.db, f.database, f.tenant, ports.JobOutbox, "outbox_events", time.Minute, nil); err == nil {
			t.Fatal("missing worker authorizer accepted")
		}
		for _, kind := range []ports.JobKind{ports.JobAsync, ports.JobScheduled, ports.JobCallback} {
			if _, err := NewEffectDeliveryStore(f.db, f.database, f.tenant, kind, "unimplemented", time.Minute, allowWorker); err == nil {
				t.Fatal("unimplemented lifecycle accepted", kind)
			}
		}
		deny := errors.New("worker identity revoked")
		store, err := NewEffectDeliveryStore(f.db, f.database, f.tenant, ports.JobOutbox, "outbox_events", time.Minute, func(context.Context, *sql.Tx, uuid.UUID, ports.JobKind, EffectWorkerAction) error { return deny })
		if err != nil {
			t.Fatal(err)
		}
		if lease, err := store.Claim(ctx); !errors.Is(err, deny) || lease != nil {
			t.Fatal("revoked identity claimed work", lease, err)
		}
	})
	t.Run("transport exhaustion never allocates a new owner attempt", func(t *testing.T) {
		v := jobOutboxInput(f, d)
		if err := queued.Mutate(ctx, func(r EffectRepositories) error {
			_, err := r.OutboxEvents.CreateMany(ctx, []integration.OutboxEvent{v})
			return err
		}); err != nil {
			t.Fatal(err)
		}
		store, err := NewEffectDeliveryStore(f.db, f.database, f.tenant, ports.JobOutbox, policies[ports.JobOutbox].Name, time.Minute, allowWorker)
		if err != nil {
			t.Fatal(err)
		}
		var original *EffectDeliveryLease
		for attempt := 1; attempt <= 3; attempt++ {
			lease, err := store.Claim(ctx)
			if err != nil || lease == nil || lease.Attempt != attempt || lease.Identity.ResourceID.String() != v.ID {
				t.Fatal("exhaustion claim", lease, err)
			}
			if original == nil {
				original = lease
			} else if lease.ID != original.ID || lease.Identity != original.Identity {
				t.Fatal("retry allocated a different logical delivery")
			}
			if err := store.Finish(ctx, *lease, true, "provider_unavailable", func(*sql.Tx) error { return nil }); err != nil {
				t.Fatal(err)
			}
			if attempt < 3 {
				if _, err := f.db.ExecContext(ctx, "UPDATE "+schema+".decision_engine_effect_job_deliveries SET available_at=UTC_TIMESTAMP(6) WHERE id=?", mysqlstore.UUID(lease.ID)); err != nil {
					t.Fatal(err)
				}
			}
		}
		var state, code string
		var count int
		if err := f.db.QueryRowContext(ctx, "SELECT state,last_error_code,error_count FROM "+schema+".decision_engine_effect_job_deliveries WHERE id=?", mysqlstore.UUID(original.ID)).Scan(&state, &code, &count); err != nil || state != "discarded" || code != "delivery_attempts_exhausted" || count != 3 {
			t.Fatal("exhaustion state", state, code, count, err)
		}
		if lease, err := store.Claim(ctx); err != nil || lease != nil {
			t.Fatal("exhausted transport claimed again", lease, err)
		}
		var receipts int
		if err := f.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+schema+".decision_engine_effect_job_receipts WHERE tenant_id=? AND resource_id=?", mysqlstore.UUID(f.tenant), mysqlstore.UUID(uuid.MustParse(v.ID))).Scan(&receipts); err != nil || receipts != 1 {
			t.Fatal("lost exhaustion receipt", receipts, err)
		}
	})
}
