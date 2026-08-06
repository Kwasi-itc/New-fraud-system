package dto

import "github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"

type DeferredIngestResponse struct {
	ID           string  `json:"id"`
	TenantID     string  `json:"tenant_id"`
	ObjectType   string  `json:"object_type"`
	Mode         string  `json:"mode"`
	Status       string  `json:"status"`
	AttemptCount int     `json:"attempt_count"`
	ErrorMessage *string `json:"error_message,omitempty"`
	RequestedAt  string  `json:"requested_at"`
	StartedAt    *string `json:"started_at,omitempty"`
	CompletedAt  *string `json:"completed_at,omitempty"`
}

func AdaptDeferredIngest(execution ingestion.DeferredIngest) DeferredIngestResponse {
	response := DeferredIngestResponse{
		ID:           execution.ID,
		TenantID:     execution.TenantID,
		ObjectType:   execution.ObjectType,
		Mode:         string(execution.Mode),
		Status:       string(execution.Status),
		AttemptCount: execution.AttemptCount,
		ErrorMessage: execution.ErrorMessage,
		RequestedAt:  execution.RequestedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
	}
	if execution.StartedAt != nil {
		startedAt := execution.StartedAt.UTC().Format("2006-01-02T15:04:05Z07:00")
		response.StartedAt = &startedAt
	}
	if execution.CompletedAt != nil {
		completedAt := execution.CompletedAt.UTC().Format("2006-01-02T15:04:05Z07:00")
		response.CompletedAt = &completedAt
	}
	return response
}
