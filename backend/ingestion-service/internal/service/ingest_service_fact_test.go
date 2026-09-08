package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/ports"
)

type aggregateFactWriterStub struct {
	prepared ports.PreparedAggregateFactWrite
	prepare  error
	commit   error
	aborts   int
	prepares int
}

func (s *aggregateFactWriterStub) Prepare(context.Context, uuid.UUID, string, []map[string]any, string) (ports.PreparedAggregateFactWrite, error) {
	s.prepares++
	return s.prepared, s.prepare
}

func (s *aggregateFactWriterStub) Commit(context.Context, ports.PreparedAggregateFactWrite) error {
	return s.commit
}

func (s *aggregateFactWriterStub) Renew(context.Context, ports.PreparedAggregateFactWrite) error {
	return nil
}

func (s *aggregateFactWriterStub) Confirm(context.Context, ports.PreparedAggregateFactWrite) error {
	return nil
}

func (s *aggregateFactWriterStub) Abort(context.Context, ports.PreparedAggregateFactWrite) error {
	s.aborts++
	return nil
}

func TestPrepareFactWriteReturnsRetryableFailureWhenRedisIsUnavailable(t *testing.T) {
	writer := &aggregateFactWriterStub{prepare: errors.New("redis unavailable")}
	service := IngestService{factWriter: writer}
	_, err := service.prepareFactWrite(
		context.Background(), uuid.New(), "transactions", ingestion.ModeCreate,
		[]map[string]any{{"object_id": "txn-1"}}, nil, "request-hash",
	)
	if !errors.Is(err, ErrAggregateFactUnavailable) {
		t.Fatalf("error = %v, want ErrAggregateFactUnavailable", err)
	}
}

func TestPrepareFactWriteRejectsMutationForFactEnabledTable(t *testing.T) {
	writer := &aggregateFactWriterStub{prepared: ports.PreparedAggregateFactWrite{Prepared: true}}
	service := IngestService{factWriter: writer}
	_, err := service.prepareFactWrite(
		context.Background(), uuid.New(), "transactions", ingestion.ModePatch,
		[]map[string]any{{"object_id": "txn-1"}}, nil, "request-hash",
	)
	if !errors.Is(err, ErrFactRecordMutationRejected) || writer.aborts != 1 {
		t.Fatalf("error = %v, aborts = %d", err, writer.aborts)
	}
}

func TestCommitFactWriteReturnsRetryableFailure(t *testing.T) {
	writer := &aggregateFactWriterStub{commit: errors.New("write failed")}
	service := IngestService{factWriter: writer}
	err := service.commitFactWrite(context.Background(), ports.PreparedAggregateFactWrite{Prepared: true})
	if !errors.Is(err, ErrAggregateFactUnavailable) {
		t.Fatalf("error = %v, want ErrAggregateFactUnavailable", err)
	}
}

func TestFactIdempotencyKeyCreatesDurableInternalKeyOnlyWhenNeeded(t *testing.T) {
	if got := factIdempotencyKey(nil, "transactions", "abc123", false); got != nil {
		t.Fatalf("non-fact key = %q, want nil", *got)
	}
	got := factIdempotencyKey(nil, "transactions", "abc123", true)
	if got == nil || *got != "aggregate-fact:transactions:abc123" {
		t.Fatalf("fact key = %v", got)
	}
	provided := "caller-key"
	if got := factIdempotencyKey(&provided, "transactions", "abc123", true); got != &provided {
		t.Fatal("caller-provided idempotency key must take precedence")
	}
}

func TestValidateFactJournalAllowsMatchingPendingRecovery(t *testing.T) {
	marker, manifest, status := "marker", "manifest", "pending"
	existing := &ingestion.IdempotencyKey{FactMarker: &marker, FactManifest: &manifest, FactStatus: &status}
	write := ports.PreparedAggregateFactWrite{Prepared: true, Marker: marker, Manifest: manifest}
	if err := reconcileFactJournal(existing, &write); err != nil {
		t.Fatalf("reconcileFactJournal() error = %v", err)
	}
	write.Manifest = "changed"
	if err := reconcileFactJournal(existing, &write); !errors.Is(err, ErrAggregateFactUnavailable) {
		t.Fatalf("changed manifest error = %v, want ErrAggregateFactUnavailable", err)
	}
}

func TestValidateFactJournalTreatsAppliedPostgresReceiptAsAuthoritative(t *testing.T) {
	status := "applied"
	existing := &ingestion.IdempotencyKey{FactStatus: &status}
	write := ports.PreparedAggregateFactWrite{}
	if err := reconcileFactJournal(existing, &write); err != nil {
		t.Fatalf("applied receipt error = %v", err)
	}
}

