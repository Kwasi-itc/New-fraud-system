package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/integration"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scoring"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/mysqljobs"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

// A private SQL fixture, not a production identity provider.
func allowEffectRetryFixture(context.Context, *sql.Tx, uuid.UUID, ports.JobKind, EffectRetryAction) error {
	return nil
}
func allowEffectWorkerFixture(context.Context, *sql.Tx, uuid.UUID, ports.JobKind, EffectWorkerAction) error {
	return nil
}

func createRetryEffects(t *testing.T, f effectFixture) map[ports.JobKind]uuid.UUID {
	t.Helper()
	ctx := context.Background()
	queued, err := NewQueuedEffectStore(f.db, f.database, f.tenant, 500, 2<<20, allowPlatformFixture, f.store.screening, effectQueuePolicies())
	if err != nil {
		t.Fatal(err)
	}
	d := decisionInput(f.tenant, f.parent, f.iteration.ID)
	screen := f.screeningInput(d)
	score := scoring.Request{ID: uuid.NewString(), TenantID: screen.TenantID, ConfigID: f.score.ID, DecisionID: d.ID, ScenarioID: d.ScenarioID, Status: scoring.RequestStatusPending, RequestJSON: screen.RequestJSON, CreatedAt: d.CreatedAt, UpdatedAt: d.CreatedAt}
	wfID := f.wf.ID
	wf := workflow.Execution{ID: uuid.NewString(), TenantID: screen.TenantID, WorkflowID: &wfID, DecisionID: d.ID, ScenarioID: d.ScenarioID, ActionType: f.wf.ActionType, ActionConfig: f.wf.ActionConfig, Status: workflow.ExecutionStatusPendingDispatch, CreatedAt: d.CreatedAt}
	event := jobOutboxInput(f, d)
	err = queued.Mutate(ctx, func(r EffectRepositories) error {
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
	})
	if err != nil {
		t.Fatal(err)
	}
	return map[ports.JobKind]uuid.UUID{ports.JobWorkflow: uuid.MustParse(wf.ID), ports.JobScreening: uuid.MustParse(screen.ID), ports.JobScoring: uuid.MustParse(score.ID), ports.JobOutbox: uuid.MustParse(event.ID)}
}

func retryStores(t *testing.T, f effectFixture, kind ports.JobKind) (*EffectDeliveryStore, *EffectRetryStore) {
	t.Helper()
	policy := effectQueuePolicies()[kind]
	delivery, err := NewEffectDeliveryStore(f.db, f.database, f.tenant, kind, policy.Name, time.Minute, allowEffectWorkerFixture)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := NewEffectRetryStore(f.db, f.database, f.tenant, kind, policy, 3, allowEffectRetryFixture)
	if err != nil {
		t.Fatal(err)
	}
	return delivery, retry
}

func claimRetryEffect(t *testing.T, store *EffectDeliveryStore) EffectDeliveryLease {
	t.Helper()
	v, err := store.Claim(context.Background())
	if err != nil || v == nil {
		t.Fatal("expected delivery", v, err)
	}
	return *v
}

