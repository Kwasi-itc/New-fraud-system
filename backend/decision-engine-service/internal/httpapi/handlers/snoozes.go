package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/httpapi/dto"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/service"
)

type SnoozeHandler struct {
	snoozeService service.SnoozeService
}

func NewSnoozeHandler(snoozeService service.SnoozeService) SnoozeHandler {
	return SnoozeHandler{snoozeService: snoozeService}
}

func (h SnoozeHandler) Create(c *gin.Context) {
	var req dto.CreateRuleSnoozeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "details": err.Error()})
		return
	}
	tenantID := c.Param("tenantId")
	scenarioID := c.Param("scenarioId")
	item, err := h.snoozeService.Create(c.Request.Context(), tenantID, scenarioID, req.ObjectType, req.ObjectID, req.SnoozeGroupID, req.ExpiresAt)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "create_rule_snooze_failed", "details": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"rule_snooze": dto.AdaptRuleSnooze(item)})
}

func (h SnoozeHandler) ListActive(c *gin.Context) {
	tenantID := c.Param("tenantId")
	scenarioID := c.Param("scenarioId")
	objectType := c.Query("object_type")
	objectID := c.Query("object_id")

	items, err := h.snoozeService.ListActive(c.Request.Context(), tenantID, scenarioID, objectType, objectID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "list_rule_snoozes_failed", "details": err.Error()})
		return
	}
	out := make([]dto.RuleSnoozeResponse, len(items))
	for i, item := range items {
		out[i] = dto.AdaptRuleSnooze(item)
	}
	c.JSON(http.StatusOK, gin.H{"rule_snoozes": out})
}
