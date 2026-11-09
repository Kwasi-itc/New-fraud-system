package mysqlruntime

import (
	"context"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/display"
	"net/http"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type localAuthorWorkflow interface {
	DeleteTable(context.Context, uuid.UUID, bool, uuid.UUID) (datamodel.DeleteReport, error)
	CreateEnum(context.Context, service.CreateFieldEnumValueInput, uuid.UUID) (datamodel.FieldEnumValue, error)
	ExportModel(context.Context, uuid.UUID) (service.PortableDataModelDocument, error)
	ImportModel(context.Context, uuid.UUID, service.PortableDataModelDocument, uuid.UUID) (service.PortableImportResult, error)
	DisplaySettings(context.Context, uuid.UUID) (display.Settings, error)
	SaveDisplaySettings(context.Context, uuid.UUID, display.SaveRequest, uuid.UUID) (display.Settings, error)
	RecordDisplays(context.Context, uuid.UUID, []display.Object) (display.Batch, error)
	DeleteField(context.Context, uuid.UUID, bool, uuid.UUID) (datamodel.DeleteReport, error)
	MutateEnum(context.Context, service.UpdateFieldEnumValueInput, bool, uuid.UUID) (datamodel.FieldEnumValue, error)
	Create(context.Context, service.CreateTableInput, uuid.UUID) (datamodel.Table, error)
	CreateField(context.Context, service.CreateFieldInput, uuid.UUID) (datamodel.Field, error)
	UpdateTable(context.Context, service.UpdateTableInput, uuid.UUID) (datamodel.Table, error)
	UpdateField(context.Context, service.UpdateFieldInput, uuid.UUID) (datamodel.Field, error)
}
type localAuthorHandler struct {
	worker localAuthorWorkflow
	slots  chan struct{}
}

func NewLocalAuthorHTTP(worker *LocalCreation) (http.Handler, error) {
	if worker == nil {
		return nil, errors.New("local worker secret required")
	}
	return newLocalAuthorHTTP(worker.Secret, worker)
}
func newLocalAuthorHTTP(secret string, worker localAuthorWorkflow) (http.Handler, error) {
	if worker == nil || len(secret) != 64 {
		return nil, errors.New("local worker secret required")
	}
	h := &localAuthorHandler{worker: worker, slots: make(chan struct{}, 2)}
	r := gin.New()
	r.Use(gin.Recovery(), localAuthorMiddleware(secret))
	r.POST("/v1/tenants/:tenantId/tables", h.CreateTable)
	r.POST("/v1/tables/:tableId/fields", h.CreateField)
	r.PATCH("/v1/tables/:tableId", h.UpdateTable)
	r.PATCH("/v1/fields/:fieldId", h.UpdateField)
	r.DELETE("/v1/fields/:fieldId", h.DeleteField)
	r.PATCH("/v1/enum-values/:enumValueId", h.UpdateEnum)
	r.DELETE("/v1/enum-values/:enumValueId", h.DeleteEnum)
	r.GET("/v1/tables/:tableId/display-settings", h.GetDisplay)
	r.PUT("/v1/tables/:tableId/display-settings", h.SaveDisplay)
	r.POST("/v1/tenants/:tenantId/record-displays", h.ReadDisplays)
	r.GET("/v1/tenants/:tenantId/data-model/export", h.ExportModel)
	r.POST("/v1/tenants/:tenantId/data-model/import", h.ImportModel)
	r.DELETE("/v1/tables/:tableId", h.DeleteTable)
	r.POST("/v1/fields/:fieldId/enum-values", h.CreateEnum)
	if local, ok := worker.(*LocalCreation); ok {
		registerLocalRemaining(r, local)
	}
	return r, nil
}