func finishRetryEffect(t *testing.T, f effectFixture, store *EffectDeliveryStore, lease EffectDeliveryLease, status string) {
	t.Helper()
	schema, _ := mysqlstore.Identifier(f.database)
	err := store.Finish(context.Background(), lease, false, "", func(tx *sql.Tx) error {
		_, err := tx.ExecContext(context.Background(), "UPDATE "+schema+"."+effectOwnerTable(lease.Identity.Kind)+" SET status=? WHERE tenant_id=? AND id=?", status, mysqlstore.UUID(f.tenant), mysqlstore.UUID(lease.Identity.ResourceID))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestMySQLEffectDomainRetryRedriveAndRetention(t *testing.T) {
	f := effectsFixture(t, true)
	ctx := context.Background()
	ids := createRetryEffects(t, f)
	schema, _ := mysqlstore.Identifier(f.database)
	for _, kind := range []ports.JobKind{ports.JobWorkflow, ports.JobScreening, ports.JobScoring, ports.JobOutbox} {
		t.Run(string(kind), func(t *testing.T) {
			delivery, retry := retryStores(t, f, kind)
			lease := claimRetryEffect(t, delivery)
			if lease.Identity.ResourceID != ids[kind] {
				t.Fatal("wrong owner")
			}
			if _, err := retry.Retry(ctx, lease.Identity); !errors.Is(err, ErrEffectRetryState) {
				t.Fatal("retried active owner", err)
			}
			failure := "failed"
			if kind == ports.JobWorkflow {
				failure = "dispatch_failed"
			}
			finishRetryEffect(t, f, delivery, lease, failure)
			next, err := retry.Retry(ctx, lease.Identity)
			if err != nil || next.DomainAttempt != 2 || next.RedriveGeneration != 0 {
				t.Fatal(next, err)
			}
			if _, err := retry.Retry(ctx, lease.Identity); !errors.Is(err, ErrEffectRetryState) {
				t.Fatal("replayed domain retry", err)
			}
			if err := delivery.Heartbeat(ctx, lease); !errors.Is(err, ErrStaleEffectLease) {
				t.Fatal("old worker survived retry", err)
			}
			current := claimRetryEffect(t, delivery)
			if current.Identity != next || current.Attempt != 1 {
				t.Fatal("successor identity/transport attempt", current)
			}
			finishRetryEffect(t, f, delivery, current, failure)
			// Retention deletes transport, not the permanent attempt fence.
			if _, err := f.db.ExecContext(ctx, "DELETE FROM "+schema+".decision_engine_effect_job_deliveries WHERE id=?", mysqlstore.UUID(current.ID)); err != nil {
				t.Fatal(err)
			}
			next, err = retry.Redrive(ctx, next)
			if err != nil || next.DomainAttempt != 2 || next.RedriveGeneration != 1 {
				t.Fatal("retained receipt blocked legitimate redrive", next, err)
			}
			current = claimRetryEffect(t, delivery)
			if current.Identity != next || current.Attempt != 1 {
				t.Fatal(current)
			}
			finishRetryEffect(t, f, delivery, current, failure)
			next, err = retry.Retry(ctx, next)
			if err != nil || next.DomainAttempt != 3 || next.RedriveGeneration != 1 {
				t.Fatal(next, err)
			}
			current = claimRetryEffect(t, delivery)
			finishRetryEffect(t, f, delivery, current, failure)
			if _, err := retry.Retry(ctx, next); !errors.Is(err, ErrEffectRetryState) {
				t.Fatal("domain budget ignored", err)
			}
			next, err = retry.Redrive(ctx, next)
			if err != nil || next.DomainAttempt != 3 || next.RedriveGeneration != 2 {
				t.Fatal("explicit redrive at domain budget", next, err)
			}
			current = claimRetryEffect(t, delivery)
			success := "sent"
			if kind == ports.JobWorkflow {
				success = "dispatched"
			}
			if kind == ports.JobScreening || kind == ports.JobScoring {
				success = "completed"
			}
			finishRetryEffect(t, f, delivery, current, success)
			if _, err := retry.Redrive(ctx, next); !errors.Is(err, ErrEffectRetryState) {
				t.Fatal("successful owner redriven", err)
			}
			var n int
			if err := f.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+schema+".decision_engine_effect_job_receipts WHERE tenant_id=? AND kind=? AND resource_id=?", mysqlstore.UUID(f.tenant), string(kind), mysqlstore.UUID(ids[kind])).Scan(&n); err != nil || n != 5 {
				t.Fatal("receipt history lost or duplicate created", n, err)
			}
		})
	}
}

func TestMySQLEffectRetryRollbackAuthorityAndConcurrency(t *testing.T) {
	f := effectsFixture(t, true)
	ctx := context.Background()
	ids := createRetryEffects(t, f)
	schema, _ := mysqlstore.Identifier(f.database)
	delivery, retry := retryStores(t, f, ports.JobOutbox)
	lease := claimRetryEffect(t, delivery)
	finishRetryEffect(t, f, delivery, lease, "failed")
	t.Run("required successor failure rolls back reset and receipt", func(t *testing.T) {
		if _, err := f.db.ExecContext(ctx, "CREATE TRIGGER "+schema+".fixture_retry_failure BEFORE INSERT ON "+schema+".decision_engine_effect_job_deliveries FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='required successor failed'"); err != nil {
			t.Fatal(err)
		}
		v, err := retry.Retry(ctx, lease.Identity)
		if err == nil || v != (mysqljobs.Identity{}) {
			t.Fatal("successful retry without job", v, err)
		}
		if _, err := f.db.ExecContext(ctx, "DROP TRIGGER "+schema+".fixture_retry_failure"); err != nil {
			t.Fatal(err)
		}
		var status string
		var n int
		if err := f.db.QueryRowContext(ctx, "SELECT status FROM "+schema+".decision_engine_outbox_events WHERE tenant_id=? AND id=?", mysqlstore.UUID(f.tenant), mysqlstore.UUID(ids[ports.JobOutbox])).Scan(&status); err != nil || status != "failed" {
			t.Fatal("owner reset committed", status, err)
		}
		if err := f.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+schema+".decision_engine_effect_job_receipts WHERE tenant_id=? AND kind='outbox_event'", mysqlstore.UUID(f.tenant)).Scan(&n); err != nil || n != 1 {
			t.Fatal("successor receipt committed", n, err)
		}
	})
	t.Run("separate redrive authority and tenant scope", func(t *testing.T) {
		denied := errors.New("redrive denied")
		guarded := *retry
		guarded.authorize = func(_ context.Context, _ *sql.Tx, tenant uuid.UUID, kind ports.JobKind, action EffectRetryAction) error {
			if tenant != f.tenant || kind != ports.JobOutbox || action != EffectRedrive {
				t.Fatal("wrong authority scope")
			}
			return denied
		}
		if _, err := guarded.Redrive(ctx, lease.Identity); !errors.Is(err, denied) {
			t.Fatal(err)
		}
		foreign := lease.Identity
		foreign.TenantID = uuid.New()
		if _, err := retry.Retry(ctx, foreign); !errors.Is(err, ErrEffectRetryState) {
			t.Fatal("cross tenant identity", err)
		}
		foreignStore := *retry
		foreignStore.tenant = foreign.TenantID
		if _, err := foreignStore.Retry(ctx, foreign); !errors.Is(err, ErrEffectRetryState) {
			t.Fatal("cross tenant owner", err)
		}
	})
	t.Run("two domain retries have one winner", func(t *testing.T) {
		var wg sync.WaitGroup
		results := make(chan error, 2)
		start := make(chan struct{})
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func() { defer wg.Done(); <-start; _, err := retry.Retry(ctx, lease.Identity); results <- err }()
		}
		close(start)
		wg.Wait()
		close(results)
		wins := 0
		for err := range results {
			if err == nil {
				wins++
			} else if !errors.Is(err, ErrEffectRetryState) {
				t.Fatal(err)
			}
		}
		if wins != 1 {
			t.Fatal("duplicate successor", wins)
		}
	})
	t.Run("source-managed screening is never reset", func(t *testing.T) {
		transport, screens := retryStores(t, f, ports.JobScreening)
		v := claimRetryEffect(t, transport)
		finishRetryEffect(t, f, transport, v, "failed")
		if _, err := f.db.ExecContext(ctx, "UPDATE "+schema+".decision_engine_screening_executions SET projection_source_id=?,projection_source_version=1 WHERE tenant_id=? AND id=?", mysqlstore.UUID(uuid.New()), mysqlstore.UUID(f.tenant), mysqlstore.UUID(ids[ports.JobScreening])); err != nil {
			t.Fatal(err)
		}
		if _, err := screens.Retry(ctx, v.Identity); !errors.Is(err, ErrEffectSourceOwned) {
			t.Fatal("reset foreign owner's result", err)
		}
		if _, err := screens.Redrive(ctx, v.Identity); !errors.Is(err, ErrEffectSourceOwned) {
			t.Fatal("redrove foreign owner's result", err)
		}
	})
}

