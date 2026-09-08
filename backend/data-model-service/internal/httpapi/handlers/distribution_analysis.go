package handlers

import (
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/gin-gonic/gin"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/service"
)

const maximumDistributionSampleBytes = 32 << 20

type DistributionAnalysisHandler struct {
	service service.DistributionAnalysisService
	stored  service.DistributionValueReader
}

func NewDistributionAnalysisHandler(service service.DistributionAnalysisService, stored ...service.DistributionValueReader) DistributionAnalysisHandler {
	handler := DistributionAnalysisHandler{service: service}
	if len(stored) > 0 {
		handler.stored = stored[0]
	}
	return handler
}

func (h DistributionAnalysisHandler) AnalyzeStored(c *gin.Context) {
	tenantID, err := uuid.Parse(c.Param("tenantId"))
	if err != nil {
		writeBadRequest(c, "invalid tenantId")
		return
	}
	var request struct {
		TableName string `json:"table_name" binding:"required"`
		FieldName string `json:"field_name" binding:"required"`
	}
	if err := c.ShouldBindJSON(&request); err != nil || h.stored == nil {
		writeBadRequest(c, "table_name and field_name are required")
		return
	}
	analysis, err := h.service.AnalyzeStored(c.Request.Context(), h.stored, tenantID, request.TableName, request.FieldName)
	if err != nil {
		writeBadRequest(c, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"analysis": analysis})
}

func (h DistributionAnalysisHandler) AnalyzeSample(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maximumDistributionSampleBytes)
	column := strings.TrimSpace(c.PostForm("column"))
	if column == "" {
		writeBadRequest(c, "column is required")
		return
	}
	file, _, err := c.Request.FormFile("file")
	if err != nil {
		writeBadRequest(c, "a CSV file is required")
		return
	}
	defer file.Close()

	analysis, err := h.service.AnalyzeCSV(file, column)
	if err != nil {
		writeBadRequest(c, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"analysis": analysis})
}
