package handlers

import (
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/httpapi/dto"
	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/service"
)

type UploadLogHandler struct {
	service service.UploadLogService
}

func NewUploadLogHandler(service service.UploadLogService) UploadLogHandler {
	return UploadLogHandler{service: service}
}

func (h UploadLogHandler) CreateCSV(c *gin.Context) {
	tenantID, err := uuid.Parse(c.Param("tenantId"))
	if err != nil {
		writeBadRequest(c, "invalid tenantId")
		return
	}
	mode := ingestion.ModeCreate
	if c.Query("mode") == "patch" {
		mode = ingestion.ModePatch
	}

	fileHeader, err := c.FormFile("file")
	if err != nil {
		writeBadRequest(c, "multipart file field 'file' is required")
		return
	}
	file, err := fileHeader.Open()
	if err != nil {
		writeBadRequest(c, "unable to open uploaded file")
		return
	}
	defer file.Close()

	payload, err := io.ReadAll(file)
	if err != nil {
		writeBadRequest(c, "unable to read uploaded file")
		return
	}

	log, err := h.service.Create(c.Request.Context(), tenantID, c.Param("objectType"), mode, fileHeader.Filename, fileHeader.Header.Get("Content-Type"), payload)
	if err != nil {
		writeServiceError(c, err)
		return
	}

	c.JSON(http.StatusAccepted, gin.H{"upload_log": dto.AdaptUploadLog(log)})
}

func (h UploadLogHandler) List(c *gin.Context) {
	tenantID, err := uuid.Parse(c.Param("tenantId"))
	if err != nil {
		writeBadRequest(c, "invalid tenantId")
		return
	}
	logs, err := h.service.List(c.Request.Context(), tenantID, c.Param("objectType"))
	if err != nil {
		writeServiceError(c, err)
		return
	}
	response := make([]dto.UploadLogResponse, len(logs))
	for i, log := range logs {
		response[i] = dto.AdaptUploadLog(log)
	}
	c.JSON(http.StatusOK, gin.H{"upload_logs": response})
}

func (h UploadLogHandler) Get(c *gin.Context) {
	id, err := uuid.Parse(c.Param("uploadLogId"))
	if err != nil {
		writeBadRequest(c, "invalid uploadLogId")
		return
	}
	log, err := h.service.Get(c.Request.Context(), id)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"upload_log": dto.AdaptUploadLog(log)})
}
