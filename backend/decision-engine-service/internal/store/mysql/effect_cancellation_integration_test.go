package mysql

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/mysqljobs"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func TestMySQLEffectCancellationAllKindsPreservesResultsAndReceipts(t *testing.T) {
	f := effectsFixture(t, true, true, true)
	ctx := context.Background()
	ids := createRetryEffects(t, f)
	schema, _ := mysqlstore.Identifier(f.database)
	for _, kind := range []ports.JobKind{ports.JobWorkflow, ports.JobScreening, ports.JobScoring, ports.JobOutbox} {
		t.Run(string(kind), func(t *testing.T) {
			delivery, lifecycle := retryStores(t, f, kind)
			identity := mysqljobs.Identity{Version: 1, TenantID: f.tenant, Kind: kind, ResourceID: ids[kind], DomainAttempt: 1}
			cancelled, err := lifecycle.Cancel(ctx, identity, "operator_cancelled")
			if err != nil || cancelled.Identity != identity || cancelled.CancelledAt.IsZero() || cancelled.Replay {
				t.Fatal(cancelled, err)
			}
			if v, err := delivery.Claim(ctx); err != nil || v != nil {
				t.Fatal("cancelled job claimed", v, err)
			}
			if _, err := lifecycle.Retry(ctx, identity); !errors.Is(err, ErrEffectRetryState) {
				t.Fatal("cancellation bypassed redrive authority", err)
			}
			replay, err := lifecycle.Cancel(ctx, identity, "operator_cancelled")
			if err != nil || !replay.Replay || replay.CancelledAt != cancelled.CancelledAt {
				t.Fatal("cancellation replay changed durable record", replay, err)
			}
			if _, err := lifecycle.Cancel(ctx, identity, "different_reason"); !errors.Is(err, ErrEffectRetryState) {
				t.Fatal("rewrote cancellation reason", err)
			}
			next, err := lifecycle.Redrive(ctx, identity)
			if err != nil || next.DomainAttempt != 1 || next.RedriveGeneration != 1 {
				t.Fatal(next, err)
			}
			lease := claimRetryEffect(t, delivery)
			if lease.Identity != next {
				t.Fatal(lease)
			}
			var before string
			if err := f.db.QueryRowContext(ctx, "SELECT status FROM "+schema+"."+effectOwnerTable(kind)+" WHERE tenant_id=? AND id=?", mysqlstore.UUID(f.tenant), mysqlstore.UUID(ids[kind])).Scan(&before); err != nil {
				t.Fatal(err)
			}
			if _, err := lifecycle.Cancel(ctx, next, "inflight_cancelled"); err != nil {
				t.Fatal(err)
			}
			if err := delivery.Heartbeat(ctx, lease); !errors.Is(err, ErrStaleEffectLease) {
				t.Fatal("cancelled worker renewed", err)
			}
			called := false
			if err := delivery.Finish(ctx, lease, false, "", func(*sql.Tx) error { called = true; return nil }); !errors.Is(err, ErrStaleEffectLease) || called {
				t.Fatal("cancelled worker committed", called, err)
			}
			var after string
			if err := f.db.QueryRowContext(ctx, "SELECT status FROM "+schema+"."+effectOwnerTable(kind)+" WHERE tenant_id=? AND id=?", mysqlstore.UUID(f.tenant), mysqlstore.UUID(ids[kind])).Scan(&after); err != nil || after != before {
				t.Fatal("cancellation rewrote business result", after, before, err)
			}
			if _, err := f.db.ExecContext(ctx, "DELETE FROM "+schema+".decision_engine_effect_job_deliveries WHERE id=?", mysqlstore.UUID(lease.ID)); err != nil {
				t.Fatal(err)
			}
			if v, err := lifecycle.Cancel(ctx, next, "inflight_cancelled"); err != nil || !v.Replay {
				t.Fatal("retention lost cancellation", v, err)
			}
			next, err = lifecycle.Redrive(ctx, next)
			if err != nil || next.RedriveGeneration != 2 {
				t.Fatal("cancelled receipt suppressed redrive", next, err)
			}
			lease = claimRetryEffect(t, delivery)
			success := "sent"
			if kind == ports.JobWorkflow {
				success = "dispatched"
			}
			if kind == ports.JobScreening || kind == ports.JobScoring {
				success = "completed"
			}
			finishRetryEffect(t, f, delivery, lease, success)
			if _, err := lifecycle.Cancel(ctx, next, "too_late"); !errors.Is(err, ErrEffectRetryState) {
				t.Fatal("cancelled completed/sent work", err)
			}
		})
	}
	for _, query := range []string{
		"UPDATE " + schema + ".decision_engine_effect_cancellations SET reason_code='rewritten'",
		"DELETE FROM " + schema + ".decision_engine_effect_cancellations",
		"DELETE FROM " + schema + ".decision_engine_effect_job_receipts",
	} {
		if _, err := f.db.ExecContext(ctx, query); err == nil {
			t.Fatal("permanent receipt/cancellation modified", query)
		}
	}
}

