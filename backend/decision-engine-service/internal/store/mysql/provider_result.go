package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/mysqljobs"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
	"reflect"
)

// ApplyScoringResult is a fenced owner mutation for an accepted provider result.
// It never modifies the machine decision. Receipt recording and delivery happen
// before this call; a provider transaction must not be retained across it.
func (s *EffectStore) ApplyScoringResult(ctx context.Context, request ports.DispatchRequest, reference string, result json.RawMessage) error {
	if s == nil || request.Kind != ports.JobScoring || request.TenantID != s.configuration.tenant.String() || len(result) > s.configuration.maxBytes || !json.Valid(result) {
		return ErrTenantScope
	}
	id, err := uuid.Parse(request.ResourceID)
	if err != nil {
		return ErrTenantScope
	}
	expected := mysqljobs.Identity{Version: 1, TenantID: s.configuration.tenant, Kind: ports.JobScoring, ResourceID: id, DomainAttempt: request.DomainAttempt, RedriveGeneration: request.RedriveGeneration}
	return mysqlstore.Mutation(ctx, s.configuration.db, func(tx *sql.Tx) error {
		c := s.configuration
		if err := c.authorize(ctx, tx, c.tenant, true); err != nil {
			return err
		}
		status, _, err := lockEffectOwner(ctx, tx, c.schema, expected)
		if err != nil {
			return err
		}
		receipt, current, err := currentEffectReceipt(ctx, tx, c.schema, expected)
		if err != nil {
			return err
		}
		if current != expected {
			return ErrEffectRetryState
		}
		var outcome, ref, hash string
		if err := tx.QueryRowContext(ctx, "SELECT outcome,provider_reference,LOWER(HEX(payload_hash)) FROM "+c.schema+".decision_engine_dispatch_intents WHERE tenant_id=? AND kind=? AND receipt_id=?", mysqlstore.UUID(c.tenant), string(ports.JobScoring), mysqlstore.UUID(receipt)).Scan(&outcome, &ref, &hash); err != nil {
			return err
		}
		if outcome != "accepted" || ref != reference || hash != request.PayloadSHA256 {
			return ErrEffectJobOwnerState
		}
		if status == "completed" {
			var prior []byte
			if err := tx.QueryRowContext(ctx, "SELECT response_json FROM "+c.schema+".decision_engine_scoring_requests WHERE tenant_id=? AND id=?", mysqlstore.UUID(c.tenant), mysqlstore.UUID(id)).Scan(&prior); err != nil {
				return err
			}
			var a, b any
			if json.Unmarshal(prior, &a) != nil || json.Unmarshal(result, &b) != nil || !reflect.DeepEqual(a, b) {
				return ErrEffectRetryState
			}
			return nil
		}
		if status != "sent" {
			// Acceptance and sent status commit together. A later status change
			// supersedes this result; it must not occupy the pending inbox forever.
			return ErrEffectRetryState
		}
		changed, err := tx.ExecContext(ctx, "UPDATE "+c.schema+".decision_engine_scoring_requests SET status='completed',response_json=?,completed_at=UTC_TIMESTAMP(6),updated_at=UTC_TIMESTAMP(6),failed_at=NULL,last_error='' WHERE tenant_id=? AND id=? AND provider_reference=?", mysqlstore.JSON(result), mysqlstore.UUID(c.tenant), mysqlstore.UUID(id), reference)
		if err != nil {
			return err
		}
		n, err := changed.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return ErrEffectRetryState
		}
		return nil
	})
}
