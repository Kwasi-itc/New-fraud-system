package mysql

import (
	"context"
	"database/sql"
	"errors"
	"math"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/mysqljobs"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

var (
	ErrEffectRetryState  = errors.New("effect retry requires the current eligible owner and a terminal delivery")
	ErrEffectSourceOwned = errors.New("screening source owns this projection; retry requires its owner contract")
)

type EffectRetryAction string

const (
	EffectDomainRetry EffectRetryAction = "domain_retry"
	EffectRedrive     EffectRetryAction = "redrive"
	EffectCancel      EffectRetryAction = "cancel"
)

// EffectRetryAuthorizer checks trusted identity, tenant, kind and the specific
// retry/redrive/cancel permission using only local SQL in this transaction. Worker
// claim authority alone does not confer either permission. No provider or
// public endpoint is activated by this staged store.
type EffectRetryAuthorizer func(context.Context, *sql.Tx, uuid.UUID, ports.JobKind, EffectRetryAction) error

// EffectRetryStore resets ONE decision-owned dispatch and reserves its required
// successor atomically. It never calls a provider or writes screening-owned
// results. A terminal queue receipt does not imply successful business work.
//
// Domain attempts and generations are monotonically nondecreasing: Retry adds
// one domain attempt; Redrive keeps that attempt and adds one generation. A
// redrive does not replenish the configured domain budget. This permits an
// indexed latest-receipt fence using the existing exact identity key, even
// after terminal transport retention. Neither operation deletes receipts or
// automatically repeats after an ambiguous COMMIT.
type EffectRetryStore struct {
	db          *sql.DB
	schema      string
	tenant      uuid.UUID
	kind        ports.JobKind
	policy      EffectQueuePolicy
	maxAttempts int
	authorize   EffectRetryAuthorizer
}

func NewEffectRetryStore(db *sql.DB, database string, tenant uuid.UUID, kind ports.JobKind, policy EffectQueuePolicy, maxDomainAttempts int, authorize EffectRetryAuthorizer) (*EffectRetryStore, error) {
	schema, err := mysqlstore.Identifier(database)
	if err != nil {
		return nil, err
	}
	if db == nil || tenant == uuid.Nil || effectOwnerTable(kind) == "" || !effectQueueName.MatchString(policy.Name) || policy.MaxAttempts < 1 || policy.MaxAttempts > math.MaxInt32 || maxDomainAttempts < 1 || maxDomainAttempts > math.MaxInt32 || authorize == nil {
		return nil, errors.New("pool, tenant/effect scope, explicit queue/domain budgets and retry authority required")
	}
	return &EffectRetryStore{db, schema, tenant, kind, policy, maxDomainAttempts, authorize}, nil
}

func (s *EffectRetryStore) Retry(ctx context.Context, expected mysqljobs.Identity) (mysqljobs.Identity, error) {
	return s.advance(ctx, expected, EffectDomainRetry)
}

// Redrive is an explicit, separately authorized operation, not transport retry.
// Provider 'sent' means an uncertain/awaiting business result, not permission for
// automatic resend. Only this explicit redrive can reset an unowned provider's
// sent state. It cannot undo earlier external side effects; the receiving
// provider still needs a resource-level idempotency/reconciliation contract.
func (s *EffectRetryStore) Redrive(ctx context.Context, expected mysqljobs.Identity) (mysqljobs.Identity, error) {
	return s.advance(ctx, expected, EffectRedrive)
}

func (s *EffectRetryStore) advance(ctx context.Context, expected mysqljobs.Identity, action EffectRetryAction) (mysqljobs.Identity, error) {
	if s == nil || s.db == nil || s.authorize == nil || expected.Validate() != nil || expected.TenantID != s.tenant || expected.Kind != s.kind {
		return mysqljobs.Identity{}, ErrEffectRetryState
	}
	next, err := nextEffectIdentity(expected, action, s.maxAttempts)
	if err != nil {
		return mysqljobs.Identity{}, err
	}
	err = mysqlstore.Mutation(ctx, s.db, func(tx *sql.Tx) error {
		if err := s.authorize(ctx, tx, s.tenant, s.kind, action); err != nil {
			return err
		}
		// Resource -> latest permanent receipt -> exact transport. The resource
		// lock serializes producers even if retention removed the transport row.
		status, sourceOwned, err := lockEffectOwner(ctx, tx, s.schema, expected)
		if err != nil {
			return err
		}
		if sourceOwned {
			return ErrEffectSourceOwned
		}
		eligible := effectRetryEligible(s.kind, status, action)
		if !eligible && (action != EffectRedrive || !effectDispatchPending(s.kind, status)) {
			return ErrEffectRetryState
		}
		id, current, err := currentEffectReceipt(ctx, tx, s.schema, expected)
		if err != nil {
			return err
		}
		if current != expected {
			return ErrEffectRetryState
		}
		if !eligible {
			// A cancelled dispatch deliberately preserves the business row.
			// Only its durable cancellation record authorizes this otherwise
			// pending-state redrive, including after transport retention.
			_, found, err := effectCancellation(ctx, tx, s.schema, id, expected)
			if err != nil {
				return err
			}
			if !found {
				return ErrEffectRetryState
			}
		}
		var state, queue string
		err = tx.QueryRowContext(ctx, "SELECT state,queue_name FROM "+s.schema+".decision_engine_effect_job_deliveries WHERE tenant_id=? AND kind=? AND id=? FOR UPDATE", mysqlstore.UUID(s.tenant), string(s.kind), mysqlstore.UUID(id)).Scan(&state, &queue)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err == nil && (queue != s.policy.Name || state != "completed" && state != "discarded" && state != "cancelled") {
			// Expired running work must go through fenced rescue first. Do not
			// overwrite an in-flight delivery or assume it was never sent.
			return ErrEffectRetryState
		}
		if err := resetEffectOwner(ctx, tx, s.schema, expected); err != nil {
			return err
		}
		return insertEffectSuccessor(ctx, tx, s.schema, next, s.policy)
	})
	if err != nil {
		return mysqljobs.Identity{}, err // No success identity on uncertain COMMIT.
	}
	return next, nil
}

func nextEffectIdentity(v mysqljobs.Identity, action EffectRetryAction, maximum int) (mysqljobs.Identity, error) {
	if v.Validate() != nil || effectOwnerTable(v.Kind) == "" || maximum < 1 || maximum > math.MaxInt32 || v.DomainAttempt > maximum {
		return mysqljobs.Identity{}, ErrEffectRetryState
	}
	switch action {
	case EffectDomainRetry:
		if v.DomainAttempt >= maximum {
			return mysqljobs.Identity{}, ErrEffectRetryState
		}
		v.DomainAttempt++
	case EffectRedrive:
		if v.RedriveGeneration == math.MaxInt32 {
			return mysqljobs.Identity{}, ErrEffectRetryState
		}
		v.RedriveGeneration++
	default:
		return mysqljobs.Identity{}, ErrEffectRetryState
	}
	return v, v.Validate()
}

func effectRetryEligible(kind ports.JobKind, status string, action EffectRetryAction) bool {
	if action != EffectDomainRetry && action != EffectRedrive {
		return false
	}
	switch kind {
	case ports.JobWorkflow:
		return status == "dispatch_failed"
	case ports.JobOutbox:
		return status == "failed"
	case ports.JobScreening, ports.JobScoring:
		return status == "failed" || action == EffectRedrive && status == "sent"
	}
	return false
}

func lockEffectOwner(ctx context.Context, tx *sql.Tx, schema string, v mysqljobs.Identity) (string, bool, error) {
	table := effectOwnerTable(v.Kind)
	if table == "" {
		return "", false, ErrEffectRetryState
	}
	projection := "FALSE"
	if v.Kind == ports.JobScreening {
		projection = "projection_source_id IS NOT NULL"
	}
	var status string
	var source bool
	err := tx.QueryRowContext(ctx, "SELECT status,"+projection+" FROM "+schema+"."+table+" WHERE tenant_id=? AND id=? FOR UPDATE", mysqlstore.UUID(v.TenantID), mysqlstore.UUID(v.ResourceID)).Scan(&status, &source)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrEffectRetryState
	}
	return status, source, err
}

