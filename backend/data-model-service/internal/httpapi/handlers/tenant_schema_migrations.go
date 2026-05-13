package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/httpapi/dto"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/service"
)

type TenantSchemaMigrationHandler struct {
	service service.TenantSchemaMigrationService
}

func NewTenantSchemaMigrationHandler(service service.TenantSchemaMigrationService) TenantSchemaMigrationHandler {
	return TenantSchemaMigrationHandler{service: service}
}

func (h TenantSchemaMigrationHandler) List(c *gin.Context) {
	tenantID, ok := parseUUIDParam(c, "tenantId")
	if !ok {
		return
	}
	migrations, err := h.service.List(c.Request.Context(), tenantID)
	if err != nil {
		writeError(c, err)
		return
	}
	response := make([]dto.TenantSchemaMigrationResponse, len(migrations))
	for i, migration := range migrations {
		response[i] = dto.AdaptTenantSchemaMigration(migration)
	}
	c.JSON(http.StatusOK, gin.H{"tenant_schema_migrations": response})
}
