package dto

import "github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"

type UploadLogResponse struct {
	ID             string  `json:"id"`
	TenantID       string  `json:"tenant_id"`
	ObjectType     string  `json:"object_type"`
	Mode           string  `json:"mode"`
	Filename       string  `json:"filename"`
	ContentType    string  `json:"content_type"`
	Status         string  `json:"status"`
	TotalRows      int     `json:"total_rows"`
	SuccessfulRows int     `json:"successful_rows"`
	FailedRows     int     `json:"failed_rows"`
	AttemptCount   int     `json:"attempt_count"`
	ErrorMessage   *string `json:"error_message,omitempty"`
}

func AdaptUploadLog(log ingestion.UploadLog) UploadLogResponse {
	return UploadLogResponse{
		ID:             log.ID,
		TenantID:       log.TenantID,
		ObjectType:     log.ObjectType,
		Mode:           string(log.Mode),
		Filename:       log.Filename,
		ContentType:    log.ContentType,
		Status:         string(log.Status),
		TotalRows:      log.TotalRows,
		SuccessfulRows: log.SuccessfulRows,
		FailedRows:     log.FailedRows,
		AttemptCount:   log.AttemptCount,
		ErrorMessage:   log.ErrorMessage,
	}
}