// Caller must already hold the resource lock. Only lifecycle producers may
// insert successor receipts; both counters never decrease. LIMIT 1 walks the
// existing de_effect_job_identity unique index backwards, without histories.
func currentEffectReceipt(ctx context.Context, tx *sql.Tx, schema string, expected mysqljobs.Identity) (uuid.UUID, mysqljobs.Identity, error) {
	v := mysqljobs.Identity{Version: 1, TenantID: expected.TenantID, Kind: expected.Kind, ResourceID: expected.ResourceID}
	var id mysqlstore.UUID
	err := tx.QueryRowContext(ctx, currentEffectReceiptSQL(schema), mysqlstore.UUID(v.TenantID), string(v.Kind), mysqlstore.UUID(v.ResourceID)).Scan(&id, &v.DomainAttempt, &v.RedriveGeneration)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrEffectRetryState
	}
	if err == nil {
		err = v.Validate()
	}
	return uuid.UUID(id), v, err
}

func currentEffectReceiptSQL(schema string) string {
	return "SELECT id,domain_attempt,redrive_generation FROM " + schema + ".decision_engine_effect_job_receipts WHERE tenant_id=? AND kind=? AND resource_id=? ORDER BY domain_attempt DESC,redrive_generation DESC LIMIT 1 FOR UPDATE"
}

