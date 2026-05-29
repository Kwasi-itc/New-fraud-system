package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/service"
)

type ProviderHandler struct {
	service service.ScreeningService
}

func NewProviderHandler(service service.ScreeningService) ProviderHandler {
	return ProviderHandler{service: service}
}

func (h ProviderHandler) GetCatalog(c *gin.Context) {
	item, err := h.service.GetDatasetCatalog(c.Request.Context(), c.Query("provider"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.Data(http.StatusOK, "application/json", item.RawPayload)
}

func (h ProviderHandler) GetFreshness(c *gin.Context) {
	item, err := h.service.GetDatasetFreshness(c.Request.Context(), c.Query("provider"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.Data(http.StatusOK, "application/json", item.RawPayload)
}
