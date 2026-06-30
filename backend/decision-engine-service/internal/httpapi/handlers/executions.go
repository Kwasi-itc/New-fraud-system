package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/httpapi/dto"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/service"
)

type ExecutionHandler struct {
	executionService service.ExecutionService
}

func NewExecutionHandler(executionService service.ExecutionService) ExecutionHandler {
	return ExecutionHandler{executionService: executionService}
}

func (h ExecutionHandler) CreateScheduledExecution(c *gin.Context) {
	var req dto.CreateScheduledExecutionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "details": err.Error()})
		return
	}
	tenantID := c.Param("tenantId")
	scenarioID := c.Param("scenarioId")
	item, err := h.executionService.CreateScheduledExecution(c.Request.Context(), tenantID, scenarioID, req.ScheduledFor, dto.AdaptScheduledExecutionRequest(req))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "create_scheduled_execution_failed", "details": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"scheduled_execution": dto.AdaptScheduledExecution(item)})
}

func (h ExecutionHandler) ListScheduledExecutionsByScenario(c *gin.Context) {
	tenantID := c.Param("tenantId")
	scenarioID := c.Param("scenarioId")
	items, err := h.executionService.ListScheduledExecutionsByScenario(c.Request.Context(), tenantID, scenarioID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "list_scheduled_executions_failed", "details": err.Error()})
		return
	}
	out := make([]dto.ScheduledExecutionResponse, len(items))
	for i, item := range items {
		out[i] = dto.AdaptScheduledExecution(item)
	}
	c.JSON(http.StatusOK, gin.H{"scheduled_executions": out})
}

func (h ExecutionHandler) GetScheduledExecution(c *gin.Context) {
	tenantID := c.Param("tenantId")
	scenarioID := c.Param("scenarioId")
	executionID := c.Param("executionId")
	item, err := h.executionService.GetScheduledExecutionByID(c.Request.Context(), tenantID, scenarioID, executionID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "scheduled_execution_not_found", "details": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"scheduled_execution": dto.AdaptScheduledExecution(item)})
}

func (h ExecutionHandler) CreateAsyncDecisionExecution(c *gin.Context) {
	var req dto.CreateAsyncDecisionExecutionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "details": err.Error()})
		return
	}
	tenantID := c.Param("tenantId")
	item, err := h.executionService.CreateAsyncDecisionExecution(c.Request.Context(), tenantID, dto.AdaptAsyncExecutionRequest(req))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "create_async_decision_execution_failed", "details": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"async_decision_execution": dto.AdaptAsyncDecisionExecution(item)})
}

func (h ExecutionHandler) ListAsyncDecisionExecutionsByTenant(c *gin.Context) {
	tenantID := c.Param("tenantId")
	items, err := h.executionService.ListAsyncDecisionExecutionsByTenant(c.Request.Context(), tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "list_async_decision_executions_failed", "details": err.Error()})
		return
	}
	out := make([]dto.AsyncDecisionExecutionResponse, len(items))
	for i, item := range items {
		out[i] = dto.AdaptAsyncDecisionExecution(item)
	}
	c.JSON(http.StatusOK, gin.H{"async_decision_executions": out})
}

func (h ExecutionHandler) GetRecurringSchedule(c *gin.Context) {
	tenantID := c.Param("tenantId")
	scenarioID := c.Param("scenarioId")
	item, err := h.executionService.GetRecurringSchedule(c.Request.Context(), tenantID, scenarioID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "get_recurring_schedule_failed", "details": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"recurring_schedule": dto.AdaptRecurringSchedule(item)})
}

func (h ExecutionHandler) UpdateRecurringSchedule(c *gin.Context) {
	var req dto.RecurringScheduleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "details": err.Error()})
		return
	}
	tenantID := c.Param("tenantId")
	scenarioID := c.Param("scenarioId")
	item, err := h.executionService.UpdateRecurringSchedule(c.Request.Context(), tenantID, scenarioID, dto.AdaptRecurringScheduleRequest(req))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "update_recurring_schedule_failed", "details": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"recurring_schedule": dto.AdaptRecurringSchedule(item)})
}
