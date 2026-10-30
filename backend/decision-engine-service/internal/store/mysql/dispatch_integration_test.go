package mysql

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func dispatchFixture(t *testing.T) (effectFixture, map[ports.JobKind]uuid.UUID, *EffectStore) {
	t.Helper()
	f := effectsFixture(t, true, true, true, true)
	ids := createRetryEffects(t, f)
	owner, err := NewQueuedEffectStore(f.db, f.database, f.tenant, 500, 2<<20, allowPlatformFixture, f.store.screening, effectQueuePolicies())
	if err != nil {
		t.Fatal(err)
	}
	return f, ids, owner
}

func TestMySQLDispatchRestrictedWorkerCannotBypassIntentOwnership(t *testing.T) {
	f, _, _ := dispatchFixture(t)
	ctx := context.Background()
	schema, _ := mysqlstore.Identifier(f.database)
	user := "de_dispatch_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:16]
	account := "`" + user + "`@'%'"
	if _, err := f.db.Exec("CREATE USER " + account + " IDENTIFIED BY 'fixture-dispatch-only'"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := f.db.Exec("DROP USER " + account); err != nil {
			t.Error(err)
		}
	})
	for _, table := range []string{"decision_engine_dispatch_intents", "decision_engine_outbox_events", "decision_engine_effect_job_receipts", "decision_engine_effect_job_deliveries"} {
		grant := "SELECT,UPDATE"
		if table == "decision_engine_dispatch_intents" {
			grant += ",INSERT"
		}
		if _, err := f.db.Exec("GRANT " + grant + " ON " + schema + "." + table + " TO " + account); err != nil {
			t.Fatal(err)
		}
	}
	db, err := mysqlstore.Open(ctx, platformConfig(f.database, user, "fixture-dispatch-only"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	owner, err := NewQueuedEffectStore(db, f.database, f.tenant, 500, 2<<20, allowPlatformFixture, nil, effectQueuePolicies())
	if err != nil {
		t.Fatal(err)
	}
	delivery, err := NewEffectDeliveryStore(db, f.database, f.tenant, ports.JobOutbox, effectQueuePolicies()[ports.JobOutbox].Name, time.Minute, allowEffectWorkerFixture)
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewEffectDispatchStore(owner, delivery)
	if err != nil {
		t.Fatal(err)
	}
	lease, err := store.Claim(ctx)
	if err != nil || lease == nil {
		t.Fatal(lease, err)
	}
	intent, err := store.Prepare(ctx, *lease)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Complete(ctx, *lease, intent.Request, acceptedDispatch(intent), time.Second); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		"DROP TABLE " + schema + ".decision_engine_dispatch_intents",
		"DELETE FROM " + schema + ".decision_engine_dispatch_intents",
		"UPDATE " + schema + ".decision_engine_dispatch_intents SET tenant_id=UUID_TO_BIN(UUID())",
		"UPDATE " + schema + ".decision_engine_dispatch_intents SET payload=X'7b7d'",
	} {
		if _, err := db.Exec(query); err == nil {
			t.Fatal("worker bypassed ownership", query)
		}
	}
}
func acceptedDispatch(intent DispatchIntent) ports.DispatchReceipt {
	return ports.DispatchReceipt{Key: intent.Request.Key, PayloadSHA256: intent.Request.PayloadSHA256, Outcome: ports.DispatchAccepted, Reference: "receipt-1"}
}
func TestMySQLDispatchAllEffectsIntentReceiptAtomicityAndRetention(t *testing.T) {
	f, ids, owner := dispatchFixture(t)
	ctx := context.Background()
	schema, _ := mysqlstore.Identifier(f.database)
	for _, kind := range []ports.JobKind{ports.JobWorkflow, ports.JobScreening, ports.JobScoring, ports.JobOutbox} {
		t.Run(string(kind), func(t *testing.T) {
			delivery, _ := retryStores(t, f, kind)
			store, err := NewEffectDispatchStore(owner, delivery)
			if err != nil {
				t.Fatal(err)
			}
			lease := claimRetryEffect(t, delivery)
			intent, err := store.Prepare(ctx, lease)
			if err != nil || intent.Resumed || intent.Request.ResourceID != ids[kind].String() {
				t.Fatal(intent, err)
			}
			resume, err := store.Prepare(ctx, lease)
			if err != nil || !resume.Resumed || string(resume.Request.Payload) != string(intent.Request.Payload) || resume.Request.Key != intent.Request.Key {
				t.Fatal("intent not stable", resume, err)
			}
			foreign := lease
			foreign.Identity.TenantID = uuid.New()
			if _, err := store.Prepare(ctx, foreign); !errors.Is(err, ErrStaleEffectLease) {
				t.Fatal("foreign intent", err)
			}
			bad := acceptedDispatch(intent)
			bad.PayloadSHA256 = "another-body"
			if err := store.Complete(ctx, lease, intent.Request, bad, time.Second); !errors.Is(err, ErrDispatchReceipt) {
				t.Fatal("wrong receipt", err)
			}
			if kind == ports.JobWorkflow {
				if _, err := f.db.Exec("CREATE TRIGGER " + schema + ".fixture_dispatch_fail BEFORE UPDATE ON " + schema + ".decision_engine_effect_job_deliveries FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='ack unavailable'"); err != nil {
					t.Fatal(err)
				}
				if err := store.Complete(ctx, lease, intent.Request, acceptedDispatch(intent), time.Second); err == nil {
					t.Fatal("ack error swallowed")
				}
				if committed, err := store.CompletionCommitted(ctx, lease, acceptedDispatch(intent)); err != nil || committed {
					t.Fatal("partial receipt committed", committed, err)
				}
				if _, err := f.db.Exec("DROP TRIGGER " + schema + ".fixture_dispatch_fail"); err != nil {
					t.Fatal(err)
				}
			}
			if err := store.Complete(ctx, lease, intent.Request, acceptedDispatch(intent), time.Second); err != nil {
				t.Fatal(err)
			}
			if committed, err := store.CompletionCommitted(ctx, lease, acceptedDispatch(intent)); err != nil || !committed {
				t.Fatal(committed, err)
			}
			var status string
			if err := f.db.QueryRow("SELECT status FROM "+schema+"."+effectOwnerTable(kind)+" WHERE id=?", mysqlstore.UUID(ids[kind])).Scan(&status); err != nil {
				t.Fatal(err)
			}
			want := "sent"
			if kind == ports.JobWorkflow {
				want = "dispatched"
			}
			if status != want {
				t.Fatal(status, want)
			}
			if err := store.Complete(ctx, lease, intent.Request, acceptedDispatch(intent), time.Second); err == nil {
				t.Fatal("completed twice")
			}
			if _, err := f.db.Exec("DELETE FROM "+schema+".decision_engine_effect_job_deliveries WHERE id=?", mysqlstore.UUID(lease.ID)); err != nil {
				t.Fatal(err)
			}
			if committed, err := store.CompletionCommitted(ctx, lease, acceptedDispatch(intent)); err != nil || !committed {
				t.Fatal("retention lost evidence", committed, err)
			}
		})
	}
	for _, q := range []string{"DELETE FROM " + schema + ".decision_engine_dispatch_intents", "UPDATE " + schema + ".decision_engine_dispatch_intents SET outcome='rejected'"} {
		if _, err := f.db.Exec(q); err == nil {
			t.Fatal("rewrote permanent dispatch", q)
		}
	}
}