func (s *EffectDeliveryStore) fenceCurrentEffect(ctx context.Context, tx *sql.Tx, v mysqljobs.Identity, id uuid.UUID) error {
	currentID, current, err := currentEffectReceipt(ctx, tx, s.schema, v)
	if errors.Is(err, ErrEffectRetryState) {
		return ErrStaleEffectLease
	}
	if err != nil {
		return err
	}
	if current != v || currentID != id {
		return ErrStaleEffectLease
	}
	return nil
}

func resetEffectOwner(ctx context.Context, tx *sql.Tx, schema string, v mysqljobs.Identity) error {
	set := "status='pending'"
	switch v.Kind {
	case ports.JobWorkflow:
		set = "status='pending_dispatch'"
	case ports.JobScreening, ports.JobScoring:
		set += ",response_json=JSON_OBJECT(),provider_reference='',last_error='',updated_at=UTC_TIMESTAMP(6),sent_at=NULL,completed_at=NULL,failed_at=NULL"
	case ports.JobOutbox:
	default:
		return ErrEffectRetryState
	}
	_, err := tx.ExecContext(ctx, "UPDATE "+schema+"."+effectOwnerTable(v.Kind)+" SET "+set+" WHERE tenant_id=? AND id=?", mysqlstore.UUID(v.TenantID), mysqlstore.UUID(v.ResourceID))
	return err
}

func insertEffectSuccessor(ctx context.Context, tx *sql.Tx, schema string, v mysqljobs.Identity, policy EffectQueuePolicy) error {
	_, column, _, ok := effectJobKind(effectOwnerTable(v.Kind))
	if !ok || v.Validate() != nil {
		return ErrEffectRetryState
	}
	id := mysqlstore.UUID(uuid.New())
	if _, err := tx.ExecContext(ctx, "INSERT INTO "+schema+".decision_engine_effect_job_receipts(id,tenant_id,resource_id,kind,domain_attempt,redrive_generation,"+column+",created_at) VALUES(?,?,?,?,?,?,?,UTC_TIMESTAMP(6))", id, mysqlstore.UUID(v.TenantID), mysqlstore.UUID(v.ResourceID), string(v.Kind), v.DomainAttempt, v.RedriveGeneration, mysqlstore.UUID(v.ResourceID)); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, "INSERT INTO "+schema+".decision_engine_effect_job_deliveries(id,tenant_id,kind,queue_name,available_at,created_at,max_attempts) VALUES(?,?,?,?,UTC_TIMESTAMP(6),UTC_TIMESTAMP(6),?)", id, mysqlstore.UUID(v.TenantID), string(v.Kind), policy.Name, policy.MaxAttempts)
	return err
}
