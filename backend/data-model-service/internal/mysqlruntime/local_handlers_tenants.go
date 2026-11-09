package mysqlruntime

import (
	"database/sql"
	"errors"
	"log/slog"
	"net/http"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/httpapi/dto"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type localTenantHandler struct{ operations *localTenantOperations }

func (h *localTenantHandler) Register(r *gin.Engine) {
	r.GET("/v1/tenants", h.List)
	r.POST("/v1/tenants", h.Create)
	r.GET("/v1/tenants/:tenantId", h.Get)
	r.POST("/v1/tenants/:tenantId/provision", h.Provision)
}
func (h *localTenantHandler) List(c *gin.Context) {
	records, err := h.operations.catalog.List(c.Request.Context())
	if err != nil {
		c.JSON(503, gin.H{"error": "tenant_catalog_unavailable"})
		return
	}
	result := make([]dto.TenantResponse, len(records))
	for i, record := range records {
		result[i] = dto.AdaptTenant(record)
	}
	c.JSON(200, gin.H{"tenants": result})
}
func (h *localTenantHandler) Create(c *gin.Context) {
	var input dto.CreateTenantRequest
	if c.ShouldBindJSON(&input) != nil {
		c.JSON(400, gin.H{"error": "invalid_request"})
		return
	}
	create := tenant.CreateInput{Name: input.Name, ExternalKey: input.ExternalKey}
	if err := tenant.ValidateCreate(create); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	record, err := h.operations.tenants.Create(c.Request.Context(), create)
	if err != nil {
		slog.ErrorContext(c.Request.Context(), "local tenant registration failed", "error", err)
		c.JSON(503, gin.H{"error": "registration_failed", "message": "Check tenant catalogue before retrying; commit outcome may be uncertain."})
		return
	}
	c.JSON(201, gin.H{"tenant": dto.AdaptTenant(record)})
}
func (h *localTenantHandler) Get(c *gin.Context) {
	id, err := uuid.Parse(c.Param("tenantId"))
	if err != nil || id == uuid.Nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "bad_parameter", "message": "invalid tenant id"}})
		return
	}
	ctx := c.Request.Context()
	record, err := h.operations.Get(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"code": "not_found", "message": "tenant not found"}})
		return
	}
	if err != nil {
		slog.ErrorContext(ctx, "local tenant lookup failed", "error", err)
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"code": "tenant_unavailable", "message": "tenant lookup unavailable"}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"tenant": dto.AdaptTenant(record)})
}
func (h *localTenantHandler) Provision(c *gin.Context) {
	id, err := uuid.Parse(c.Param("tenantId"))
	if err != nil || id == uuid.Nil {
		c.JSON(400, gin.H{"error": gin.H{"code": "bad_parameter", "message": "invalid tenant id"}})
		return
	}
	record, err := h.operations.Provision(c.Request.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		c.JSON(404, gin.H{"error": gin.H{"code": "not_found", "message": "tenant not found"}})
		return
	}
	if err != nil {
		slog.ErrorContext(c.Request.Context(), "local tenant provisioning failed", "error", err)
		c.JSON(503, gin.H{"error": gin.H{"code": "provisioning_unavailable", "message": "Provisioning could not be confirmed; retry this endpoint to recover."}})
		return
	}
	c.JSON(200, gin.H{"tenant": dto.AdaptTenant(record)})
}
