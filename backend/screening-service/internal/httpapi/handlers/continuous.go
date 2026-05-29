package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/service"
)

type ContinuousHandler struct {
	service service.ScreeningService
}

func NewContinuousHandler(service service.ScreeningService) ContinuousHandler {
	return ContinuousHandler{service: service}
}

type continuousConfigRequest struct {
	Name          string         `json:"name"`
	ObjectType    string         `json:"object_type"`
	Provider      string         `json:"provider"`
	FieldMap      map[string]any `json:"field_map"`
	ReviewInboxID *string        `json:"review_inbox_id"`
	Enabled       bool           `json:"enabled"`
}

type monitoredObjectRequest struct {
	ObjectType string         `json:"object_type"`
	ObjectID   string         `json:"object_id"`
	Attributes map[string]any `json:"attributes"`
}

type requeueMonitoredObjectRequest struct {
	Attributes map[string]any `json:"attributes"`
}

func (h ContinuousHandler) ListConfigs(c *gin.Context) {
	items, err := h.service.ListContinuousConfigs(c.Request.Context(), c.Param("tenantId"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, items)
}

func (h ContinuousHandler) CreateConfig(c *gin.Context) {
	var body continuousConfigRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	fieldMap, err := json.Marshal(body.FieldMap)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	item, err := h.service.CreateContinuousConfig(c.Request.Context(), c.Param("tenantId"), body.Name, body.ObjectType, body.Provider, fieldMap, body.ReviewInboxID, body.Enabled)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, item)
}

func (h ContinuousHandler) GetConfig(c *gin.Context) {
	item, err := h.service.GetContinuousConfig(c.Request.Context(), c.Param("tenantId"), c.Param("configId"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, item)
}

func (h ContinuousHandler) UpdateConfig(c *gin.Context) {
	var body continuousConfigRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	fieldMap, err := json.Marshal(body.FieldMap)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	item, err := h.service.UpdateContinuousConfig(c.Request.Context(), c.Param("tenantId"), c.Param("configId"), body.Name, body.ObjectType, body.Provider, fieldMap, body.ReviewInboxID, body.Enabled)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, item)
}

func (h ContinuousHandler) DeleteConfig(c *gin.Context) {
	if err := h.service.DeleteContinuousConfig(c.Request.Context(), c.Param("tenantId"), c.Param("configId")); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

func (h ContinuousHandler) ListMonitoredObjects(c *gin.Context) {
	items, err := h.service.ListMonitoredObjects(c.Request.Context(), c.Param("tenantId"), c.Param("configId"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, items)
}

func (h ContinuousHandler) CreateMonitoredObject(c *gin.Context) {
	var body monitoredObjectRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	attributes, err := json.Marshal(body.Attributes)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	item, err := h.service.CreateMonitoredObject(c.Request.Context(), c.Param("tenantId"), c.Param("configId"), body.ObjectType, body.ObjectID, attributes)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, item)
}

func (h ContinuousHandler) GetMonitoredObject(c *gin.Context) {
	item, err := h.service.GetMonitoredObject(c.Request.Context(), c.Param("tenantId"), c.Param("monitoredObjectId"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, item)
}

func (h ContinuousHandler) DeleteMonitoredObject(c *gin.Context) {
	if err := h.service.DeleteMonitoredObject(c.Request.Context(), c.Param("tenantId"), c.Param("monitoredObjectId")); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

func (h ContinuousHandler) RequeueMonitoredObject(c *gin.Context) {
	var body requeueMonitoredObjectRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	attributes, err := json.Marshal(body.Attributes)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	item, err := h.service.RequeueMonitoredObject(c.Request.Context(), c.Param("tenantId"), c.Param("monitoredObjectId"), attributes)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, item)
}
