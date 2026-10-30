package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/httpapi/dto"
)

type DeferredIngestHandler struct {
	service DeferredService
}

type DeferredIngestMetricsHandler struct {
	provider func(*gin.Context) (any, error)
}

func NewDeferredIngestHandler(service DeferredService) DeferredIngestHandler {
	return DeferredIngestHandler{service: service}
}

func NewDeferredIngestMetricsHandler(provider func(*gin.Context) (any, error)) DeferredIngestMetricsHandler {
	return DeferredIngestMetricsHandler{provider: provider}
}

func (h DeferredIngestHandler) Get(c *gin.Context) {
	id, err := uuid.Parse(c.Param("deferredIngestId"))
	if err != nil {
		writeBadRequest(c, "invalid deferredIngestId")
		return
	}
	execution, err := h.service.Get(c.Request.Context(), id)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"deferred_ingest": dto.AdaptDeferredIngest(execution)})
}

func (h DeferredIngestMetricsHandler) Get(c *gin.Context) {
	snapshot, err := h.provider(c)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"deferred_ingest_metrics": snapshot})
}
