package mysqlruntime

import (
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/httpapi/dto"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func (h *localAuthorHandler) CreateField(c *gin.Context) {
	id, err := uuid.Parse(c.Param("tableId"))
	if err != nil || id == uuid.Nil {
		c.JSON(400, gin.H{"error": "invalid_table"})
		return
	}
	var body dto.CreateFieldRequest
	if decodeLocalPatch(c, &body) != nil {
		c.JSON(400, gin.H{"error": "invalid_request"})
		return
	}
	typ := datamodel.DataType(body.DataType)
	if err := datamodel.ValidateFieldCreate(body.Name, typ, body.IsEnum, body.IsUnique); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	seeds := make([]service.CreateFieldEnumValueSeed, len(body.EnumValues))
	for i, seed := range body.EnumValues {
		seeds[i] = service.CreateFieldEnumValueSeed{Value: seed.Value, Label: seed.Label, SortOrder: seed.SortOrder}
	}
	if err := service.ValidateFieldSeeds(service.CreateFieldInput{IsEnum: body.IsEnum, DataType: typ, EnumValues: seeds}); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	op := uuid.NewSHA1(id, []byte("local-create-field/v1/"+datamodel.NormalizeName(body.Name)))
	if key := c.GetHeader("Idempotency-Key"); key != "" {
		op, err = uuid.Parse(key)
		if err != nil || op == uuid.Nil {
			c.JSON(400, gin.H{"error": "invalid_idempotency_key"})
			return
		}
	}
	c.Header("Idempotency-Key", op.String())
	select {
	case h.slots <- struct{}{}:
		defer func() { <-h.slots }()
	case <-c.Request.Context().Done():
		c.JSON(503, gin.H{"error": "authoring_busy"})
		return
	}
	value, err := h.worker.CreateField(c.Request.Context(), service.CreateFieldInput{TableID: id, Name: body.Name, Alias: body.Alias, Description: body.Description, DataType: typ, Nullable: body.Nullable, IsUnique: body.IsUnique, IsEnum: body.IsEnum, EnumValues: seeds}, op)
	if err != nil {
		localUpdateError(c, err)
		return
	}
	c.JSON(201, gin.H{"field": dto.AdaptField(value)})
}
