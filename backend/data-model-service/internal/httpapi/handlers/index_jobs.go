package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/httpapi/dto"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/service"
)

type IndexJobHandler struct {
	service service.IndexJobService
}

func NewIndexJobHandler(service service.IndexJobService) IndexJobHandler {
	return IndexJobHandler{service: service}
}

func (h IndexJobHandler) Create(c *gin.Context) {
	tenantID, ok := parseUUIDParam(c, "tenantId")
	if !ok {
		return
	}
	var request dto.CreateIndexJobRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		writeBadRequest(c, err.Error())
		return
	}
	indexType, err := datamodel.ParseIndexJobType(request.IndexType)
	if err != nil {
		writeBadRequest(c, err.Error())
		return
	}
	job, err := h.service.Create(c.Request.Context(), service.CreateIndexJobInput{
		TenantID:             tenantID,
		TableID:              request.TableID,
		IndexType:            indexType,
		Columns:              request.Columns,
		RequestedByOperation: request.RequestedByOperation,
		ScheduledAt:          request.ScheduledAt,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"index_job": dto.AdaptIndexJob(job)})
}

func (h IndexJobHandler) List(c *gin.Context) {
	tenantID, ok := parseUUIDParam(c, "tenantId")
	if !ok {
		return
	}
	jobs, err := h.service.List(c.Request.Context(), tenantID)
	if err != nil {
		writeError(c, err)
		return
	}
	response := make([]dto.IndexJobResponse, len(jobs))
	for i, job := range jobs {
		response[i] = dto.AdaptIndexJob(job)
	}
	c.JSON(http.StatusOK, gin.H{"index_jobs": response})
}

func (h IndexJobHandler) Get(c *gin.Context) {
	jobID, ok := parseUUIDParam(c, "jobId")
	if !ok {
		return
	}
	job, err := h.service.Get(c.Request.Context(), jobID)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"index_job": dto.AdaptIndexJob(job)})
}

func (h IndexJobHandler) Retry(c *gin.Context) {
	jobID, ok := parseUUIDParam(c, "jobId")
	if !ok {
		return
	}
	job, err := h.service.Retry(c.Request.Context(), jobID)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"index_job": dto.AdaptIndexJob(job)})
}
