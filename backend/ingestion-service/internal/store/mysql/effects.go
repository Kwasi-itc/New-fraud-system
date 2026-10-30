package mysql

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

// EffectStore appends ingestion-owned audit/outbox rows to an authorized owner
// transaction. It neither authorizes nor commits. The owner must propagate every
// error so its entire transaction (including physical data and receipt) rolls back.
// Table/name binding must come from the owner's authorized registry snapshot.
type EffectStore struct {
	tx            *sql.Tx
	schema        string
	tenant, table uuid.UUID
	objectType    string
	maxRowBytes   int
}

func NewEffectStore(tx *sql.Tx, database string, tenant, table uuid.UUID, objectType string, maxRowBytes int) (*EffectStore, error) {
	schema, err := mysqlstore.Identifier(database)
	if err != nil {
		return nil, err
	}
	if tx == nil || tenant == uuid.Nil || table == uuid.Nil || objectType == "" || !utf8.ValidString(objectType) || maxRowBytes < 1 || maxRowBytes > 16<<20 || len(objectType) > maxRowBytes {
		return nil, errors.New("authorized transaction, table scope and bounded effects required")
	}
	return &EffectStore{tx: tx, schema: schema, tenant: tenant, table: table, objectType: objectType, maxRowBytes: maxRowBytes}, nil
}

// RecordIngestion validates the pair before either INSERT. No generic upsert or
// error suppression is used; duplicate IDs abort the owner's transaction.
func (s *EffectStore) RecordIngestion(ctx context.Context, audit ingestion.IngestionAudit, event ingestion.OutboxEvent) error {
	a, err := s.auditArgs(audit)
	if err != nil {
		return err
	}
	e, err := s.eventArgs(event)
	if err != nil {
		return err
	}
	if audit.Status != "succeeded" || audit.ObjectID != event.AggregateKey || !audit.CreatedAt.Equal(event.CreatedAt) || (event.EventType != "record.ingested" && event.EventType != "record.updated") {
		return errors.New("audit and record event must describe the same successful effect")
	}
	if err := s.insertAudit(ctx, a); err != nil {
		return err
	}
	return s.insertEvent(ctx, e)
}

// RecordBatch writes 1-500 ordered pairs in two bounded multi-row INSERTs.
// maxBatchBytes caps bound value bytes across both statements (UUID/time values
// count as 16 bytes). SQL/parameter overhead is separately bounded by 500 rows.
// All rows are validated before any writes; any SQL failure must roll back the
// owner transaction. The final batch event is appended by that same owner.
func (s *EffectStore) RecordBatch(ctx context.Context, audits []ingestion.IngestionAudit, events []ingestion.OutboxEvent, maxBatchBytes int) error {
	if s == nil || len(audits) == 0 || len(audits) > 500 || len(audits) != len(events) || maxBatchBytes < 1 || maxBatchBytes > 16<<20 {
		return errors.New("bounded matching audit/event batch required")
	}
	auditValues := make([]any, 0, len(audits)*12)
	eventValues := make([]any, 0, len(events)*9)
	remaining := maxBatchBytes
	for i, audit := range audits {
		event := events[i]
		a, err := s.auditArgs(audit)
		if err != nil {
			return err
		}
		e, err := s.eventArgs(event)
		if err != nil {
			return err
		}
		if audit.Status != "succeeded" || audit.ObjectID != event.AggregateKey || !audit.CreatedAt.Equal(event.CreatedAt) || (event.EventType != "record.ingested" && event.EventType != "record.updated") {
			return errors.New("audit/event batch pair mismatch")
		}
		for _, values := range [][]any{a, e} {
			for _, value := range values {
				size := 16
				switch v := value.(type) {
				case string:
					size = len(v)
				case []byte:
					size = len(v)
				case nil:
					size = 0
				}
				if size > remaining {
					return errors.New("effect batch byte limit exceeded")
				}
				remaining -= size
			}
		}
		auditValues = append(auditValues, a...)
		eventValues = append(eventValues, e...)
	}
	if err := s.insertAudit(ctx, auditValues); err != nil {
		return err
	}
	return s.insertEvent(ctx, eventValues)
}

// AppendAudit supports the separate validation-failure transaction. Failure
// auditing is not silently absorbed and does not emit a success event.
func (s *EffectStore) AppendAudit(ctx context.Context, audit ingestion.IngestionAudit) error {
	a, err := s.auditArgs(audit)
	if err != nil {
		return err
	}
	return s.insertAudit(ctx, a)
}

// AppendOutbox also supports the batch.ingestion.completed event after all
// accepted records. The owner must keep it in the same batch transaction.
func (s *EffectStore) AppendOutbox(ctx context.Context, event ingestion.OutboxEvent) error {
	a, err := s.eventArgs(event)
	if err != nil {
		return err
	}
	return s.insertEvent(ctx, a)
}

