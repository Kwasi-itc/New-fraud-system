package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/httpapi/dto"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/service"
)

type WorkflowHandler struct {
	workflowService service.WorkflowService
}

func NewWorkflowHandler(workflowService service.WorkflowService) WorkflowHandler {
	return WorkflowHandler{workflowService: workflowService}
}

func (h WorkflowHandler) Create(c *gin.Context) {
	var req dto.CreateWorkflowRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "details": err.Error()})
		return
	}
	tenantID := c.Param("tenantId")
	scenarioID := c.Param("scenarioId")
	item, err := h.workflowService.Create(
		c.Request.Context(),
		tenantID,
		scenarioID,
		req.Name,
		req.Description,
		req.AllowedOutcomes,
		req.ActionType,
		req.ActionConfig,
		req.Active,
	)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "create_workflow_failed", "details": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"workflow": dto.AdaptWorkflow(item)})
}

func (h WorkflowHandler) Get(c *gin.Context) {
	tenantID := c.Param("tenantId")
	scenarioID := c.Param("scenarioId")
	workflowID := c.Param("workflowId")
	item, err := h.workflowService.GetByID(c.Request.Context(), tenantID, scenarioID, workflowID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "get_workflow_failed", "details": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"workflow": dto.AdaptWorkflow(item)})
}

func (h WorkflowHandler) ListByScenario(c *gin.Context) {
	tenantID := c.Param("tenantId")
	scenarioID := c.Param("scenarioId")
	items, err := h.workflowService.ListByScenario(c.Request.Context(), tenantID, scenarioID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "list_workflows_failed", "details": err.Error()})
		return
	}
	out := make([]dto.WorkflowResponse, len(items))
	for i, item := range items {
		out[i] = dto.AdaptWorkflow(item)
	}
	c.JSON(http.StatusOK, gin.H{"workflows": out})
}

func (h WorkflowHandler) Update(c *gin.Context) {
	var req dto.UpdateWorkflowRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "details": err.Error()})
		return
	}
	tenantID := c.Param("tenantId")
	scenarioID := c.Param("scenarioId")
	workflowID := c.Param("workflowId")
	item, err := h.workflowService.Update(
		c.Request.Context(),
		tenantID,
		scenarioID,
		workflowID,
		req.Name,
		req.Description,
		req.AllowedOutcomes,
		req.ActionType,
		req.ActionConfig,
		req.Active,
	)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "update_workflow_failed", "details": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"workflow": dto.AdaptWorkflow(item)})
}

func (h WorkflowHandler) Delete(c *gin.Context) {
	tenantID := c.Param("tenantId")
	scenarioID := c.Param("scenarioId")
	workflowID := c.Param("workflowId")
	if err := h.workflowService.Delete(c.Request.Context(), tenantID, scenarioID, workflowID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "delete_workflow_failed", "details": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

func (h WorkflowHandler) Reorder(c *gin.Context) {
	var orderedIDs []string
	if err := c.ShouldBindJSON(&orderedIDs); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "details": err.Error()})
		return
	}
	tenantID := c.Param("tenantId")
	scenarioID := c.Param("scenarioId")
	if err := h.workflowService.Reorder(c.Request.Context(), tenantID, scenarioID, orderedIDs); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "reorder_workflows_failed", "details": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

func (h WorkflowHandler) ListByDecision(c *gin.Context) {
	tenantID := c.Param("tenantId")
	decisionID := c.Param("decisionId")
	items, err := h.workflowService.ListExecutionsByDecision(c.Request.Context(), tenantID, decisionID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "list_workflow_executions_failed", "details": err.Error()})
		return
	}
	out := make([]dto.WorkflowExecutionResponse, len(items))
	for i, item := range items {
		out[i] = dto.AdaptWorkflowExecution(item)
	}
	c.JSON(http.StatusOK, gin.H{"workflow_executions": out})
}
