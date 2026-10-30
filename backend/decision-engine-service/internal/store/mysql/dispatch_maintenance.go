package mysql

import (
	"context"
	"database/sql"
	"errors"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
)

var ErrDispatchReconciliationRequired = errors.New("dispatch has a durable send intent; reconcile before exhausting delivery")

// Receipt-aware maintenance is mandatory in the dispatch-enabled application.
// A lost external response is NOT proof of rejection. Retriable deliveries can
// resume reconciliation through DispatchWorker; at terminal transport exhaustion
// an existing intent needs explicit outcome reconciliation, never a new attempt.
// These constructors require the 149-step lineage; old component-only adapters
// retain their separate pre-dispatch maintenance contract.
func NewReceiptAwareEffectMaintenance(delivery *EffectDeliveryStore) (*DeliveryMaintenanceStore, error) {
	if delivery == nil {
		return nil, ErrOwnerContractUnavailable
	}
	copy := *delivery
	copy.receiptAwareExhaustion = true
	return NewOwnedEffectDeliveryMaintenance(&copy)
}
func NewReceiptAwareExecutionMaintenance(delivery *ExecutionDeliveryStore, owner *ExecutionStore) (*DeliveryMaintenanceStore, error) {
	if delivery == nil || delivery.transport == nil {
		return nil, ErrOwnerContractUnavailable
	}
	copy := *delivery.transport
	copy.receiptAwareExhaustion = true
	return NewExecutionDeliveryMaintenance(&ExecutionDeliveryStore{&copy}, owner)
}

func (s *EffectDeliveryStore) checkDispatchExhaustion(ctx context.Context, tx *sql.Tx, v ExpiredDelivery) error {
	if !s.receiptAwareExhaustion || s.executionKinds && s.kind != ports.JobCallback {
		return nil
	}
	var id mysqlstore.UUID
	err := tx.QueryRowContext(ctx, "SELECT receipt_id FROM "+s.schema+".decision_engine_dispatch_intents WHERE tenant_id=? AND receipt_id=? FOR UPDATE", mysqlstore.UUID(s.tenant), mysqlstore.UUID(v.ID)).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return ErrDispatchReconciliationRequired
}
