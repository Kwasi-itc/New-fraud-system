package service

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/ports"
)

type IngestService struct {
	dataModelReader ports.DataModelReader
	txManager       ports.TransactionManager
	readDataReader  ports.TenantDataReader
	eventStore      ports.EventStore
	idGenerator     ports.IDGenerator
	clock           ports.Clock
}

type RecordLookupResult struct {
	ObjectID   string         `json:"object_id"`
	ObjectType string         `json:"object_type"`
	Fields     map[string]any `json:"fields"`
}

type RecordListResult struct {
	Records []RecordLookupResult `json:"records"`
}

type RecordQueryResult struct {
	Records []RecordLookupResult `json:"records"`
}

type AggregateResult struct {
	Value any `json:"value"`
}

type IngestInput struct {
	TenantID       uuid.UUID
	ObjectType     string
	Mode           ingestion.Mode
	Payload        map[string]any
	IdempotencyKey *string
}

type BatchIngestInput struct {
	TenantID       uuid.UUID
	ObjectType     string
	Mode           ingestion.Mode
	Records        []map[string]any
	IdempotencyKey *string
}

type validatedRecord struct {
	normalized map[string]any
	objectID   string
}

func NewIngestService(
	dataModelReader ports.DataModelReader,
	txManager ports.TransactionManager,
	readDataReader ports.TenantDataReader,
	idGenerator ports.IDGenerator,
	clock ports.Clock,
	eventStores ...ports.EventStore,
) IngestService {
	var eventStore ports.EventStore
	if len(eventStores) > 0 {
		eventStore = eventStores[0]
	}
	return IngestService{
		dataModelReader: dataModelReader,
		txManager:       txManager,
		readDataReader:  readDataReader,
		idGenerator:     idGenerator,
		clock:           clock,
		eventStore:      eventStore,
	}
}

