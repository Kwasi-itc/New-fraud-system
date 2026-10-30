package mysql

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/mysqljobs"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/workers"
	"github.com/google/uuid"
)

var ErrStaleEffectLease = errors.New("effect delivery lease is stale, expired or outside worker scope")

type EffectWorkerAction string

const (
	EffectWorkerClaim     EffectWorkerAction = "claim"
	EffectWorkerHeartbeat EffectWorkerAction = "heartbeat"
	EffectWorkerFinish    EffectWorkerAction = "finish"
	EffectWorkerRescue    EffectWorkerAction = "rescue"
	EffectWorkerCleanup   EffectWorkerAction = "cleanup"
)

// EffectWorkerAuthorizer is mandatory and distinct from browser authorization.
// The provider must verify trusted service identity, tenant AND kind/action in
// this transaction, before any owner/queue locks. No network calls or retained
// transaction. No provider is configured by this staged adapter.
type EffectWorkerAuthorizer func(context.Context, *sql.Tx, uuid.UUID, ports.JobKind, EffectWorkerAction) error

type EffectDeliveryLease struct {
	ID, Token uuid.UUID
	Identity  mysqljobs.Identity
	Attempt   int // Transport claim, never a domain attempt.
}

// EffectDeliveryStore is deliberately tenant/kind scoped: it cannot infer global
// worker authority from an empty tenant or claim another service's queue. It
// does not start a poller or perform network delivery. Exactly-once external
// effects are NOT promised; receivers need the stable Identity as idempotency key.
type EffectDeliveryStore struct {
	db                     *sql.DB
	schema                 string
	tenant                 uuid.UUID
	kind                   ports.JobKind
	queue                  string
	lease                  time.Duration
	authorize              EffectWorkerAuthorizer
	executionKinds         bool // Fixed owner catalog, never a caller-supplied table name.
	receiptAwareExhaustion bool // Requires installed dispatch-intent lineage.
}

func NewEffectDeliveryStore(db *sql.DB, database string, tenant uuid.UUID, kind ports.JobKind, queue string, lease time.Duration, authorize EffectWorkerAuthorizer) (*EffectDeliveryStore, error) {
	return newScopedDeliveryStore(db, database, tenant, kind, queue, lease, authorize, false)
}

func newScopedDeliveryStore(db *sql.DB, database string, tenant uuid.UUID, kind ports.JobKind, queue string, lease time.Duration, authorize EffectWorkerAuthorizer, executionKinds bool) (*EffectDeliveryStore, error) {
	schema, err := mysqlstore.Identifier(database)
	if err != nil {
		return nil, err
	}
	if db == nil || tenant == uuid.Nil || !effectQueueName.MatchString(queue) || authorize == nil || lease < time.Millisecond || lease > time.Hour || lease%time.Microsecond != 0 {
		return nil, errors.New("pool, explicit worker authority/scope, queue and microsecond lease in 1ms..1h required")
	}
	_, _, _, effect := effectJobKind(effectOwnerTable(kind))
	if (!executionKinds && !effect) || (executionKinds && executionOwner(kind) == "") {
		return nil, errors.New("job kind is outside this delivery owner's catalog")
	}
	return &EffectDeliveryStore{db: db, schema: schema, tenant: tenant, kind: kind, queue: queue, lease: lease, authorize: authorize, executionKinds: executionKinds}, nil
}

func (s *EffectDeliveryStore) deliveryTable() string {
	if s.executionKinds {
		return "decision_engine_execution_job_deliveries"
	}
	return "decision_engine_effect_job_deliveries"
}
func (s *EffectDeliveryStore) receiptTable() string {
	if s.executionKinds {
		return "decision_engine_execution_job_receipts"
	}
	return "decision_engine_effect_job_receipts"
}

func effectOwnerTable(kind ports.JobKind) string {
	switch kind {
	case ports.JobWorkflow:
		return "decision_engine_workflow_executions"
	case ports.JobScreening:
		return "decision_engine_screening_executions"
	case ports.JobScoring:
		return "decision_engine_scoring_requests"
	case ports.JobOutbox:
		return "decision_engine_outbox_events"
	}
	return ""
}

