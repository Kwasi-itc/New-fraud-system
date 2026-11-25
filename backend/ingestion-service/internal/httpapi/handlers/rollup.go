package handlers

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/ports"
	storepostgres "github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/store/postgres"
)

const DefaultMaxRollupsPerTable = 4

type RollupHandler struct {
	repo        storepostgres.RollupRepository
	cache       *storepostgres.RollupDefinitionCache
	modelReader ports.DataModelReader
	maxPerTable int
}

func NewRollupHandler(
	repo storepostgres.RollupRepository,
	cache *storepostgres.RollupDefinitionCache,
	modelReader ports.DataModelReader,
	maxPerTable int,
) RollupHandler {
	if maxPerTable <= 0 {
		maxPerTable = DefaultMaxRollupsPerTable
	}
	return RollupHandler{
		repo:        repo,
		cache:       cache,
		modelReader: modelReader,
		maxPerTable: maxPerTable,
	}
}

type createRollupRequest struct {
	TableName  string `json:"table_name" binding:"required"`
	GroupField string `json:"group_field" binding:"required"`
	TimeField  string `json:"time_field" binding:"required"`
	ValueField string `json:"value_field" binding:"required"`
}

func (h RollupHandler) Create(c *gin.Context) {
	tenantID, err := uuid.Parse(c.Param("tenantId"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid tenantId"})
		return
	}
	var req createRollupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	model, err := h.modelReader.GetPublishedDataModel(c.Request.Context(), tenantID)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	table, ok := model.Tables[req.TableName]
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "table_name not found in published model"})
		return
	}
	if _, ok := table.Fields[req.GroupField]; !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "group_field not found on table"})
		return
	}
	timeField, ok := table.Fields[req.TimeField]
	if !ok || !isTimestampType(timeField.DataType) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "time_field must be TIMESTAMPTZ"})
		return
	}
	valueType, ok := resolveRollupValueType(model, table, req.ValueField)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "value_field must be int, float, or record lookup field"})
		return
	}

	activeCount, err := h.repo.CountActiveByTable(c.Request.Context(), tenantID.String(), table.Name)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if activeCount >= h.maxPerTable {
		c.JSON(http.StatusConflict, gin.H{"error": "per-table rollup definition cap exceeded"})
		return
	}

	def, err := h.repo.UpsertDefinition(c.Request.Context(), ingestion.RollupDefinition{
		TenantID:     tenantID.String(),
		ID:           uuid.NewString(),
		TableName:    table.Name,
		GroupField:   req.GroupField,
		TimeField:    req.TimeField,
		ValueField:   req.ValueField,
		ValueType:    valueType,
		ModelVersion: model.RevisionID,
		Status:       ingestion.RollupStatusCapturing,
		CreatedAt:    time.Now().UTC(),
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if h.cache != nil {
		h.cache.Invalidate(tenantID.String(), table.Name)
	}
	c.JSON(http.StatusCreated, def)
}

func (h RollupHandler) List(c *gin.Context) {
	tenantID, err := uuid.Parse(c.Param("tenantId"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid tenantId"})
		return
	}
	tableName := strings.TrimSpace(c.Query("table_name"))
	var defs []ingestion.RollupDefinition
	if tableName != "" {
		defs, err = h.repo.ListDefinitionsByTable(c.Request.Context(), tenantID.String(), tableName)
	} else {
		defs, err = h.repo.ListDefinitionsByTenant(c.Request.Context(), tenantID.String())
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"definitions": defs})
}

func (h RollupHandler) Disable(c *gin.Context) {
	tenantID, err := uuid.Parse(c.Param("tenantId"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid tenantId"})
		return
	}
	defID := c.Param("definitionId")
	def, err := h.repo.GetDefinition(c.Request.Context(), tenantID.String(), defID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	if err := h.repo.UpdateDefinitionStatus(c.Request.Context(), tenantID.String(), defID, ingestion.RollupStatusDisabled, nil); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if h.cache != nil {
		h.cache.Invalidate(tenantID.String(), def.TableName)
	}
	c.JSON(http.StatusOK, gin.H{"status": "disabled"})
}

func (h RollupHandler) Metrics(c *gin.Context) {
	snap := storepostgres.RollupMetricsSnapshot()
	if pending, err := h.repo.CountPendingDeltas(c.Request.Context()); err == nil {
		snap.RollupPendingDeltas = pending
	}
	if byStatus, err := h.repo.CountDefinitionsByStatus(c.Request.Context()); err == nil {
		snap.RollupDefinitionsByStatus = byStatus
	}
	c.JSON(http.StatusOK, gin.H{"rollup_metrics": snap})
}

func isTimestampType(dt string) bool {
	s := strings.ToLower(strings.TrimSpace(dt))
	return strings.Contains(s, "time") || s == "date"
}

func resolveRollupValueType(model ingestion.PublishedDataModel, table ingestion.ObjectSchema, valueField string) (ingestion.RollupValueType, bool) {
	if valueField == model.RecordLookupField {
		return ingestion.RollupValueTypeLookupCount, true
	}
	f, ok := table.Fields[valueField]
	if !ok {
		return "", false
	}
	s := strings.ToLower(strings.TrimSpace(f.DataType))
	switch s {
	case "int", "integer", "int4", "int8", "bigint":
		return ingestion.RollupValueTypeInt, true
	case "float", "float8", "double precision", "numeric":
		return ingestion.RollupValueTypeFloat, true
	default:
		return "", false
	}
}
