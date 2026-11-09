package mysqlruntime

import (
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/httpapi/dto"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/service"
	store "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func localMutationIDs(c *gin.Context, param string) (uuid.UUID, uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param(param))
	if err != nil || id == uuid.Nil {
		c.JSON(400, gin.H{"error": "invalid_id"})
		return id, uuid.Nil, false
	}
	op := uuid.New()
	if key := c.GetHeader("Idempotency-Key"); key != "" {
		op, err = uuid.Parse(key)
		if err != nil || op == uuid.Nil {
			c.JSON(400, gin.H{"error": "Idempotency-Key must be a nonzero UUID"})
			return id, op, false
		}
	}
	c.Header("Idempotency-Key", op.String())
	return id, op, true
}
func (h *localAuthorHandler) DeleteField(c *gin.Context) {
	id, op, ok := localMutationIDs(c, "fieldId")
	if !ok {
		return
	}
	dry := c.Query("dry_run")
	if dry != "" && dry != "true" && dry != "false" {
		c.JSON(400, gin.H{"error": "dry_run must be true or false"})
		return
	}
	select {
	case h.slots <- struct{}{}:
		defer func() { <-h.slots }()
	case <-c.Request.Context().Done():
		c.Status(503)
		return
	}
	report, err := h.worker.DeleteField(c.Request.Context(), id, dry == "true", op)
	if errors.Is(err, service.ErrFieldDependency) || errors.Is(err, store.ErrFieldDeletionDependency) {
		response := dto.AdaptDeleteReport(report)
		c.JSON(409, gin.H{"error": err.Error(), "performed": response.Performed, "conflicts": response.Conflicts})
		return
	}
	if err != nil {
		localUpdateError(c, err)
		return
	}
	c.JSON(200, dto.AdaptDeleteReport(report))
}
func (h *localAuthorHandler) UpdateEnum(c *gin.Context) { h.mutateEnum(c, false) }
func (h *localAuthorHandler) DeleteEnum(c *gin.Context) { h.mutateEnum(c, true) }
func (h *localAuthorHandler) mutateEnum(c *gin.Context, remove bool) {
	id, op, ok := localMutationIDs(c, "enumValueId")
	if !ok {
		return
	}
	var body dto.UpdateFieldEnumValueRequest
	if !remove {
		if decodeLocalPatch(c, &body) != nil {
			c.JSON(400, gin.H{"error": "invalid_request"})
			return
		}
		if body.Value == nil && body.Label == nil && body.SortOrder == nil {
			c.JSON(400, gin.H{"error": "empty_patch"})
			return
		}
	}
	value, err := h.worker.MutateEnum(c.Request.Context(), service.UpdateFieldEnumValueInput{EnumValueID: id, Value: body.Value, Label: body.Label, SortOrder: body.SortOrder}, remove, op)
	if err != nil {
		localUpdateError(c, err)
		return
	}
	if remove {
		c.Status(204)
		return
	}
	c.JSON(200, gin.H{"enum_value": dto.AdaptFieldEnumValue(value)})
}

func (h *localAuthorHandler) DeleteTable(c *gin.Context) {
	id, op, ok := localMutationIDs(c, "tableId")
	if !ok {
		return
	}
	dry := c.Query("dry_run")
	if dry != "" && dry != "true" && dry != "false" {
		c.JSON(400, gin.H{"error": "dry_run must be true or false"})
		return
	}
	select {
	case h.slots <- struct{}{}:
		defer func() { <-h.slots }()
	case <-c.Request.Context().Done():
		c.Status(503)
		return
	}
	report, err := h.worker.DeleteTable(c.Request.Context(), id, dry == "true", op)
	if errors.Is(err, store.ErrTableDeletionDependency) {
		response := dto.AdaptDeleteReport(report)
		c.JSON(409, gin.H{"error": err.Error(), "performed": response.Performed, "conflicts": response.Conflicts})
		return
	}
	if err != nil {
		localUpdateError(c, err)
		return
	}
	c.JSON(200, dto.AdaptDeleteReport(report))
}

func (h *localAuthorHandler) CreateEnum(c *gin.Context) {
	id, op, ok := localMutationIDs(c, "fieldId")
	if !ok {
		return
	}
	var body dto.CreateFieldEnumValueRequest
	if decodeLocalPatch(c, &body) != nil {
		c.JSON(400, gin.H{"error": "invalid_request"})
		return
	}
	value, err := h.worker.CreateEnum(c.Request.Context(), service.CreateFieldEnumValueInput{FieldID: id, Value: body.Value, Label: body.Label, SortOrder: body.SortOrder}, op)
	if err != nil {
		localUpdateError(c, err)
		return
	}
	c.JSON(201, gin.H{"enum_value": dto.AdaptFieldEnumValue(value)})
}
