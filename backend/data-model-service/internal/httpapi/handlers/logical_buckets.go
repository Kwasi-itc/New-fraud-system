package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/httpapi/dto"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/service"
)

type LogicalBucketHandler struct {
	service service.LogicalBucketService
}

func NewLogicalBucketHandler(service service.LogicalBucketService) LogicalBucketHandler {
	return LogicalBucketHandler{service: service}
}

func (h LogicalBucketHandler) Create(c *gin.Context) {
	tenantID, ok := parseUUIDParam(c, "tenantId")
	if !ok {
		return
	}
	tableID, ok := parseUUIDParam(c, "tableId")
	if !ok {
		return
	}
	var request dto.CreateLogicalBucketRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		writeBadRequest(c, err.Error())
		return
	}
	item, err := h.service.Create(c.Request.Context(), service.CreateLogicalBucketInput{
		TenantID: tenantID, TableID: tableID,
		TimestampFieldID: request.TimestampFieldID, Timezone: request.Timezone,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"logical_bucket": dto.AdaptLogicalBucket(item)})
}

func (h LogicalBucketHandler) List(c *gin.Context) {
	tableID, ok := parseUUIDParam(c, "tableId")
	if !ok {
		return
	}
	items, err := h.service.ListByTable(c.Request.Context(), tableID)
	if err != nil {
		writeError(c, err)
		return
	}
	response := make([]dto.LogicalBucketResponse, len(items))
	for i, item := range items {
		response[i] = dto.AdaptLogicalBucket(item)
	}
	c.JSON(http.StatusOK, gin.H{"logical_buckets": response})
}

func (h LogicalBucketHandler) Get(c *gin.Context) {
	id, ok := parseUUIDParam(c, "logicalBucketId")
	if !ok {
		return
	}
	item, err := h.service.Get(c.Request.Context(), id)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"logical_bucket": dto.AdaptLogicalBucket(item)})
}

func (h LogicalBucketHandler) Retire(c *gin.Context) {
	id, ok := parseUUIDParam(c, "logicalBucketId")
	if !ok {
		return
	}
	item, err := h.service.Retire(c.Request.Context(), id)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"logical_bucket": dto.AdaptLogicalBucket(item)})
}

func (h LogicalBucketHandler) RetryActivation(c *gin.Context) {
	id, ok := parseUUIDParam(c, "logicalBucketId")
	if !ok {
		return
	}
	item, err := h.service.RetryActivation(c.Request.Context(), id)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"logical_bucket": dto.AdaptLogicalBucket(item)})
}