// Claim locks only a delivery row. Resource work starts after its commit; failed
// or ambiguous COMMIT returns NO lease and requires expiry/reconciliation.
func (s *EffectDeliveryStore) Claim(ctx context.Context) (*EffectDeliveryLease, error) {
	var result *EffectDeliveryLease
	err := mysqlstore.Mutation(ctx, s.db, func(tx *sql.Tx) error {
		if err := s.authorize(ctx, tx, s.tenant, s.kind, EffectWorkerClaim); err != nil {
			return err
		}
		var v EffectDeliveryLease
		var count int64
		err := tx.QueryRowContext(ctx, "SELECT id,attempt,error_count FROM "+s.schema+"."+s.deliveryTable()+" WHERE tenant_id=? AND kind=? AND queue_name=? AND state='available' AND available_at<=UTC_TIMESTAMP(6) AND attempt<max_attempts ORDER BY available_at,id LIMIT 1 FOR UPDATE SKIP LOCKED", mysqlstore.UUID(s.tenant), string(s.kind), s.queue).Scan((*mysqlstore.UUID)(&v.ID), &v.Attempt, &count)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := workers.ValidateErrorHistory(v.Attempt, &count, false); err != nil {
			return err
		}
		v.Identity, err = s.receipt(ctx, tx, v.ID)
		if err != nil {
			return err
		}
		v.Attempt++
		v.Token = uuid.New()
		if _, err := tx.ExecContext(ctx, "UPDATE "+s.schema+"."+s.deliveryTable()+" SET state='running',attempt=?,lease_token=?,lease_until=TIMESTAMPADD(MICROSECOND,?,UTC_TIMESTAMP(6)) WHERE id=?", v.Attempt, mysqlstore.UUID(v.Token), s.lease.Microseconds(), mysqlstore.UUID(v.ID)); err != nil {
			return err
		}
		result = &v
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s *EffectDeliveryStore) receipt(ctx context.Context, tx *sql.Tx, id uuid.UUID) (mysqljobs.Identity, error) {
	v := mysqljobs.Identity{Version: 1, Kind: s.kind, TenantID: s.tenant}
	err := tx.QueryRowContext(ctx, "SELECT resource_id,domain_attempt,redrive_generation FROM "+s.schema+"."+s.receiptTable()+" WHERE tenant_id=? AND kind=? AND id=?", mysqlstore.UUID(s.tenant), string(s.kind), mysqlstore.UUID(id)).Scan((*mysqlstore.UUID)(&v.ResourceID), &v.DomainAttempt, &v.RedriveGeneration)
	if err != nil {
		return v, err
	}
	// Successor identities are allocated only by the locked owner lifecycle;
	// decoding a receipt is not authority to mutate that owner.
	if v.Validate() != nil {
		return v, mysqljobs.ErrIdentity
	}
	return v, nil
}

func (s *EffectDeliveryStore) validLease(v EffectDeliveryLease) bool {
	return v.ID != uuid.Nil && v.Token != uuid.Nil && v.Attempt > 0 && v.Attempt <= 2147483647 && v.Identity.Validate() == nil && v.Identity.TenantID == s.tenant && v.Identity.Kind == s.kind
}

func effectDeliveryChanged(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrStaleEffectLease
	}
	return nil
}

func (s *EffectDeliveryStore) leaseWhere(v EffectDeliveryLease) []any {
	return []any{mysqlstore.UUID(s.tenant), string(s.kind), s.queue, mysqlstore.UUID(v.ID), mysqlstore.UUID(v.Token), v.Attempt}
}

const effectLeasePredicate = " WHERE tenant_id=? AND kind=? AND queue_name=? AND id=? AND state='running' AND lease_token=? AND attempt=? AND lease_until>UTC_TIMESTAMP(6)"

func (s *EffectDeliveryStore) Heartbeat(ctx context.Context, v EffectDeliveryLease) error {
	if !s.validLease(v) {
		return ErrStaleEffectLease
	}
	return mysqlstore.Mutation(ctx, s.db, func(tx *sql.Tx) error {
		if err := s.authorize(ctx, tx, s.tenant, s.kind, EffectWorkerHeartbeat); err != nil {
			return err
		}
		identity, err := s.receipt(ctx, tx, v.ID)
		if err != nil {
			return err
		}
		if identity != v.Identity {
			return ErrStaleEffectLease
		}
		args := append([]any{s.lease.Microseconds()}, s.leaseWhere(v)...)
		result, err := tx.ExecContext(ctx, "UPDATE "+s.schema+"."+s.deliveryTable()+" SET lease_until=TIMESTAMPADD(MICROSECOND,?,UTC_TIMESTAMP(6)),heartbeat_version=heartbeat_version+1"+effectLeasePredicate+" AND heartbeat_version<9223372036854775807", args...)
		return effectDeliveryChanged(result, err)
	})
}

var effectErrorCode = regexp.MustCompile(`^[a-z0-9_]{1,64}$`)

// Finish commits short LOCAL owner effects and the queue outcome together. The
// callback is mandatory and receives only this owner transaction; it must verify
// business state and write the actual outcome, never perform external I/O.
// Lock order is resource -> receipt -> delivery. Claim/heartbeat lock delivery
// only. Expiry is checked again AFTER effects so stale work rolls back entirely.
// A retry uses the shared River error-count policy, not a caller-invented delay.
func (s *EffectDeliveryStore) Finish(ctx context.Context, v EffectDeliveryLease, retry bool, errorCode string, effects func(*sql.Tx) error) error {
	return s.finish(ctx, v, retry, errorCode, nil, effects)
}
func (s *EffectDeliveryStore) finish(ctx context.Context, v EffectDeliveryLease, retry bool, errorCode string, beforeLocks func(context.Context, *sql.Tx) error, effects func(*sql.Tx) error) error {
	if s.executionKinds {
		return errors.New("execution outcome requires its lease-fenced owner lifecycle")
	}
	if !s.validLease(v) || effects == nil || retry && !effectErrorCode.MatchString(errorCode) || !retry && errorCode != "" {
		return errors.New("valid lease, explicit local outcome and redacted retry code required")
	}
	return mysqlstore.Mutation(ctx, s.db, func(tx *sql.Tx) error {
		if err := s.authorize(ctx, tx, s.tenant, s.kind, EffectWorkerFinish); err != nil {
			return err
		}
		if beforeLocks != nil {
			if err := beforeLocks(ctx, tx); err != nil {
				return err
			}
		}
		var resource mysqlstore.UUID
		if err := tx.QueryRowContext(ctx, "SELECT id FROM "+s.schema+"."+effectOwnerTable(s.kind)+" WHERE tenant_id=? AND id=? FOR UPDATE", mysqlstore.UUID(s.tenant), mysqlstore.UUID(v.Identity.ResourceID)).Scan(&resource); err != nil {
			return err
		}
		if err := s.fenceCurrentEffect(ctx, tx, v.Identity, v.ID); err != nil {
			return err
		}
		// Explicit current read closes snapshot/race ambiguities before locking
		// the delivery; scalar FK identity cannot be rewritten by runtime code.
		var receipt mysqlstore.UUID
		if err := tx.QueryRowContext(ctx, "SELECT id FROM "+s.schema+".decision_engine_effect_job_receipts WHERE tenant_id=? AND kind=? AND resource_id=? AND domain_attempt=? AND redrive_generation=? AND id=? FOR UPDATE", mysqlstore.UUID(s.tenant), string(s.kind), mysqlstore.UUID(v.Identity.ResourceID), v.Identity.DomainAttempt, v.Identity.RedriveGeneration, mysqlstore.UUID(v.ID)).Scan(&receipt); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrStaleEffectLease
			}
			return err
		}
		var maxAttempts int
		var priorErrors int64
		err := tx.QueryRowContext(ctx, "SELECT max_attempts,error_count FROM "+s.schema+".decision_engine_effect_job_deliveries"+effectLeasePredicate+" FOR UPDATE", s.leaseWhere(v)...).Scan(&maxAttempts, &priorErrors)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrStaleEffectLease
		}
		if err != nil {
			return err
		}
		if err := workers.ValidateErrorHistory(v.Attempt, &priorErrors, retry); err != nil {
			return err
		}
		outcome, err := workers.ResolveDeliveryOutcome(v.Attempt, maxAttempts, retry)
		if err != nil {
			return err
		}
		var delay time.Duration
		if retry && outcome.State == workers.Available {
			delay, err = (workers.RiverDeliveryRetryPolicy{}).Delay(int(priorErrors))
			if err != nil {
				return err
			}
		}
		if err := effects(tx); err != nil {
			return err
		}
		if outcome.State == workers.Discarded {
			errorCode = "delivery_attempts_exhausted"
		}
		finished := "NULL"
		if outcome.State != workers.Available {
			finished = "UTC_TIMESTAMP(6)"
		}
		increment := 0
		if retry {
			increment = 1
		}
		args := []any{string(outcome.State), delay.Microseconds(), errorCode, increment}
		args = append(args, s.leaseWhere(v)...)
		result, err := tx.ExecContext(ctx, "UPDATE "+s.schema+".decision_engine_effect_job_deliveries SET state=?,available_at=TIMESTAMPADD(MICROSECOND,?,UTC_TIMESTAMP(6)),last_error_code=?,error_count=error_count+?,lease_token=NULL,lease_until=NULL,finished_at="+finished+effectLeasePredicate, args...)
		// Receipts are never released here, even on exhaustion. An authorized
		// domain retry/redrive must allocate its own tuple in a later lifecycle.
		return effectDeliveryChanged(result, err)
	})
}
