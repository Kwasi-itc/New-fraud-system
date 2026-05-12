package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Kwasi-itc/marble-datamodel-service/internal/httpapi/dto"
	"github.com/Kwasi-itc/marble-datamodel-service/internal/service"
)

type SchemaChangeHandler struct {
	service service.SchemaChangeService
}

func NewSchemaChangeHandler(service service.SchemaChangeService) SchemaChangeHandler {
	return SchemaChangeHandler{service: service}
}

func (h SchemaChangeHandler) List(c *gin.Context) {
	tenantID, ok := parseUUIDParam(c, "tenantId")
	if !ok {
		return
	}
	changes, err := h.service.List(c.Request.Context(), tenantID)
	if err != nil {
		writeError(c, err)
		return
	}
	response := make([]dto.SchemaChangeResponse, len(changes))
	for i, change := range changes {
		response[i] = dto.AdaptSchemaChange(change)
	}
	c.JSON(http.StatusOK, gin.H{"schema_changes": response})
}

