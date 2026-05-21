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
)

type UploadLogService struct {
	uploadLogs    ports.UploadLogRepository
	ingestService IngestService
	idGenerator   ports.IDGenerator
	clock         ports.Clock
	maxAttempts   int
}

func NewUploadLogService(
	uploadLogs ports.UploadLogRepository,
	ingestService IngestService,
	idGenerator ports.IDGenerator,
	clock ports.Clock,
	maxAttempts int,
) UploadLogService {
	if maxAttempts <= 0 {
		maxAttempts = 3
	}
	return UploadLogService{
		uploadLogs:    uploadLogs,
		ingestService: ingestService,
		idGenerator:   idGenerator,
		clock:         clock,
		maxAttempts:   maxAttempts,
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
	return log, s.uploadLogs.Create(ctx, log)
}

func (s UploadLogService) List(ctx context.Context, tenantID uuid.UUID, objectType string) ([]ingestion.UploadLog, error) {
	return s.uploadLogs.ListByTenantAndObjectType(ctx, tenantID, objectType)
}

func (s UploadLogService) Get(ctx context.Context, id uuid.UUID) (ingestion.UploadLog, error) {
	return s.uploadLogs.GetByID(ctx, id)
}

func (s UploadLogService) ProcessNextUploaded(ctx context.Context) (bool, error) {
	now := s.clock.Now()
	log, err := s.uploadLogs.ClaimNextUploaded(ctx, now)
	if err != nil {
		return false, err
	}
	if log == nil {
		return false, nil
	}

	records, err := decodeCSV(log.Payload)
	if err != nil {
		return true, s.handleRetryableFailure(ctx, log, now, err.Error())
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
		return true, s.handleRetryableFailure(ctx, log, now, err.Error())
	}
	if len(validationErrors) > 0 {
		message := summarizeValidationErrors(validationErrors)
		log.Status = ingestion.UploadLogStatusFailed
		log.ErrorMessage = &message
		log.FailedRows = len(records)
		log.CompletedAt = &now
		_ = s.uploadLogs.Update(ctx, *log)
		return true, nil
	}

	log.Status = ingestion.UploadLogStatusCompleted
	log.SuccessfulRows = len(results)
	log.CompletedAt = &now
	return true, s.uploadLogs.Update(ctx, *log)
}

func (s UploadLogService) handleRetryableFailure(ctx context.Context, log *ingestion.UploadLog, now time.Time, message string) error {
	log.ErrorMessage = &message
	if log.AttemptCount < s.maxAttempts {
		log.Status = ingestion.UploadLogStatusUploaded
		log.StartedAt = nil
		log.CompletedAt = nil
		return s.uploadLogs.Update(ctx, *log)
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
