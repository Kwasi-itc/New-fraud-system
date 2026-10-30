package mysql

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/integration"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scoring"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/mysqljobs"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/workers"
	"github.com/google/uuid"
)

func TestMySQLEffectRescueAllKindsRequireAtomicOwnerExhaustion(t *testing.T) {
	f := effectsFixture(t, true)
	ctx := context.Background()
	schema, _ := mysqlstore.Identifier(f.database)
	policies := effectQueuePolicies()
	queued, err := NewQueuedEffectStore(f.db, f.database, f.tenant, 500, 2<<20, allowPlatformFixture, f.store.screening, policies)
	if err != nil {
		t.Fatal(err)
	}
	d := decisionInput(f.tenant, f.parent, f.iteration.ID)
	screen := f.screeningInput(d)
	score := scoring.Request{ID: uuid.NewString(), TenantID: screen.TenantID, ConfigID: f.score.ID, DecisionID: d.ID, ScenarioID: f.parent.String(), Status: scoring.RequestStatusPending, RequestJSON: screen.RequestJSON, CreatedAt: d.CreatedAt, UpdatedAt: d.CreatedAt}
	wfID := f.wf.ID
	wf := workflow.Execution{ID: uuid.NewString(), TenantID: screen.TenantID, WorkflowID: &wfID, DecisionID: d.ID, ScenarioID: f.parent.String(), ActionType: f.wf.ActionType, ActionConfig: f.wf.ActionConfig, Status: workflow.ExecutionStatusPendingDispatch, CreatedAt: d.CreatedAt}
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
	for _, test := range []struct {
		kind       ports.JobKind
		id, status string
	}{{ports.JobWorkflow, wf.ID, "dispatch_failed"}, {ports.JobScreening, screen.ID, "failed"}, {ports.JobScoring, score.ID, "failed"}, {ports.JobOutbox, event.ID, "failed"}} {
		t.Run(string(test.kind), func(t *testing.T) {
			auth := func(_ context.Context, tx *sql.Tx, tenant uuid.UUID, kind ports.JobKind, action EffectWorkerAction) error {
				if tx == nil || tenant != f.tenant || kind != test.kind || (action != EffectWorkerClaim && action != EffectWorkerHeartbeat && action != EffectWorkerRescue) {
					return ErrDecisionAuthorizationDenied
				}
				return nil
			}
			worker, err := NewEffectDeliveryStore(f.db, f.database, f.tenant, test.kind, policies[test.kind].Name, time.Minute, auth)
			if err != nil {
				t.Fatal(err)
			}
			exhaust := func(ctx context.Context, tx *sql.Tx, identity mysqljobs.Identity) error {
				if identity.ResourceID.String() != test.id || identity.TenantID != f.tenant || identity.Kind != test.kind {
					return ErrTenantScope
				}
				result, err := tx.ExecContext(ctx, "UPDATE "+schema+"."+effectOwnerTable(test.kind)+" SET status=? WHERE tenant_id=? AND id=?", test.status, mysqlstore.UUID(f.tenant), mysqlstore.UUID(identity.ResourceID))
				return effectDeliveryChanged(result, err)
			}
			for attempt := 1; attempt <= 3; attempt++ {
				lease, err := worker.Claim(ctx)
				if err != nil || lease == nil || lease.Attempt != attempt {
					t.Fatal(lease, err)
				}
				if _, err := f.db.Exec("UPDATE "+schema+".decision_engine_effect_job_deliveries SET lease_until=UTC_TIMESTAMP(6)-INTERVAL 1 SECOND WHERE tenant_id=? AND id=? AND lease_token=?", mysqlstore.UUID(f.tenant), mysqlstore.UUID(lease.ID), mysqlstore.UUID(lease.Token)); err != nil {
					t.Fatal(err)
				}
				candidates, err := worker.Expired(ctx, nil, 1)
				if err != nil || len(candidates) != 1 || candidates[0].Identity != lease.Identity {
					t.Fatal(candidates, err)
				}
				candidate := candidates[0]
				if _, err := worker.Rescue(ctx, candidate, nil); err == nil {
					t.Fatal("missing exhaustion handler accepted")
				}
				if attempt == 3 {
					injected := errors.New("exhaustion successor unavailable")
					if _, err := worker.Rescue(ctx, candidate, func(ctx context.Context, tx *sql.Tx, id mysqljobs.Identity) error {
						if err := exhaust(ctx, tx, id); err != nil {
							return err
						}
						return injected
					}); !errors.Is(err, injected) {
						t.Fatal(err)
					}
					var status string
					if err := f.db.QueryRow("SELECT status FROM "+schema+"."+effectOwnerTable(test.kind)+" WHERE id=?", mysqlstore.UUID(lease.Identity.ResourceID)).Scan(&status); err != nil || status == test.status {
						t.Fatal("owner exhaustion did not roll back", status, err)
					}
				}
				state, err := worker.Rescue(ctx, candidate, exhaust)
				if err != nil {
					t.Fatal(err)
				}
				if attempt < 3 && state != workers.Available || attempt == 3 && state != workers.Discarded {
					t.Fatal(state)
				}
				if _, err := worker.Rescue(ctx, candidate, exhaust); !errors.Is(err, ErrStaleEffectLease) {
					t.Fatal("candidate reused", err)
				}
				if err := worker.Heartbeat(ctx, *lease); !errors.Is(err, ErrStaleEffectLease) {
					t.Fatal("expired worker retained token", err)
				}
				if attempt < 3 {
					if _, err := f.db.Exec("UPDATE "+schema+".decision_engine_effect_job_deliveries SET available_at=UTC_TIMESTAMP(6)-INTERVAL 1 SECOND WHERE id=? AND state='available'", mysqlstore.UUID(lease.ID)); err != nil {
						t.Fatal(err)
					}
				}
			}
			var status string
			if err := f.db.QueryRow("SELECT status FROM "+schema+"."+effectOwnerTable(test.kind)+" WHERE id=?", mysqlstore.UUID(uuid.MustParse(test.id))).Scan(&status); err != nil || status != test.status {
				t.Fatal(status, err)
			}
		})
	}
	// Retained terminal work plus a 5,000-row available backlog must not make
	// expired discovery scan payloads/history. Setup itself uses bounded batches.
	for batch := 0; batch < 10; batch++ {
		events := make([]integration.OutboxEvent, 500)
		for i := range events {
			events[i] = jobOutboxInput(f, d)
		}
		if err := queued.Mutate(ctx, func(r EffectRepositories) error { _, err := r.OutboxEvents.CreateMany(ctx, events); return err }); err != nil {
			t.Fatal(err)
		}
	}
	worker, err := NewEffectDeliveryStore(f.db, f.database, f.tenant, ports.JobOutbox, policies[ports.JobOutbox].Name, time.Minute, func(_ context.Context, tx *sql.Tx, tenant uuid.UUID, kind ports.JobKind, action EffectWorkerAction) error {
		if tx == nil || tenant != f.tenant || kind != ports.JobOutbox || (action != EffectWorkerClaim && action != EffectWorkerRescue) {
			return ErrDecisionAuthorizationDenied
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := worker.Claim(ctx)
	if err != nil || lease == nil {
		t.Fatal(lease, err)
	}
	if _, err := f.db.Exec("UPDATE "+schema+".decision_engine_effect_job_deliveries SET lease_until=UTC_TIMESTAMP(6)-INTERVAL 1 SECOND WHERE id=?", mysqlstore.UUID(lease.ID)); err != nil {
		t.Fatal(err)
	}
	assertExpiredDeliveryPlan(t, f.db, worker)
}
