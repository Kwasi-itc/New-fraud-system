package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/service"
)

type DatasetUpdateHandler struct {
	service service.ScreeningService
}

func NewDatasetUpdateHandler(service service.ScreeningService) DatasetUpdateHandler {
	return DatasetUpdateHandler{service: service}
}

type createDatasetUpdateJobRequest struct {
	Provider string `json:"provider"`
	JobType  string `json:"job_type"`
	Cursor   string `json:"cursor"`
}

func (h DatasetUpdateHandler) Create(c *gin.Context) {
	var body createDatasetUpdateJobRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	item, err := h.service.CreateDatasetUpdateJob(c.Request.Context(), c.Param("tenantId"), body.Provider, body.JobType, body.Cursor)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, item)
}

func (h DatasetUpdateHandler) List(c *gin.Context) {
	items, err := h.service.ListDatasetUpdateJobs(c.Request.Context(), c.Param("tenantId"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, items)
}

func (h DatasetUpdateHandler) Get(c *gin.Context) {
	item, err := h.service.GetDatasetUpdateJob(c.Request.Context(), c.Param("tenantId"), c.Param("jobId"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, item)
}

func (h DatasetUpdateHandler) Retry(c *gin.Context) {
	item, err := h.service.RetryDatasetUpdateJob(c.Request.Context(), c.Param("tenantId"), c.Param("jobId"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, item)
}
