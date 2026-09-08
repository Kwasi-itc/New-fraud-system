package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/ports"
)

type AggregateFactBackfillHandler struct {
	backfiller ports.AggregateFactBackfiller
}

func NewAggregateFactBackfillHandler(backfiller ports.AggregateFactBackfiller) AggregateFactBackfillHandler {
	return AggregateFactBackfillHandler{backfiller: backfiller}
}

func (h AggregateFactBackfillHandler) Status(c *gin.Context) {
	tenantID, ok := aggregateFactTenantID(c)
	if !ok {
		return
	}
	if h.backfiller == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"code": "aggregate_fact_unavailable", "message": "aggregate fact backfill is not configured"}})
		return
	}
	status, err := h.backfiller.Status(c.Request.Context(), tenantID)
	if err != nil {
		writeServiceError(c, err, "tenant_id", tenantID.String())
		return
	}
	c.JSON(http.StatusOK, gin.H{"aggregate_fact_backfill": status})
}

func (h AggregateFactBackfillHandler) Backfill(c *gin.Context) {
	tenantID, ok := aggregateFactTenantID(c)
	if !ok {
		return
	}
	if h.backfiller == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"code": "aggregate_fact_unavailable", "message": "aggregate fact backfill is not configured"}})
		return
	}
	force := false
	if raw := c.Query("force"); raw != "" {
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			writeBadRequest(c, "force must be true or false", "tenant_id", tenantID.String())
			return
		}
		force = parsed
	}
	result, err := h.backfiller.Backfill(c.Request.Context(), tenantID, force)
	if err != nil {
		writeServiceError(c, err, "tenant_id", tenantID.String())
		return
	}
	c.JSON(http.StatusOK, gin.H{"aggregate_fact_backfill": result})
}

func aggregateFactTenantID(c *gin.Context) (uuid.UUID, bool) {
	tenantID, err := uuid.Parse(c.Param("tenantId"))
	if err != nil {
		writeBadRequest(c, "invalid tenantId", "tenant_id", c.Param("tenantId"))
		return uuid.Nil, false
	}
	return tenantID, true
}
