package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/httpapi/dto"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/service"
)

type PlatformHandler struct {
	service service.PlatformService
}

func NewPlatformHandler(service service.PlatformService) PlatformHandler {
	return PlatformHandler{service: service}
}

func (h PlatformHandler) CreateCustomListEntry(c *gin.Context) {
	var req dto.CreateCustomListEntryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "details": err.Error()})
		return
	}
	item, err := h.service.CreateCustomListEntry(c.Request.Context(), c.Param("tenantId"), req.ListName, req.Value)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "create_custom_list_entry_failed", "details": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"custom_list_entry": dto.AdaptCustomListEntry(item)})
}

func (h PlatformHandler) ListCustomListEntries(c *gin.Context) {
	items, err := h.service.ListCustomListEntries(c.Request.Context(), c.Param("tenantId"), c.Query("list_name"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "list_custom_list_entries_failed", "details": err.Error()})
		return
	}
	out := make([]dto.CustomListEntryResponse, len(items))
	for i, item := range items {
		out[i] = dto.AdaptCustomListEntry(item)
	}
	c.JSON(http.StatusOK, gin.H{"custom_list_entries": out})
}

func (h PlatformHandler) CreateRecordTag(c *gin.Context) {
	var req dto.CreateRecordTagRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "details": err.Error()})
		return
	}
	item, err := h.service.CreateRecordTag(c.Request.Context(), c.Param("tenantId"), req.ObjectType, req.ObjectID, req.Tag)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "create_record_tag_failed", "details": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"record_tag": dto.AdaptRecordTag(item)})
}

func (h PlatformHandler) ListRecordTags(c *gin.Context) {
	items, err := h.service.ListRecordTags(c.Request.Context(), c.Param("tenantId"), c.Query("object_type"), c.Query("object_id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "list_record_tags_failed", "details": err.Error()})
		return
	}
	out := make([]dto.RecordTagResponse, len(items))
	for i, item := range items {
		out[i] = dto.AdaptRecordTag(item)
	}
	c.JSON(http.StatusOK, gin.H{"record_tags": out})
}

func (h PlatformHandler) CreateRiskSnapshot(c *gin.Context) {
	var req dto.CreateRiskSnapshotRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "details": err.Error()})
		return
	}
	item, err := h.service.CreateRiskSnapshot(c.Request.Context(), c.Param("tenantId"), req.ObjectType, req.ObjectID, req.RiskLevel)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "create_risk_snapshot_failed", "details": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"risk_snapshot": dto.AdaptRiskSnapshot(item)})
}

func (h PlatformHandler) CreateIPFlag(c *gin.Context) {
	var req dto.CreateIPFlagRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "details": err.Error()})
		return
	}
	item, err := h.service.CreateIPFlag(c.Request.Context(), c.Param("tenantId"), req.IPAddress, req.Flag)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "create_ip_flag_failed", "details": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"ip_flag": dto.AdaptIPFlag(item)})
}

func (h PlatformHandler) ListIPFlags(c *gin.Context) {
	items, err := h.service.ListIPFlags(c.Request.Context(), c.Param("tenantId"), c.Query("ip_address"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "list_ip_flags_failed", "details": err.Error()})
		return
	}
	out := make([]dto.IPFlagResponse, len(items))
	for i, item := range items {
		out[i] = dto.AdaptIPFlag(item)
	}
	c.JSON(http.StatusOK, gin.H{"ip_flags": out})
}
