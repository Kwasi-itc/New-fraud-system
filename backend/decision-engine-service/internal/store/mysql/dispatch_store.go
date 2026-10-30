package mysql

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
	"unicode/utf8"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/integration"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/mysqljobs"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
)

var ErrDispatchReceipt = errors.New("invalid or inconsistent provider dispatch receipt")

// DispatchStore is a private composition of real decision owners and transport,
// not an arbitrary tx callback. It installs no provider and starts no worker.
type DispatchStore struct {
	effects    *EffectStore
	executions *ExecutionStore
	delivery   *EffectDeliveryStore
	maxBytes   int
}

func NewEffectDispatchStore(owner *EffectStore, delivery *EffectDeliveryStore) (*DispatchStore, error) {
	if owner == nil || owner.configuration == nil || delivery == nil || delivery.executionKinds || delivery.authorize == nil {
		return nil, ErrDecisionAuthorizationUnavailable
	}
	c := owner.configuration
	if c.db != delivery.db || c.schema != delivery.schema || c.tenant != delivery.tenant || owner.queues[delivery.kind].Name != delivery.queue {
		return nil, ErrTenantScope
	}
	return &DispatchStore{effects: owner, delivery: delivery, maxBytes: c.maxBytes}, nil
}
func NewCallbackDispatchStore(owner *ExecutionStore, delivery *ExecutionDeliveryStore) (*DispatchStore, error) {
	if owner == nil || owner.configuration == nil || delivery == nil || delivery.transport == nil {
		return nil, ErrDecisionAuthorizationUnavailable
	}
	c, d := owner.configuration, delivery.transport
	if !d.executionKinds || d.kind != ports.JobCallback || c.db != d.db || c.schema != d.schema || c.tenant != d.tenant || owner.policy.Queues[ports.JobCallback].Name != d.queue {
		return nil, ErrTenantScope
	}
	return &DispatchStore{executions: owner, delivery: d, maxBytes: c.maxBytes}, nil
}
func (s *DispatchStore) LeaseDuration() (time.Duration, error) {
	if s == nil || s.delivery == nil {
		return 0, ErrDecisionAuthorizationUnavailable
	}
	return s.delivery.lease, nil
}
func (s *DispatchStore) Claim(ctx context.Context) (*EffectDeliveryLease, error) {
	if s == nil || s.delivery == nil {
		return nil, ErrDecisionAuthorizationUnavailable
	}
	return s.delivery.Claim(ctx)
}
func (s *DispatchStore) Heartbeat(ctx context.Context, lease EffectDeliveryLease) error {
	if s == nil || s.delivery == nil {
		return ErrDecisionAuthorizationUnavailable
	}
	return s.delivery.Heartbeat(ctx, lease)
}
func callbackLease(v EffectDeliveryLease) ExecutionLease {
	return ExecutionLease{ID: v.ID, Token: v.Token, Identity: v.Identity, TransportAttempt: v.Attempt}
}

type DispatchIntent struct {
	Request ports.DispatchRequest
	Resumed bool
}