func (s IngestService) Ingest(ctx context.Context, input IngestInput) (ingestion.RecordResult, []ingestion.ValidationError, error) {
	model, err := s.dataModelReader.GetPublishedDataModel(ctx, input.TenantID)
	if err != nil {
		return ingestion.RecordResult{}, nil, err
	}
	if !model.Writable {
		return ingestion.RecordResult{}, nil, fmt.Errorf("tenant is not writable for ingestion")
	}

	normalized, objectID, validationErrors := ingestion.ValidateRecord(model, input.ObjectType, input.Payload, input.Mode)
	now := s.clock.Now()
	if len(validationErrors) > 0 {
		stampObjectID(validationErrors, objectID)
		_ = s.txManager.Run(ctx, func(store ports.MutationStore) error {
			return store.Audits().Create(ctx, ingestion.IngestionAudit{
				ID:              s.idGenerator.New().String(),
				TenantID:        input.TenantID.String(),
				ObjectType:      input.ObjectType,
				ObjectID:        objectID,
				Mode:            input.Mode,
				RevisionID:      model.RevisionID,
				Status:          "validation_failed",
				Payload:         ingestion.MarshalPayload(input.Payload),
				ValidationError: ingestion.MarshalValidationErrors(validationErrors),
				IdempotencyKey:  input.IdempotencyKey,
				CreatedAt:       now,
			})
		})
		return ingestion.RecordResult{}, validationErrors, nil
	}

	requestHash, err := hashRequest(normalized)
	if err != nil {
		return ingestion.RecordResult{}, nil, err
	}
	if isEventTable(model, input.ObjectType) {
		result, err := s.ingestEvent(ctx, model, input, normalized, objectID, requestHash, now)
		return result, nil, err
	}

	var result ingestion.RecordResult
	err = s.txManager.Run(ctx, func(store ports.MutationStore) error {
		if input.IdempotencyKey != nil {
			existing, err := store.Idempotency().Get(ctx, input.TenantID, *input.IdempotencyKey)
			if err != nil {
				return err
			}
			if existing != nil {
				if existing.RequestHash == requestHash {
					if existing.ResponseKind != ports.IdempotencyResponseKindSingle {
						return ErrIdempotencyKeyReused
					}
					if err := json.Unmarshal(existing.ResponsePayload, &result); err != nil {
						return fmt.Errorf("unmarshal stored idempotent response: %w", err)
					}
					result.Replayed = true
					return nil
				}
				return ErrIdempotencyKeyReused
			}
		}

		action, err := store.TenantWriter().UpsertRecord(ctx, model, input.ObjectType, normalized, input.Mode, now)
		if err != nil {
			return err
		}

		if err := store.Audits().Create(ctx, ingestion.IngestionAudit{
			ID:              s.idGenerator.New().String(),
			TenantID:        input.TenantID.String(),
			ObjectType:      input.ObjectType,
			ObjectID:        objectID,
			Mode:            input.Mode,
			RevisionID:      model.RevisionID,
			Status:          "succeeded",
			Payload:         ingestion.MarshalPayload(normalized),
			ValidationError: []byte("[]"),
			IdempotencyKey:  input.IdempotencyKey,
			CreatedAt:       now,
		}); err != nil {
			return err
		}

		eventType := "record.ingested"
		if action == "updated" {
			eventType = "record.updated"
		}
		eventPayload, _ := json.Marshal(map[string]any{
			"tenant_id":   input.TenantID,
			"object_type": input.ObjectType,
			"object_id":   objectID,
			"mode":        input.Mode,
			"revision_id": model.RevisionID,
			"action":      action,
			"record":      normalized,
			"ingested_at": now,
		})
		if err := store.OutboxEvents().Create(ctx, ingestion.OutboxEvent{
			ID:            s.idGenerator.New().String(),
			TenantID:      input.TenantID.String(),
			EventType:     eventType,
			AggregateType: input.ObjectType,
			AggregateKey:  objectID,
			Payload:       eventPayload,
			Status:        "pending",
			CreatedAt:     now,
		}); err != nil {
			return err
		}

		result = ingestion.RecordResult{
			ObjectID:   objectID,
			Action:     action,
			RevisionID: model.RevisionID,
		}

		if input.IdempotencyKey != nil {
			responsePayload, err := json.Marshal(result)
			if err != nil {
				return fmt.Errorf("marshal idempotent response: %w", err)
			}
			if err := store.Idempotency().Create(ctx, ingestion.IdempotencyKey{
				TenantID:        input.TenantID.String(),
				Key:             *input.IdempotencyKey,
				RequestHash:     requestHash,
				ResponseKind:    ports.IdempotencyResponseKindSingle,
				ResponsePayload: responsePayload,
				CreatedAt:       now,
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return ingestion.RecordResult{}, nil, err
	}

	return result, nil, nil
}

func (s IngestService) BatchIngest(ctx context.Context, input BatchIngestInput) ([]ingestion.RecordResult, []ingestion.ValidationError, error) {
	model, err := s.dataModelReader.GetPublishedDataModel(ctx, input.TenantID)
	if err != nil {
		return nil, nil, err
	}
	if !model.Writable {
		return nil, nil, fmt.Errorf("tenant is not writable for ingestion")
	}

	if len(input.Records) == 0 {
		return nil, []ingestion.ValidationError{{
			Field:   "records",
			Code:    "empty_batch",
			Message: "batch request must contain at least one record",
		}}, nil
	}
	if len(input.Records) > 500 {
		return nil, []ingestion.ValidationError{{
			Field:   "records",
			Code:    "batch_too_large",
			Message: "batch request exceeds the maximum supported size of 500 records",
		}}, nil
	}

	seenObjectIDs := make(map[string]struct{}, len(input.Records))
	validated := make([]validatedRecord, 0, len(input.Records))
	validationErrors := make([]ingestion.ValidationError, 0)
	now := s.clock.Now()
	for _, record := range input.Records {
		normalized, objectID, errs := ingestion.ValidateRecord(model, input.ObjectType, record, input.Mode)
		if len(errs) > 0 {
			stampObjectID(errs, objectID)
			validationErrors = append(validationErrors, errs...)
			continue
		}
		if _, exists := seenObjectIDs[objectID]; exists {
			validationErrors = append(validationErrors, ingestion.ValidationError{
				ObjectID: objectID,
				Field:    model.RecordLookupField,
				Code:     "duplicate_object_id",
				Message:  fmt.Sprintf("object_id %s appears more than once in the batch", objectID),
			})
			continue
		}
		seenObjectIDs[objectID] = struct{}{}
		validated = append(validated, validatedRecord{normalized: normalized, objectID: objectID})
	}

	if len(validationErrors) > 0 {
		_ = s.txManager.Run(ctx, func(store ports.MutationStore) error {
			for _, record := range input.Records {
				objectID, _ := record[model.RecordLookupField].(string)
				if err := store.Audits().Create(ctx, ingestion.IngestionAudit{
					ID:              s.idGenerator.New().String(),
					TenantID:        input.TenantID.String(),
					ObjectType:      input.ObjectType,
					ObjectID:        objectID,
					Mode:            input.Mode,
					RevisionID:      model.RevisionID,
					Status:          "validation_failed",
					Payload:         ingestion.MarshalPayload(record),
					ValidationError: ingestion.MarshalValidationErrors(filterErrorsForObject(validationErrors, objectID)),
					IdempotencyKey:  input.IdempotencyKey,
					CreatedAt:       now,
				}); err != nil {
					return err
				}
			}
			return nil
		})
		return nil, validationErrors, nil
	}

	requestHash, err := hashRequest(input.Records)
	if err != nil {
		return nil, nil, err
	}
	if isEventTable(model, input.ObjectType) {
		results, err := s.ingestEventBatch(ctx, model, input, validated, requestHash, now)
		return results, nil, err
	}

	results := make([]ingestion.RecordResult, 0, len(validated))
	err = s.txManager.Run(ctx, func(store ports.MutationStore) error {
		if input.IdempotencyKey != nil {
			existing, err := store.Idempotency().Get(ctx, input.TenantID, *input.IdempotencyKey)
			if err != nil {
				return err
			}
			if existing != nil {
				if existing.RequestHash == requestHash {
					if existing.ResponseKind != ports.IdempotencyResponseKindBatch {
						return ErrIdempotencyKeyReused
					}
					if err := json.Unmarshal(existing.ResponsePayload, &results); err != nil {
						return fmt.Errorf("unmarshal stored idempotent batch response: %w", err)
					}
					for i := range results {
						results[i].Replayed = true
					}
					return nil
				}
				return ErrIdempotencyKeyReused
			}
		}

		for _, record := range validated {
			action, err := store.TenantWriter().UpsertRecord(ctx, model, input.ObjectType, record.normalized, input.Mode, now)
			if err != nil {
				return err
			}

			if err := store.Audits().Create(ctx, ingestion.IngestionAudit{
				ID:              s.idGenerator.New().String(),
				TenantID:        input.TenantID.String(),
				ObjectType:      input.ObjectType,
				ObjectID:        record.objectID,
				Mode:            input.Mode,
				RevisionID:      model.RevisionID,
				Status:          "succeeded",
				Payload:         ingestion.MarshalPayload(record.normalized),
				ValidationError: []byte("[]"),
				IdempotencyKey:  input.IdempotencyKey,
				CreatedAt:       now,
			}); err != nil {
				return err
			}

			eventType := "record.ingested"
			if action == "updated" {
				eventType = "record.updated"
			}
			eventPayload, _ := json.Marshal(map[string]any{
				"tenant_id":   input.TenantID,
				"object_type": input.ObjectType,
				"object_id":   record.objectID,
				"mode":        input.Mode,
				"revision_id": model.RevisionID,
				"action":      action,
				"record":      record.normalized,
				"ingested_at": now,
			})
			if err := store.OutboxEvents().Create(ctx, ingestion.OutboxEvent{
				ID:            s.idGenerator.New().String(),
				TenantID:      input.TenantID.String(),
				EventType:     eventType,
				AggregateType: input.ObjectType,
				AggregateKey:  record.objectID,
				Payload:       eventPayload,
				Status:        "pending",
				CreatedAt:     now,
			}); err != nil {
				return err
			}

			results = append(results, ingestion.RecordResult{
				ObjectID:   record.objectID,
				Action:     action,
				RevisionID: model.RevisionID,
			})
		}

		batchEventPayload, _ := json.Marshal(map[string]any{
			"tenant_id":   input.TenantID,
			"object_type": input.ObjectType,
			"mode":        input.Mode,
			"revision_id": model.RevisionID,
			"count":       len(results),
			"ingested_at": now,
		})
		if err := store.OutboxEvents().Create(ctx, ingestion.OutboxEvent{
			ID:            s.idGenerator.New().String(),
			TenantID:      input.TenantID.String(),
			EventType:     "batch.ingestion.completed",
			AggregateType: input.ObjectType,
			AggregateKey:  input.ObjectType,
			Payload:       batchEventPayload,
			Status:        "pending",
			CreatedAt:     now,
		}); err != nil {
			return err
		}

		if input.IdempotencyKey != nil {
			responsePayload, err := json.Marshal(results)
			if err != nil {
				return fmt.Errorf("marshal idempotent batch response: %w", err)
			}
			if err := store.Idempotency().Create(ctx, ingestion.IdempotencyKey{
				TenantID:        input.TenantID.String(),
				Key:             *input.IdempotencyKey,
				RequestHash:     requestHash,
				ResponseKind:    ports.IdempotencyResponseKindBatch,
				ResponsePayload: responsePayload,
				CreatedAt:       now,
			}); err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		return nil, nil, err
	}

	return results, nil, nil
}

func hashRequest(value any) (string, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("marshal request for idempotency hashing: %w", err)
	}
	sum := sha1.Sum(body)
	return hex.EncodeToString(sum[:]), nil
}

func stampObjectID(errors []ingestion.ValidationError, objectID string) {
	for i := range errors {
		errors[i].ObjectID = objectID
	}
}

func filterErrorsForObject(errors []ingestion.ValidationError, objectID string) []ingestion.ValidationError {
	filtered := make([]ingestion.ValidationError, 0)
	for _, err := range errors {
		if err.ObjectID == objectID {
			filtered = append(filtered, err)
		}
	}
	if len(filtered) == 0 {
		return slices.Clone(errors)
	}
	return filtered
}

func isEventTable(model ingestion.PublishedDataModel, objectType string) bool {
	table, ok := model.Tables[objectType]
	return ok && table.StorageClass == "event"
}

func (s IngestService) ingestEvent(ctx context.Context, model ingestion.PublishedDataModel, input IngestInput, normalized map[string]any, objectID, requestHash string, now time.Time) (ingestion.RecordResult, error) {
	if s.eventStore == nil {
		return ingestion.RecordResult{}, fmt.Errorf("event store is required for event table ingestion")
	}
	if input.Mode != ingestion.ModeCreate {
		return ingestion.RecordResult{}, fmt.Errorf("event tables are append-only and do not support patch ingestion")
	}
	if err := s.lockEventSchema(ctx, model, input.ObjectType); err != nil {
		return ingestion.RecordResult{}, err
	}
	result := ingestion.RecordResult{ObjectID: objectID, Action: "created", RevisionID: model.RevisionID}
	// Single event writes never access PostgreSQL, including when callers send
	// Idempotency-Key. The stable ID lets ClickHouse's ReplacingMergeTree
	// collapse exact retries without a per-event receipt or ledger row.
	eventID := stableEventID(input.TenantID, input.ObjectType, objectID, requestHash)
	if err := s.eventStore.Write(ctx, model, input.ObjectType, eventID, objectID, requestHash, normalized, now); err != nil {
		return ingestion.RecordResult{}, err
	}
	return result, nil
}

func (s IngestService) ingestEventBatch(ctx context.Context, model ingestion.PublishedDataModel, input BatchIngestInput, validated []validatedRecord, batchRequestHash string, now time.Time) ([]ingestion.RecordResult, error) {
	if s.eventStore == nil {
		return nil, fmt.Errorf("event store is required for event table ingestion")
	}
	if input.Mode != ingestion.ModeCreate {
		return nil, fmt.Errorf("event tables are append-only and do not support patch ingestion")
	}
	results := make([]ingestion.RecordResult, len(validated))
	replayed, err := s.loadEventIdempotency(ctx, input.TenantID, input.IdempotencyKey, batchRequestHash, ports.IdempotencyResponseKindBatch, &results)
	if err != nil {
		return nil, err
	}
	if replayed {
		for i := range results {
			results[i].Replayed = true
		}
		return results, nil
	}
	if err := s.lockEventSchema(ctx, model, input.ObjectType); err != nil {
		return nil, err
	}

	writes := make([]ports.EventWrite, len(validated))
	for i, record := range validated {
		recordHash, err := hashRequest(record.normalized)
		if err != nil {
			return nil, err
		}
		results[i] = ingestion.RecordResult{ObjectID: record.objectID, Action: "created", RevisionID: model.RevisionID}
		writes[i] = ports.EventWrite{
			EventID:     stableEventID(input.TenantID, input.ObjectType, record.objectID, recordHash),
			ObjectID:    record.objectID,
			RequestHash: recordHash,
			Payload:     record.normalized,
			IngestedAt:  now,
		}
	}
	if err := s.eventStore.WriteBatch(ctx, model, input.ObjectType, writes); err != nil {
		return nil, err
	}
	if err := s.storeEventIdempotency(ctx, input.TenantID, input.IdempotencyKey, batchRequestHash, ports.IdempotencyResponseKindBatch, results, now); err != nil {
		return nil, err
	}
	return results, nil
}

func (s IngestService) lockEventSchema(ctx context.Context, model ingestion.PublishedDataModel, objectType string) error {
	table, ok := model.Tables[objectType]
	if !ok {
		return fmt.Errorf("object type %s is not available", objectType)
	}
	if table.EventSchemaLockedAt != nil {
		return nil
	}
	if strings.TrimSpace(table.EventSchemaRevision) == "" {
		return fmt.Errorf("event table %s is missing event_schema_revision", objectType)
	}
	if err := s.dataModelReader.LockEventTableSchema(ctx, model.TenantID, table.ID, table.EventSchemaRevision); err != nil {
		return fmt.Errorf("lock event table schema: %w", err)
	}
	return nil
}

func stableEventID(tenantID uuid.UUID, objectType, objectID, requestHash string) string {
	return uuid.NewSHA1(tenantID, []byte(objectType+"\x00"+objectID+"\x00"+requestHash)).String()
}

func (s IngestService) loadEventIdempotency(ctx context.Context, tenantID uuid.UUID, key *string, requestHash, responseKind string, response any) (bool, error) {
	if key == nil {
		return false, nil
	}
	if s.txManager == nil {
		return false, fmt.Errorf("PostgreSQL idempotency store is required when Idempotency-Key is provided")
	}
	replayed := false
	err := s.txManager.Run(ctx, func(store ports.MutationStore) error {
		existing, err := store.Idempotency().Get(ctx, tenantID, *key)
		if err != nil {
			return err
		}
		if existing == nil {
			return nil
		}
		if existing.RequestHash != requestHash || existing.ResponseKind != responseKind {
			return ErrIdempotencyKeyReused
		}
		if err := json.Unmarshal(existing.ResponsePayload, response); err != nil {
			return fmt.Errorf("unmarshal stored idempotent event response: %w", err)
		}
		replayed = true
		return nil
	})
	return replayed, err
}

func (s IngestService) storeEventIdempotency(ctx context.Context, tenantID uuid.UUID, key *string, requestHash, responseKind string, response any, now time.Time) error {
	if key == nil {
		return nil
	}
	body, err := json.Marshal(response)
	if err != nil {
		return fmt.Errorf("marshal idempotent event response: %w", err)
	}
	return s.txManager.Run(ctx, func(store ports.MutationStore) error {
		return store.Idempotency().Create(ctx, ingestion.IdempotencyKey{
			TenantID:        tenantID.String(),
			Key:             *key,
			RequestHash:     requestHash,
			ResponseKind:    responseKind,
			ResponsePayload: body,
			CreatedAt:       now,
		})
	})
}

func (s IngestService) GetRecord(ctx context.Context, tenantID uuid.UUID, objectType, objectID string) (RecordLookupResult, error) {
	model, err := s.dataModelReader.GetPublishedDataModel(ctx, tenantID)
	if err != nil {
		return RecordLookupResult{}, err
	}
	if isEventTable(model, objectType) {
		if s.eventStore == nil {
			return RecordLookupResult{}, fmt.Errorf("event store is required for event table reads")
		}
		record, eventErr := s.eventStore.GetRecord(ctx, model, objectType, objectID)
		if eventErr == nil {
			return RecordLookupResult{ObjectID: objectID, ObjectType: objectType, Fields: record}, nil
		}
		if !legacyBridgeActive(model.Tables[objectType], s.clock.Now()) {
			return RecordLookupResult{}, eventErr
		}
		record, err = s.getLegacyRecord(ctx, model, objectType, objectID)
		if err != nil {
			return RecordLookupResult{}, eventErr
		}
		return RecordLookupResult{ObjectID: objectID, ObjectType: objectType, Fields: record}, nil
	}

	reader := s.readDataReader
	if reader == nil {
		var result RecordLookupResult
		err = s.txManager.Run(ctx, func(store ports.MutationStore) error {
			record, err := store.TenantReader().GetRecord(ctx, model, objectType, objectID)
			if err != nil {
				return err
			}
			result = RecordLookupResult{
				ObjectID:   objectID,
				ObjectType: objectType,
				Fields:     record,
			}
			return nil
		})
		if err != nil {
			return RecordLookupResult{}, err
		}
		return result, nil
	}

	record, err := reader.GetRecord(ctx, model, objectType, objectID)
	if err != nil {
		return RecordLookupResult{}, err
	}
	return RecordLookupResult{
		ObjectID:   objectID,
		ObjectType: objectType,
		Fields:     record,
	}, nil
}

func (s IngestService) ListRecords(ctx context.Context, tenantID uuid.UUID, objectType string, limit int) (RecordListResult, error) {
	model, err := s.dataModelReader.GetPublishedDataModel(ctx, tenantID)
	if err != nil {
		return RecordListResult{}, err
	}
	if isEventTable(model, objectType) {
		if s.eventStore == nil {
			return RecordListResult{}, fmt.Errorf("event store is required for event table reads")
		}
		eventRecords, err := s.eventStore.ListRecords(ctx, model, objectType, limit)
		if err != nil {
			return RecordListResult{}, err
		}
		if legacyBridgeActive(model.Tables[objectType], s.clock.Now()) {
			legacy, legacyErr := s.listLegacyRecords(ctx, model, objectType, limit)
			if legacyErr != nil {
				return RecordListResult{}, legacyErr
			}
			eventRecords = mergeRecords(eventRecords, legacy, model.RecordLookupField, limit)
		}
		return adaptRecordList(eventRecords, objectType, model.RecordLookupField), nil
	}

	reader := s.readDataReader
	if reader == nil {
		var result RecordListResult
		err = s.txManager.Run(ctx, func(store ports.MutationStore) error {
			records, err := store.TenantReader().ListRecords(ctx, model, objectType, limit)
			if err != nil {
				return err
			}
			result.Records = make([]RecordLookupResult, len(records))
			for i, record := range records {
				objectID := ""
				if value, ok := record[model.RecordLookupField]; ok && value != nil {
					objectID = fmt.Sprint(value)
				}
				result.Records[i] = RecordLookupResult{
					ObjectID:   objectID,
					ObjectType: objectType,
					Fields:     record,
				}
			}
			return nil
		})
		if err != nil {
			return RecordListResult{}, err
		}
		return result, nil
	}

	records, err := reader.ListRecords(ctx, model, objectType, limit)
	if err != nil {
		return RecordListResult{}, err
	}
	out := RecordListResult{Records: make([]RecordLookupResult, len(records))}
	for i, record := range records {
		objectID := ""
		if value, ok := record[model.RecordLookupField]; ok && value != nil {
			objectID = fmt.Sprint(value)
		}
		out.Records[i] = RecordLookupResult{
			ObjectID:   objectID,
			ObjectType: objectType,
			Fields:     record,
		}
	}
	return out, nil
}

func (s IngestService) QueryRecords(ctx context.Context, tenantID uuid.UUID, objectType, fieldName, value string, limit int) (RecordQueryResult, error) {
	model, err := s.dataModelReader.GetPublishedDataModel(ctx, tenantID)
	if err != nil {
		return RecordQueryResult{}, err
	}
	if isEventTable(model, objectType) {
		if s.eventStore == nil {
			return RecordQueryResult{}, fmt.Errorf("event store is required for event table reads")
		}
		eventRecords, err := s.eventStore.QueryRecords(ctx, model, objectType, fieldName, value, limit)
		if err != nil {
			return RecordQueryResult{}, err
		}
		if legacyBridgeActive(model.Tables[objectType], s.clock.Now()) {
			legacy, legacyErr := s.queryLegacyRecords(ctx, model, objectType, fieldName, value, limit)
			if legacyErr != nil {
				return RecordQueryResult{}, legacyErr
			}
			eventRecords = mergeRecords(eventRecords, legacy, model.RecordLookupField, limit)
		}
		list := adaptRecordList(eventRecords, objectType, model.RecordLookupField)
		return RecordQueryResult{Records: list.Records}, nil
	}

	reader := s.readDataReader
	if reader == nil {
		var result RecordQueryResult
		err = s.txManager.Run(ctx, func(store ports.MutationStore) error {
			records, err := store.TenantReader().QueryRecords(ctx, model, objectType, fieldName, value, limit)
			if err != nil {
				return err
			}
			result.Records = make([]RecordLookupResult, len(records))
			for i, record := range records {
				objectID := ""
				if raw, ok := record[model.RecordLookupField]; ok && raw != nil {
					objectID = fmt.Sprint(raw)
				}
				result.Records[i] = RecordLookupResult{
					ObjectID:   objectID,
					ObjectType: objectType,
					Fields:     record,
				}
			}
			return nil
		})
		if err != nil {
			return RecordQueryResult{}, err
		}
		return result, nil
	}

	records, err := reader.QueryRecords(ctx, model, objectType, fieldName, value, limit)
	if err != nil {
		return RecordQueryResult{}, err
	}
	result := RecordQueryResult{Records: make([]RecordLookupResult, len(records))}
	for i, record := range records {
		objectID := ""
		if raw, ok := record[model.RecordLookupField]; ok && raw != nil {
			objectID = fmt.Sprint(raw)
		}
		result.Records[i] = RecordLookupResult{
			ObjectID:   objectID,
			ObjectType: objectType,
			Fields:     record,
		}
	}
	return result, nil
}

func (s IngestService) AggregateRecords(ctx context.Context, tenantID uuid.UUID, query ingestion.AggregateQuery) (AggregateResult, error) {
	model, err := s.dataModelReader.GetPublishedDataModel(ctx, tenantID)
	if err != nil {
		return AggregateResult{}, err
	}
	if isEventTable(model, query.ObjectType) {
		table := model.Tables[query.ObjectType]
		if !hasEventTimeLowerBound(query.Filter, table.EventTimeField) {
			return AggregateResult{}, fmt.Errorf("event aggregate requires a lower-bound filter on %s", table.EventTimeField)
		}
		if s.eventStore == nil {
			return AggregateResult{}, fmt.Errorf("event store is required for event table aggregates")
		}
		if !legacyBridgeActive(table, s.clock.Now()) {
			value, err := s.eventStore.AggregateRecords(ctx, model, query)
			return AggregateResult{Value: value}, err
		}
		return s.aggregateHybrid(ctx, model, table, query)
	}

	reader := s.readDataReader
	if reader == nil {
		var result AggregateResult
		err = s.txManager.Run(ctx, func(store ports.MutationStore) error {
			value, err := store.TenantReader().AggregateRecords(ctx, model, query)
			if err != nil {
				return err
			}
			result.Value = value
			return nil
		})
		if err != nil {
			return AggregateResult{}, err
		}
		return result, nil
	}
	value, err := reader.AggregateRecords(ctx, model, query)
	if err != nil {
		return AggregateResult{}, err
	}
	return AggregateResult{Value: value}, nil
}

func legacyBridgeActive(table ingestion.ObjectSchema, now time.Time) bool {
	return table.StorageCutoverAt != nil && table.LegacyReadUntil != nil && now.Before(*table.LegacyReadUntil) && table.LegacyReadUntil.After(*table.StorageCutoverAt)
}

func hasEventTimeLowerBound(filter *ingestion.AggregateFilter, field string) bool {
	if filter == nil {
		return false
	}
	if strings.EqualFold(filter.Kind, ingestion.AggregateFilterKindPredicate) {
		return filter.Field == field && (strings.EqualFold(filter.Op, "gt") || strings.EqualFold(filter.Op, "gte"))
	}
	op := strings.ToLower(strings.TrimSpace(filter.Operator))
	if op == "" {
		op = "and"
	}
	if op != "and" {
		return false
	}
	for i := range filter.Children {
		if hasEventTimeLowerBound(&filter.Children[i], field) {
			return true
		}
	}
	return false
}

func (s IngestService) aggregateHybrid(ctx context.Context, model ingestion.PublishedDataModel, _ ingestion.ObjectSchema, query ingestion.AggregateQuery) (AggregateResult, error) {
	name := strings.ToLower(strings.TrimSpace(query.Aggregate))
	if name == "count_distinct" {
		return AggregateResult{}, fmt.Errorf("aggregate %s is unavailable during the PostgreSQL-to-ClickHouse bridge window", name)
	}
	if name == "avg" {
		return s.aggregateHybridAverage(ctx, model, query)
	}
	// Cutover is an ingestion-time boundary, not an event-time boundary. New
	// events may legitimately carry historical timestamps, so both stores must
	// receive the rule's original bounded event-time filter during the bridge.
	legacy, err := s.aggregateLegacy(ctx, model, query)
	if err != nil {
		return AggregateResult{}, err
	}
	current, err := s.eventStore.AggregateRecords(ctx, model, query)
	if err != nil {
		return AggregateResult{}, err
	}
	combined, err := combineAggregateValues(name, legacy, current)
	if err != nil {
		return AggregateResult{}, err
	}
	return AggregateResult{Value: combined}, nil
}

func (s IngestService) aggregateHybridAverage(ctx context.Context, model ingestion.PublishedDataModel, query ingestion.AggregateQuery) (AggregateResult, error) {
	sumQuery := query
	sumQuery.Aggregate = "sum"
	countQuery := query
	countQuery.Aggregate = "count"
	legacySum, err := s.aggregateLegacy(ctx, model, sumQuery)
	if err != nil {
		return AggregateResult{}, err
	}
	legacyCount, err := s.aggregateLegacy(ctx, model, countQuery)
	if err != nil {
		return AggregateResult{}, err
	}
	eventSum, err := s.eventStore.AggregateRecords(ctx, model, sumQuery)
	if err != nil {
		return AggregateResult{}, err
	}
	eventCount, err := s.eventStore.AggregateRecords(ctx, model, countQuery)
	if err != nil {
		return AggregateResult{}, err
	}
	total, err := combineAggregateValues("sum", legacySum, eventSum)
	if err != nil {
		return AggregateResult{}, err
	}
	count, err := combineAggregateValues("count", legacyCount, eventCount)
	if err != nil {
		return AggregateResult{}, err
	}
	totalNumber, _ := numericValue(total)
	countNumber, _ := numericValue(count)
	if countNumber == 0 {
		return AggregateResult{Value: nil}, nil
	}
	return AggregateResult{Value: totalNumber / countNumber}, nil
}

func combineAggregateValues(name string, legacy, current any) (any, error) {
	if name == "count" || name == "sum" {
		left, err := numericValue(legacy)
		if err != nil {
			return nil, err
		}
		right, err := numericValue(current)
		if err != nil {
			return nil, err
		}
		return left + right, nil
	}
	if legacy == nil {
		return current, nil
	}
	if current == nil {
		return legacy, nil
	}
	left, leftErr := numericValue(legacy)
	right, rightErr := numericValue(current)
	if leftErr == nil && rightErr == nil {
		if name == "min" && left < right {
			return legacy, nil
		}
		if name == "max" && left > right {
			return legacy, nil
		}
		return current, nil
	}
	leftText, rightText := fmt.Sprint(legacy), fmt.Sprint(current)
	if name == "min" && leftText < rightText {
		return legacy, nil
	}
	if name == "max" && leftText > rightText {
		return legacy, nil
	}
	return current, nil
}

func numericValue(value any) (float64, error) {
	switch typed := value.(type) {
	case int64:
		return float64(typed), nil
	case int:
		return float64(typed), nil
	case float64:
		return typed, nil
	case json.Number:
		return typed.Float64()
	default:
		return 0, fmt.Errorf("aggregate returned non-numeric value %T", value)
	}
}

func (s IngestService) getLegacyRecord(ctx context.Context, model ingestion.PublishedDataModel, objectType, objectID string) (map[string]any, error) {
	if s.readDataReader != nil {
		return s.readDataReader.GetRecord(ctx, model, objectType, objectID)
	}
	var value map[string]any
	err := s.txManager.Run(ctx, func(store ports.MutationStore) error {
		var err error
		value, err = store.TenantReader().GetRecord(ctx, model, objectType, objectID)
		return err
	})
	return value, err
}

func (s IngestService) listLegacyRecords(ctx context.Context, model ingestion.PublishedDataModel, objectType string, limit int) ([]map[string]any, error) {
	if s.readDataReader != nil {
		return s.readDataReader.ListRecords(ctx, model, objectType, limit)
	}
	var value []map[string]any
	err := s.txManager.Run(ctx, func(store ports.MutationStore) error {
		var err error
		value, err = store.TenantReader().ListRecords(ctx, model, objectType, limit)
		return err
	})
	return value, err
}

func (s IngestService) queryLegacyRecords(ctx context.Context, model ingestion.PublishedDataModel, objectType, fieldName, value string, limit int) ([]map[string]any, error) {
	if s.readDataReader != nil {
		return s.readDataReader.QueryRecords(ctx, model, objectType, fieldName, value, limit)
	}
	var records []map[string]any
	err := s.txManager.Run(ctx, func(store ports.MutationStore) error {
		var err error
		records, err = store.TenantReader().QueryRecords(ctx, model, objectType, fieldName, value, limit)
		return err
	})
	return records, err
}

func (s IngestService) aggregateLegacy(ctx context.Context, model ingestion.PublishedDataModel, query ingestion.AggregateQuery) (any, error) {
	if s.readDataReader != nil {
		return s.readDataReader.AggregateRecords(ctx, model, query)
	}
	var value any
	err := s.txManager.Run(ctx, func(store ports.MutationStore) error {
		var err error
		value, err = store.TenantReader().AggregateRecords(ctx, model, query)
		return err
	})
	return value, err
}

func adaptRecordList(records []map[string]any, objectType, lookupField string) RecordListResult {
	out := RecordListResult{Records: make([]RecordLookupResult, len(records))}
	for i, record := range records {
		out.Records[i] = RecordLookupResult{ObjectID: fmt.Sprint(record[lookupField]), ObjectType: objectType, Fields: record}
	}
	return out
}

func mergeRecords(primary, legacy []map[string]any, lookupField string, limit int) []map[string]any {
	if limit <= 0 {
		limit = 100
	}
	seen := make(map[string]struct{}, len(primary))
	out := make([]map[string]any, 0, min(limit, len(primary)+len(legacy)))
	for _, group := range [][]map[string]any{primary, legacy} {
		for _, record := range group {
			key := fmt.Sprint(record[lookupField])
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, record)
			if len(out) == limit {
				return out
			}
		}
	}
	return out
}
