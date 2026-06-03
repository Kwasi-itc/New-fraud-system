package handlers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/httpapi/dto"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/service"
)

type InternalScreeningHandler struct {
	screeningService service.ScreeningService
}

type screeningStatusUpdateRequest struct {
	TenantID          string     `json:"tenant_id"`
	ScreeningID       string     `json:"screening_id"`
	DecisionID        string     `json:"decision_id"`
	ScenarioID        string     `json:"scenario_id"`
	ScreeningConfigID string     `json:"screening_config_id"`
	Status            string     `json:"status"`
	Provider          string     `json:"provider"`
	ObjectType        string     `json:"object_type"`
	ObjectID          string     `json:"object_id"`
	ProviderReference string     `json:"provider_reference"`
	LastError         string     `json:"last_error"`
	Partial           bool       `json:"partial"`
	IdempotencyKey    string     `json:"idempotency_key"`
	CompletedAt       *time.Time `json:"completed_at"`
	MatchCount        int        `json:"match_count"`
}

func NewInternalScreeningHandler(screeningService service.ScreeningService) InternalScreeningHandler {
	return InternalScreeningHandler{screeningService: screeningService}
}

func (h InternalScreeningHandler) UpdateStatus(c *gin.Context) {
	var req screeningStatusUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "details": err.Error()})
		return
	}

	tenantID := req.TenantID
	if tenantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tenant_id is required"})
		return
	}

	item, err := h.screeningService.UpdateExecutionStatusFromScreeningCallback(c.Request.Context(), tenantID, service.ScreeningStatusUpdate{
		ScreeningID:       req.ScreeningID,
		DecisionID:        req.DecisionID,
		ScenarioID:        req.ScenarioID,
		ScreeningConfigID: req.ScreeningConfigID,
		Status:            req.Status,
		Provider:          req.Provider,
		ObjectType:        req.ObjectType,
		ObjectID:          req.ObjectID,
		ProviderReference: req.ProviderReference,
		LastError:         req.LastError,
		Partial:           req.Partial,
		IdempotencyKey:    req.IdempotencyKey,
		CompletedAt:       req.CompletedAt,
		MatchCount:        req.MatchCount,
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "update_screening_execution_status_failed", "details": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"screening_execution": dto.AdaptScreeningExecution(item)})
}
