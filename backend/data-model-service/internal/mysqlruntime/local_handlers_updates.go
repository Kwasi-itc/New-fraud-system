package mysqlruntime

import (
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/httpapi/dto"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func (h *localAuthorHandler) UpdateTable(c *gin.Context) { h.update(c, false) }
func (h *localAuthorHandler) UpdateField(c *gin.Context) { h.update(c, true) }
func (h *localAuthorHandler) update(c *gin.Context, field bool) {
	parameter := "tableId"
	if field {
		parameter = "fieldId"
	}
	id, err := uuid.Parse(c.Param(parameter))
	if err != nil || id == uuid.Nil {
		c.JSON(400, gin.H{"error": "invalid_id"})
		return
	}
	var input any
	if field {
		var body dto.UpdateFieldRequest
		if decodeLocalPatch(c, &body) != nil {
			c.JSON(400, gin.H{"error": "invalid_request"})
			return
		}
		if body.Description == nil && body.Alias == nil && body.Nullable == nil && body.IsUnique == nil && body.IsEnum == nil {
			c.JSON(400, gin.H{"error": "empty_patch"})
			return
		}
		input = service.UpdateFieldInput{FieldID: id, Description: body.Description, Alias: body.Alias, Nullable: body.Nullable, IsUnique: body.IsUnique, IsEnum: body.IsEnum}
	} else {
		var body dto.UpdateTableRequest
		if decodeLocalPatch(c, &body) != nil {
			c.JSON(400, gin.H{"error": "invalid_request"})
			return
		}
		if body.SemanticType != nil {
			if err := datamodel.ValidateSemanticType(*body.SemanticType); err != nil {
				c.JSON(400, gin.H{"error": err.Error()})
				return
			}
		}
		if body.Description == nil && body.Alias == nil && body.SemanticType == nil && body.CaptionField == nil {
			c.JSON(400, gin.H{"error": "empty_patch"})
			return
		}
		input = service.UpdateTableInput{TableID: id, Description: body.Description, Alias: body.Alias, SemanticType: body.SemanticType, CaptionField: body.CaptionField}
	}
	op := uuid.New()
	if supplied := c.GetHeader("Idempotency-Key"); supplied != "" {
		op, err = uuid.Parse(supplied)
		if err != nil || op == uuid.Nil {
			c.JSON(400, gin.H{"error": "Idempotency-Key must be a nonzero UUID"})
			return
		}
	}
	c.Header("Idempotency-Key", op.String())

	if field {
		select {
		case h.slots <- struct{}{}:
			defer func() { <-h.slots }()
		case <-c.Request.Context().Done():
			c.JSON(503, gin.H{"error": "authoring_busy"})
			return
		}
		value, err := h.worker.UpdateField(c.Request.Context(), input.(service.UpdateFieldInput), op)
		if err != nil {
			localUpdateError(c, err)
			return
		}
		c.JSON(200, gin.H{"field": dto.AdaptField(value)})
	} else {
		value, err := h.worker.UpdateTable(c.Request.Context(), input.(service.UpdateTableInput), op)
		if err != nil {
			localUpdateError(c, err)
			return
		}
		c.JSON(200, gin.H{"table": dto.AdaptTable(value)})
	}
}
