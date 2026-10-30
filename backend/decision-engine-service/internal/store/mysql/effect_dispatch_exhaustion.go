package mysql

import (
	"context"
	"database/sql"
	"errors"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/mysqljobs"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
)

// NewOwnedEffectDeliveryMaintenance supplies a real decision-owned LOCAL SQL
// dispatch-exhaustion handler, not a successful no-op. The transport rechecks
// trusted rescue/cleanup authority and the current owner/receipt/expired lease.
// Sent/completed work and source-managed screening require reconciliation; they
// are not overwritten with a fabricated failure. This does not start a poller,
// dispatch an external effect or implement the foreign owner's result handler.
func NewOwnedEffectDeliveryMaintenance(s *EffectDeliveryStore) (*DeliveryMaintenanceStore, error) {
	if s == nil || s.executionKinds || s.db == nil || s.authorize == nil || effectOwnerTable(s.kind) == "" {
		return nil, errors.New("initialized four-effect transport required")
	}
	return NewEffectDeliveryMaintenance(s, s.dispatchExhaustion)
}

func (s *EffectDeliveryStore) dispatchExhaustion(ctx context.Context, tx *sql.Tx, identity mysqljobs.Identity) error {
	if tx == nil || identity.Validate() != nil || identity.Kind != s.kind || identity.TenantID != s.tenant {
		return ErrEffectRetryState
	}
	status, sourceOwned, err := lockEffectOwner(ctx, tx, s.schema, identity)
	if err != nil {
		return err
	}
	if sourceOwned {
		return ErrEffectSourceOwned
	}
	if !effectDispatchPending(s.kind, status) && !effectRetryEligible(s.kind, status, EffectDomainRetry) {
		return ErrEffectRetryState
	}
	set := "status='failed'"
	if s.kind == ports.JobWorkflow {
		set = "status='dispatch_failed'"
	}
	if s.kind == ports.JobScreening || s.kind == ports.JobScoring {
		set += ",updated_at=UTC_TIMESTAMP(6),failed_at=COALESCE(failed_at,UTC_TIMESTAMP(6)),last_error='delivery_attempts_exhausted'"
	}
	_, err = tx.ExecContext(ctx, "UPDATE "+s.schema+"."+effectOwnerTable(s.kind)+" SET "+set+" WHERE tenant_id=? AND id=?", mysqlstore.UUID(s.tenant), mysqlstore.UUID(identity.ResourceID))
	return err
}
