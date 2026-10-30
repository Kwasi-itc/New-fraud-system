package mysql

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/mysqljobs"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/workers"
	"github.com/google/uuid"
)

// EffectCancellationRecord is a permanent decision-owned DISPATCH cancellation,
// not a cancelled screening/scoring result. It survives transport retention.
// Public adapters must expose this separate outcome rather than pretending a
// provider's business status changed. Actor auditing belongs to the trusted
// authorization provider; this adapter never invents a user identity.
type EffectCancellationRecord struct {
	Identity    mysqljobs.Identity
	ReasonCode  string
	CancelledAt time.Time
	Replay      bool
}

// Cancel requires the appended cancellation lineage and separate authority. It
// locks one resource -> latest receipt -> exact transport. The record and active
// delivery cancellation commit together without clearing results/evidence or
// making network calls. Already-sent/completed business work cannot be undone.
// An in-flight receiver may still accept an earlier send; worker fencing only
// prevents its later local acknowledgement from committing.
func (s *EffectRetryStore) Cancel(ctx context.Context, expected mysqljobs.Identity, reasonCode string) (EffectCancellationRecord, error) {
	if s == nil || s.db == nil || s.authorize == nil || expected.Validate() != nil || expected.TenantID != s.tenant || expected.Kind != s.kind || !effectErrorCode.MatchString(reasonCode) {
		return EffectCancellationRecord{}, ErrEffectRetryState
	}
	var out EffectCancellationRecord
	err := mysqlstore.Mutation(ctx, s.db, func(tx *sql.Tx) error {
		if err := s.authorize(ctx, tx, s.tenant, s.kind, EffectCancel); err != nil {
			return err
		}
		status, sourceOwned, err := lockEffectOwner(ctx, tx, s.schema, expected)
		if err != nil {
			return err
		}
		id, current, err := currentEffectReceipt(ctx, tx, s.schema, expected)
		if err != nil {
			return err
		}
		if current != expected {
			return ErrEffectRetryState
		}
		prior, found, err := effectCancellation(ctx, tx, s.schema, id, expected)
		if err != nil {
			return err
		}
		if found {
			if prior.ReasonCode != reasonCode {
				return ErrEffectRetryState
			}
			prior.Replay = true
			out = prior
			return nil
		}
		if sourceOwned {
			return ErrEffectSourceOwned
		}
		if !effectDispatchPending(s.kind, status) {
			return ErrEffectRetryState
		}
		var state string
		err = tx.QueryRowContext(ctx, "SELECT state FROM "+s.schema+".decision_engine_effect_job_deliveries WHERE tenant_id=? AND kind=? AND queue_name=? AND id=? FOR UPDATE", mysqlstore.UUID(s.tenant), string(s.kind), s.policy.Name, mysqlstore.UUID(id)).Scan(&state)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrEffectRetryState
		}
		if err != nil {
			return err
		}
		if state != string(workers.Available) && state != string(workers.Running) {
			return ErrEffectRetryState
		}
		outcome, _, err := workers.ResolveCancellation(workers.DeliveryState(state))
		if err != nil {
			return err
		}
		if outcome.State != workers.Cancelled {
			return ErrEffectRetryState
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO "+s.schema+".decision_engine_effect_cancellations(receipt_id,tenant_id,kind,resource_id,domain_attempt,redrive_generation,reason_code,cancelled_at) VALUES(?,?,?,?,?,?,?,UTC_TIMESTAMP(6))", mysqlstore.UUID(id), mysqlstore.UUID(s.tenant), string(s.kind), mysqlstore.UUID(expected.ResourceID), expected.DomainAttempt, expected.RedriveGeneration, reasonCode); err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, "UPDATE "+s.schema+".decision_engine_effect_job_deliveries SET state='cancelled',lease_token=NULL,lease_until=NULL,finished_at=UTC_TIMESTAMP(6),last_error_code=? WHERE tenant_id=? AND kind=? AND queue_name=? AND id=? AND state IN('available','running')", reasonCode, mysqlstore.UUID(s.tenant), string(s.kind), s.policy.Name, mysqlstore.UUID(id))
		if err := effectDeliveryChanged(result, err); err != nil {
			return err
		}
		out, found, err = effectCancellation(ctx, tx, s.schema, id, expected)
		if err == nil && !found {
			return ErrEffectRetryState
		}
		return err
	})
	if err != nil {
		return EffectCancellationRecord{}, err
	}
	return out, nil
}

func effectDispatchPending(kind ports.JobKind, status string) bool {
	switch kind {
	case ports.JobWorkflow:
		return status == "pending_dispatch"
	case ports.JobScreening, ports.JobScoring:
		return status == "pending" || status == "queued"
	case ports.JobOutbox:
		return status == "pending"
	}
	return false
}

func effectCancellation(ctx context.Context, tx *sql.Tx, schema string, id uuid.UUID, identity mysqljobs.Identity) (EffectCancellationRecord, bool, error) {
	out := EffectCancellationRecord{Identity: identity}
	var at mysqlstore.DateTime
	err := tx.QueryRowContext(ctx, "SELECT reason_code,cancelled_at FROM "+schema+".decision_engine_effect_cancellations WHERE receipt_id=? AND tenant_id=? AND kind=? AND resource_id=? AND domain_attempt=? AND redrive_generation=?", mysqlstore.UUID(id), mysqlstore.UUID(identity.TenantID), string(identity.Kind), mysqlstore.UUID(identity.ResourceID), identity.DomainAttempt, identity.RedriveGeneration).Scan(&out.ReasonCode, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return EffectCancellationRecord{}, false, nil
	}
	if err != nil {
		return EffectCancellationRecord{}, false, err
	}
	out.CancelledAt = at.Time
	return out, true, nil
}
