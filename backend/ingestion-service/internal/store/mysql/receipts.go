// Package mysql contains staged ingestion adapters; no runtime selects them.
package mysql

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
	"unicode/utf8"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

var ErrReceiptConflict = errors.New("ingestion receipt key conflicts with original command")
var ErrReceiptLimit = errors.New("ingestion receipt capacity exceeded")

// ReceiptRequest is built by the trusted owner from a versioned canonical
// command. Hash alone is never authority or a substitute for scoped identity.
// Keys retain tenant-wide uniqueness across tables, modes and response kinds.
type ReceiptRequest struct {
	TenantID, TableID uuid.UUID
	Key               string
	Mode, Kind        string
	Hash              [32]byte
	// createdAt is supplied only by the authorized legacy importer.
	createdAt *time.Time
}

type ReceiptLimits struct{ KeyBytes, ResultBytes, CollisionRows int }
type ReceiptResult struct {
	Payload  json.RawMessage
	Replayed bool
}

// ReceiptAuthorizer performs current authorization before any receipt locks.
// ReceiptTxAuthorizer rechecks local authority on the owner's transaction; it
// must not make network calls or take broad tenant mutation locks.
type ReceiptAuthorizer func(context.Context, ReceiptRequest) error
type ReceiptTxAuthorizer func(context.Context, *sql.Tx, ReceiptRequest) error

type ReceiptManager struct {
	db                        *sql.DB
	schema                    string
	tenant                    uuid.UUID
	limits                    ReceiptLimits
	authorize                 ReceiptAuthorizer
	authorizeTx               ReceiptTxAuthorizer
	digest                    func([]byte) [32]byte
	bucketTable, receiptTable string
}

func NewReceiptManager(db *sql.DB, database string, tenant uuid.UUID, limits ReceiptLimits, authorize ReceiptAuthorizer, authorizeTx ReceiptTxAuthorizer) (*ReceiptManager, error) {
	schema, err := mysqlstore.Identifier(database)
	if err != nil {
		return nil, err
	}
	if db == nil || tenant == uuid.Nil || authorize == nil || authorizeTx == nil || limits.KeyBytes < 1 || limits.KeyBytes > 1<<20 || limits.ResultBytes < 1 || limits.ResultBytes > 16<<20 || limits.CollisionRows < 1 || limits.CollisionRows > 65535 {
		return nil, errors.New("scoped database, authorizers and bounded receipt limits required")
	}
	return &ReceiptManager{db: db, schema: schema, tenant: tenant, limits: limits, authorize: authorize, authorizeTx: authorizeTx, digest: sha256.Sum256,
		bucketTable: schema + ".ingestion_receipt_buckets", receiptTable: schema + ".ingestion_request_receipts"}, nil
}

// Execute holds one narrow tenant/digest bucket through owner effects and the
// immutable response INSERT. Full key bytes determine identity, never the digest.
// The callback is adapter-local: no driver leaks into domain ports. It must use
// this transaction for every local effect, not commit/rollback or perform DDL,
// session changes or network work. There is no automatic retry on errors.
// Result is empty on every failure, including uncertain COMMIT acknowledgement.
func (m *ReceiptManager) Execute(ctx context.Context, request ReceiptRequest, effects func(context.Context, *sql.Tx) (json.RawMessage, error)) (ReceiptResult, error) {
	return m.execute(ctx, request, effects, nil, nil)
}

