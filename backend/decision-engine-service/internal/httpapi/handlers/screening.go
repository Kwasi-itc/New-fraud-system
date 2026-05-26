package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/httpapi/dto"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/service"
)

type ScreeningHandler struct {
	screeningService service.ScreeningService
}

func NewScreeningHandler(screeningService service.ScreeningService) ScreeningHandler {
	return ScreeningHandler{screeningService: screeningService}
}

func (h ScreeningHandler) CreateConfig(c *gin.Context) {
	var req dto.CreateScreeningConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "details": err.Error()})
		return
	}
	tenantID := c.Param("tenantId")
	scenarioID := c.Param("scenarioId")
	item, err := h.screeningService.CreateConfig(c.Request.Context(), tenantID, scenarioID, req.Name, req.AllowedOutcomes, req.Provider, req.ConfigJSON, req.Active)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "create_screening_config_failed", "details": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"screening_config": dto.AdaptScreeningConfig(item)})
}

func (h ScreeningHandler) GetConfig(c *gin.Context) {
	tenantID := c.Param("tenantId")
	scenarioID := c.Param("scenarioId")
	configID := c.Param("configId")
	item, err := h.screeningService.GetConfig(c.Request.Context(), tenantID, scenarioID, configID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "get_screening_config_failed", "details": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"screening_config": dto.AdaptScreeningConfig(item)})
}

func (h ScreeningHandler) ListConfigsByScenario(c *gin.Context) {
	tenantID := c.Param("tenantId")
	scenarioID := c.Param("scenarioId")
	items, err := h.screeningService.ListConfigsByScenario(c.Request.Context(), tenantID, scenarioID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "list_screening_configs_failed", "details": err.Error()})
		return
	}
	out := make([]dto.ScreeningConfigResponse, len(items))
	for i, item := range items {
		out[i] = dto.AdaptScreeningConfig(item)
	}
	c.JSON(http.StatusOK, gin.H{"screening_configs": out})
}

func (h ScreeningHandler) UpdateConfig(c *gin.Context) {
	var req dto.UpdateScreeningConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "details": err.Error()})
		return
	}
	tenantID := c.Param("tenantId")
	scenarioID := c.Param("scenarioId")
	configID := c.Param("configId")
	item, err := h.screeningService.UpdateConfig(c.Request.Context(), tenantID, scenarioID, configID, req.Name, req.AllowedOutcomes, req.Provider, req.ConfigJSON, req.Active)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "update_screening_config_failed", "details": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"screening_config": dto.AdaptScreeningConfig(item)})
}

func (h ScreeningHandler) DeleteConfig(c *gin.Context) {
	tenantID := c.Param("tenantId")
	scenarioID := c.Param("scenarioId")
	configID := c.Param("configId")
	if err := h.screeningService.DeleteConfig(c.Request.Context(), tenantID, scenarioID, configID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "delete_screening_config_failed", "details": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

func (h ScreeningHandler) ListExecutionsByDecision(c *gin.Context) {
	tenantID := c.Param("tenantId")
	decisionID := c.Param("decisionId")
	items, err := h.screeningService.ListExecutionsByDecision(c.Request.Context(), tenantID, decisionID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "list_screening_executions_failed", "details": err.Error()})
		return
	}
	out := make([]dto.ScreeningExecutionResponse, len(items))
	for i, item := range items {
		out[i] = dto.AdaptScreeningExecution(item)
	}
	c.JSON(http.StatusOK, gin.H{"screening_executions": out})
}

func (h ScreeningHandler) GetExecution(c *gin.Context) {
	tenantID := c.Param("tenantId")
	executionID := c.Param("executionId")
	item, err := h.screeningService.GetExecution(c.Request.Context(), tenantID, executionID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "get_screening_execution_failed", "details": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"screening_execution": dto.AdaptScreeningExecution(item)})
}

func (h ScreeningHandler) UpdateExecutionStatus(c *gin.Context) {
	var req dto.UpdateScreeningExecutionStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "details": err.Error()})
		return
	}
	tenantID := c.Param("tenantId")
	executionID := c.Param("executionId")
	item, err := h.screeningService.UpdateExecutionStatus(c.Request.Context(), tenantID, executionID, req.Status, req.ProviderReference, req.ResponseJSON, req.LastError)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "update_screening_execution_status_failed", "details": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"screening_execution": dto.AdaptScreeningExecution(item)})
}

func (h ScreeningHandler) RetryExecution(c *gin.Context) {
	tenantID := c.Param("tenantId")
	executionID := c.Param("executionId")
	item, err := h.screeningService.RetryExecution(c.Request.Context(), tenantID, executionID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "retry_screening_execution_failed", "details": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"screening_execution": dto.AdaptScreeningExecution(item)})
}