func TestMySQLEffectRetryReceiptFenceAndPlan(t *testing.T) {
	f := effectsFixture(t, true)
	ctx := context.Background()
	ids := createRetryEffects(t, f)
	schema, _ := mysqlstore.Identifier(f.database)
	delivery, _ := retryStores(t, f, ports.JobOutbox)
	old := claimRetryEffect(t, delivery)
	// Inject a superseding receipt to exercise the fence independently of token
	// state. Real retry refuses running transports; no API bypass is provided.
	err := mysqlstore.Mutation(ctx, f.db, func(tx *sql.Tx) error {
		if _, _, err := lockEffectOwner(ctx, tx, schema, old.Identity); err != nil {
			return err
		}
		next := old.Identity
		next.DomainAttempt++
		return insertEffectSuccessor(ctx, tx, schema, next, effectQueuePolicies()[ports.JobOutbox])
	})
	if err != nil {
		t.Fatal(err)
	}
	called := false
	if err := delivery.Finish(ctx, old, false, "", func(*sql.Tx) error { called = true; return nil }); !errors.Is(err, ErrStaleEffectLease) || called {
		t.Fatal("superseded worker mutated owner", err, called)
	}
	if _, err := f.db.ExecContext(ctx, "UPDATE "+schema+".decision_engine_effect_job_deliveries SET lease_until=TIMESTAMPADD(SECOND,-1,UTC_TIMESTAMP(6)) WHERE id=?", mysqlstore.UUID(old.ID)); err != nil {
		t.Fatal(err)
	}
	expired, err := delivery.Expired(ctx, nil, 10)
	if err != nil || len(expired) != 1 {
		t.Fatal(expired, err)
	}
	if _, err := delivery.Rescue(ctx, expired[0], func(context.Context, *sql.Tx, mysqljobs.Identity) error { called = true; return nil }); !errors.Is(err, ErrStaleEffectLease) || called {
		t.Fatal("superseded rescue mutated owner", err, called)
	}
	// Representative long retry history: exact resource range, reverse index
	// lookup, one row. No payload/history loading or arbitrary subset scanning.
	err = mysqlstore.Mutation(ctx, f.db, func(tx *sql.Tx) error {
		for start := 3; start < 5003; start += 100 {
			var values []string
			var args []any
			for i := start; i < start+100; i++ {
				values = append(values, "(?,?,?,'outbox_event',?,0,?,UTC_TIMESTAMP(6))")
				args = append(args, mysqlstore.UUID(uuid.New()), mysqlstore.UUID(f.tenant), mysqlstore.UUID(ids[ports.JobOutbox]), i, mysqlstore.UUID(ids[ports.JobOutbox]))
			}
			if _, err := tx.ExecContext(ctx, "INSERT INTO "+schema+".decision_engine_effect_job_receipts(id,tenant_id,resource_id,kind,domain_attempt,redrive_generation,outbox_id,created_at) VALUES"+strings.Join(values, ","), args...); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var plan string
	if err := f.db.QueryRowContext(ctx, "EXPLAIN ANALYZE "+currentEffectReceiptSQL(schema), mysqlstore.UUID(f.tenant), string(ports.JobOutbox), mysqlstore.UUID(ids[ports.JobOutbox])).Scan(&plan); err != nil {
		t.Fatal(err)
	}
	t.Log(plan)
	if !strings.Contains(plan, "de_effect_job_identity") || strings.Contains(strings.ToLower(plan), "table scan") || strings.Contains(strings.ToLower(plan), "sort:") {
		t.Fatal("unbounded latest receipt plan", plan)
	}
	err = mysqlstore.Mutation(ctx, f.db, func(tx *sql.Tx) error {
		_, latest, err := currentEffectReceipt(ctx, tx, schema, old.Identity)
		if err != nil {
			return err
		}
		if latest.DomainAttempt != 5002 {
			return fmt.Errorf("wrong latest attempt: %d", latest.DomainAttempt)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
