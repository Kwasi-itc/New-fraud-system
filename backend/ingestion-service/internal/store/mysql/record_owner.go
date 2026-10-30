package mysql

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"time"
	"unicode/utf8"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

// RecordModelLoader is an adapter-local read contract. It must use only this
// transaction, perform bounded local reads and return the complete published
// table contract. It must not perform network calls or acquire another pool.
// It is invoked only for new effects, never to reconstruct historical replay.
type RecordModelLoader func(context.Context, *sql.Tx, ReceiptRequest) (ingestion.PublishedDataModel, error)

type RecordOwnerLimits struct {
	Receipt                                                    ReceiptLimits
	CommandBytes, WriteBytes, EffectRowBytes, EffectBatchBytes int
}

// RecordValidationError preserves validation failures separately from storage
// failures. If the separate failure audit cannot commit, Execute joins that
// error with this value; callers must not report a clean validation-only result.
type RecordValidationError struct{ Violations []ingestion.ValidationError }

func (e *RecordValidationError) Error() string { return "ingestion record validation failed" }

// KeyedRecordOwner composes the staged primitives into one atomic keyed command.
// It is deliberately not selected by the PostgreSQL API/worker composition root.
// Current trusted authorization and the metadata loader are required integration
// contracts, not optional permissive defaults. Legacy hashes are not accepted.
type KeyedRecordOwner struct {
	receipts             *ReceiptManager
	database, objectType string
	table                uuid.UUID
	load                 RecordModelLoader
	limits               RecordOwnerLimits
	now                  func() time.Time
}

func NewKeyedRecordOwner(db *sql.DB, database string, tenant, table uuid.UUID, objectType string,
	limits RecordOwnerLimits, authorize ReceiptAuthorizer, authorizeTx ReceiptTxAuthorizer,
	load RecordModelLoader, now func() time.Time) (*KeyedRecordOwner, error) {
	if table == uuid.Nil || objectType == "" || !utf8.ValidString(objectType) || len(objectType) > limits.EffectRowBytes || load == nil || now == nil {
		return nil, errors.New("trusted table scope, local model loader and clock required")
	}
	for _, limit := range []int{limits.CommandBytes, limits.WriteBytes, limits.EffectRowBytes, limits.EffectBatchBytes} {
		if limit < 1 || limit > 16<<20 {
			return nil, errors.New("record owner byte limits required")
		}
	}
	m, err := NewReceiptManager(db, database, tenant, limits.Receipt, authorize, authorizeTx)
	if err != nil {
		return nil, err
	}
	return &KeyedRecordOwner{m, database, objectType, table, load, limits, now}, nil
}

// Execute hashes raw content, then detaches it from caller-owned maps before
// authorization callbacks. Single results retain the source object shape; batch
// results retain ordered arrays. ReceiptResult.Replayed describes replay without
// rewriting immutable stored JSON. Every failure returns no provisional result.
func (o *KeyedRecordOwner) Execute(ctx context.Context, command ingestion.ReceiptCommand, key string) (ReceiptResult, error) {
	return o.execute(ctx, command, key, recordHooks{})
}

type recordHooks struct {
	before  func(context.Context, *sql.Tx) error
	after   func(context.Context, *sql.Tx, ReceiptResult) error
	invalid func(context.Context, *sql.Tx, *RecordValidationError) error
}

