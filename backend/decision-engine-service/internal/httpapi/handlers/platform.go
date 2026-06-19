package handlers

import (
	"encoding/csv"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/platform"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/httpapi/dto"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/service"
)

type PlatformHandler struct {
	service service.PlatformService
}

func NewPlatformHandler(service service.PlatformService) PlatformHandler {
	return PlatformHandler{service: service}
}

func (h PlatformHandler) CreateCustomList(c *gin.Context) {
	var req dto.CreateCustomListRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "details": err.Error()})
		return
	}
	item, err := h.service.CreateCustomList(c.Request.Context(), c.Param("tenantId"), req.Name, req.Description, req.Kind)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "create_custom_list_failed", "details": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"custom_list": dto.AdaptCustomList(item)})
}

func (h PlatformHandler) ListCustomLists(c *gin.Context) {
	items, err := h.service.ListCustomLists(c.Request.Context(), c.Param("tenantId"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "list_custom_lists_failed", "details": err.Error()})
		return
	}
	out := make([]dto.CustomListResponse, len(items))
	for i, item := range items {
		out[i] = dto.AdaptCustomList(item)
	}
	c.JSON(http.StatusOK, gin.H{"custom_lists": out})
}

func (h PlatformHandler) GetCustomList(c *gin.Context) {
	item, err := h.service.GetCustomList(c.Request.Context(), c.Param("tenantId"), c.Param("listId"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "get_custom_list_failed", "details": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"custom_list": dto.AdaptCustomList(item)})
}

func (h PlatformHandler) UpdateCustomList(c *gin.Context) {
	var req dto.UpdateCustomListRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "details": err.Error()})
		return
	}
	item, err := h.service.UpdateCustomList(c.Request.Context(), c.Param("tenantId"), c.Param("listId"), req.Name, req.Description, req.Kind)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "update_custom_list_failed", "details": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"custom_list": dto.AdaptCustomList(item)})
}

func (h PlatformHandler) DeleteCustomList(c *gin.Context) {
	if err := h.service.DeleteCustomList(c.Request.Context(), c.Param("tenantId"), c.Param("listId")); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "delete_custom_list_failed", "details": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

func (h PlatformHandler) CreateCustomListEntry(c *gin.Context) {
	var req dto.CreateCustomListEntryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "details": err.Error()})
		return
	}
	item, err := h.service.CreateCustomListEntry(c.Request.Context(), c.Param("tenantId"), c.Param("listId"), req.Value)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "create_custom_list_entry_failed", "details": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"custom_list_entry": dto.AdaptCustomListEntry(item)})
}

func (h PlatformHandler) UpdateCustomListEntry(c *gin.Context) {
	var req dto.CreateCustomListEntryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "details": err.Error()})
		return
	}
	item, err := h.service.UpdateCustomListEntry(c.Request.Context(), c.Param("tenantId"), c.Param("listId"), c.Param("entryId"), req.Value)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "update_custom_list_entry_failed", "details": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"custom_list_entry": dto.AdaptCustomListEntry(item)})
}

func (h PlatformHandler) ImportCustomListEntries(c *gin.Context) {
	fileHeader, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "details": "missing file"})
		return
	}

	file, err := fileHeader.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "details": err.Error()})
		return
	}
	defer file.Close()

	values, err := parseCustomListCSV(file)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_csv", "details": err.Error()})
		return
	}

	importedCount, err := h.service.ImportCustomListEntries(c.Request.Context(), c.Param("tenantId"), c.Param("listId"), values)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "import_custom_list_entries_failed", "details": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"imported_count": importedCount})
}

func (h PlatformHandler) ListCustomListEntries(c *gin.Context) {
	var (
		items    []dto.CustomListEntryResponse
		err      error
		rawItems []platform.CustomListEntry
	)
	if listID := c.Param("listId"); listID != "" {
		rawItems, err = h.service.ListCustomListEntriesByListID(c.Request.Context(), c.Param("tenantId"), listID)
	} else {
		rawItems, err = h.service.ListCustomListEntries(c.Request.Context(), c.Param("tenantId"), c.Query("list_name"))
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "list_custom_list_entries_failed", "details": err.Error()})
		return
	}
	items = make([]dto.CustomListEntryResponse, len(rawItems))
	for i, item := range rawItems {
		items[i] = dto.AdaptCustomListEntry(item)
	}
	c.JSON(http.StatusOK, gin.H{"custom_list_entries": items})
}

func (h PlatformHandler) DeleteCustomListEntry(c *gin.Context) {
	if err := h.service.DeleteCustomListEntry(c.Request.Context(), c.Param("tenantId"), c.Param("listId"), c.Param("entryId")); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "delete_custom_list_entry_failed", "details": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

func parseCustomListCSV(reader io.Reader) ([]string, error) {
	csvReader := csv.NewReader(reader)
	csvReader.FieldsPerRecord = -1

	records, err := csvReader.ReadAll()
	if err != nil {
		return nil, err
	}

	values := make([]string, 0, len(records))
	for index, record := range records {
		if len(record) == 0 {
			continue
		}

		value := strings.TrimSpace(strings.TrimPrefix(record[0], "\uFEFF"))
		if value == "" {
			continue
		}
		if index == 0 {
			lower := strings.ToLower(value)
			if lower == "value" || lower == "values" {
				continue
			}
		}
		values = append(values, value)
	}

	return values, nil
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
