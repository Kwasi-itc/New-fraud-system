package mysql

import (
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

// LegacyImport is an OFFLINE trusted export, not a request from the ingestion
// API. RawJSON is the original object/records array before legacy float decoding.
// HistoricalModel and successful audits must come from reconciled source state.
// Missing original bytes, historical schema or attribution is an import error;
// callers must block activation, never invent a v1 hash from a legacy SHA-1.
type LegacyImport struct {
	Receipt         ingestion.IdempotencyKey
	HistoricalModel ingestion.PublishedDataModel
	ObjectType      string
	Mode            ingestion.Mode
	RawJSON         []byte
	Audits          []ingestion.IngestionAudit
}

func canonicalJSON(data []byte) ([]byte, error) {
	if !utf8.Valid(data) || !json.Valid(data) {
		return nil, errors.New("valid UTF-8 source JSON required")
	}
	var value any
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	if err := d.Decode(&value); err != nil {
		return nil, err
	}
	return json.Marshal(value)
}

func translateLegacy(in LegacyImport, maxBytes int) (ingestion.ReceiptCommand, []byte, [32]byte, error) {
	var zero ingestion.ReceiptCommand
	var hash [32]byte
	if err := boundLegacyEvidence(in, maxBytes); err != nil {
		return zero, nil, hash, err
	}
	if maxBytes < 1 || maxBytes > 16<<20 || len(in.RawJSON) > maxBytes || len(in.Receipt.ResponsePayload) > maxBytes || !utf8.Valid(in.RawJSON) || !json.Valid(in.RawJSON) {
		return zero, nil, hash, errors.New("bounded complete legacy source required")
	}
	if _, err := (mysqlstore.DateTime{Time: in.Receipt.CreatedAt}).Value(); err != nil {
		return zero, nil, hash, errors.New("legacy receipt creation time must be representable without precision loss")
	}
	tenant, err := uuid.Parse(in.Receipt.TenantID)
	if err != nil || tenant != in.HistoricalModel.TenantID || tenant == uuid.Nil {
		return zero, nil, hash, errors.New("legacy tenant mismatch")
	}
	table, ok := in.HistoricalModel.Tables[in.ObjectType]
	if !ok || table.ID == uuid.Nil || in.Receipt.Key == "" {
		return zero, nil, hash, errors.New("legacy table/key attribution required")
	}
	var oldRecords, newRecords []map[string]any
	d := json.NewDecoder(bytes.NewReader(in.RawJSON))
	d.UseNumber()
	switch in.Receipt.ResponseKind {
	case "single":
		var old, new map[string]any
		if err = json.Unmarshal(in.RawJSON, &old); err != nil {
			return zero, nil, hash, err
		}
		if err = d.Decode(&new); err != nil {
			return zero, nil, hash, err
		}
		oldRecords = []map[string]any{old}
		newRecords = []map[string]any{new}
	case "batch":
		if err = json.Unmarshal(in.RawJSON, &oldRecords); err != nil {
			return zero, nil, hash, err
		}
		if err = d.Decode(&newRecords); err != nil {
			return zero, nil, hash, err
		}
	default:
		return zero, nil, hash, errors.New("unknown legacy response kind")
	}
	command := ingestion.ReceiptCommand{TenantID: tenant, TableID: table.ID, Mode: in.Mode, Kind: in.Receipt.ResponseKind, Records: newRecords}
	if _, _, err := ingestion.CanonicalReceiptCommand(command, maxBytes); err != nil {
		return zero, nil, hash, err
	}
	if len(in.Audits) != len(oldRecords) {
		return zero, nil, hash, errors.New("complete ordered legacy success audits required")
	}
	normalized := make([]map[string]any, len(oldRecords))
	ids := make([]string, len(oldRecords))
	seen := map[string]bool{}
	for i, raw := range oldRecords {
		n, id, violations := ingestion.ValidateRecord(in.HistoricalModel, in.ObjectType, raw, in.Mode)
		if len(violations) > 0 || seen[id] {
			return zero, nil, hash, errors.New("legacy request does not validate against historical model")
		}
		seen[id] = true
		normalized[i] = n
		ids[i] = id
		a := in.Audits[i]
		if a.TenantID != tenant.String() || a.ObjectType != in.ObjectType || a.ObjectID != id || a.Mode != in.Mode || a.Status != "succeeded" || a.RevisionID != in.HistoricalModel.RevisionID || a.IdempotencyKey == nil || *a.IdempotencyKey != in.Receipt.Key || len(a.Payload) > maxBytes {
			return zero, nil, hash, errors.New("legacy audit attribution mismatch")
		}
		want, err := json.Marshal(n)
		if err != nil {
			return zero, nil, hash, err
		}
		got, err := canonicalJSON(a.Payload)
		if err != nil || !bytes.Equal(want, got) {
			return zero, nil, hash, errors.New("legacy normalized audit differs")
		}
	}
	var hashed any = oldRecords
	if command.Kind == "single" {
		hashed = normalized[0]
	}
	wire, err := json.Marshal(hashed)
	if err != nil {
		return zero, nil, hash, err
	}
	oldHash := sha1.Sum(wire)
	if strings.ToLower(in.Receipt.RequestHash) != hex.EncodeToString(oldHash[:]) {
		return zero, nil, hash, errors.New("legacy SHA-1 does not match original decoder/hash contract")
	}
	var results []ingestion.RecordResult
	if command.Kind == "single" {
		var r ingestion.RecordResult
		err = json.Unmarshal(in.Receipt.ResponsePayload, &r)
		results = []ingestion.RecordResult{r}
	} else {
		err = json.Unmarshal(in.Receipt.ResponsePayload, &results)
	}
	if err != nil || len(results) != len(ids) {
		return zero, nil, hash, errors.New("legacy result shape mismatch")
	}
	for i, r := range results {
		if r.ObjectID != ids[i] || r.RevisionID != in.HistoricalModel.RevisionID || r.Replayed || (r.Action != "created" && r.Action != "updated") {
			return zero, nil, hash, errors.New("legacy original result identity mismatch")
		}
	}
	response, err := canonicalJSON(in.Receipt.ResponsePayload)
	if err != nil {
		return zero, nil, hash, err
	}
	// Provenance pins all source evidence, including historical metadata and audit
	// IDs. It is not a signature or proof of authenticity of an untrusted export.
	provenance, err := json.Marshal(in)
	if err != nil {
		return zero, nil, hash, err
	}
	hash = sha256.Sum256(provenance)
	return command, response, hash, nil
}

func LegacyImportSchema(database string) (string, error) {
	s, err := mysqlstore.Identifier(database)
	if err != nil {
		return "", err
	}
	return "CREATE TABLE " + s + `.ingestion_legacy_imports(tenant_id BINARY(16) NOT NULL,key_digest BINARY(32) NOT NULL,slot SMALLINT UNSIGNED NOT NULL,
source_sha1 BINARY(20) NOT NULL,source_evidence_sha256 BINARY(32) NOT NULL,original_response_sha256 BINARY(32) NOT NULL,
imported_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),PRIMARY KEY(tenant_id,key_digest,slot),
FOREIGN KEY(tenant_id,key_digest,slot) REFERENCES ` + s + `.ingestion_request_receipts(tenant_id,key_digest,slot)) ENGINE=InnoDB`, nil
}

// LegacyImporter requires a separate operator policy in addition to normal
// tenant authorization. It is never constructed by the ingestion API/worker.
type LegacyImporter struct {
	owner     *KeyedRecordOwner
	authorize ReceiptTxAuthorizer
}

func NewLegacyImporter(owner *KeyedRecordOwner, authorize ReceiptTxAuthorizer) (*LegacyImporter, error) {
	if owner == nil || authorize == nil {
		return nil, errors.New("owner and explicit operator authorization required")
	}
	return &LegacyImporter{owner, authorize}, nil
}

// Import is an operator-only adapter entry point. It writes no physical
// data/audit/outbox effects. The original result and export provenance commit
// together, with current authorization on every call and ambiguous-commit replay.
// API/worker code must never expose this method to ordinary ingestion callers.
func (i *LegacyImporter) Import(ctx context.Context, in LegacyImport) (ReceiptResult, error) {
	o := i.owner
	command, response, evidence, err := translateLegacy(in, o.limits.CommandBytes)
	if err != nil {
		return ReceiptResult{}, err
	}
	if command.TenantID != o.receipts.tenant || command.TableID != o.table || in.ObjectType != o.objectType {
		return ReceiptResult{}, ErrOwnerAuthorization
	}
	request, err := NewReceiptRequest(command, in.Receipt.Key, o.limits.CommandBytes)
	if err != nil {
		return ReceiptResult{}, err
	}
	request.createdAt = &in.Receipt.CreatedAt
	oldHash, err := hex.DecodeString(in.Receipt.RequestHash)
	if err != nil || len(oldHash) != 20 {
		return ReceiptResult{}, errors.New("invalid source hash")
	}
	digest := o.receipts.digest([]byte(request.Key))
	resultHash := sha256.Sum256(response)
	after := func(ctx context.Context, tx *sql.Tx, r ReceiptResult) error {
		if !bytes.Equal(r.Payload, response) {
			return ErrReceiptConflict
		}
		var slot int
		if err := tx.QueryRowContext(ctx, "SELECT slot FROM "+o.receipts.receiptTable+" WHERE tenant_id=? AND key_digest=? AND request_key=?", mysqlstore.UUID(request.TenantID), digest[:], []byte(request.Key)).Scan(&slot); err != nil {
			return err
		}
		if r.Replayed {
			var same bool
			if err := tx.QueryRowContext(ctx, "SELECT source_sha1=? AND source_evidence_sha256=? AND original_response_sha256=? FROM "+o.receipts.schema+".ingestion_legacy_imports WHERE tenant_id=? AND key_digest=? AND slot=?", oldHash, evidence[:], resultHash[:], mysqlstore.UUID(request.TenantID), digest[:], slot).Scan(&same); err != nil {
				return err
			}
			if !same {
				return ErrReceiptConflict
			}
			return nil
		}
		_, err := tx.ExecContext(ctx, "INSERT INTO "+o.receipts.schema+".ingestion_legacy_imports(tenant_id,key_digest,slot,source_sha1,source_evidence_sha256,original_response_sha256) VALUES(?,?,?,?,?,?)", mysqlstore.UUID(request.TenantID), digest[:], slot, oldHash, evidence[:], resultHash[:])
		return err
	}
	return o.receipts.execute(ctx, request, func(context.Context, *sql.Tx) (json.RawMessage, error) { return response, nil }, func(ctx context.Context, tx *sql.Tx) error { return i.authorize(ctx, tx, request) }, after)
}