func (s *EffectStore) insertAudit(ctx context.Context, args []any) error {
	values := strings.TrimSuffix(strings.Repeat("(?,?,?,?,?,?,?,?,?,?,?,?),", len(args)/12), ",")
	_, err := s.tx.ExecContext(ctx, "INSERT INTO "+s.schema+".ingestion_audit(id,tenant_id,table_id,object_type,object_id,mode,revision_id,status,payload,validation_errors,idempotency_key,created_at) VALUES "+values, args...)
	return err
}

func (s *EffectStore) insertEvent(ctx context.Context, args []any) error {
	values := strings.TrimSuffix(strings.Repeat("(?,?,?,?,?,?,?,?,?),", len(args)/9), ",")
	_, err := s.tx.ExecContext(ctx, "INSERT INTO "+s.schema+".ingestion_outbox_events(id,tenant_id,table_id,event_type,aggregate_type,aggregate_key,payload,status,created_at) VALUES "+values, args...)
	return err
}

func (s *EffectStore) identity(id, tenant, objectType string) (uuid.UUID, error) {
	if s == nil || s.tx == nil {
		return uuid.Nil, errors.New("effect store is not initialized")
	}
	i, err := uuid.Parse(id)
	if err != nil || i == uuid.Nil {
		return uuid.Nil, errors.New("effect ID required")
	}
	t, err := uuid.Parse(tenant)
	if err != nil || t != s.tenant || objectType != s.objectType {
		return uuid.Nil, errors.New("effect scope mismatch")
	}
	return i, nil
}

func (s *EffectStore) textBudget(values ...string) error {
	remaining := s.maxRowBytes
	for _, value := range values {
		if !utf8.ValidString(value) || len(value) > remaining {
			return errors.New("effect text invalid or exceeds row byte limit")
		}
		remaining -= len(value)
	}
	return nil
}

func effectJSON(value []byte, kind byte) bool {
	v := bytes.TrimSpace(value)
	return len(v) != 0 && v[0] == kind && utf8.Valid(v) && json.Valid(v)
}

func (s *EffectStore) auditArgs(a ingestion.IngestionAudit) ([]any, error) {
	id, err := s.identity(a.ID, a.TenantID, a.ObjectType)
	if err != nil {
		return nil, err
	}
	key := ""
	var storedKey any
	if a.IdempotencyKey != nil {
		key = *a.IdempotencyKey
		storedKey = []byte(key)
	}
	if len(a.Payload) > s.maxRowBytes || len(a.ValidationError) > s.maxRowBytes {
		return nil, errors.New("audit JSON exceeds row byte limit")
	}
	if err := s.textBudget(a.ObjectType, a.ObjectID, string(a.Mode), a.RevisionID, a.Status, string(a.Payload), string(a.ValidationError), key); err != nil {
		return nil, err
	}
	if (a.Mode != ingestion.ModeCreate && a.Mode != ingestion.ModePatch) || a.RevisionID == "" || (a.Status != "succeeded" && a.Status != "validation_failed") || !effectJSON(a.Payload, '{') || !effectJSON(a.ValidationError, '[') {
		return nil, errors.New("invalid ingestion audit")
	}
	if a.Status == "succeeded" && a.ObjectID == "" {
		return nil, errors.New("successful audit requires object identity")
	}
	at, err := (mysqlstore.DateTime{Time: a.CreatedAt}).Value()
	if err != nil {
		return nil, err
	}
	return []any{mysqlstore.UUID(id), mysqlstore.UUID(s.tenant), mysqlstore.UUID(s.table), a.ObjectType, a.ObjectID, string(a.Mode), a.RevisionID, a.Status, string(a.Payload), string(a.ValidationError), storedKey, at}, nil
}

func (s *EffectStore) eventArgs(e ingestion.OutboxEvent) ([]any, error) {
	id, err := s.identity(e.ID, e.TenantID, e.AggregateType)
	if err != nil {
		return nil, err
	}
	if len(e.Payload) > s.maxRowBytes {
		return nil, errors.New("outbox JSON exceeds row byte limit")
	}
	if err := s.textBudget(e.EventType, e.AggregateType, e.AggregateKey, string(e.Payload), e.Status); err != nil {
		return nil, err
	}
	if (e.EventType != "record.ingested" && e.EventType != "record.updated" && e.EventType != "batch.ingestion.completed") || e.AggregateKey == "" || e.Status != "pending" || !effectJSON(e.Payload, '{') {
		return nil, errors.New("invalid ingestion outbox event")
	}
	if e.EventType == "batch.ingestion.completed" && e.AggregateKey != s.objectType {
		return nil, errors.New("batch event aggregate mismatch")
	}
	at, err := (mysqlstore.DateTime{Time: e.CreatedAt}).Value()
	if err != nil {
		return nil, err
	}
	return []any{mysqlstore.UUID(id), mysqlstore.UUID(s.tenant), mysqlstore.UUID(s.table), e.EventType, e.AggregateType, e.AggregateKey, string(e.Payload), e.Status, at}, nil
}
