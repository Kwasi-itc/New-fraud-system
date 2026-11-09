package mysqlruntime

import (
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/httpapi/dto"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/httpapi/handlers"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func (h *localAuthorHandler) ExportModel(c *gin.Context) {
	id, err := uuid.Parse(c.Param("tenantId"))
	if err != nil || id == uuid.Nil {
		c.JSON(400, gin.H{"error": "invalid_tenant"})
		return
	}
	document, err := h.worker.ExportModel(c.Request.Context(), id)
	if err != nil {
		localUpdateError(c, err)
		return
	}
	c.JSON(200, dto.PortableDataModelExportResponse{DataModel: handlers.AdaptPortableDocument(document)})
}
func (h *localAuthorHandler) ImportModel(c *gin.Context) {
	id, op, ok := localMutationIDs(c, "tenantId")
	if !ok {
		return
	}
	var body struct {
		DataModel *dto.PortableDataModelDocument `json:"data_model"`
	}
	if decodeLocalPatch(c, &body) != nil || body.DataModel == nil {
		c.JSON(400, gin.H{"error": "data_model is required and must match the portable schema"})
		return
	}
	document := handlers.AdaptPortableDocumentRequest(*body.DataModel)
	if err := validateLocalPortable(document); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	select {
	case h.slots <- struct{}{}:
		defer func() { <-h.slots }()
	case <-c.Request.Context().Done():
		c.JSON(503, gin.H{"error": "authoring_busy"})
		return
	}
	value, err := h.worker.ImportModel(c.Request.Context(), id, document, op)
	if err != nil {
		localUpdateError(c, err)
		return
	}
	c.JSON(200, dto.PortableDataModelImportResponse{Summary: dto.PortableImportSummary{TablesCreated: value.TablesCreated, FieldsCreated: value.FieldsCreated, LinksCreated: value.LinksCreated, PivotsCreated: value.PivotsCreated, TableOptionsUpserted: value.TableOptionsUpserted, NavigationOptionsCreated: value.NavigationOptionsCreated, RevisionID: value.RevisionID}})
}
