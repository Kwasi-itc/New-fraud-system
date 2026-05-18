package dto

import (
	"time"

	"github.com/google/uuid"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
)

type CreateIndexJobRequest struct {
	TableID              uuid.UUID  `json:"table_id" binding:"required"`
	IndexType            string     `json:"index_type" binding:"required"`
	Columns              []string   `json:"columns" binding:"required"`
	RequestedByOperation string     `json:"requested_by_operation"`
	ScheduledAt          *time.Time `json:"scheduled_at"`
}

type IndexJobResponse struct {
	ID                   uuid.UUID  `json:"id"`
	TenantID             uuid.UUID  `json:"tenant_id"`
	TableID              *uuid.UUID `json:"table_id,omitempty"`
	TableName            string     `json:"table_name"`
	IndexType            string     `json:"index_type"`
	Columns              []string   `json:"columns"`
	Status               string     `json:"status"`
	RequestedByOperation string     `json:"requested_by_operation"`
	ErrorMessage         *string    `json:"error_message,omitempty"`
	AttemptCount         int        `json:"attempt_count"`
	RequestedAt          time.Time  `json:"requested_at"`
	StartedAt            *time.Time `json:"started_at,omitempty"`
	CompletedAt          *time.Time `json:"completed_at,omitempty"`
	ScheduledAt          *time.Time `json:"scheduled_at,omitempty"`
	DedupeKey            string     `json:"dedupe_key"`
}

func AdaptIndexJob(job datamodel.IndexJob) IndexJobResponse {
	return IndexJobResponse{
		ID:                   job.ID,
		TenantID:             job.TenantID,
		TableID:              job.TableID,
		TableName:            job.TableName,
		IndexType:            string(job.IndexType),
		Columns:              job.Columns,
		Status:               string(job.Status),
		RequestedByOperation: job.RequestedByOperation,
		ErrorMessage:         job.ErrorMessage,
		AttemptCount:         job.AttemptCount,
		RequestedAt:          job.RequestedAt,
		StartedAt:            job.StartedAt,
		CompletedAt:          job.CompletedAt,
		ScheduledAt:          job.ScheduledAt,
		DedupeKey:            job.DedupeKey,
	}
}
