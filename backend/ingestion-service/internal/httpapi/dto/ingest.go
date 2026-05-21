package dto

import "github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"

type IngestResultResponse struct {
	ObjectID   string `json:"object_id"`
	Action     string `json:"action"`
	RevisionID string `json:"revision_id"`
	Replayed   bool   `json:"replayed"`
}

type ValidationErrorResponse struct {
	ObjectID string `json:"object_id,omitempty"`
	Field    string `json:"field"`
	Code     string `json:"code"`
	Message  string `json:"message"`
}

func AdaptIngestResult(result ingestion.RecordResult) IngestResultResponse {
	return IngestResultResponse{
		ObjectID:   result.ObjectID,
		Action:     result.Action,
		RevisionID: result.RevisionID,
		Replayed:   result.Replayed,
	}
}

func AdaptValidationErrors(errors []ingestion.ValidationError) []ValidationErrorResponse {
	response := make([]ValidationErrorResponse, len(errors))
	for i, err := range errors {
		response[i] = ValidationErrorResponse{
			ObjectID: err.ObjectID,
			Field:    err.Field,
			Code:     err.Code,
			Message:  err.Message,
		}
	}
	return response
}
