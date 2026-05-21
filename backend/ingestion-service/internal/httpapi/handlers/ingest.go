package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/httpapi/dto"
	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/service"
)

type IngestHandler struct {
	ingestService service.IngestService
}

func NewIngestHandler(ingestService service.IngestService) IngestHandler {
	return IngestHandler{ingestService: ingestService}
}

func (h IngestHandler) PostIngest(c *gin.Context) {
	h.ingest(c, ingestion.ModeCreate)
}

func (h IngestHandler) PatchIngest(c *gin.Context) {
	h.ingest(c, ingestion.ModePatch)
}

func (h IngestHandler) PostBatchIngest(c *gin.Context) {
	h.batchIngest(c, ingestion.ModeCreate)
}

func (h IngestHandler) PatchBatchIngest(c *gin.Context) {
	h.batchIngest(c, ingestion.ModePatch)
}

func (h IngestHandler) ingest(c *gin.Context, mode ingestion.Mode) {
	tenantID, err := uuid.Parse(c.Param("tenantId"))
	if err != nil {
		writeBadRequest(c, "invalid tenantId")
		return
	}

	var payload map[string]any
	if err := c.ShouldBindJSON(&payload); err != nil {
		writeBadRequest(c, "request body must be a JSON object")
		return
	}

	result, validationErrors, err := h.ingestService.Ingest(c.Request.Context(), service.IngestInput{
		TenantID:       tenantID,
		ObjectType:     c.Param("objectType"),
		Mode:           mode,
		Payload:        payload,
		IdempotencyKey: optionalHeader(c.GetHeader("Idempotency-Key")),
	})
	if err != nil {
		writeServiceError(c, err)
		return
	}
	if len(validationErrors) > 0 {
		c.JSON(http.StatusUnprocessableEntity, gin.H{
			"error": gin.H{
				"code":    "validation_failed",
				"message": "payload validation failed",
			},
			"validation_errors": dto.AdaptValidationErrors(validationErrors),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"result": dto.AdaptIngestResult(result),
	})
}

func (h IngestHandler) batchIngest(c *gin.Context, mode ingestion.Mode) {
	tenantID, err := uuid.Parse(c.Param("tenantId"))
	if err != nil {
		writeBadRequest(c, "invalid tenantId")
		return
	}

	var records []map[string]any
	decoder := json.NewDecoder(c.Request.Body)
	decoder.UseNumber()
	if err := decoder.Decode(&records); err != nil {
		writeBadRequest(c, "request body must be a JSON array of records")
		return
	}

	results, validationErrors, err := h.ingestService.BatchIngest(c.Request.Context(), service.BatchIngestInput{
		TenantID:       tenantID,
		ObjectType:     c.Param("objectType"),
		Mode:           mode,
		Records:        records,
		IdempotencyKey: optionalHeader(c.GetHeader("Idempotency-Key")),
	})
	if err != nil {
		writeServiceError(c, err)
		return
	}
	if len(validationErrors) > 0 {
		c.JSON(http.StatusUnprocessableEntity, gin.H{
			"error": gin.H{
				"code":    "validation_failed",
				"message": "batch validation failed",
			},
			"validation_errors": dto.AdaptValidationErrors(validationErrors),
		})
		return
	}

	response := make([]dto.IngestResultResponse, len(results))
	for i, result := range results {
		response[i] = dto.AdaptIngestResult(result)
	}
	c.JSON(http.StatusOK, gin.H{
		"results": response,
	})
}

func optionalHeader(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func writeBadRequest(c *gin.Context, message string) {
	c.JSON(http.StatusBadRequest, gin.H{
		"error": gin.H{
			"code":    "bad_parameter",
			"message": message,
		},
	})
}

func writeServiceError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrIdempotencyKeyReused):
		c.JSON(http.StatusConflict, gin.H{
			"error": gin.H{
				"code":    "idempotency_key_reused",
				"message": err.Error(),
			},
		})
	default:
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"code":    "bad_parameter",
				"message": err.Error(),
			},
		})
	}
}
