package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/ports"
)

func TestEventIngestBypassesPostgresEvenWithIdempotencyKey(t *testing.T) {
	t.Parallel()
	tenantID := uuid.New()
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	txManager := &memoryEventTransactionManager{repo: &memoryIdempotencyRepository{}}
	events := &memoryEventStore{}
	service := NewIngestService(
		stubPublishedModelReader{model: eventTransactionsModel(tenantID)},
		txManager,
		nil,
		&fixedIDGenerator{},
		fixedClock{now: now},
		events,
	)
	input := IngestInput{
		TenantID:       tenantID,
		ObjectType:     "transactions",
		Mode:           ingestion.ModeCreate,
		IdempotencyKey: stringPointer("live-event-1"),
		Payload: map[string]any{
			"object_id": "txn-1",
			"date":      "2026-08-20T09:59:00Z",
			"amount":    42.5,
		},
	}

	first, validationErrors, err := service.Ingest(context.Background(), input)
	if err != nil || len(validationErrors) != 0 {
		t.Fatalf("first ingest: result=%+v validation=%v error=%v", first, validationErrors, err)
	}
	second, validationErrors, err := service.Ingest(context.Background(), input)
	if err != nil || len(validationErrors) != 0 {
		t.Fatalf("second ingest: result=%+v validation=%v error=%v", second, validationErrors, err)
	}
	if txManager.runs != 0 {
		t.Fatalf("single event ingest used PostgreSQL %d times", txManager.runs)
	}
	if events.writeCalls != 2 {
		t.Fatalf("event writes = %d, want 2", events.writeCalls)
	}
	if events.eventIDs[0] != events.eventIDs[1] {
		t.Fatalf("exact retries must use a stable event ID: %q != %q", events.eventIDs[0], events.eventIDs[1])
	}
	if first.Replayed || second.Replayed {
		t.Fatal("ledger-free single event requests must not claim a synchronous replay")
	}
}

func stringPointer(value string) *string { return &value }

func TestEventBatchStoresOnlyRequestLevelIdempotency(t *testing.T) {
	t.Parallel()
	tenantID := uuid.New()
	key := "seed-batch-1"
	txManager := &memoryEventTransactionManager{repo: &memoryIdempotencyRepository{}}
	events := &memoryEventStore{}
	service := NewIngestService(
		stubPublishedModelReader{model: eventTransactionsModel(tenantID)},
		txManager,
		nil,
		&fixedIDGenerator{},
		fixedClock{now: time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)},
		events,
	)
	input := BatchIngestInput{
		TenantID:       tenantID,
		ObjectType:     "transactions",
		Mode:           ingestion.ModeCreate,
		IdempotencyKey: &key,
		Records: []map[string]any{{
			"object_id": "txn-batch-1",
			"date":      "2026-08-20T09:59:00Z",
			"amount":    42.5,
		}},
	}

	first, validationErrors, err := service.BatchIngest(context.Background(), input)
	if err != nil || len(validationErrors) != 0 || len(first) != 1 {
		t.Fatalf("first batch ingest: results=%+v validation=%v error=%v", first, validationErrors, err)
	}
	if events.batchWriteCalls != 1 || txManager.repo.record == nil {
		t.Fatalf("first batch state: writes=%d idempotency=%+v", events.batchWriteCalls, txManager.repo.record)
	}

	second, validationErrors, err := service.BatchIngest(context.Background(), input)
	if err != nil || len(validationErrors) != 0 || len(second) != 1 {
		t.Fatalf("replayed batch ingest: results=%+v validation=%v error=%v", second, validationErrors, err)
	}
	if events.batchWriteCalls != 1 || !second[0].Replayed {
		t.Fatalf("idempotent replay wrote to ClickHouse: writes=%d results=%+v", events.batchWriteCalls, second)
	}
	if txManager.runs != 3 {
		t.Fatalf("PostgreSQL transactions = %d, want precheck+receipt+replay check", txManager.runs)
	}
}

