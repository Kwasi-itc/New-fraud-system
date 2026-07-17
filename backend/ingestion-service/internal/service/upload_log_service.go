package service

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/riverjobs"
)

type UploadLogService struct {
	uploadLogs    ports.UploadLogRepository
	ingestService IngestService
	txManager     ports.TransactionManager
	idGenerator   ports.IDGenerator
	clock         ports.Clock
	maxAttempts   int
	enqueuer      riverjobs.UploadLogEnqueuer
}

func NewUploadLogService(
	uploadLogs ports.UploadLogRepository,
	ingestService IngestService,
	txManager ports.TransactionManager,
	idGenerator ports.IDGenerator,
	clock ports.Clock,
	maxAttempts int,
	enqueuer riverjobs.UploadLogEnqueuer,
) UploadLogService {
	if maxAttempts <= 0 {
		maxAttempts = 3
	}
	if enqueuer == nil {
		enqueuer = riverjobs.NoopUploadLogEnqueuer{}
	}
	return UploadLogService{
		uploadLogs:    uploadLogs,
		ingestService: ingestService,
		txManager:     txManager,
		idGenerator:   idGenerator,
		clock:         clock,
		maxAttempts:   maxAttempts,
		enqueuer:      enqueuer,
	}
}

func (s UploadLogService) Create(ctx context.Context, tenantID uuid.UUID, objectType string, mode ingestion.Mode, filename, contentType string, payload []byte) (ingestion.UploadLog, error) {
	now := s.clock.Now()
	log := ingestion.UploadLog{
		ID:          s.idGenerator.New().String(),
		TenantID:    tenantID.String(),
		ObjectType:  objectType,
		Mode:        mode,
		Filename:    filename,
		ContentType: contentType,
		Status:      ingestion.UploadLogStatusUploaded,
		Payload:     payload,
		RequestedAt: now,
	}
	logID, err := uuid.Parse(log.ID)
	if err != nil {
		return ingestion.UploadLog{}, err
	}
	if s.txManager == nil {
		if err := s.uploadLogs.Create(ctx, log); err != nil {
			return ingestion.UploadLog{}, err
		}
		if err := s.enqueuer.Enqueue(ctx, logID, nil); err != nil {
			return ingestion.UploadLog{}, err
		}
		return log, nil
	}
	if err := s.txManager.Run(ctx, func(store ports.MutationStore) error {
		if err := store.UploadLogs().Create(ctx, log); err != nil {
			return err
		}
		return s.enqueuer.EnqueueTx(ctx, store.RawTx(), logID, nil)
	}); err != nil {
		return ingestion.UploadLog{}, err
	}
	return log, nil
}

func (s UploadLogService) List(ctx context.Context, tenantID uuid.UUID, objectType string) ([]ingestion.UploadLog, error) {
	return s.uploadLogs.ListByTenantAndObjectType(ctx, tenantID, objectType)
}

func (s UploadLogService) Get(ctx context.Context, id uuid.UUID) (ingestion.UploadLog, error) {
	return s.uploadLogs.GetByID(ctx, id)
}

func (s UploadLogService) RunLog(ctx context.Context, id uuid.UUID) error {
	now := s.clock.Now()
	log, err := s.uploadLogs.StartAttempt(ctx, id, now)
	if err != nil {
		return err
	}

	records, err := decodeCSV(log.Payload)
	if err != nil {
		return s.handleRetryableFailure(ctx, &log, now, err.Error())
	}

	log.TotalRows = len(records)
	results, validationErrors, err := s.ingestService.BatchIngest(ctx, BatchIngestInput{
		TenantID:   uuid.MustParse(log.TenantID),
		ObjectType: log.ObjectType,
		Mode:       log.Mode,
		Records:    records,
	})
	if err != nil {
		log.FailedRows = len(records)
		return s.handleRetryableFailure(ctx, &log, now, err.Error())
	}
	if len(validationErrors) > 0 {
		message := summarizeValidationErrors(validationErrors)
		log.Status = ingestion.UploadLogStatusFailed
		log.ErrorMessage = &message
		log.FailedRows = len(records)
		log.CompletedAt = &now
		return s.uploadLogs.Update(ctx, log)
	}

	log.Status = ingestion.UploadLogStatusCompleted
	log.SuccessfulRows = len(results)
	log.CompletedAt = &now
	return s.uploadLogs.Update(ctx, log)
}

func (s UploadLogService) handleRetryableFailure(ctx context.Context, log *ingestion.UploadLog, now time.Time, message string) error {
	log.ErrorMessage = &message
	if log.AttemptCount < s.maxAttempts {
		log.Status = ingestion.UploadLogStatusUploaded
		log.StartedAt = nil
		log.CompletedAt = nil
		id, err := uuid.Parse(log.ID)
		if err != nil {
			return err
		}
		if s.txManager == nil {
			if err := s.uploadLogs.Update(ctx, *log); err != nil {
				return err
			}
			return s.enqueuer.Enqueue(ctx, id, nil)
		}
		return s.txManager.Run(ctx, func(store ports.MutationStore) error {
			if err := store.UploadLogs().Update(ctx, *log); err != nil {
				return err
			}
			return s.enqueuer.EnqueueTx(ctx, store.RawTx(), id, nil)
		})
	}
	log.Status = ingestion.UploadLogStatusFailed
	log.CompletedAt = &now
	return s.uploadLogs.Update(ctx, *log)
}

func decodeCSV(payload []byte) ([]map[string]any, error) {
	reader := csv.NewReader(strings.NewReader(string(payload)))
	headers, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("read csv header: %w", err)
	}

	records := make([]map[string]any, 0)
	for {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read csv row: %w", err)
		}
		record := make(map[string]any, len(headers))
		for i, header := range headers {
			value := ""
			if i < len(row) {
				value = strings.TrimSpace(row[i])
			}
			if value == "" {
				record[header] = nil
			} else {
				record[header] = value
			}
		}
		records = append(records, record)
	}
	return records, nil
}

func summarizeValidationErrors(errors []ingestion.ValidationError) string {
	if len(errors) == 0 {
		return ""
	}
	if len(errors) == 1 {
		return errors[0].Message
	}
	return fmt.Sprintf("%s; plus %d more validation errors", errors[0].Message, len(errors)-1)
}
