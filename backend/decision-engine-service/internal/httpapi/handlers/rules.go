package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/httpapi/dto"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/service"
)

type RuleHandler struct {
	ruleService service.RuleService
}

func NewRuleHandler(ruleService service.RuleService) RuleHandler {
	return RuleHandler{ruleService: ruleService}
}

func (h RuleHandler) ListRules(c *gin.Context) {
	tenantID := c.Param("tenantId")
	scenarioID := c.Param("scenarioId")
	iterationID := c.Param("iterationId")

	items, err := h.ruleService.ListByIteration(c.Request.Context(), tenantID, scenarioID, iterationID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "list_rules_failed", "details": err.Error()})
		return
	}
	out := make([]dto.RuleResponse, len(items))
	for i, item := range items {
		out[i] = dto.AdaptRule(item)
	}
	c.JSON(http.StatusOK, gin.H{"rules": out})
}

func (h RuleHandler) ListRuleGroups(c *gin.Context) {
	tenantID := c.Param("tenantId")
	scenarioID := c.Param("scenarioId")

	items, err := h.ruleService.ListRuleGroupsByScenario(c.Request.Context(), tenantID, scenarioID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "list_rule_groups_failed", "details": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"rule_groups": items})
}

func (h RuleHandler) CreateRule(c *gin.Context) {
	var req dto.CreateRuleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "details": err.Error()})
		return
	}

	tenantID := c.Param("tenantId")
	scenarioID := c.Param("scenarioId")
	iterationID := c.Param("iterationId")

	item, err := h.ruleService.Create(
		c.Request.Context(),
		tenantID,
		scenarioID,
		iterationID,
		req.DisplayOrder,
		req.Name,
		req.Description,
		req.Formula,
		req.ScoreModifier,
		req.RuleGroup,
		req.SnoozeGroupID,
		req.StableRuleID,
	)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "create_rule_failed", "details": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"rule": dto.AdaptRule(item)})
}

func (h RuleHandler) UpdateRule(c *gin.Context) {
	var req dto.UpdateRuleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "details": err.Error()})
		return
	}

	tenantID := c.Param("tenantId")
	scenarioID := c.Param("scenarioId")
	iterationID := c.Param("iterationId")
	ruleID := c.Param("ruleId")

	item, err := h.ruleService.Update(
		c.Request.Context(),
		tenantID,
		scenarioID,
		iterationID,
		ruleID,
		req.DisplayOrder,
		req.Name,
		req.Description,
		req.Formula,
		req.ScoreModifier,
		req.RuleGroup,
		req.SnoozeGroupID,
		req.StableRuleID,
	)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "update_rule_failed", "details": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"rule": dto.AdaptRule(item)})
}

func (h RuleHandler) DeleteRule(c *gin.Context) {
	tenantID := c.Param("tenantId")
	scenarioID := c.Param("scenarioId")
	iterationID := c.Param("iterationId")
	ruleID := c.Param("ruleId")

	if err := h.ruleService.Delete(c.Request.Context(), tenantID, scenarioID, iterationID, ruleID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "delete_rule_failed", "details": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}
