package mysql

import (
	"context"
	"errors"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
)

func TestMySQLReceiptAwareMaintenanceDoesNotExhaustUnreconciledSend(t *testing.T) {
	f, _, owner := dispatchFixture(t)
	ctx := context.Background()
	delivery, _ := retryStores(t, f, ports.JobOutbox)
	store, err := NewEffectDispatchStore(owner, delivery)
	if err != nil {
		t.Fatal(err)
	}
	lease := claimRetryEffect(t, delivery)
	if _, err := store.Prepare(ctx, lease); err != nil {
		t.Fatal(err)
	}
	schema, _ := mysqlstore.Identifier(f.database)
	if _, err := f.db.Exec("UPDATE "+schema+".decision_engine_effect_job_deliveries SET attempt=max_attempts,error_count=max_attempts-1,lease_until=TIMESTAMPADD(SECOND,-1,UTC_TIMESTAMP(6)) WHERE id=?", mysqlstore.UUID(lease.ID)); err != nil {
		t.Fatal(err)
	}
	maintenance, err := NewReceiptAwareEffectMaintenance(delivery)
	if err != nil {
		t.Fatal(err)
	}
	if n, _, err := maintenance.MaintainPage(ctx, 10, false, nil); !errors.Is(err, ErrDispatchReconciliationRequired) || n != 0 {
		t.Fatal("unreconciled send was exhausted", n, err)
	}
	var state, status, outcome string
	if err := f.db.QueryRow("SELECT d.state,o.status,i.outcome FROM "+schema+".decision_engine_effect_job_deliveries d JOIN "+schema+".decision_engine_effect_job_receipts r ON r.id=d.id AND r.tenant_id=d.tenant_id JOIN "+schema+".decision_engine_outbox_events o ON o.id=r.resource_id AND o.tenant_id=r.tenant_id JOIN "+schema+".decision_engine_dispatch_intents i ON i.receipt_id=d.id AND i.tenant_id=d.tenant_id WHERE d.id=?", mysqlstore.UUID(lease.ID)).Scan(&state, &status, &outcome); err != nil {
		t.Fatal(err)
	}
	if state != "running" || status != "pending" || outcome != "unknown" {
		t.Fatal("lost response became false failure", state, status, outcome)
	}
}
