package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/httpapi/dto"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/service"
)

type AccessorHandler struct {
	accessorService service.AccessorService
}

func NewAccessorHandler(accessorService service.AccessorService) AccessorHandler {
	return AccessorHandler{accessorService: accessorService}
}

func (h AccessorHandler) ListByScenario(c *gin.Context) {
	tenantID := c.Param("tenantId")
	scenarioID := c.Param("scenarioId")

	result, err := h.accessorService.ListByScenario(c.Request.Context(), tenantID, scenarioID)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "list_accessors_failed", "details": err.Error()})
		return
	}

	payloadAccessors := make([]dto.NodeResponse, len(result.PayloadAccessors))
	for i, item := range result.PayloadAccessors {
		payloadAccessors[i] = dto.AdaptNode(item)
	}
	databaseAccessors := make([]dto.NodeResponse, len(result.DatabaseAccessors))
	for i, item := range result.DatabaseAccessors {
		databaseAccessors[i] = dto.AdaptNode(item)
	}

	c.JSON(http.StatusOK, gin.H{
		"payload_accessors":  payloadAccessors,
		"database_accessors": databaseAccessors,
	})
}
