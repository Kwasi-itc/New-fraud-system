package mysqlruntime

import (
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/display"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"net/http"
)

func (h *localAuthorHandler) GetDisplay(c *gin.Context) {
	id, err := uuid.Parse(c.Param("tableId"))
	if err != nil || id == uuid.Nil {
		c.JSON(400, gin.H{"error": "invalid_id"})
		return
	}
	value, err := h.worker.DisplaySettings(c.Request.Context(), id)
	if err != nil {
		localUpdateError(c, err)
		return
	}
	c.JSON(200, value)
}
func (h *localAuthorHandler) SaveDisplay(c *gin.Context) {
	id, op, ok := localMutationIDs(c, "tableId")
	if !ok {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 128<<10)
	var input display.SaveRequest
	if decodeLocalPatch(c, &input) != nil {
		c.JSON(400, gin.H{"error": "invalid_display_settings"})
		return
	}
	if input.Version == nil || *input.Version < 0 {
		c.JSON(400, gin.H{"error": "nonnegative settings version is required"})
		return
	}
	value, err := h.worker.SaveDisplaySettings(c.Request.Context(), id, input, op)
	if err != nil {
		localUpdateError(c, err)
		return
	}
	c.JSON(200, value)
}
func (h *localAuthorHandler) ReadDisplays(c *gin.Context) {
	id, err := uuid.Parse(c.Param("tenantId"))
	if err != nil || id == uuid.Nil {
		c.JSON(400, gin.H{"error": "invalid_id"})
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 128<<10)
	var input struct {
		Objects []display.Object `json:"objects"`
	}
	if decodeLocalPatch(c, &input) != nil {
		c.JSON(400, gin.H{"error": "invalid_records"})
		return
	}
	if err := display.ValidateObjects(input.Objects); err != nil {
		localUpdateError(c, err)
		return
	}
	value, err := h.worker.RecordDisplays(c.Request.Context(), id, input.Objects)
	if err != nil {
		localUpdateError(c, err)
		return
	}
	c.JSON(200, value)
}