func TestEventBatchRejectsReusedIdempotencyKey(t *testing.T) {
	t.Parallel()
	tenantID := uuid.New()
	key := "seed-batch-1"
	txManager := &memoryEventTransactionManager{repo: &memoryIdempotencyRepository{}}
	events := &memoryEventStore{}
	service := NewIngestService(
		stubPublishedModelReader{model: eventTransactionsModel(tenantID)},
		txManager,
		nil,
		&fixedIDGenerator{},
		fixedClock{now: time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)},
		events,
	)
	input := BatchIngestInput{
		TenantID:       tenantID,
		ObjectType:     "transactions",
		Mode:           ingestion.ModeCreate,
		IdempotencyKey: &key,
		Records: []map[string]any{{
			"object_id": "txn-batch-1",
			"date":      "2026-08-20T09:59:00Z",
			"amount":    42.5,
		}},
	}
	if _, _, err := service.BatchIngest(context.Background(), input); err != nil {
		t.Fatalf("first batch ingest: %v", err)
	}
	input.Records[0]["amount"] = 99.0
	if _, _, err := service.BatchIngest(context.Background(), input); !errors.Is(err, ErrIdempotencyKeyReused) {
		t.Fatalf("reused key error = %v, want %v", err, ErrIdempotencyKeyReused)
	}
	if events.batchWriteCalls != 1 {
		t.Fatalf("conflicting replay wrote to ClickHouse: %d writes", events.batchWriteCalls)
	}
}

func TestEventIngestLocksPublishedSchemaBeforeWriting(t *testing.T) {
	tenantID := uuid.New()
	model := eventTransactionsModel(tenantID)
	reader := &recordingEventModelReader{model: model}
	events := &memoryEventStore{}
	service := NewIngestService(
		reader,
		&memoryEventTransactionManager{repo: &memoryIdempotencyRepository{}},
		nil,
		&fixedIDGenerator{},
		fixedClock{now: time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)},
		events,
	)

	_, validationErrors, err := service.Ingest(context.Background(), IngestInput{
		TenantID: tenantID, ObjectType: "transactions", Mode: ingestion.ModeCreate,
		Payload: map[string]any{"object_id": "txn-lock", "date": "2026-08-20T11:59:00Z", "amount": 10},
	})
	if err != nil || len(validationErrors) != 0 {
		t.Fatalf("Ingest() error/validation = %v / %#v", err, validationErrors)
	}
	wantTable := model.Tables["transactions"]
	if reader.lockCalls != 1 || reader.lockedTableID != wantTable.ID || reader.lockedRevision != wantTable.EventSchemaRevision {
		t.Fatalf("schema lock = calls:%d table:%s revision:%s", reader.lockCalls, reader.lockedTableID, reader.lockedRevision)
	}
	if events.writeCalls != 1 {
		t.Fatalf("event writes = %d, want 1", events.writeCalls)
	}
}

func eventTransactionsModel(tenantID uuid.UUID) ingestion.PublishedDataModel {
	cutover := time.Date(2026, 8, 20, 8, 0, 0, 0, time.UTC)
	return ingestion.PublishedDataModel{
		TenantID:            tenantID,
		RevisionID:          "event-revision-1",
		Writable:            true,
		RecordLookupField:   "object_id",
		ManagedSystemFields: []string{"updated_at", "valid_from", "valid_until"},
		Tables: map[string]ingestion.ObjectSchema{
			"transactions": {
				ID:                  uuid.New(),
				Name:                "transactions",
				StorageClass:        "event",
				EventTimeField:      "date",
				EventSchemaRevision: "event-schema-1",
				StorageCutoverAt:    &cutover,
				LegacyReadUntil:     &cutover,
				Fields: map[string]ingestion.FieldSchema{
					"object_id": {Name: "object_id", DataType: "string"},
					"date":      {Name: "date", DataType: "timestamp"},
					"amount":    {Name: "amount", DataType: "float"},
				},
			},
		},
	}
}

