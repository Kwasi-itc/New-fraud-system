package mysql

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/integration"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func TestDecisionJobBatchValidationIdempotencyAndPoisoning(t *testing.T) {
	f, unit, _ := combinedFixture(t, true)
	ctx := context.Background()
	schema, _ := mysqlstore.Identifier(f.database)
	count := func() int {
		t.Helper()
		var n int
		if err := f.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+schema+".decision_engine_effect_job_receipts").Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	d := decisionInput(f.tenant, f.parent, f.iteration.ID)
	events := make([]integration.OutboxEvent, 120)
	for i := range events {
		events[i] = jobOutboxInput(f, d)
	}
	jobs := make([]ports.JobRequest, 0, 121)
	for _, v := range events {
		jobs = append(jobs, ports.JobRequest{Kind: ports.JobOutbox, TenantID: f.tenant.String(), ResourceID: v.ID})
	}
	jobs = append(jobs, jobs[0])
	if err := unit.RunDecisionMutation(ctx, func(s ports.DecisionMutationStore) error {
		if _, err := s.Decisions().Create(ctx, d); err != nil {
			return err
		}
		if _, err := s.OutboxEvents().CreateMany(ctx, events); err != nil {
			return err
		}
		return s.(ports.DecisionJobBatchEnqueuer).EnqueueBatch(ctx, jobs)
	}); err != nil {
		t.Fatal(err)
	}
	if got := count(); got != 120 {
		t.Fatal("duplicated batch scheduling", got)
	}
	at := time.Now()
	for _, tc := range []struct {
		name   string
		mutate func([]ports.JobRequest) []ports.JobRequest
	}{
		{"foreign tenant", func(j []ports.JobRequest) []ports.JobRequest { j[1].TenantID = uuid.NewString(); return j }},
		{"invalid identity", func(j []ports.JobRequest) []ports.JobRequest { j[1].ResourceID = "invalid"; return j }},
		{"execution kind", func(j []ports.JobRequest) []ports.JobRequest { j[1].Kind = ports.JobAsync; return j }},
		{"scheduled initial job", func(j []ports.JobRequest) []ports.JobRequest { j[1].ScheduledAt = &at; return j }},
		{"bound", func(j []ports.JobRequest) []ports.JobRequest {
			out := make([]ports.JobRequest, 501)
			for i := range out {
				out[i] = j[0]
			}
			return out
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bad := tc.mutate(append([]ports.JobRequest(nil), jobs...))
			candidate := decisionInput(f.tenant, f.parent, f.iteration.ID)
			var batchErr error
			err := unit.RunDecisionMutation(ctx, func(s ports.DecisionMutationStore) error {
				if _, err := s.Decisions().Create(ctx, candidate); err != nil {
					return err
				}
				batchErr = s.(ports.DecisionJobBatchEnqueuer).EnqueueBatch(ctx, bad)
				return nil // Intentionally ignore the error: it must still poison the unit.
			})
			if batchErr == nil {
				t.Fatal("invalid batch accepted")
			}
			if err == nil {
				t.Fatal("ignored batch error committed")
			}
			var n int
			if err := f.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+schema+".decision_engine_decisions WHERE id=?", mysqlstore.UUID(uuid.MustParse(candidate.ID))).Scan(&n); err != nil || n != 0 {
				t.Fatal("partial decision committed", n, err)
			}
			if got := count(); got != 120 {
				t.Fatal("receipt count changed", got)
			}
		})
	}
	// A fresh invocation still checks owner state; no transaction-local cache
	// can turn a terminal owner into successful initial scheduling.
	if err := f.store.Mutate(ctx, func(r EffectRepositories) error {
		return r.OutboxEvents.UpdateStatus(ctx, events[0].ID, integration.OutboxStatusSent)
	}); err != nil {
		t.Fatal(err)
	}
	if err := unit.RunDecisionMutation(ctx, func(s ports.DecisionMutationStore) error {
		return s.(ports.DecisionJobBatchEnqueuer).EnqueueBatch(ctx, jobs)
	}); !errors.Is(err, ErrEffectJobOwnerState) {
		t.Fatal("terminal owner accepted", err)
	}
}