func (o *KeyedRecordOwner) execute(ctx context.Context, command ingestion.ReceiptCommand, key string, hooks recordHooks) (ReceiptResult, error) {
	if o == nil || command.TenantID != o.receipts.tenant || command.TableID != o.table {
		return ReceiptResult{}, errors.New("record owner scope mismatch")
	}
	wire, hash, err := ingestion.CanonicalReceiptCommand(command, o.limits.CommandBytes)
	if err != nil {
		return ReceiptResult{}, err
	}
	var frozen struct {
		Records []map[string]any `json:"records"`
	}
	decoder := json.NewDecoder(bytes.NewReader(wire))
	decoder.UseNumber()
	if err := decoder.Decode(&frozen); err != nil {
		return ReceiptResult{}, err
	}
	command.Records = frozen.Records
	request := ReceiptRequest{TenantID: command.TenantID, TableID: command.TableID, Key: key,
		Mode: string(command.Mode), Kind: command.Kind, Hash: hash}
	var rejectedModel ingestion.PublishedDataModel
	result, err := o.receipts.execute(ctx, request, func(ctx context.Context, tx *sql.Tx) (json.RawMessage, error) {
		model, err := o.load(ctx, tx, request)
		if err != nil {
			return nil, err
		}
		version := model.PublicationVersion
		if version == uuid.Nil {
			version, err = uuid.Parse(model.RevisionID)
		}
		if err != nil || model.Tables[o.objectType].ID != o.table || !slices.Contains(model.ManagedSystemFields, "updated_at") {
			return nil, ErrWriteMapping
		}
		writer, err := NewRecordWriter(ctx, tx, o.database, request.TenantID, model, o.objectType, version)
		if err != nil {
			return nil, err
		}
		records, violations := o.validate(model, command)
		if len(violations) > 0 {
			rejectedModel = model
			return nil, &RecordValidationError{Violations: violations}
		}
		at := o.now().UTC().Truncate(time.Microsecond)
		writes, err := writer.WriteBatch(ctx, records, command.Mode, at, o.limits.WriteBytes)
		if err != nil {
			return nil, err
		}
		effects, err := NewEffectStore(tx, o.database, command.TenantID, command.TableID, o.objectType, o.limits.EffectRowBytes)
		if err != nil {
			return nil, err
		}
		audits := make([]ingestion.IngestionAudit, len(records))
		events := make([]ingestion.OutboxEvent, len(records))
		results := make([]ingestion.RecordResult, len(records))
		for i, record := range records {
			objectID := record["object_id"].(string)
			payload, err := json.Marshal(record)
			if err != nil {
				return nil, err
			}
			audits[i] = ingestion.IngestionAudit{ID: uuid.NewString(), TenantID: command.TenantID.String(),
				ObjectType: o.objectType, ObjectID: objectID, Mode: command.Mode, RevisionID: model.RevisionID,
				Status: "succeeded", Payload: payload, ValidationError: []byte("[]"), IdempotencyKey: &key, CreatedAt: at}
			eventType := "record.ingested"
			if writes[i].Action == "updated" {
				eventType = "record.updated"
			}
			payload, err = json.Marshal(map[string]any{"tenant_id": command.TenantID, "object_type": o.objectType,
				"object_id": objectID, "mode": command.Mode, "revision_id": model.RevisionID,
				"action": writes[i].Action, "record": record, "ingested_at": at})
			if err != nil {
				return nil, err
			}
			events[i] = ingestion.OutboxEvent{ID: uuid.NewString(), TenantID: command.TenantID.String(), EventType: eventType,
				AggregateType: o.objectType, AggregateKey: objectID, Payload: payload, Status: "pending", CreatedAt: at}
			results[i] = ingestion.RecordResult{ObjectID: objectID, Action: writes[i].Action, RevisionID: model.RevisionID}
		}
		if err := effects.RecordBatch(ctx, audits, events, o.limits.EffectBatchBytes); err != nil {
			return nil, err
		}
		if command.Kind == "single" {
			return json.Marshal(results[0])
		}
		payload, err := json.Marshal(map[string]any{"tenant_id": command.TenantID, "object_type": o.objectType,
			"mode": command.Mode, "revision_id": model.RevisionID, "count": len(results), "ingested_at": at})
		if err != nil {
			return nil, err
		}
		if err := effects.AppendOutbox(ctx, ingestion.OutboxEvent{ID: uuid.NewString(), TenantID: command.TenantID.String(),
			EventType: "batch.ingestion.completed", AggregateType: o.objectType, AggregateKey: o.objectType,
			Payload: payload, Status: "pending", CreatedAt: at}); err != nil {
			return nil, err
		}
		return json.Marshal(results)
	}, hooks.before, hooks.after)
	var validation *RecordValidationError
	if errors.As(err, &validation) {
		// The failed data transaction has already rolled back. Failure auditing is
		// a separate authorized transaction and does not reserve a successful key.
		if auditErr := o.auditFailure(ctx, request, command, rejectedModel, validation, hooks); auditErr != nil {
			return ReceiptResult{}, errors.Join(err, auditErr)
		}
	}
	return result, err
}

func (o *KeyedRecordOwner) validate(model ingestion.PublishedDataModel, command ingestion.ReceiptCommand) ([]map[string]any, []ingestion.ValidationError) {
	records := make([]map[string]any, len(command.Records))
	var violations []ingestion.ValidationError
	seen := make(map[string]bool, len(records))
	for i, raw := range command.Records {
		normalized, id, failures := ingestion.ValidateRecord(model, o.objectType, raw, command.Mode)
		for j := range failures {
			failures[j].ObjectID = id
		}
		violations = append(violations, failures...)
		if len(failures) == 0 && seen[id] {
			violations = append(violations, ingestion.ValidationError{ObjectID: id, Field: "object_id", Code: "duplicate_object_id", Message: "duplicate normalized object_id in batch"})
		}
		seen[id] = true
		records[i] = normalized
	}
	return records, violations
}

func (o *KeyedRecordOwner) auditFailure(ctx context.Context, request ReceiptRequest, command ingestion.ReceiptCommand, model ingestion.PublishedDataModel, validation *RecordValidationError, hooks recordHooks) error {
	if err := o.receipts.authorize(ctx, request); err != nil {
		return err
	}
	return mysqlstore.Mutation(ctx, o.receipts.db, func(tx *sql.Tx) error {
		if hooks.before != nil {
			if err := hooks.before(ctx, tx); err != nil {
				return err
			}
		}
		if err := o.receipts.authorizeTx(ctx, tx, request); err != nil {
			return err
		}
		effects, err := NewEffectStore(tx, o.database, command.TenantID, command.TableID, o.objectType, o.limits.EffectRowBytes)
		if err != nil {
			return err
		}
		at := o.now().UTC().Truncate(time.Microsecond)
		remaining := o.limits.EffectBatchBytes
		for _, raw := range command.Records {
			_, id, _ := ingestion.ValidateRecord(model, o.objectType, raw, command.Mode)
			payload, err := json.Marshal(raw)
			if err != nil {
				return err
			}
			failures := make([]ingestion.ValidationError, 0)
			for _, failure := range validation.Violations {
				if failure.ObjectID == id {
					failures = append(failures, failure)
				}
			}
			details, err := json.Marshal(failures)
			if err != nil {
				return err
			}
			remaining -= len(payload) + len(details) + len(id) + len(o.objectType) + len(request.Key) + len(model.RevisionID) + 128
			if remaining < 0 {
				return errors.New("validation audit batch byte limit exceeded")
			}
			if err := effects.AppendAudit(ctx, ingestion.IngestionAudit{ID: uuid.NewString(), TenantID: command.TenantID.String(),
				ObjectType: o.objectType, ObjectID: id, Mode: command.Mode, RevisionID: model.RevisionID, Status: "validation_failed",
				Payload: payload, ValidationError: details, IdempotencyKey: &request.Key, CreatedAt: at}); err != nil {
				return err
			}
		}
		if hooks.invalid != nil {
			return hooks.invalid(ctx, tx, validation)
		}
		return nil
	})
}
