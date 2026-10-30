package mysql

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

var ErrLegacyRejected = errors.New("legacy source evidence rejected")

// Preview performs the same evidence/scope checks as Import with SELECT-only
// access. It is a point-in-time report, never a reservation or import approval.
func (i *LegacyImporter) Preview(ctx context.Context, in LegacyImport) (string, error) {
	o := i.owner
	command, response, evidence, err := translateLegacy(in, o.limits.CommandBytes)
	if err != nil {
		return "", errors.Join(ErrLegacyRejected, err)
	}
	if command.TenantID != o.receipts.tenant || command.TableID != o.table || in.ObjectType != o.objectType {
		return "", ErrOwnerAuthorization
	}
	request, err := NewReceiptRequest(command, in.Receipt.Key, o.limits.CommandBytes)
	if err != nil {
		return "", errors.Join(ErrLegacyRejected, err)
	}
	if len(request.Key) > o.receipts.limits.KeyBytes {
		return "", ErrReceiptLimit
	}
	if err = o.receipts.authorize(ctx, request); err != nil {
		return "", err
	}
	tx, err := o.receipts.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if err = o.receipts.authorizeTx(ctx, tx, request); err != nil {
		return "", err
	}
	if err = i.authorize(ctx, tx, request); err != nil {
		return "", err
	}
	digest := o.receipts.digest([]byte(request.Key))
	rows, err := tx.QueryContext(ctx, "SELECT slot,request_key=?,table_id,mode,response_kind,request_hash,OCTET_LENGTH(response_payload),CASE WHEN request_key=? AND OCTET_LENGTH(response_payload)<=? THEN response_payload ELSE NULL END FROM "+o.receipts.receiptTable+" WHERE tenant_id=? AND key_digest=? ORDER BY slot LIMIT ?", []byte(request.Key), []byte(request.Key), o.receipts.limits.ResultBytes, mysqlstore.UUID(request.TenantID), digest[:], o.receipts.limits.CollisionRows+1)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	count, match := 0, -1
	for rows.Next() {
		var slot, size int
		var same bool
		var table mysqlstore.UUID
		var mode, kind string
		var hash, payload []byte
		if err = rows.Scan(&slot, &same, &table, &mode, &kind, &hash, &size, &payload); err != nil {
			return "", err
		}
		if slot != count {
			return "", errors.New("invalid receipt bucket sequence")
		}
		count++
		if count > o.receipts.limits.CollisionRows {
			return "", ErrReceiptLimit
		}
		if same {
			if match >= 0 || uuid.UUID(table) != request.TableID || mode != request.Mode || kind != request.Kind || !bytes.Equal(hash, request.Hash[:]) {
				return "", ErrReceiptConflict
			}
			if size > o.receipts.limits.ResultBytes {
				return "", ErrReceiptLimit
			}
			normalized, e := canonicalJSON(payload)
			if e != nil {
				return "", e
			}
			if !bytes.Equal(normalized, response) {
				return "", ErrReceiptConflict
			}
			match = slot
		}
	}
	if err = rows.Err(); err != nil {
		return "", err
	}
	if err = rows.Close(); err != nil {
		return "", err
	}
	status := "would_import"
	if match >= 0 {
		oldHash, err := hex.DecodeString(in.Receipt.RequestHash)
		if err != nil {
			return "", err
		}
		resultHash := sha256.Sum256(response)
		var same bool
		err = tx.QueryRowContext(ctx, "SELECT source_sha1=? AND source_evidence_sha256=? AND original_response_sha256=? FROM "+o.receipts.schema+".ingestion_legacy_imports WHERE tenant_id=? AND key_digest=? AND slot=?", oldHash, evidence[:], resultHash[:], mysqlstore.UUID(request.TenantID), digest[:], match).Scan(&same)
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrReceiptConflict
		}
		if err != nil {
			return "", err
		}
		if !same {
			return "", ErrReceiptConflict
		}
		status = "already_imported"
	} else if count == o.receipts.limits.CollisionRows {
		return "", ErrReceiptLimit
	}
	if err = tx.Commit(); err != nil {
		return "", err
	}
	return status, nil
}