// Hooks are adapter-private; work fencing precedes receipt/publication locks and
// local work completion joins the same transaction, including receipt replay.
func (m *ReceiptManager) execute(ctx context.Context, request ReceiptRequest, effects func(context.Context, *sql.Tx) (json.RawMessage, error), before func(context.Context, *sql.Tx) error, after func(context.Context, *sql.Tx, ReceiptResult) error) (ReceiptResult, error) {
	if m == nil || effects == nil || request.TenantID != m.tenant || request.TableID == uuid.Nil || request.Key == "" || request.Hash == ([32]byte{}) || (request.Mode != "create" && request.Mode != "patch") || (request.Kind != "single" && request.Kind != "batch") {
		return ReceiptResult{}, errors.New("scoped canonical ingestion request and owner effects required")
	}
	var createdAt any
	if request.createdAt != nil {
		var err error
		createdAt, err = (mysqlstore.DateTime{Time: *request.createdAt}).Value()
		if err != nil {
			return ReceiptResult{}, err
		}
	}
	if len(request.Key) > m.limits.KeyBytes {
		return ReceiptResult{}, ErrReceiptLimit
	}
	if err := m.authorize(ctx, request); err != nil {
		return ReceiptResult{}, err
	}
	var result ReceiptResult
	err := mysqlstore.Mutation(ctx, m.db, func(tx *sql.Tx) error {
		if before != nil {
			if err := before(ctx, tx); err != nil {
				return err
			}
		}
		if err := m.authorizeTx(ctx, tx, request); err != nil {
			return err
		}
		digest := m.digest([]byte(request.Key))
		// INSERT on an existing PK obtains the bucket lock, including when the
		// key was absent at request start. It never updates another owner's row.
		if _, err := tx.ExecContext(ctx, "INSERT INTO "+m.bucketTable+"(tenant_id,key_digest) VALUES (?,?) ON DUPLICATE KEY UPDATE key_digest=key_digest", mysqlstore.UUID(m.tenant), digest[:]); err != nil {
			return err
		}
		// Serialization can wait behind another request. Recheck current permission
		// after acquiring the bucket and before either replay or new effects.
		if err := m.authorizeTx(ctx, tx, request); err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, "SELECT slot,request_key=?,table_id,mode,response_kind,request_hash,OCTET_LENGTH(response_payload),CASE WHEN request_key=? AND OCTET_LENGTH(response_payload)<=? THEN response_payload ELSE NULL END FROM "+m.receiptTable+" WHERE tenant_id=? AND key_digest=? ORDER BY slot LIMIT ?", []byte(request.Key), []byte(request.Key), m.limits.ResultBytes, mysqlstore.UUID(m.tenant), digest[:], m.limits.CollisionRows+1)
		if err != nil {
			return err
		}
		var count int
		var found bool
		for rows.Next() {
			var slot int
			var matches bool
			var table mysqlstore.UUID
			var mode, kind string
			var hash, payload []byte
			var size int64
			if err = rows.Scan(&slot, &matches, &table, &mode, &kind, &hash, &size, &payload); err != nil {
				rows.Close()
				return err
			}
			if slot != count {
				rows.Close()
				return errors.New("invalid receipt bucket sequence")
			}
			count++
			if count > m.limits.CollisionRows {
				rows.Close()
				return ErrReceiptLimit
			}
			if matches {
				if found || uuid.UUID(table) != request.TableID || mode != request.Mode || kind != request.Kind || len(hash) != 32 || string(hash) != string(request.Hash[:]) {
					rows.Close()
					return ErrReceiptConflict
				}
				if size > int64(m.limits.ResultBytes) {
					rows.Close()
					return ErrReceiptLimit
				}
				if !utf8.Valid(payload) || !json.Valid(payload) {
					rows.Close()
					return errors.New("invalid retained ingestion response")
				}
				found = true
				result = ReceiptResult{Payload: append(json.RawMessage(nil), payload...), Replayed: true}
			}
		}
		if err = rows.Err(); err != nil {
			rows.Close()
			return err
		}
		if err = rows.Close(); err != nil {
			return err
		}
		if found {
			if after != nil {
				return after(ctx, tx, result)
			}
			return nil
		}
		if count == m.limits.CollisionRows {
			return ErrReceiptLimit
		}
		payload, err := effects(ctx, tx)
		if err != nil {
			return err
		}
		if len(payload) > m.limits.ResultBytes {
			return ErrReceiptLimit
		}
		if !utf8.Valid(payload) || !json.Valid(payload) {
			return errors.New("valid original ingestion response required")
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO "+m.receiptTable+"(tenant_id,key_digest,slot,request_key,table_id,mode,response_kind,request_hash,response_payload,created_at) VALUES (?,?,?,?,?,?,?,?,?,COALESCE(?,UTC_TIMESTAMP(6)))", mysqlstore.UUID(m.tenant), digest[:], count, []byte(request.Key), mysqlstore.UUID(request.TableID), request.Mode, request.Kind, request.Hash[:], []byte(payload), createdAt); err != nil {
			return err
		}
		result = ReceiptResult{Payload: append(json.RawMessage(nil), payload...)}
		if after != nil {
			return after(ctx, tx, result)
		}
		return nil
	})
	if err != nil {
		return ReceiptResult{}, err
	}
	return result, nil
}
