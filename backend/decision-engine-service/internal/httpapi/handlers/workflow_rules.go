package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/httpapi/dto"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/service"
)

type WorkflowRuleHandler struct {
	service service.WorkflowRuleService
}

func NewWorkflowRuleHandler(service service.WorkflowRuleService) WorkflowRuleHandler {
	return WorkflowRuleHandler{service: service}
}

func (h WorkflowRuleHandler) ListByScenario(c *gin.Context) {
	items, err := h.service.ListByScenario(c.Request.Context(), c.Param("tenantId"), c.Param("scenarioId"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "list_workflow_rules_failed", "details": err.Error()})
		return
	}
	out := make([]dto.WorkflowRuleResponse, len(items))
	for i, item := range items {
		out[i] = dto.AdaptStructuredWorkflow(item)
	}
	c.JSON(http.StatusOK, gin.H{"workflow_rules": out})
}

func (h WorkflowRuleHandler) Get(c *gin.Context) {
	item, err := h.service.GetByID(c.Request.Context(), c.Param("tenantId"), c.Param("scenarioId"), c.Param("ruleId"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "get_workflow_rule_failed", "details": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"workflow_rule": dto.AdaptStructuredWorkflow(item)})
}

func (h WorkflowRuleHandler) Create(c *gin.Context) {
	var req dto.CreateWorkflowRuleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "details": err.Error()})
		return
	}
	item, err := h.service.CreateRule(c.Request.Context(), c.Param("tenantId"), c.Param("scenarioId"), req.Name, req.Fallthrough)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "create_workflow_rule_failed", "details": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"workflow_rule": dto.AdaptStructuredWorkflow(item)})
}

func (h WorkflowRuleHandler) Update(c *gin.Context) {
	var req dto.UpdateWorkflowRuleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "details": err.Error()})
		return
	}
	item, err := h.service.UpdateRule(c.Request.Context(), c.Param("tenantId"), c.Param("scenarioId"), c.Param("ruleId"), req.Name, req.Fallthrough)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "update_workflow_rule_failed", "details": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"workflow_rule": dto.AdaptStructuredWorkflow(item)})
}

func (h WorkflowRuleHandler) Delete(c *gin.Context) {
	if err := h.service.DeleteRule(c.Request.Context(), c.Param("tenantId"), c.Param("scenarioId"), c.Param("ruleId")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "delete_workflow_rule_failed", "details": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

func (h WorkflowRuleHandler) Reorder(c *gin.Context) {
	var ids []string
	if err := c.ShouldBindJSON(&ids); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "details": err.Error()})
		return
	}
	if err := h.service.ReorderRules(c.Request.Context(), c.Param("tenantId"), c.Param("scenarioId"), ids); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "reorder_workflow_rules_failed", "details": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

func (h WorkflowRuleHandler) CreateCondition(c *gin.Context) {
	var req dto.WorkflowConditionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "details": err.Error()})
		return
	}
	item, err := h.service.CreateCondition(c.Request.Context(), c.Param("tenantId"), c.Param("scenarioId"), c.Param("ruleId"), req.Function, req.Params)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "create_workflow_condition_failed", "details": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"workflow_condition": dto.AdaptWorkflowConditionRecord(item)})
}

func (h WorkflowRuleHandler) UpdateCondition(c *gin.Context) {
	var req dto.WorkflowConditionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "details": err.Error()})
		return
	}
	item, err := h.service.UpdateCondition(c.Request.Context(), c.Param("tenantId"), c.Param("scenarioId"), c.Param("ruleId"), c.Param("conditionId"), req.Function, req.Params)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "update_workflow_condition_failed", "details": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"workflow_condition": dto.AdaptWorkflowConditionRecord(item)})
}

func (h WorkflowRuleHandler) DeleteCondition(c *gin.Context) {
	if err := h.service.DeleteCondition(c.Request.Context(), c.Param("tenantId"), c.Param("ruleId"), c.Param("conditionId")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "delete_workflow_condition_failed", "details": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

func (h WorkflowRuleHandler) CreateAction(c *gin.Context) {
	var req dto.WorkflowActionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "details": err.Error()})
		return
	}
	item, err := h.service.CreateAction(c.Request.Context(), c.Param("tenantId"), c.Param("scenarioId"), c.Param("ruleId"), req.ActionType, req.ActionConfig)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "create_workflow_action_failed", "details": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"workflow_action": dto.AdaptWorkflowActionRecord(item)})
}

func (h WorkflowRuleHandler) UpdateAction(c *gin.Context) {
	var req dto.WorkflowActionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "details": err.Error()})
		return
	}
	item, err := h.service.UpdateAction(c.Request.Context(), c.Param("tenantId"), c.Param("ruleId"), c.Param("actionId"), req.ActionType, req.ActionConfig)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "update_workflow_action_failed", "details": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"workflow_action": dto.AdaptWorkflowActionRecord(item)})
}

func (h WorkflowRuleHandler) DeleteAction(c *gin.Context) {
	if err := h.service.DeleteAction(c.Request.Context(), c.Param("tenantId"), c.Param("ruleId"), c.Param("actionId")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "delete_workflow_action_failed", "details": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}