func TestMySQLEffectCancellationRollbackScopeAndFinishRace(t *testing.T) {
	f := effectsFixture(t, true, true, true)
	ctx := context.Background()
	ids := createRetryEffects(t, f)
	schema, _ := mysqlstore.Identifier(f.database)
	delivery, lifecycle := retryStores(t, f, ports.JobOutbox)
	lease := claimRetryEffect(t, delivery)
	if _, err := f.db.ExecContext(ctx, "CREATE TRIGGER "+schema+".fixture_cancel_failure BEFORE UPDATE ON "+schema+".decision_engine_effect_job_deliveries FOR EACH ROW BEGIN IF NEW.state='cancelled' THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='cancel failed'; END IF; END"); err != nil {
		t.Fatal(err)
	}
	if v, err := lifecycle.Cancel(ctx, lease.Identity, "operator_cancelled"); err == nil || v != (EffectCancellationRecord{}) {
		t.Fatal("record committed without transport cancellation", v, err)
	}
	if _, err := f.db.ExecContext(ctx, "DROP TRIGGER "+schema+".fixture_cancel_failure"); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := f.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+schema+".decision_engine_effect_cancellations").Scan(&count); err != nil || count != 0 {
		t.Fatal("cancellation record survived rollback", count, err)
	}
	if err := delivery.Heartbeat(ctx, lease); err != nil {
		t.Fatal("failed cancellation invalidated worker", err)
	}
	denied := errors.New("cancel authority denied")
	guarded := *lifecycle
	guarded.authorize = func(_ context.Context, _ *sql.Tx, tenant uuid.UUID, kind ports.JobKind, action EffectRetryAction) error {
		if tenant != f.tenant || kind != ports.JobOutbox || action != EffectCancel {
			t.Fatal("wrong cancellation authority")
		}
		return denied
	}
	if _, err := guarded.Cancel(ctx, lease.Identity, "denied"); !errors.Is(err, denied) {
		t.Fatal(err)
	}
	foreign := lease.Identity
	foreign.TenantID = uuid.New()
	if _, err := lifecycle.Cancel(ctx, foreign, "foreign"); !errors.Is(err, ErrEffectRetryState) {
		t.Fatal("cross-tenant cancellation", err)
	}
	for _, reason := range []string{"", "private provider message", "UPPER", "customer@example.test"} {
		if _, err := lifecycle.Cancel(ctx, lease.Identity, reason); !errors.Is(err, ErrEffectRetryState) {
			t.Fatal("unredacted reason accepted", reason, err)
		}
	}
	// Both commands lock the same resource before receipt/delivery: exactly one
	// wins, without a completed result being reset or a cancellation overwritten.
	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make(chan error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		_, err := lifecycle.Cancel(ctx, lease.Identity, "racing_cancel")
		errs <- err
	}()
	go func() {
		defer wg.Done()
		<-start
		errs <- delivery.Finish(ctx, lease, false, "", func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, "UPDATE "+schema+".decision_engine_outbox_events SET status='sent' WHERE tenant_id=? AND id=?", mysqlstore.UUID(f.tenant), mysqlstore.UUID(ids[ports.JobOutbox]))
			return err
		})
	}()
	close(start)
	wg.Wait()
	close(errs)
	wins := 0
	for err := range errs {
		if err == nil {
			wins++
		} else if !errors.Is(err, ErrEffectRetryState) && !errors.Is(err, ErrStaleEffectLease) {
			t.Fatal(err)
		}
	}
	if wins != 1 {
		t.Fatal("finish and cancel both won/lost", wins)
	}
	var state, status string
	if err := f.db.QueryRowContext(ctx, "SELECT d.state,o.status FROM "+schema+".decision_engine_effect_job_deliveries d JOIN "+schema+".decision_engine_outbox_events o ON o.id=? AND o.tenant_id=d.tenant_id WHERE d.id=?", mysqlstore.UUID(ids[ports.JobOutbox]), mysqlstore.UUID(lease.ID)).Scan(&state, &status); err != nil {
		t.Fatal(err)
	}
	if !(state == "cancelled" && status == "pending" || state == "completed" && status == "sent") {
		t.Fatal("inconsistent finish/cancel outcome", state, status)
	}
	// A foreign owner projection cannot be cancelled via a decision-side write.
	_, screen := retryStores(t, f, ports.JobScreening)
	identity := mysqljobs.Identity{Version: 1, TenantID: f.tenant, Kind: ports.JobScreening, ResourceID: ids[ports.JobScreening], DomainAttempt: 1}
	if _, err := f.db.ExecContext(ctx, "UPDATE "+schema+".decision_engine_screening_executions SET projection_source_id=?,projection_source_version=1 WHERE id=? AND tenant_id=?", mysqlstore.UUID(uuid.New()), mysqlstore.UUID(identity.ResourceID), mysqlstore.UUID(f.tenant)); err != nil {
		t.Fatal(err)
	}
	if _, err := screen.Cancel(ctx, identity, "source_owned"); !errors.Is(err, ErrEffectSourceOwned) {
		t.Fatal("cancelled screening-owned projection", err)
	}
	// Trigger proves the exact receipt binding, not just same tenant/kind.
	if _, err := f.db.ExecContext(ctx, "INSERT INTO "+schema+".decision_engine_effect_cancellations(receipt_id,tenant_id,kind,resource_id,domain_attempt,redrive_generation,reason_code,cancelled_at) VALUES(?,?,?,?,1,0,'forged',UTC_TIMESTAMP(6))", mysqlstore.UUID(lease.ID), mysqlstore.UUID(f.tenant), string(ports.JobOutbox), mysqlstore.UUID(uuid.New())); err == nil {
		t.Fatal("forged cancellation resource accepted")
	}
}
