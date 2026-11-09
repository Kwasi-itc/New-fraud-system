package mysql

import (
	"context"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/integration"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func TestRequiredEffectProducerDoesNotLockUnrelatedOwners(t *testing.T) {
	f := effectsFixture(t, true)
	ctx := context.Background()
	events := make([]integration.OutboxEvent, 5)
	for i := range events {
		events[i] = jobOutboxInput(f, decisionInput(f.tenant, f.parent, f.iteration.ID))
	}
	if err := f.store.Mutate(ctx, func(r EffectRepositories) error { _, err := r.OutboxEvents.CreateMany(ctx, events); return err }); err != nil {
		t.Fatal(err)
	}
	queued, err := NewQueuedEffectStore(f.db, f.database, f.tenant, 500, 2<<20, allowPlatformFixture, f.store.screening, effectQueuePolicies())
	if err != nil {
		t.Fatal(err)
	}
	schema, _ := mysqlstore.Identifier(f.database)
	blocker, err := f.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback()
	var locked mysqlstore.UUID
	if err := blocker.QueryRowContext(ctx, "SELECT id FROM "+schema+".decision_engine_outbox_events WHERE id=? FOR UPDATE", mysqlstore.UUID(uuid.MustParse(events[0].ID))).Scan(&locked); err != nil {
		t.Fatal(err)
	}
	// Hold another owner's row for the entire call. The producer must finish
	// while it is locked, without a tenant/status scan or broad lock acquisition.
	workCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ids := []string{events[1].ID, events[2].ID, events[3].ID, events[4].ID}
	if err := queued.Mutate(workCtx, func(r EffectRepositories) error {
		return r.OutboxEvents.enqueueCreated(workCtx, "decision_engine_outbox_events", ids)
	}); err != nil {
		t.Fatal("unrelated locked event blocked required job producer", err)
	}
	var count int
	if err := f.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+schema+".decision_engine_effect_job_receipts").Scan(&count); err != nil || count != 4 {
		t.Fatal("unexpected required receipts", count, err)
	}
}
