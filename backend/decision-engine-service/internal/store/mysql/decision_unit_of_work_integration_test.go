package mysql

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/decision"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/execution"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/integration"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/platform"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scoring"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func combinedFixture(t *testing.T, dispatch ...bool) (effectFixture, *DecisionUnitOfWorkStore, *ExecutionStore) {
	t.Helper()
	f := effectsFixture(t, true, true, true, len(dispatch) > 0 && dispatch[0])
	ctx := context.Background()
	a := authoringStore(t, f.platformFixture, f.tenant, 500, 2<<20)
	if err := a.Mutate(ctx, func(r AuthoringRepositories) error {
		_, err := r.Iterations.Commit(ctx, f.tenant.String(), f.parent.String(), f.iteration.ID, time.Now().UTC().Truncate(time.Microsecond))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Publish(ctx, PublicationInput{f.parent, uuid.MustParse(f.iteration.ID), uuid.New(), uuid.New()}, func(context.Context, *sql.Tx, PublicationSnapshot) error { return nil }); err != nil {
		t.Fatal(err)
	}
	effects, err := NewQueuedEffectStore(f.db, f.database, f.tenant, 500, 2<<20, allowPlatformFixture, f.store.screening, effectQueuePolicies())
	if err != nil {
		t.Fatal(err)
	}
	effects, err = effects.WithCustomLists(allowAuthoringFixture, true)
	if err != nil {
		t.Fatal(err)
	}
	x := executionTestStore(t, f.platformFixture, f.tenant)
	u, err := NewDecisionUnitOfWorkStore(effects, x)
	if err != nil {
		t.Fatal(err)
	}
	return f, u, x
}

func persistCombinedOutputs(ctx context.Context, f effectFixture, s ports.DecisionMutationStore, d decision.Decision) error {
	if _, err := s.Decisions().Create(ctx, d); err != nil {
		return err
	}
	v := f.screeningInput(d)
	if _, err := s.ScreeningExecutions().CreateMany(ctx, []screening.Execution{v}); err != nil {
		return err
	}
	score := scoring.Request{ID: uuid.NewString(), TenantID: v.TenantID, ConfigID: f.score.ID, DecisionID: d.ID, ScenarioID: d.ScenarioID, Status: scoring.RequestStatusPending, RequestJSON: v.RequestJSON, CreatedAt: d.CreatedAt, UpdatedAt: d.CreatedAt}
	if _, err := s.ScoringRequests().CreateMany(ctx, []scoring.Request{score}); err != nil {
		return err
	}
	wfID := f.wf.ID
	wf := workflow.Execution{ID: uuid.NewString(), TenantID: v.TenantID, WorkflowID: &wfID, DecisionID: d.ID, ScenarioID: d.ScenarioID, ActionType: f.wf.ActionType, ActionConfig: f.wf.ActionConfig, Status: workflow.ExecutionStatusPendingDispatch, CreatedAt: d.CreatedAt}
	if _, err := s.WorkflowExecutions().CreateMany(ctx, []workflow.Execution{wf}); err != nil {
		return err
	}
	out := jobOutboxInput(f, d)
	if _, err := s.OutboxEvents().CreateMany(ctx, []integration.OutboxEvent{out}); err != nil {
		return err
	}
	// Source DecisionService explicitly enqueues after CreateMany. This must
	// verify/deduplicate the already atomic initial delivery, not create another.
	for _, job := range []ports.JobRequest{{Kind: ports.JobWorkflow, ResourceID: wf.ID}, {Kind: ports.JobScreening, ResourceID: v.ID}, {Kind: ports.JobScoring, ResourceID: score.ID}, {Kind: ports.JobOutbox, ResourceID: out.ID}} {
		job.TenantID = f.tenant.String()
		if err := s.Enqueue(ctx, job); err != nil {
			return err
		}
	}
	return nil
}

func TestMySQLCombinedTransactionRequiredJobsCapabilitiesAndRollback(t *testing.T) {
	f, u, _ := combinedFixture(t)
	ctx := context.Background()
	schema, _ := mysqlstore.Identifier(f.database)
	d := decisionInput(f.tenant, f.parent, f.iteration.ID)
	list := platform.CustomList{ID: uuid.NewString(), TenantID: f.tenant.String(), Name: "Combined authorized list", Kind: "generic_text", CreatedAt: d.CreatedAt, UpdatedAt: d.CreatedAt}
	if err := u.Run(ctx, ExecutionRequest, func(v DecisionUnitOfWork) error {
		lists, err := v.Effects.CustomLists()
		if err != nil {
			return err
		}
		if lists.tx != v.Effects.Decisions.tx {
			return errors.New("list is not on decision transaction")
		}
		if _, err := lists.CreateList(ctx, list); err != nil {
			return err
		}
		if err := persistCombinedOutputs(ctx, f, decisionMutation{v.Effects}, d); err != nil {
			return err
		}
		x, err := v.Executions()
		if err != nil {
			return err
		}
		if _, err = x.Scheduled.Create(ctx, scheduledInput(f.tenant, f.parent, uuid.MustParse(f.iteration.ID))); err != nil {
			return err
		}
		_, err = x.Async.Create(ctx, asyncInput(f.tenant, f.parent))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if executionCount(t, f.platformFixture, "decision_engine_effect_job_receipts") != 4 || executionCount(t, f.platformFixture, "decision_engine_execution_job_receipts") != 2 || executionCount(t, f.platformFixture, "decision_engine_custom_lists") != 1 {
		t.Fatal("required jobs not combined")
	}
	baseline := executionCount(t, f.platformFixture, "decision_engine_decisions")
	if _, err := f.db.ExecContext(ctx, "CREATE TRIGGER "+schema+".fixture_combined_failure BEFORE INSERT ON "+schema+".decision_engine_execution_job_deliveries FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='required job failed'"); err != nil {
		t.Fatal(err)
	}
	err := u.Run(ctx, ExecutionRequest, func(v DecisionUnitOfWork) error {
		lists, err := v.Effects.CustomLists()
		if err != nil {
			return err
		}
		rollbackList := list
		rollbackList.ID, rollbackList.Name = uuid.NewString(), "Must roll back with execution job"
		if _, err := lists.CreateList(ctx, rollbackList); err != nil {
			return err
		}
		d := decisionInput(f.tenant, f.parent, f.iteration.ID)
		if _, err := v.Effects.Decisions.Create(ctx, d); err != nil {
			return err
		}
		x, err := v.Executions()
		if err != nil {
			return err
		}
		_, _ = x.Async.Create(ctx, asyncInput(f.tenant, f.parent)) // Intentionally ignored.
		return nil
	})
	if err == nil || executionCount(t, f.platformFixture, "decision_engine_decisions") != baseline || executionCount(t, f.platformFixture, "decision_engine_custom_lists") != 1 {
		t.Fatal("ignored required job failure committed decision", err)
	}
	if _, err := f.db.ExecContext(ctx, "DROP TRIGGER "+schema+".fixture_combined_failure"); err != nil {
		t.Fatal(err)
	}
	err = u.Run(ctx, "", func(v DecisionUnitOfWork) error {
		d := decisionInput(f.tenant, f.parent, f.iteration.ID)
		if _, err := v.Effects.Decisions.Create(ctx, d); err != nil {
			return err
		}
		_, _ = v.Executions() // Ignored capability error must also roll back.
		return nil
	})
	if !errors.Is(err, ErrDecisionAuthorizationUnavailable) || executionCount(t, f.platformFixture, "decision_engine_decisions") != baseline {
		t.Fatal("missing capability committed", err)
	}
	denied := errors.New("execution authority revoked")
	guarded := *u.executions
	guarded.authorize = func(context.Context, *sql.Tx, uuid.UUID, ExecutionAction) error { return denied }
	guardedUnit, err := NewDecisionUnitOfWorkStore(u.effects, &guarded)
	if err != nil {
		t.Fatal(err)
	}
	called := false
	if err := guardedUnit.Run(ctx, ExecutionRequest, func(DecisionUnitOfWork) error { called = true; return nil }); !errors.Is(err, denied) || called {
		t.Fatal("callback ran without all authorities", called, err)
	}
	if err := u.RunDecisionMutation(ctx, func(v ports.DecisionMutationStore) error {
		d := decisionInput(f.tenant, f.parent, f.iteration.ID)
		if _, err := v.Decisions().Create(ctx, d); err != nil {
			return err
		}
		_ = v.Enqueue(ctx, ports.JobRequest{Kind: ports.JobAsync, TenantID: f.tenant.String(), ResourceID: uuid.NewString()})
		return nil
	}); err == nil || executionCount(t, f.platformFixture, "decision_engine_decisions") != baseline {
		t.Fatal("unfenced execution enqueue committed", err)
	}
}

func TestMySQLCombinedAsyncCompletionCallbackAtomicityAndFences(t *testing.T) {
	f, u, x := combinedFixture(t)
	ctx := context.Background()
	var owner AsyncCreation
	if err := x.Run(ctx, ExecutionRequest, func(r ExecutionRepositories) error {
		var err error
		owner, err = r.Async.Create(ctx, asyncInput(f.tenant, f.parent))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	lease := executionTestLease(t, f.platformFixture, f.tenant, uuid.MustParse(owner.Record.ID), ports.JobAsync, 1, 0)
	if err := x.Run(ctx, ExecutionRun, func(r ExecutionRepositories) error { _, err := r.Async.Start(ctx, lease); return err }); err != nil {
		t.Fatal(err)
	}
	schema, _ := mysqlstore.Identifier(f.database)
	if _, err := f.db.ExecContext(ctx, "CREATE TRIGGER "+schema+".fixture_callback_failure BEFORE INSERT ON "+schema+".decision_engine_execution_job_deliveries FOR EACH ROW BEGIN IF NEW.kind='async_decision_execution_callback' THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='callback failed'; END IF; END"); err != nil {
		t.Fatal(err)
	}
	d := decisionInput(f.tenant, f.parent, f.iteration.ID)
	d.ObjectType = "wallets"
	persist := func(s ports.DecisionMutationStore) error { return persistCombinedOutputs(ctx, f, s, d) }
	if err := u.CompleteAsync(ctx, lease, []byte(`{"ok":true}`), persist); err == nil {
		t.Fatal("missing callback committed outputs")
	}
	if executionCount(t, f.platformFixture, "decision_engine_decisions") != 0 || executionCount(t, f.platformFixture, "decision_engine_effect_job_receipts") != 0 {
		t.Fatal("outputs escaped completion rollback")
	}
	if _, err := f.db.ExecContext(ctx, "DROP TRIGGER "+schema+".fixture_callback_failure"); err != nil {
		t.Fatal(err)
	}
	if err := u.CompleteAsync(ctx, lease, []byte(`{"ok":true}`), persist); err != nil {
		t.Fatal(err)
	}
	if executionCount(t, f.platformFixture, "decision_engine_effect_job_receipts") != 4 || executionCount(t, f.platformFixture, "decision_engine_execution_job_receipts") != 2 {
		t.Fatal("effect jobs/callback missing")
	}
	called := false
	if err := u.CompleteAsync(ctx, lease, []byte(`{}`), func(ports.DecisionMutationStore) error { called = true; return nil }); err == nil || called {
		t.Fatal("completed worker persisted again", called, err)
	}
	if err := x.Run(ctx, ExecutionRead, func(r ExecutionRepositories) error {
		v, err := r.Async.GetByID(ctx, f.tenant.String(), owner.Record.ID)
		if err == nil && (v.Status != execution.StatusCompleted || v.CallbackStatus != "pending") {
			t.Fatal(v)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func TestMySQLCombinedScheduledCompletionBindsOwnerAndRechecksExpiry(t *testing.T) {
	f, u, x := combinedFixture(t)
	ctx := context.Background()
	var owner ScheduledCreation
	if err := x.Run(ctx, ExecutionRequest, func(r ExecutionRepositories) error {
		var err error
		owner, err = r.Scheduled.Create(ctx, scheduledInput(f.tenant, f.parent, uuid.MustParse(f.iteration.ID)))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	lease := executionTestLease(t, f.platformFixture, f.tenant, uuid.MustParse(owner.Record.ID), ports.JobScheduled, 1, 0)
	if err := x.Run(ctx, ExecutionRun, func(r ExecutionRepositories) error { _, err := r.Scheduled.Start(ctx, lease); return err }); err != nil {
		t.Fatal(err)
	}
	bad := decisionInput(f.tenant, f.parent, f.iteration.ID)
	bad.ScenarioID = uuid.NewString()
	if err := u.CompleteScheduled(ctx, lease, func(s ports.DecisionMutationStore) error { _, _ = s.Decisions().Create(ctx, bad); return nil }); !errors.Is(err, ErrOwnerRelationshipConflict) {
		t.Fatal("wrong owner output accepted", err)
	}
	schema, _ := mysqlstore.Identifier(f.database)
	if _, err := f.db.ExecContext(ctx, "UPDATE "+schema+".decision_engine_execution_job_deliveries SET lease_until=TIMESTAMPADD(SECOND,2,UTC_TIMESTAMP(6)) WHERE id=?", mysqlstore.UUID(lease.ID)); err != nil {
		t.Fatal(err)
	}
	called := false
	err := u.CompleteScheduled(ctx, lease, func(s ports.DecisionMutationStore) error {
		called = true
		d := decisionInput(f.tenant, f.parent, f.iteration.ID)
		if _, err := s.Decisions().Create(ctx, d); err != nil {
			return err
		}
		time.Sleep(2100 * time.Millisecond) // Fault injection ONLY: expire after writes.
		return nil
	})
	if !errors.Is(err, ErrExecutionLease) || !called || executionCount(t, f.platformFixture, "decision_engine_decisions") != 0 {
		t.Fatal("late worker committed outputs", called, err)
	}
}