func TestMySQLDispatchCancellationRaceAndRevocation(t *testing.T) {
	f, _, owner := dispatchFixture(t)
	ctx := context.Background()
	delivery, lifecycle := retryStores(t, f, ports.JobOutbox)
	store, err := NewEffectDispatchStore(owner, delivery)
	if err != nil {
		t.Fatal(err)
	}
	lease := claimRetryEffect(t, delivery)
	intent, err := store.Prepare(ctx, lease)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		results <- store.Complete(ctx, lease, intent.Request, acceptedDispatch(intent), time.Second)
	}()
	go func() {
		defer wg.Done()
		_, err := lifecycle.Cancel(ctx, lease.Identity, "operator_cancelled")
		results <- err
	}()
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		} else if !errors.Is(err, ErrStaleEffectLease) && !errors.Is(err, ErrEffectRetryState) {
			t.Fatal(err)
		}
	}
	if successes != 1 {
		t.Fatal("cancellation and finish both committed", successes)
	}
	denied, err := NewQueuedEffectStore(f.db, f.database, f.tenant, 500, 2<<20, func(context.Context, *sql.Tx, uuid.UUID, bool) error { return ErrDecisionAuthorizationDenied }, f.store.screening, effectQueuePolicies())
	if err != nil {
		t.Fatal(err)
	}
	revoked, err := NewEffectDispatchStore(denied, delivery)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := revoked.Prepare(ctx, lease); !errors.Is(err, ErrDecisionAuthorizationDenied) {
		t.Fatal("revoked prepare", err)
	}
	if _, err := revoked.CompletionCommitted(ctx, lease, acceptedDispatch(intent)); !errors.Is(err, ErrDecisionAuthorizationDenied) {
		t.Fatal("revoked reconcile", err)
	}
}