func TestBatchIngestPrecheckReturnsCompletedReplayWithoutPreparingFacts(t *testing.T) {
	tenantID := uuid.New()
	key := "completed-batch"
	records := []map[string]any{{"object_id": "txn-1", "status": "pending"}}
	requestHash, err := hashRequest(records)
	if err != nil {
		t.Fatalf("hashRequest() error = %v", err)
	}
	response, err := json.Marshal([]ingestion.RecordResult{{
		ObjectID: "txn-1", Action: "created", RevisionID: "rev",
	}})
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	reader := &idempotencyReaderStub{record: &ingestion.IdempotencyKey{
		TenantID: tenantID.String(), Key: key, RequestHash: requestHash,
		ResponseKind: ports.IdempotencyResponseKindBatch, ResponsePayload: response,
	}}
	writer := &aggregateFactWriterStub{prepared: ports.PreparedAggregateFactWrite{Prepared: true}}
	txManager := &countingTransactionManager{}
	service := NewIngestService(
		factTestDataModelReader{}, txManager, nil, factTestIDGenerator{}, factTestClock{},
	)
	service.SetIdempotencyReader(reader)
	service.SetAggregateFactWriter(writer)

	results, validationErrors, err := service.BatchIngest(context.Background(), BatchIngestInput{
		TenantID: tenantID, ObjectType: "transactions", Mode: ingestion.ModeCreate,
		Records: records, IdempotencyKey: &key,
	})
	if err != nil {
		t.Fatalf("BatchIngest() error = %v", err)
	}
	if len(validationErrors) != 0 {
		t.Fatalf("validation errors = %#v", validationErrors)
	}
	if len(results) != 1 || !results[0].Replayed || results[0].ObjectID != "txn-1" {
		t.Fatalf("results = %#v", results)
	}
	if writer.prepares != 0 {
		t.Fatalf("fact prepares = %d, want 0", writer.prepares)
	}
	if txManager.calls != 0 {
		t.Fatalf("write transactions = %d, want 0", txManager.calls)
	}
}

func TestIdempotencyPrecheckLeavesPendingFactReceiptForRecovery(t *testing.T) {
	status := "pending"
	key := "pending-batch"
	reader := &idempotencyReaderStub{record: &ingestion.IdempotencyKey{
		Key: key, RequestHash: "hash", ResponseKind: ports.IdempotencyResponseKindBatch,
		FactStatus: &status,
	}}
	service := IngestService{idempotencyReader: reader}

	existing, err := service.precheckCompletedIdempotency(
		context.Background(), uuid.New(), &key, "hash", ports.IdempotencyResponseKindBatch,
	)
	if err != nil {
		t.Fatalf("precheckCompletedIdempotency() error = %v", err)
	}
	if existing != nil {
		t.Fatalf("pending receipt was treated as completed: %#v", existing)
	}
}

type idempotencyReaderStub struct {
	record *ingestion.IdempotencyKey
	err    error
}

func (s *idempotencyReaderStub) Get(context.Context, uuid.UUID, string) (*ingestion.IdempotencyKey, error) {
	return s.record, s.err
}
func (*idempotencyReaderStub) Create(context.Context, ingestion.IdempotencyKey) error { return nil }
func (*idempotencyReaderStub) MarkFactApplied(context.Context, uuid.UUID, string, string, string) error {
	return nil
}

type countingTransactionManager struct{ calls int }

func (s *countingTransactionManager) Run(context.Context, func(ports.MutationStore) error) error {
	s.calls++
	return errors.New("unexpected write transaction")
}

type factTestDataModelReader struct{}

func (factTestDataModelReader) GetPublishedDataModel(context.Context, uuid.UUID) (ingestion.PublishedDataModel, error) {
	return ingestion.PublishedDataModel{
		RevisionID: "rev", Writable: true, RecordLookupField: "object_id",
		ManagedSystemFields: []string{"object_id", "updated_at", "valid_from", "valid_until"},
		Tables: map[string]ingestion.ObjectSchema{
			"transactions": {
				Fields: map[string]ingestion.FieldSchema{
					"status": {Name: "status", DataType: "string", Nullable: false},
				},
			},
		},
	}, nil
}

type factTestClock struct{}

func (factTestClock) Now() time.Time { return time.Unix(0, 0).UTC() }

type factTestIDGenerator struct{}

func (factTestIDGenerator) New() uuid.UUID { return uuid.Nil }

var _ ports.AggregateFactWriter = (*aggregateFactWriterStub)(nil)
var _ ports.IdempotencyRepository = (*idempotencyReaderStub)(nil)
var _ ports.TransactionManager = (*countingTransactionManager)(nil)