// Prepare commits intent before IO. A resumed intent retains its ORIGINAL body,
// even if a mutable owner row has changed. It must be reconciled before resend.
func (s *DispatchStore) Prepare(ctx context.Context, lease EffectDeliveryLease) (out DispatchIntent, err error) {
	if s == nil || s.delivery == nil || !s.delivery.validLease(lease) {
		return out, ErrStaleEffectLease
	}
	if s.executions != nil {
		err = s.executions.Run(ctx, ExecutionRun, func(r ExecutionRepositories) error {
			v, err := r.Async.StartCallback(ctx, callbackLease(lease))
			if err != nil {
				return err
			}
			body := map[string]any{"execution_id": v.ID, "tenant_id": v.TenantID, "scenario_id": v.ScenarioID, "object_type": v.ObjectType, "status": v.Status, "event": "async_decision_execution." + string(v.Status)}
			if len(v.ResultBody) > 0 {
				body["result_body"] = json.RawMessage(v.ResultBody)
			}
			if v.LastError != "" {
				body["error"] = v.LastError
			}
			// Callback URL is destination material for the provider policy, not
			// permission to send internal credentials to caller-controlled hosts.
			return s.prepareIntent(ctx, r.Async.tx, lease, map[string]any{"callback_url": v.CallbackURL, "body": body}, &out)
		})
	} else {
		err = mysqlstore.Mutation(ctx, s.delivery.db, func(tx *sql.Tx) error {
			if err := s.authorizeEffect(ctx, tx); err != nil {
				return err
			}
			status, owned, err := lockEffectOwner(ctx, tx, s.delivery.schema, lease.Identity)
			if err != nil {
				return err
			}
			if owned {
				return ErrEffectSourceOwned
			}
			if !effectDispatchPending(lease.Identity.Kind, status) {
				return ErrEffectJobOwnerState
			}
			if err := s.lockEffectDispatchLease(ctx, tx, lease); err != nil {
				return err
			}
			r := s.effects.repositories(tx, false, nil, nil)
			var body any
			tenant, id := lease.Identity.TenantID.String(), lease.Identity.ResourceID.String()
			switch lease.Identity.Kind {
			case ports.JobWorkflow:
				v, e := r.WorkflowExecutions.GetByID(ctx, tenant, id)
				if e != nil {
					return e
				}
				body = map[string]any{"execution_id": v.ID, "tenant_id": v.TenantID, "workflow_id": v.WorkflowID, "workflow_rule_id": v.WorkflowRuleID, "workflow_action_id": v.WorkflowActionID, "decision_id": v.DecisionID, "scenario_id": v.ScenarioID, "action_type": v.ActionType, "action_config": json.RawMessage(v.ActionConfig), "created_at": v.CreatedAt}
			case ports.JobScreening:
				v, e := r.ScreeningExecutions.GetByID(ctx, tenant, id)
				if e != nil {
					return e
				}
				body = map[string]any{"execution_id": v.ID, "request": json.RawMessage(v.RequestJSON)}
			case ports.JobScoring:
				v, e := r.ScoringRequests.GetByID(ctx, tenant, id)
				if e != nil {
					return e
				}
				body = map[string]any{"request_id": v.ID, "request": json.RawMessage(v.RequestJSON)}
			case ports.JobOutbox:
				v, e := r.OutboxEvents.GetByID(ctx, tenant, id)
				if e != nil {
					return e
				}
				body = map[string]any{"event_id": v.ID, "tenant_id": v.TenantID, "aggregate_type": v.AggregateType, "aggregate_id": v.AggregateID, "event_type": v.EventType, "payload": json.RawMessage(v.Payload), "created_at": v.CreatedAt}
			default:
				return ErrDispatchReceipt
			}
			if err := s.prepareIntent(ctx, tx, lease, body, &out); err != nil {
				return err
			}
			return s.checkDispatchLease(ctx, tx, lease)
		})
	}
	if err != nil {
		return DispatchIntent{}, err
	}
	return out, nil
}

func (s *DispatchStore) authorizeEffect(ctx context.Context, tx *sql.Tx) error {
	d := s.delivery
	if err := d.authorize(ctx, tx, d.tenant, d.kind, EffectWorkerFinish); err != nil {
		return err
	}
	return s.effects.configuration.authorize(ctx, tx, d.tenant, true)
}
func (s *DispatchStore) lockEffectDispatchLease(ctx context.Context, tx *sql.Tx, v EffectDeliveryLease) error {
	d := s.delivery
	if err := d.fenceCurrentEffect(ctx, tx, v.Identity, v.ID); err != nil {
		return err
	}
	var id mysqlstore.UUID
	if err := tx.QueryRowContext(ctx, "SELECT id FROM "+d.schema+"."+d.receiptTable()+" WHERE tenant_id=? AND kind=? AND id=? FOR UPDATE", mysqlstore.UUID(d.tenant), string(d.kind), mysqlstore.UUID(v.ID)).Scan(&id); err != nil {
		return err
	}
	return s.checkDispatchLease(ctx, tx, v)
}
func (s *DispatchStore) checkDispatchLease(ctx context.Context, tx *sql.Tx, v EffectDeliveryLease) error {
	var id mysqlstore.UUID
	err := tx.QueryRowContext(ctx, "SELECT id FROM "+s.delivery.schema+"."+s.delivery.deliveryTable()+effectLeasePredicate+" FOR UPDATE", s.delivery.leaseWhere(v)...).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrStaleEffectLease
	}
	return err
}

