package service

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/ports"
)

type IngestService struct {
	dataModelReader   ports.DataModelReader
	txManager         ports.TransactionManager
	readDataReader    ports.TenantDataReader
	idGenerator       ports.IDGenerator
	clock             ports.Clock
	factWriter        ports.AggregateFactWriter
	idempotencyReader ports.IdempotencyRepository
}

func (s *IngestService) SetAggregateFactWriter(writer ports.AggregateFactWriter) {
	s.factWriter = writer
}

// SetIdempotencyReader enables a read-only pre-check before aggregate facts are
// prepared. The transactional idempotency check remains authoritative and is
// still required to protect concurrent requests.
func (s *IngestService) SetIdempotencyReader(reader ports.IdempotencyRepository) {
	s.idempotencyReader = reader
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

func NewIngestService(
	dataModelReader ports.DataModelReader,
	txManager ports.TransactionManager,
	readDataReader ports.TenantDataReader,
	idGenerator ports.IDGenerator,
	clock ports.Clock,
) IngestService {
	return IngestService{
		dataModelReader: dataModelReader,
		txManager:       txManager,
		readDataReader:  readDataReader,
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
	precheckKey := factIdempotencyKey(input.IdempotencyKey, input.ObjectType, requestHash, true)
	existing, err := s.precheckCompletedIdempotency(ctx, input.TenantID, precheckKey, requestHash, ports.IdempotencyResponseKindSingle)
	if err != nil {
		return ingestion.RecordResult{}, nil, err
	}
	if existing != nil {
		var result ingestion.RecordResult
		if err := json.Unmarshal(existing.ResponsePayload, &result); err != nil {
			return ingestion.RecordResult{}, nil, fmt.Errorf("unmarshal stored idempotent response: %w", err)
		}
		result.Replayed = true
		return result, nil, nil
	}
	factWrite, err := s.prepareFactWrite(ctx, input.TenantID, input.ObjectType, input.Mode, []map[string]any{normalized}, input.IdempotencyKey, requestHash)
	if err != nil {
		return ingestion.RecordResult{}, nil, err
	}
	effectiveIdempotencyKey := factIdempotencyKey(input.IdempotencyKey, input.ObjectType, requestHash, factWrite.Prepared)
	stopFactLease := s.maintainFactWriteLease(factWrite)

	var result ingestion.RecordResult
	idempotencyReplayed := false
	replayedFactStatus := ""
	err = s.txManager.Run(ctx, func(store ports.MutationStore) error {
		if effectiveIdempotencyKey != nil {
			existing, err := store.Idempotency().Get(ctx, input.TenantID, *effectiveIdempotencyKey)
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
					if err := reconcileFactJournal(existing, &factWrite); err != nil {
						return err
					}
					if existing.FactStatus != nil {
						replayedFactStatus = *existing.FactStatus
					}
					idempotencyReplayed = true
					return nil
				}
				return ErrIdempotencyKeyReused
			}
		}
		if factWrite.PendingRetry {
			return fmt.Errorf("%w: an identical fact-enabled ingestion is still in progress", ErrAggregateFactUnavailable)
		}

		action, err := store.TenantWriter().UpsertRecord(ctx, model, input.ObjectType, normalized, input.Mode, now)
		if err != nil {
			return err
		}
		if factWrite.Prepared && action == "updated" {
			return ErrFactRecordMutationRejected
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

		if effectiveIdempotencyKey != nil {
			responsePayload, err := json.Marshal(result)
			if err != nil {
				return fmt.Errorf("marshal idempotent response: %w", err)
			}
			factMarker, factManifest, factStatus, factUpdatedAt := factJournalFields(factWrite, now)
			if err := store.Idempotency().Create(ctx, ingestion.IdempotencyKey{
				TenantID:        input.TenantID.String(),
				Key:             *effectiveIdempotencyKey,
				RequestHash:     requestHash,
				ResponseKind:    ports.IdempotencyResponseKindSingle,
				ResponsePayload: responsePayload,
				FactMarker:      factMarker,
				FactManifest:    factManifest,
				FactStatus:      factStatus,
				FactUpdatedAt:   factUpdatedAt,
				CreatedAt:       now,
			}); err != nil {
				return err
			}
		}
		return nil
	})
	stopFactLease()
	if err != nil {
		s.abortFactWrite(factWrite)
		return ingestion.RecordResult{}, nil, err
	}
	if idempotencyReplayed && replayedFactStatus != "pending" {
		if factWrite.OwnsMarker {
			// A durable applied (or pre-journal) response proves this event was
			// already accepted. Remove any new marker created after Redis expiry.
			s.abortFactWrite(factWrite)
		} else if factWrite.AlreadyApplied {
			s.confirmFactWrite(factWrite)
		}
		return result, nil, nil
	}
	if err := s.commitFactWrite(ctx, factWrite); err != nil {
		return ingestion.RecordResult{}, nil, err
	}
	if err := s.markFactWriteApplied(ctx, input.TenantID, effectiveIdempotencyKey, factWrite); err != nil {
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
	precheckKey := factIdempotencyKey(input.IdempotencyKey, input.ObjectType, requestHash, true)
	existing, err := s.precheckCompletedIdempotency(ctx, input.TenantID, precheckKey, requestHash, ports.IdempotencyResponseKindBatch)
	if err != nil {
		return nil, nil, err
	}
	if existing != nil {
		var results []ingestion.RecordResult
		if err := json.Unmarshal(existing.ResponsePayload, &results); err != nil {
			return nil, nil, fmt.Errorf("unmarshal stored idempotent batch response: %w", err)
		}
		for index := range results {
			results[index].Replayed = true
		}
		return results, nil, nil
	}
	normalizedRecords := make([]map[string]any, len(validated))
	for index := range validated {
		normalizedRecords[index] = validated[index].normalized
	}
	factWrite, err := s.prepareFactWrite(ctx, input.TenantID, input.ObjectType, input.Mode, normalizedRecords, input.IdempotencyKey, requestHash)
	if err != nil {
		return nil, nil, err
	}
	effectiveIdempotencyKey := factIdempotencyKey(input.IdempotencyKey, input.ObjectType, requestHash, factWrite.Prepared)
	stopFactLease := s.maintainFactWriteLease(factWrite)

	results := make([]ingestion.RecordResult, 0, len(validated))
	idempotencyReplayed := false
	replayedFactStatus := ""
	err = s.txManager.Run(ctx, func(store ports.MutationStore) error {
		if effectiveIdempotencyKey != nil {
			existing, err := store.Idempotency().Get(ctx, input.TenantID, *effectiveIdempotencyKey)
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
					if err := reconcileFactJournal(existing, &factWrite); err != nil {
						return err
					}
					if existing.FactStatus != nil {
						replayedFactStatus = *existing.FactStatus
					}
					idempotencyReplayed = true
					return nil
				}
				return ErrIdempotencyKeyReused
			}
		}
		if factWrite.PendingRetry {
			return fmt.Errorf("%w: an identical fact-enabled ingestion is still in progress", ErrAggregateFactUnavailable)
		}

		for _, record := range validated {
			action, err := store.TenantWriter().UpsertRecord(ctx, model, input.ObjectType, record.normalized, input.Mode, now)
			if err != nil {
				return err
			}
			if factWrite.Prepared && action == "updated" {
				return ErrFactRecordMutationRejected
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

		if effectiveIdempotencyKey != nil {
			responsePayload, err := json.Marshal(results)
			if err != nil {
				return fmt.Errorf("marshal idempotent batch response: %w", err)
			}
			factMarker, factManifest, factStatus, factUpdatedAt := factJournalFields(factWrite, now)
			if err := store.Idempotency().Create(ctx, ingestion.IdempotencyKey{
				TenantID:        input.TenantID.String(),
				Key:             *effectiveIdempotencyKey,
				RequestHash:     requestHash,
				ResponseKind:    ports.IdempotencyResponseKindBatch,
				ResponsePayload: responsePayload,
				FactMarker:      factMarker,
				FactManifest:    factManifest,
				FactStatus:      factStatus,
				FactUpdatedAt:   factUpdatedAt,
				CreatedAt:       now,
			}); err != nil {
				return err
			}
		}

		return nil
	})
	stopFactLease()
	if err != nil {
		s.abortFactWrite(factWrite)
		return nil, nil, err
	}
	if idempotencyReplayed && replayedFactStatus != "pending" {
		if factWrite.OwnsMarker {
			s.abortFactWrite(factWrite)
		} else if factWrite.AlreadyApplied {
			s.confirmFactWrite(factWrite)
		}
		return results, nil, nil
	}
	if err := s.commitFactWrite(ctx, factWrite); err != nil {
		return nil, nil, err
	}
	if err := s.markFactWriteApplied(ctx, input.TenantID, effectiveIdempotencyKey, factWrite); err != nil {
		return nil, nil, err
	}

	return results, nil, nil
}

func (s IngestService) prepareFactWrite(ctx context.Context, tenantID uuid.UUID, objectType string, mode ingestion.Mode, records []map[string]any, idempotencyKey *string, requestHash string) (ports.PreparedAggregateFactWrite, error) {
	if s.factWriter == nil {
		return ports.PreparedAggregateFactWrite{}, nil
	}
	marker := tenantID.String() + "|" + objectType + "|hash:" + requestHash
	if idempotencyKey != nil {
		marker = tenantID.String() + "|" + objectType + "|idempotency:" + *idempotencyKey
	}
	write, err := s.factWriter.Prepare(ctx, tenantID, objectType, records, marker)
	if err != nil {
		return ports.PreparedAggregateFactWrite{}, fmt.Errorf("%w: %v", ErrAggregateFactUnavailable, err)
	}
	if write.Prepared && mode != ingestion.ModeCreate {
		s.abortFactWrite(write)
		return ports.PreparedAggregateFactWrite{}, ErrFactRecordMutationRejected
	}
	return write, nil
}

func (s IngestService) commitFactWrite(ctx context.Context, write ports.PreparedAggregateFactWrite) error {
	if s.factWriter == nil || !write.Prepared {
		return nil
	}
	if err := s.factWriter.Commit(ctx, write); err != nil {
		return fmt.Errorf("%w: %v", ErrAggregateFactUnavailable, err)
	}
	return nil
}

func (s IngestService) abortFactWrite(write ports.PreparedAggregateFactWrite) {
	if s.factWriter == nil || !write.Prepared {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = s.factWriter.Abort(ctx, write)
}

func (s IngestService) maintainFactWriteLease(write ports.PreparedAggregateFactWrite) func() {
	if s.factWriter == nil || !write.Prepared || write.AlreadyApplied || !write.OwnsMarker {
		return func() {}
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(20 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				_ = s.factWriter.Renew(ctx, write)
				cancel()
			case <-stop:
				return
			}
		}
	}()
	return func() {
		close(stop)
		<-done
	}
}

func reconcileFactJournal(existing *ingestion.IdempotencyKey, write *ports.PreparedAggregateFactWrite) error {
	if existing.FactStatus == nil {
		return nil
	}
	if *existing.FactStatus == "applied" {
		if write.AlreadyApplied && existing.FactMarker != nil && existing.FactManifest != nil && *existing.FactMarker == write.Marker {
			write.Manifest = *existing.FactManifest
		}
		return nil
	}
	if *existing.FactStatus != "pending" || existing.FactMarker == nil || existing.FactManifest == nil {
		return fmt.Errorf("%w: durable aggregate fact journal is invalid", ErrAggregateFactUnavailable)
	}
	if !write.Prepared || *existing.FactMarker != write.Marker {
		return fmt.Errorf("%w: aggregate fact definition changed while an ingestion was pending", ErrAggregateFactUnavailable)
	}
	if write.AlreadyApplied {
		// The Redis marker is proof that the old manifest completed. Preserve
		// that durable identity when a definition changed before PostgreSQL's
		// completion receipt could be updated.
		write.Manifest = *existing.FactManifest
		return nil
	}
	if *existing.FactManifest != write.Manifest {
		return fmt.Errorf("%w: aggregate fact definition changed while an ingestion was pending", ErrAggregateFactUnavailable)
	}
	return nil
}

func factJournalFields(write ports.PreparedAggregateFactWrite, now time.Time) (*string, *string, *string, *time.Time) {
	if !write.Prepared {
		return nil, nil, nil, nil
	}
	marker, manifest, status, updatedAt := write.Marker, write.Manifest, "pending", now
	return &marker, &manifest, &status, &updatedAt
}

func (s IngestService) markFactWriteApplied(ctx context.Context, tenantID uuid.UUID, idempotencyKey *string, write ports.PreparedAggregateFactWrite) error {
	if !write.Prepared || idempotencyKey == nil {
		return nil
	}
	if err := s.txManager.Run(ctx, func(store ports.MutationStore) error {
		return store.Idempotency().MarkFactApplied(ctx, tenantID, *idempotencyKey, write.Marker, write.Manifest)
	}); err != nil {
		return fmt.Errorf("%w: persist aggregate fact completion: %v", ErrAggregateFactUnavailable, err)
	}
	s.confirmFactWrite(write)
	return nil
}

func (s IngestService) confirmFactWrite(write ports.PreparedAggregateFactWrite) {
	if s.factWriter == nil || !write.Prepared {
		return
	}
	confirmCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = s.factWriter.Confirm(confirmCtx, write)
}

func factIdempotencyKey(provided *string, objectType, requestHash string, factEnabled bool) *string {
	if provided != nil || !factEnabled {
		return provided
	}
	key := "aggregate-fact:" + objectType + ":" + requestHash
	return &key
}

func (s IngestService) precheckCompletedIdempotency(
	ctx context.Context,
	tenantID uuid.UUID,
	key *string,
	requestHash string,
	responseKind string,
) (*ingestion.IdempotencyKey, error) {
	if s.idempotencyReader == nil || key == nil {
		return nil, nil
	}
	existing, err := s.idempotencyReader.Get(ctx, tenantID, *key)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, nil
	}
	if existing.RequestHash != requestHash || existing.ResponseKind != responseKind {
		return nil, ErrIdempotencyKeyReused
	}
	if existing.FactStatus == nil || *existing.FactStatus == "applied" {
		return existing, nil
	}
	if *existing.FactStatus == "pending" {
		// PostgreSQL accepted the records, but Valkey completion has not been
		// confirmed. Continue through Prepare/Commit so the durable journal can
		// recover the exact fact write.
		return nil, nil
	}
	return nil, fmt.Errorf("%w: durable aggregate fact journal is invalid", ErrAggregateFactUnavailable)
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
