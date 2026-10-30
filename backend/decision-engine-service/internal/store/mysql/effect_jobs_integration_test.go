package mysql

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/decision"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/integration"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scoring"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func TestMySQLRequiredEffectJobsAtomicOwnerBindingAndReceipts(t *testing.T) {
	f := effectsFixture(t, true)
	ctx := context.Background()
	queued, err := NewQueuedEffectStore(f.db, f.database, f.tenant, 500, 2<<20, allowPlatformFixture, f.store.screening, effectQueuePolicies())
	if err != nil {
		t.Fatal(err)
	}
	schema, _ := mysqlstore.Identifier(f.database)
	count := func(table string) int {
		t.Helper()
		var n int
		if err := f.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+schema+"."+table).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	var outboxID string
	t.Run("all four kinds and multi-chunk batches", func(t *testing.T) {
		d := decisionInput(f.tenant, f.parent, f.iteration.ID)
		if err := queued.Mutate(ctx, func(r EffectRepositories) error {
			if _, err := r.Decisions.Create(ctx, d); err != nil {
				return err
			}
			var screens []screening.Execution
			var scores []scoring.Request
			var workflows []workflow.Execution
			var events []integration.OutboxEvent
			for i := 0; i < 120; i++ {
				v := f.screeningInput(d)
				screens = append(screens, v)
				scores = append(scores, scoring.Request{ID: uuid.NewString(), TenantID: v.TenantID, ConfigID: f.score.ID, DecisionID: d.ID, ScenarioID: f.parent.String(), Status: scoring.RequestStatusPending, RequestJSON: v.RequestJSON, CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt})
				wf := f.wf.ID
				workflows = append(workflows, workflow.Execution{ID: uuid.NewString(), TenantID: v.TenantID, WorkflowID: &wf, DecisionID: d.ID, ScenarioID: v.ScenarioID, ActionType: f.wf.ActionType, ActionConfig: f.wf.ActionConfig, Status: workflow.ExecutionStatusPendingDispatch, CreatedAt: d.CreatedAt})
				events = append(events, jobOutboxInput(f, d))
			}
			outboxID = events[0].ID
			if _, err := r.ScreeningExecutions.CreateMany(ctx, screens); err != nil {
				return err
			}
			if _, err := r.ScoringRequests.CreateMany(ctx, scores); err != nil {
				return err
			}
			if _, err := r.WorkflowExecutions.CreateMany(ctx, workflows); err != nil {
				return err
			}
			_, err := r.OutboxEvents.CreateMany(ctx, events)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if count("decision_engine_effect_job_receipts") != 480 || count("decision_engine_effect_job_deliveries") != 480 {
			t.Fatal("required jobs missing")
		}
		var valid int
		if err := f.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+schema+".decision_engine_effect_job_receipts r JOIN "+schema+".decision_engine_effect_job_deliveries d ON d.id=r.id WHERE r.tenant_id=? AND domain_attempt=1 AND redrive_generation=0 AND state='available' AND attempt=0 AND error_count=0 AND max_attempts=3", mysqlstore.UUID(f.tenant)).Scan(&valid); err != nil || valid != 480 {
			t.Fatal("identity or initial state", valid, err)
		}
	})
	t.Run("claim plan uses scoped queue index", func(t *testing.T) {
		var plan string
		err := f.db.QueryRowContext(ctx, "EXPLAIN ANALYZE SELECT id,attempt,error_count FROM "+schema+".decision_engine_effect_job_deliveries WHERE tenant_id=? AND kind='outbox_event' AND queue_name='outbox_events' AND state='available' AND available_at<=UTC_TIMESTAMP(6) AND attempt<max_attempts ORDER BY available_at,id LIMIT 1", mysqlstore.UUID(f.tenant)).Scan(&plan)
		if err != nil {
			t.Fatal(err)
		}
		t.Log(plan)
		if !strings.Contains(plan, "de_effect_delivery_claim") || strings.Contains(strings.ToLower(plan), "table scan") {
			t.Fatal("claim did not use scoped queue index", plan)
		}
	})
	t.Run("ignored later chunk failure cannot commit unscheduled resources", func(t *testing.T) {
		d := decisionInput(f.tenant, f.parent, f.iteration.ID)
		events := make([]integration.OutboxEvent, 120)
		for i := range events {
			events[i] = jobOutboxInput(f, d)
		}
		events[119].ID = outboxID // Existing DB row: the second insert chunk fails.
		err := queued.Mutate(ctx, func(r EffectRepositories) error {
			if _, err := r.Decisions.Create(ctx, d); err != nil {
				return err
			}
			_, _ = r.OutboxEvents.CreateMany(ctx, events)
			return nil
		})
		if err == nil || count("decision_engine_decisions") != 1 || count("decision_engine_outbox_events") != 120 || count("decision_engine_effect_job_receipts") != 480 {
			t.Fatal("ignored batch error committed rows without required jobs", err)
		}
	})
	t.Run("required insert failure poisons whole owner transaction", func(t *testing.T) {
		if _, err := f.db.ExecContext(ctx, "CREATE TRIGGER "+schema+".fixture_queue_failure BEFORE INSERT ON "+schema+".decision_engine_effect_job_deliveries FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='injected required queue failure'"); err != nil {
			t.Fatal(err)
		}
		d := decisionInput(f.tenant, f.parent, f.iteration.ID)
		event := jobOutboxInput(f, d)
		err := queued.Mutate(ctx, func(r EffectRepositories) error {
			if _, err := r.Decisions.Create(ctx, d); err != nil {
				return err
			}
			_, _ = r.OutboxEvents.CreateMany(ctx, []integration.OutboxEvent{event}) // Deliberately ignored by a buggy caller.
			return nil
		})
		if err == nil {
			t.Fatal("queue failure committed success")
		}
		if _, err := f.db.ExecContext(ctx, "DROP TRIGGER "+schema+".fixture_queue_failure"); err != nil {
			t.Fatal(err)
		}
		if count("decision_engine_decisions") != 1 || count("decision_engine_outbox_events") != 120 || count("decision_engine_effect_job_receipts") != 480 {
			t.Fatal("partial decision, effect or receipt committed")
		}
	})
	t.Run("existing receipts deduplicate concurrent producers and retained cleanup", func(t *testing.T) {
		var wg sync.WaitGroup
		errs := make(chan error, 2)
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				errs <- queued.Mutate(ctx, func(r EffectRepositories) error {
					return r.OutboxEvents.enqueueCreated(ctx, "decision_engine_outbox_events", []string{outboxID})
				})
			}()
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatal(err)
			}
		}
		if count("decision_engine_effect_job_deliveries") != 480 {
			t.Fatal("duplicate delivery")
		}
		if _, err := f.db.ExecContext(ctx, "DELETE d FROM "+schema+".decision_engine_effect_job_deliveries d JOIN "+schema+".decision_engine_effect_job_receipts r ON r.id=d.id WHERE r.tenant_id=? AND resource_id=?", mysqlstore.UUID(f.tenant), mysqlstore.UUID(uuid.MustParse(outboxID))); err != nil {
			t.Fatal(err)
		}
		if err := queued.Mutate(ctx, func(r EffectRepositories) error {
			return r.OutboxEvents.enqueueCreated(ctx, "decision_engine_outbox_events", []string{outboxID})
		}); err != nil {
			t.Fatal(err)
		}
		if count("decision_engine_effect_job_receipts") != 480 || count("decision_engine_effect_job_deliveries") != 479 {
			t.Fatal("retention resurrected an existing owner attempt")
		}
	})
	t.Run("nonpending owner and foreign tenant are rejected", func(t *testing.T) {
		d := decisionInput(f.tenant, f.parent, f.iteration.ID)
		v := jobOutboxInput(f, d)
		v.Status = integration.OutboxStatusSent
		if err := queued.Mutate(ctx, func(r EffectRepositories) error {
			_, err := r.OutboxEvents.CreateMany(ctx, []integration.OutboxEvent{v})
			return err
		}); !errors.Is(err, ErrEffectJobOwnerState) {
			t.Fatal("accepted terminal owner", err)
		}
		foreign, err := NewQueuedEffectStore(f.db, f.database, uuid.New(), 500, 2<<20, allowPlatformFixture, nil, effectQueuePolicies())
		if err != nil {
			t.Fatal(err)
		}
		if err := foreign.Mutate(ctx, func(r EffectRepositories) error {
			return r.OutboxEvents.enqueueCreated(ctx, "decision_engine_outbox_events", []string{outboxID})
		}); !errors.Is(err, ErrOwnerRelationshipConflict) {
			t.Fatal("foreign owner accepted", err)
		}
		// Direct SQL also cannot bind a receipt to a different tenant's resource.
		_, err = f.db.ExecContext(ctx, "INSERT INTO "+schema+".decision_engine_effect_job_receipts(id,tenant_id,resource_id,kind,domain_attempt,redrive_generation,outbox_id,created_at) VALUES(?,?,?,'outbox_event',1,0,?,UTC_TIMESTAMP(6))", mysqlstore.UUID(uuid.New()), mysqlstore.UUID(uuid.New()), mysqlstore.UUID(uuid.MustParse(outboxID)), mysqlstore.UUID(uuid.MustParse(outboxID)))
		if err == nil {
			t.Fatal("cross-tenant receipt escaped FK")
		}
	})
	t.Run("authorization and read-only failure", func(t *testing.T) {
		deny := errors.New("fixture denied")
		store, err := NewQueuedEffectStore(f.db, f.database, f.tenant, 500, 2<<20, func(context.Context, *sql.Tx, uuid.UUID, bool) error { return deny }, nil, effectQueuePolicies())
		if err != nil {
			t.Fatal(err)
		}
		called := false
		if err := store.Mutate(ctx, func(EffectRepositories) error { called = true; return nil }); !errors.Is(err, deny) || called {
			t.Fatal("authorization bypass", err)
		}
		if err := queued.ReadSnapshot(ctx, func(r EffectRepositories) error {
			_, err := r.OutboxEvents.CreateMany(ctx, []integration.OutboxEvent{jobOutboxInput(f, decisionInput(f.tenant, f.parent, f.iteration.ID))})
			return err
		}); !errors.Is(err, ErrAuthoringReadOnly) {
			t.Fatal("read-only enqueue allowed", err)
		}
	})
	t.Run("first scheduling is serialized by the exact owner", func(t *testing.T) {
		v := jobOutboxInput(f, decisionInput(f.tenant, f.parent, f.iteration.ID))
		if err := f.store.Mutate(ctx, func(r EffectRepositories) error {
			_, err := r.OutboxEvents.CreateMany(ctx, []integration.OutboxEvent{v})
			return err
		}); err != nil {
			t.Fatal(err)
		}
		before := count("decision_engine_effect_job_receipts")
		var wg sync.WaitGroup
		errs := make(chan error, 2)
		start := make(chan struct{})
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				errs <- queued.Mutate(ctx, func(r EffectRepositories) error {
					return r.OutboxEvents.enqueueCreated(ctx, "decision_engine_outbox_events", []string{v.ID})
				})
			}()
		}
		close(start)
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatal(err)
			}
		}
		if count("decision_engine_effect_job_receipts") != before+1 {
			t.Fatal("concurrent first producers did not share one exact receipt")
		}
		// The receipt remains authoritative even if transport retention removed
		// its delivery: owner deletion needs a future explicit retention policy.
		if _, err := f.db.ExecContext(ctx, "DELETE FROM "+schema+".decision_engine_outbox_events WHERE tenant_id=? AND id=?", mysqlstore.UUID(f.tenant), mysqlstore.UUID(uuid.MustParse(outboxID))); err == nil {
			t.Fatal("owner deletion orphaned retained receipt")
		}
	})
}

func jobOutboxInput(f effectFixture, d decision.Decision) integration.OutboxEvent {
	return integration.OutboxEvent{ID: uuid.NewString(), TenantID: f.tenant.String(), AggregateType: "decision", AggregateID: d.ID, EventType: "decision.created", Payload: []byte(`{"fixture":true}`), Status: integration.OutboxStatusPending, CreatedAt: d.CreatedAt}
}
