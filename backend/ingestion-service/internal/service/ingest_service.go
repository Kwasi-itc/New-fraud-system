package service

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/google/uuid"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/ports"
)

type IngestService struct {
	dataModelReader ports.DataModelReader
	txManager       ports.TransactionManager
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

func NewIngestService(
	dataModelReader ports.DataModelReader,
	txManager ports.TransactionManager,
	idGenerator ports.IDGenerator,
	clock ports.Clock,
) IngestService {
	return IngestService{
		dataModelReader: dataModelReader,
		txManager:       txManager,
		idGenerator:     idGenerator,
		clock:           clock,
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

	type validatedRecord struct {
		normalized map[string]any
		objectID   string
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

func (s IngestService) GetRecord(ctx context.Context, tenantID uuid.UUID, objectType, objectID string) (RecordLookupResult, error) {
	model, err := s.dataModelReader.GetPublishedDataModel(ctx, tenantID)
	if err != nil {
		return RecordLookupResult{}, err
	}

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

func (s IngestService) ListRecords(ctx context.Context, tenantID uuid.UUID, objectType string, limit int) (RecordListResult, error) {
	model, err := s.dataModelReader.GetPublishedDataModel(ctx, tenantID)
	if err != nil {
		return RecordListResult{}, err
	}

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

func (s IngestService) QueryRecords(ctx context.Context, tenantID uuid.UUID, objectType, fieldName, value string, limit int) (RecordQueryResult, error) {
	model, err := s.dataModelReader.GetPublishedDataModel(ctx, tenantID)
	if err != nil {
		return RecordQueryResult{}, err
	}

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
