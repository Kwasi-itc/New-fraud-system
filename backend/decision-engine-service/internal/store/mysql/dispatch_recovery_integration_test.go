package mysql

import (
	"context"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/integration"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMySQLExhaustedDispatchReconciliationPreservesIdentityAndFencesWorkers(t *testing.T) {
	f, _, owner := dispatchFixture(t)
	ctx := context.Background()
	delivery, _ := retryStores(t, f, ports.JobOutbox)
	dispatch, err := NewEffectDispatchStore(owner, delivery)
	if err != nil {
		t.Fatal(err)
	}
	first, err := dispatch.Claim(ctx)
	if err != nil || first == nil {
		t.Fatal(first, err)
	}
	intent, err := dispatch.Prepare(ctx, *first)
	if err != nil {
		t.Fatal(err)
	}
	schema, _ := mysqlstore.Identifier(f.database)
	if _, err := f.db.Exec("UPDATE "+schema+".decision_engine_effect_job_deliveries SET attempt=max_attempts,error_count=max_attempts-1,lease_until=TIMESTAMPADD(SECOND,-1,UTC_TIMESTAMP(6)) WHERE id=?", mysqlstore.UUID(first.ID)); err != nil {
		t.Fatal(err)
	}
	// The exhausted intent stays selective beside a 5,000-row available backlog.
	// Use real owner writes so receipt/queue triggers match runtime behavior.
	d := decisionInput(f.tenant, f.parent, f.iteration.ID)
	if err := owner.Mutate(ctx, func(r EffectRepositories) error { _, err := r.Decisions.Create(ctx, d); return err }); err != nil {
		t.Fatal(err)
	}
	for range 10 {
		events := make([]integration.OutboxEvent, 500)
		for i := range events {
			events[i] = jobOutboxInput(f, d)
		}
		if err := owner.Mutate(ctx, func(r EffectRepositories) error { _, err := r.OutboxEvents.CreateMany(ctx, events); return err }); err != nil {
			t.Fatal(err)
		}
	}
	query, args := delivery.expiredSelectFiltered(nil, 1, true)
	var plan string
	if err := f.db.QueryRowContext(ctx, "EXPLAIN ANALYZE "+query, args...).Scan(&plan); err != nil {
		t.Fatal(err)
	}
	t.Log(plan)
	if !strings.Contains(plan, "de_effect_delivery_rescue") || strings.Contains(strings.ToLower(plan), "table scan on d ") {
		t.Fatal("reconciliation scans transport history", plan)
	}
	var mu sync.Mutex
	var claims []*EffectDeliveryLease
	var failures []error
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			lease, err := dispatch.ClaimReconciliation(ctx)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failures = append(failures, err)
			}
			if lease != nil {
				claims = append(claims, lease)
			}
		}()
	}
	wg.Wait()
	if len(failures) > 0 || len(claims) != 1 {
		t.Fatal(claims, failures)
	}
	resumed := claims[0]
	if resumed.Identity != first.Identity || resumed.ID != first.ID || resumed.Attempt != 3 || resumed.Token == first.Token {
		t.Fatal("reconciliation changed identity or reused expired token", resumed)
	}
	next, err := dispatch.Prepare(ctx, *resumed)
	if err != nil || !next.Resumed || string(next.Request.Payload) != string(intent.Request.Payload) {
		t.Fatal(next, err)
	}
	receipt := ports.DispatchReceipt{Key: next.Request.Key, PayloadSHA256: next.Request.PayloadSHA256, Outcome: ports.DispatchAccepted, Reference: "local-retained-receipt"}
	if err := dispatch.Complete(ctx, *resumed, next.Request, receipt, time.Second); err != nil {
		t.Fatal(err)
	}
	if err := dispatch.Heartbeat(ctx, *first); !errors.Is(err, ErrStaleEffectLease) {
		t.Fatal("old worker retained authority", err)
	}
	if lease, err := dispatch.ClaimReconciliation(ctx); err != nil || lease != nil {
		t.Fatal("completed delivery reclaimed", lease, err)
	}
}