type recordingEventModelReader struct {
	model          ingestion.PublishedDataModel
	lockCalls      int
	lockedTableID  uuid.UUID
	lockedRevision string
}

func (r *recordingEventModelReader) GetPublishedDataModel(context.Context, uuid.UUID) (ingestion.PublishedDataModel, error) {
	return r.model, nil
}

func (r *recordingEventModelReader) LockEventTableSchema(_ context.Context, _ uuid.UUID, tableID uuid.UUID, revision string) error {
	r.lockCalls++
	r.lockedTableID = tableID
	r.lockedRevision = revision
	return nil
}

type memoryEventTransactionManager struct {
	repo *memoryIdempotencyRepository
	runs int
}

func (m *memoryEventTransactionManager) Run(ctx context.Context, fn func(ports.MutationStore) error) error {
	m.runs++
	return fn(memoryEventMutationStore{repo: m.repo})
}

type memoryEventMutationStore struct{ repo *memoryIdempotencyRepository }

func (memoryEventMutationStore) Audits() ports.IngestionAuditRepository     { return nil }
func (s memoryEventMutationStore) Idempotency() ports.IdempotencyRepository { return s.repo }
func (memoryEventMutationStore) OutboxEvents() ports.OutboxEventRepository  { return nil }
func (memoryEventMutationStore) UploadLogs() ports.UploadLogRepository      { return nil }
func (memoryEventMutationStore) DeferredIngests() ports.DeferredIngestRepository {
	return nil
}
func (memoryEventMutationStore) TenantWriter() ports.TenantDataWriter { return nil }
func (memoryEventMutationStore) TenantReader() ports.TenantDataReader { return nil }
func (memoryEventMutationStore) RawTx() pgx.Tx                        { return nil }

type memoryIdempotencyRepository struct{ record *ingestion.IdempotencyKey }

func (m *memoryIdempotencyRepository) Get(_ context.Context, tenantID uuid.UUID, key string) (*ingestion.IdempotencyKey, error) {
	if m.record == nil || m.record.TenantID != tenantID.String() || m.record.Key != key {
		return nil, nil
	}
	copy := *m.record
	copy.ResponsePayload = append([]byte(nil), m.record.ResponsePayload...)
	return &copy, nil
}

func (m *memoryIdempotencyRepository) Create(_ context.Context, record ingestion.IdempotencyKey) error {
	copy := record
	copy.ResponsePayload = append([]byte(nil), record.ResponsePayload...)
	m.record = &copy
	return nil
}

type memoryEventStore struct {
	writeCalls      int
	batchWriteCalls int
	eventIDs        []string
}

func (m *memoryEventStore) Write(_ context.Context, _ ingestion.PublishedDataModel, _, eventID, _, _ string, _ map[string]any, _ time.Time) error {
	m.writeCalls++
	m.eventIDs = append(m.eventIDs, eventID)
	return nil
}

func (m *memoryEventStore) WriteBatch(_ context.Context, _ ingestion.PublishedDataModel, _ string, writes []ports.EventWrite) error {
	m.batchWriteCalls++
	for _, write := range writes {
		m.eventIDs = append(m.eventIDs, write.EventID)
	}
	return nil
}

func (*memoryEventStore) GetRecord(context.Context, ingestion.PublishedDataModel, string, string) (map[string]any, error) {
	return nil, nil
}
func (*memoryEventStore) ListRecords(context.Context, ingestion.PublishedDataModel, string, int) ([]map[string]any, error) {
	return nil, nil
}
func (*memoryEventStore) QueryRecords(context.Context, ingestion.PublishedDataModel, string, string, string, int) ([]map[string]any, error) {
	return nil, nil
}
func (*memoryEventStore) AggregateRecords(context.Context, ingestion.PublishedDataModel, ingestion.AggregateQuery) (any, error) {
	return nil, nil
}
