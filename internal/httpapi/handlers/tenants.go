package handlers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Kwasi-itc/marble-datamodel-service/internal/domain/tenant"
	"github.com/Kwasi-itc/marble-datamodel-service/internal/httpapi/dto"
	"github.com/Kwasi-itc/marble-datamodel-service/internal/service"
)

type TenantHandler struct {
	service service.TenantService
}

func NewTenantHandler(service service.TenantService) TenantHandler {
	return TenantHandler{service: service}
}

func (h TenantHandler) Create(c *gin.Context) {
	var request dto.CreateTenantRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"code":    "bad_parameter",
				"message": err.Error(),
			},
		})
		return
	}

	record, err := h.service.Create(c.Request.Context(), tenant.CreateInput{
		Name:        request.Name,
		ExternalKey: request.ExternalKey,
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"code":    "bad_parameter",
				"message": err.Error(),
			},
		})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"tenant": dto.AdaptTenant(record),
	})
}

func (h TenantHandler) Get(c *gin.Context) {
	id, err := uuid.Parse(c.Param("tenantId"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"code":    "bad_parameter",
				"message": "invalid tenant id",
			},
		})
		return
	}

	record, err := h.service.Get(c.Request.Context(), id)
	if err != nil {
		c.JSON(statusFromError(err), gin.H{
			"error": gin.H{
				"code":    codeFromError(err),
				"message": err.Error(),
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"tenant": dto.AdaptTenant(record),
	})
}

func (h TenantHandler) List(c *gin.Context) {
	records, err := h.service.List(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{
				"code":    "internal_error",
				"message": err.Error(),
			},
		})
		return
	}

	response := make([]dto.TenantResponse, len(records))
	for i, record := range records {
		response[i] = dto.AdaptTenant(record)
	}

	c.JSON(http.StatusOK, gin.H{
		"tenants": response,
	})
}

func (h TenantHandler) Provision(c *gin.Context) {
	id, err := uuid.Parse(c.Param("tenantId"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"code":    "bad_parameter",
				"message": "invalid tenant id",
			},
		})
		return
	}

	record, err := h.service.Provision(c.Request.Context(), id)
	if err != nil {
		c.JSON(statusFromError(err), gin.H{
			"error": gin.H{
				"code":    codeFromError(err),
				"message": err.Error(),
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"tenant": dto.AdaptTenant(record),
	})
}

func statusFromError(err error) int {
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return http.StatusNotFound
	default:
		return http.StatusInternalServerError
	}
}

func codeFromError(err error) string {
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return "not_found"
	default:
		return "internal_error"
	}
}
