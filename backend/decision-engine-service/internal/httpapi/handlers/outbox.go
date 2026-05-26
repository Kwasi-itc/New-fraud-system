package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/httpapi/dto"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/service"
)

type OutboxHandler struct {
	outboxService service.OutboxService
}

func NewOutboxHandler(outboxService service.OutboxService) OutboxHandler {
	return OutboxHandler{outboxService: outboxService}
}

func (h OutboxHandler) ListByTenant(c *gin.Context) {
	tenantID := c.Param("tenantId")
	limit := 50
	if raw := c.Query("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_limit", "details": err.Error()})
			return
		}
		limit = parsed
	}
	items, err := h.outboxService.ListByTenant(c.Request.Context(), tenantID, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "list_outbox_events_failed", "details": err.Error()})
		return
	}
	out := make([]dto.OutboxEventResponse, len(items))
	for i, item := range items {
		out[i] = dto.AdaptOutboxEvent(item)
	}
	c.JSON(http.StatusOK, gin.H{"outbox_events": out})
}