func canonicalDispatch(raw []byte, max int) ([]byte, error) {
	if len(raw) == 0 || len(raw) > max {
		return nil, ErrProjectionLimit
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var v any
	if err := d.Decode(&v); err != nil {
		return nil, err
	}
	out, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	if len(out) > max {
		return nil, ErrProjectionLimit
	}
	return out, nil
}

func (s *DispatchStore) prepareIntent(ctx context.Context, tx *sql.Tx, lease EffectDeliveryLease, body any, out *DispatchIntent) error {
	d := s.delivery
	var payload mysqlstore.JSON
	var savedHash []byte
	var outcome string
	var size int64
	err := tx.QueryRowContext(ctx, "SELECT OCTET_LENGTH(payload),IF(OCTET_LENGTH(payload)<=?,payload,JSON_OBJECT()),payload_hash,outcome FROM "+d.schema+".decision_engine_dispatch_intents WHERE tenant_id=? AND kind=? AND receipt_id=? FOR UPDATE", s.maxBytes, mysqlstore.UUID(d.tenant), string(d.kind), mysqlstore.UUID(lease.ID)).Scan(&size, &payload, &savedHash, &outcome)
	resumed := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if resumed && (size > int64(s.maxBytes) || outcome != "unknown") {
		return ErrDispatchReceipt
	}
	if !resumed {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		payload = raw
	}
	raw, err := canonicalDispatch(payload, s.maxBytes)
	if err != nil {
		return err
	}
	hash := sha256.Sum256(raw)
	if resumed && !bytes.Equal(hash[:], savedHash) {
		return ErrDispatchReceipt
	}
	if !resumed {
		var effect, execution any = mysqlstore.UUID(lease.ID), nil
		var executionKind any
		if d.executionKinds {
			effect, execution = nil, mysqlstore.UUID(lease.ID)
			executionKind = string(d.kind)
		}
		_, err = tx.ExecContext(ctx, "INSERT INTO "+d.schema+".decision_engine_dispatch_intents(receipt_id,tenant_id,kind,resource_id,domain_attempt,redrive_generation,effect_receipt_id,execution_receipt_id,execution_kind,payload,payload_hash,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,UTC_TIMESTAMP(6))", mysqlstore.UUID(lease.ID), mysqlstore.UUID(d.tenant), string(d.kind), mysqlstore.UUID(lease.Identity.ResourceID), lease.Identity.DomainAttempt, lease.Identity.RedriveGeneration, effect, execution, executionKind, mysqlstore.JSON(raw), hash[:])
		if err != nil {
			return err
		}
	}
	key, err := dispatchIdentityKey(lease.Identity)
	if err != nil {
		return err
	}
	out.Request = ports.DispatchRequest{Key: key, Kind: d.kind, TenantID: d.tenant.String(), ResourceID: lease.Identity.ResourceID.String(), DomainAttempt: lease.Identity.DomainAttempt, RedriveGeneration: lease.Identity.RedriveGeneration, PayloadSHA256: hex.EncodeToString(hash[:]), Payload: raw}
	out.Resumed = resumed
	return nil
}

// Complete atomically records the provider's definitive receipt, owner outcome
// and queue acknowledgement. Unknown outcomes MUST NOT call this operation.
// Rejected callbacks may allocate their next DOMAIN attempt in the same tx.
func (s *DispatchStore) Complete(ctx context.Context, lease EffectDeliveryLease, request ports.DispatchRequest, receipt ports.DispatchReceipt, callbackRetry time.Duration) error {
	if s == nil || s.delivery == nil || !s.delivery.validLease(lease) || ValidateDispatchReceipt(request, receipt) != nil || receipt.Outcome != ports.DispatchAccepted && receipt.Outcome != ports.DispatchRejected {
		return ErrDispatchReceipt
	}
	if s.executions != nil {
		if callbackRetry <= 0 || callbackRetry > 24*time.Hour {
			return ErrDispatchReceipt
		}
		return s.executions.Run(ctx, ExecutionRun, func(r ExecutionRepositories) error {
			// Lock owner, exact receipt and delivery before intent; same lock order
			// as Prepare/cancel/rescue. Recheck again in CallbackOutcome.
			v, err := r.Async.locked(ctx, callbackLease(lease), ports.JobCallback)
			if err != nil {
				return err
			}
			if err := r.Async.lockLease(ctx, callbackLease(lease), ports.JobCallback); err != nil {
				return err
			}
			if err := s.recordReceipt(ctx, r.Async.tx, lease, request, receipt); err != nil {
				return err
			}
			var next *time.Time
			code := ""
			if receipt.Outcome == ports.DispatchRejected {
				code = "provider_rejected"
				if v.CallbackAttemptCount < v.CallbackMaxAttempts {
					at, err := r.Async.databaseTime(ctx)
					if err != nil {
						return err
					}
					later := at.Add(callbackRetry)
					next = &later
				}
			}
			return r.Async.CallbackOutcome(ctx, callbackLease(lease), receipt.Outcome == ports.DispatchAccepted, code, next)
		})
	}
	return s.delivery.finish(ctx, lease, false, "", func(ctx context.Context, tx *sql.Tx) error {
		return s.effects.configuration.authorize(ctx, tx, s.delivery.tenant, true)
	}, func(tx *sql.Tx) error {
		status, owned, err := lockEffectOwner(ctx, tx, s.delivery.schema, lease.Identity)
		if err != nil {
			return err
		}
		if owned {
			return ErrEffectSourceOwned
		}
		if !effectDispatchPending(lease.Identity.Kind, status) {
			return ErrEffectJobOwnerState
		}
		if err := s.recordReceipt(ctx, tx, lease, request, receipt); err != nil {
			return err
		}
		accepted := receipt.Outcome == ports.DispatchAccepted
		switch lease.Identity.Kind {
		case ports.JobWorkflow:
			status := workflow.ExecutionStatusDispatchFailed
			if accepted {
				status = workflow.ExecutionStatusDispatched
			}
			return s.effects.repositories(tx, true, nil, nil).WorkflowExecutions.UpdateStatus(ctx, lease.Identity.ResourceID.String(), status)
		case ports.JobOutbox:
			status := integration.OutboxStatusFailed
			if accepted {
				status = integration.OutboxStatusSent
			}
			return s.effects.repositories(tx, true, nil, nil).OutboxEvents.UpdateStatus(ctx, lease.Identity.ResourceID.String(), status)
		case ports.JobScreening, ports.JobScoring:
			status, stamp, code := "failed", "failed_at", "provider_rejected"
			if accepted {
				status, stamp, code = "sent", "sent_at", ""
			}
			_, err := tx.ExecContext(ctx, "UPDATE "+s.delivery.schema+"."+effectOwnerTable(lease.Identity.Kind)+" SET status=?,provider_reference=?,last_error=?,updated_at=UTC_TIMESTAMP(6),"+stamp+"=UTC_TIMESTAMP(6) WHERE tenant_id=? AND id=?", status, mysqlstore.Text(receipt.Reference), code, mysqlstore.UUID(s.delivery.tenant), mysqlstore.UUID(lease.Identity.ResourceID))
			return err
		}
		return ErrDispatchReceipt
	})
}

func ValidateDispatchReceipt(request ports.DispatchRequest, receipt ports.DispatchReceipt) error {
	if request.Key == "" || request.PayloadSHA256 == "" || receipt.Key != request.Key || receipt.PayloadSHA256 != request.PayloadSHA256 {
		return ErrDispatchReceipt
	}
	switch receipt.Outcome {
	case ports.DispatchAccepted, ports.DispatchRejected:
		if len(receipt.Reference) == 0 || len(receipt.Reference) > 512 || !utf8.ValidString(receipt.Reference) || bytes.ContainsRune([]byte(receipt.Reference), 0) {
			return ErrDispatchReceipt
		}
	case ports.DispatchNotFound, ports.DispatchUnknown:
		if receipt.Reference != "" {
			return ErrDispatchReceipt
		}
	default:
		return ErrDispatchReceipt
	}
	return nil
}
func (s *DispatchStore) recordReceipt(ctx context.Context, tx *sql.Tx, v EffectDeliveryLease, request ports.DispatchRequest, receipt ports.DispatchReceipt) error {
	key, err := dispatchIdentityKey(v.Identity)
	if err != nil {
		return err
	}
	if request.Key != key {
		return ErrDispatchReceipt
	}
	hash, err := hex.DecodeString(request.PayloadSHA256)
	if err != nil || len(hash) != 32 {
		return ErrDispatchReceipt
	}
	result, err := tx.ExecContext(ctx, "UPDATE "+s.delivery.schema+".decision_engine_dispatch_intents SET outcome=?,provider_reference=?,resolved_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND kind=? AND receipt_id=? AND resource_id=? AND domain_attempt=? AND redrive_generation=? AND payload_hash=? AND outcome='unknown'", string(receipt.Outcome), mysqlstore.Text(receipt.Reference), mysqlstore.UUID(s.delivery.tenant), string(s.delivery.kind), mysqlstore.UUID(v.ID), mysqlstore.UUID(v.Identity.ResourceID), v.Identity.DomainAttempt, v.Identity.RedriveGeneration, hash)
	return effectDeliveryChanged(result, err)
}

// CompletionCommitted is a read-only reconciliation, not a retry. Terminal
// intent is proof only because it commits in the same transaction as owner and
// queue outcome. It survives transport retention and does not accept a newer
// domain attempt or redrive generation as proof of this attempt.
func (s *DispatchStore) CompletionCommitted(ctx context.Context, lease EffectDeliveryLease, receipt ports.DispatchReceipt) (bool, error) {
	if s == nil || s.delivery == nil || !s.delivery.validLease(lease) {
		return false, ErrStaleEffectLease
	}
	var found bool
	read := func(tx *sql.Tx) error {
		var outcome, reference string
		var hash []byte
		err := tx.QueryRowContext(ctx, "SELECT outcome,provider_reference,payload_hash FROM "+s.delivery.schema+".decision_engine_dispatch_intents WHERE tenant_id=? AND kind=? AND receipt_id=? AND resource_id=? AND domain_attempt=? AND redrive_generation=?", mysqlstore.UUID(s.delivery.tenant), string(s.delivery.kind), mysqlstore.UUID(lease.ID), mysqlstore.UUID(lease.Identity.ResourceID), lease.Identity.DomainAttempt, lease.Identity.RedriveGeneration).Scan(&outcome, &reference, &hash)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		key, err := dispatchIdentityKey(lease.Identity)
		if err != nil {
			return err
		}
		found = receipt.Key == key && receipt.PayloadSHA256 == hex.EncodeToString(hash) && (outcome == "accepted" || outcome == "rejected") && outcome == string(receipt.Outcome) && reference == receipt.Reference
		return nil
	}
	var err error
	if s.executions != nil {
		err = s.executions.Run(ctx, ExecutionRun, func(r ExecutionRepositories) error { return read(r.Async.tx) })
	} else {
		err = mysqlstore.Mutation(ctx, s.delivery.db, func(tx *sql.Tx) error {
			if err := s.authorizeEffect(ctx, tx); err != nil {
				return err
			}
			return read(tx)
		})
	}
	if err != nil {
		return false, err
	}
	return found, nil
}

func dispatchIdentityKey(identity mysqljobs.Identity) (string, error) {
	// Exclude transport payload version and lease attempt: neither is a new
	// logical external delivery. Full scalar identity remains unique in SQL;
	// this digest is only the bounded provider key, not SQL deduplication proof.
	key, err := identity.Key()
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(key)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(raw)
	return "decision-v1-" + hex.EncodeToString(digest[:]), nil
}
