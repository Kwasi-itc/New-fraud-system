package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/httpapi/dto"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/service"
)

type ScoringHandler struct {
	scoringService service.ScoringService
}

func NewScoringHandler(scoringService service.ScoringService) ScoringHandler {
	return ScoringHandler{scoringService: scoringService}
}

func (h ScoringHandler) CreateConfig(c *gin.Context) {
	var req dto.CreateScoringConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "details": err.Error()})
		return
	}
	tenantID := c.Param("tenantId")
	scenarioID := c.Param("scenarioId")
	item, err := h.scoringService.CreateConfig(c.Request.Context(), tenantID, scenarioID, req.Name, req.AllowedOutcomes, req.RulesetRef, req.ConfigJSON, req.Active)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "create_scoring_config_failed", "details": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"scoring_config": dto.AdaptScoringConfig(item)})
}

func (h ScoringHandler) GetConfig(c *gin.Context) {
	tenantID := c.Param("tenantId")
	scenarioID := c.Param("scenarioId")
	configID := c.Param("configId")
	item, err := h.scoringService.GetConfig(c.Request.Context(), tenantID, scenarioID, configID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "get_scoring_config_failed", "details": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"scoring_config": dto.AdaptScoringConfig(item)})
}

func (h ScoringHandler) ListConfigsByScenario(c *gin.Context) {
	tenantID := c.Param("tenantId")
	scenarioID := c.Param("scenarioId")
	items, err := h.scoringService.ListConfigsByScenario(c.Request.Context(), tenantID, scenarioID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "list_scoring_configs_failed", "details": err.Error()})
		return
	}
	out := make([]dto.ScoringConfigResponse, len(items))
	for i, item := range items {
		out[i] = dto.AdaptScoringConfig(item)
	}
	c.JSON(http.StatusOK, gin.H{"scoring_configs": out})
}

func (h ScoringHandler) UpdateConfig(c *gin.Context) {
	var req dto.UpdateScoringConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "details": err.Error()})
		return
	}
	tenantID := c.Param("tenantId")
	scenarioID := c.Param("scenarioId")
	configID := c.Param("configId")
	item, err := h.scoringService.UpdateConfig(c.Request.Context(), tenantID, scenarioID, configID, req.Name, req.AllowedOutcomes, req.RulesetRef, req.ConfigJSON, req.Active)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "update_scoring_config_failed", "details": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"scoring_config": dto.AdaptScoringConfig(item)})
}

func (h ScoringHandler) DeleteConfig(c *gin.Context) {
	tenantID := c.Param("tenantId")
	scenarioID := c.Param("scenarioId")
	configID := c.Param("configId")
	if err := h.scoringService.DeleteConfig(c.Request.Context(), tenantID, scenarioID, configID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "delete_scoring_config_failed", "details": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

func (h ScoringHandler) ListRequestsByDecision(c *gin.Context) {
	tenantID := c.Param("tenantId")
	decisionID := c.Param("decisionId")
	items, err := h.scoringService.ListRequestsByDecision(c.Request.Context(), tenantID, decisionID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "list_scoring_requests_failed", "details": err.Error()})
		return
	}
	out := make([]dto.ScoringRequestResponse, len(items))
	for i, item := range items {
		out[i] = dto.AdaptScoringRequest(item)
	}
	c.JSON(http.StatusOK, gin.H{"scoring_requests": out})
}

func (h ScoringHandler) GetRequest(c *gin.Context) {
	tenantID := c.Param("tenantId")
	requestID := c.Param("requestId")
	item, err := h.scoringService.GetRequest(c.Request.Context(), tenantID, requestID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "get_scoring_request_failed", "details": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"scoring_request": dto.AdaptScoringRequest(item)})
}

func (h ScoringHandler) UpdateRequestStatus(c *gin.Context) {
	var req dto.UpdateScoringRequestStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "details": err.Error()})
		return
	}
	tenantID := c.Param("tenantId")
	requestID := c.Param("requestId")
	item, err := h.scoringService.UpdateRequestStatus(c.Request.Context(), tenantID, requestID, req.Status, req.ProviderReference, req.ResponseJSON, req.LastError)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "update_scoring_request_status_failed", "details": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"scoring_request": dto.AdaptScoringRequest(item)})
}

func (h ScoringHandler) RetryRequest(c *gin.Context) {
	tenantID := c.Param("tenantId")
	requestID := c.Param("requestId")
	item, err := h.scoringService.RetryRequest(c.Request.Context(), tenantID, requestID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "retry_scoring_request_failed", "details": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"scoring_request": dto.AdaptScoringRequest(item)})
}
