package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/httpapi/dto"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/service"
)

type EventSchemaHandler struct {
	service service.EventSchemaService
}

func NewEventSchemaHandler(service service.EventSchemaService) EventSchemaHandler {
	return EventSchemaHandler{service: service}
}

func (h EventSchemaHandler) Lock(c *gin.Context) {
	tenantID, ok := parseUUIDParam(c, "tenantId")
	if !ok {
		return
	}
	tableID, ok := parseUUIDParam(c, "tableId")
	if !ok {
		return
	}
	var request struct {
		SchemaRevision string `json:"schema_revision" binding:"required"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		writeBadRequest(c, err.Error())
		return
	}
	table, err := h.service.Lock(c.Request.Context(), tenantID, tableID, request.SchemaRevision)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"table": dto.AdaptTable(table)})
}
