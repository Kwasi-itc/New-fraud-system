package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type Beginner interface {
	Begin(context.Context) (pgx.Tx, error)
}

// ReceiptStore is implemented by the domain that owns the operation and audit.
// Resource authorization acquires domain locks after the identity/advisory locks.
type ReceiptStore interface {
	Authorize(context.Context, pgx.Tx, Principal) error
	Load(context.Context, pgx.Tx, uuid.UUID, uuid.UUID) (hash string, result json.RawMessage, found bool, err error)
	Record(context.Context, pgx.Tx, uuid.UUID, uuid.UUID, string, json.RawMessage) error
}
type HardenedAction struct {
	PreviousPayloadHash string // Optional exact-match digest for immutable pre-migration receipts.
	Principal           Principal
	Capability          string
	OperationID         uuid.UUID
	Domain              string
	Resource            string
	Payload             json.RawMessage
	Receipts            ReceiptStore
}

// ExecuteHardenedAction commits current authority, domain authorization,
// payload-bound idempotency, owner writes, receipt and audit as one transaction.
// All callbacks run inside this transaction and must not perform network IO.
func ExecuteHardenedAction(ctx context.Context, db Beginner, a HardenedAction, apply func(context.Context, pgx.Tx) (json.RawMessage, error)) (json.RawMessage, error) {
	if db == nil || a.Receipts == nil || apply == nil || a.OperationID == uuid.Nil || a.Domain == "" || len(a.Domain) > 128 || a.Resource == "" || len(a.Resource) > 1024 || len(a.Payload) > 1<<20 || !json.Valid(a.Payload) {
		return nil, Invalid("A bounded action, valid payload and receipt store are required.")
	}
	if !a.Principal.IsHuman() || !a.Principal.Can(a.Capability) {
		return nil, Forbidden()
	}
	// Struct encoding prevents ambiguity between the payload and actor/resource.
	bound, err := json.Marshal(struct {
		Actor, Tenant, Domain, Resource, Capability string
		Payload                                     json.RawMessage
	}{a.Principal.Subject, a.Principal.TenantID.String(), a.Domain, a.Resource, a.Capability, a.Payload})
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(bound)
	hash := hex.EncodeToString(sum[:])
	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer rollback(ctx, tx)
	if err = CheckCapability(ctx, tx, a.Principal, a.Capability, true); err != nil {
		return nil, err
	}
	lockKey := a.Principal.TenantID.String() + ":" + a.Domain + ":" + a.OperationID.String()
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, lockKey); err != nil {
		return nil, err
	}
	if err = a.Receipts.Authorize(ctx, tx, a.Principal); err != nil {
		return nil, err
	}
	previous, result, found, err := a.Receipts.Load(ctx, tx, a.Principal.TenantID, a.OperationID)
	if err != nil {
		return nil, err
	}
	if found {
		if previous != hash && (a.PreviousPayloadHash == "" || previous != a.PreviousPayloadHash) {
			return nil, Conflict()
		}
		if !json.Valid(result) {
			return nil, errors.New("stored receipt is invalid")
		}
		return result, tx.Commit(ctx)
	}
	result, err = apply(ctx, tx)
	if err != nil {
		return nil, err
	}
	if !json.Valid(result) {
		return nil, errors.New("action result must be valid JSON")
	}
	if err = a.Receipts.Record(ctx, tx, a.Principal.TenantID, a.OperationID, hash, result); err != nil {
		return nil, err
	}
	return result, tx.Commit(ctx)
}
