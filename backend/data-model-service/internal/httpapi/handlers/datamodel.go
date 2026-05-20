package handlers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/httpapi/dto"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/service"
)

type DataModelHandler struct {
	readService             service.DataModelReadService
	tableService            service.TableService
	fieldService            service.FieldService
	enumValueService        service.FieldEnumValueService
	linkService             service.LinkService
	pivotService            service.PivotService
	optionsService          service.OptionsService
	navigationOptionService service.NavigationOptionService
}

func NewDataModelHandler(
	readService service.DataModelReadService,
	tableService service.TableService,
	fieldService service.FieldService,
	enumValueService service.FieldEnumValueService,
	linkService service.LinkService,
	pivotService service.PivotService,
	optionsService service.OptionsService,
	navigationOptionService service.NavigationOptionService,
) DataModelHandler {
	return DataModelHandler{
		readService:             readService,
		tableService:            tableService,
		fieldService:            fieldService,
		enumValueService:        enumValueService,
		linkService:             linkService,
		pivotService:            pivotService,
		optionsService:          optionsService,
		navigationOptionService: navigationOptionService,
	}
}

func (h DataModelHandler) GetDataModel(c *gin.Context) {
	tenantID, ok := parseUUIDParam(c, "tenantId")
	if !ok {
		return
	}
	publishedModel, err := h.readService.Get(c.Request.Context(), tenantID)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"data_model": dto.AdaptAssembledDataModel(
			publishedModel.Model,
			publishedModel.RevisionID,
			publishedModel.Tenant.Status,
		),
	})
}

func (h DataModelHandler) ListTables(c *gin.Context) {
	tenantID, ok := parseUUIDParam(c, "tenantId")
	if !ok {
		return
	}
	tables, err := h.tableService.ListByTenant(c.Request.Context(), tenantID)
	if err != nil {
		writeError(c, err)
		return
	}
	response := make([]dto.TableResponse, len(tables))
	for i, table := range tables {
		response[i] = dto.AdaptTable(table)
	}
	c.JSON(http.StatusOK, gin.H{"tables": response})
}

func (h DataModelHandler) CreateTable(c *gin.Context) {
	tenantID, ok := parseUUIDParam(c, "tenantId")
	if !ok {
		return
	}
	var request dto.CreateTableRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		writeBadRequest(c, err.Error())
		return
	}
	table, err := h.tableService.Create(c.Request.Context(), service.CreateTableInput{
		TenantID:     tenantID,
		Name:         request.Name,
		Description:  request.Description,
		Alias:        request.Alias,
		SemanticType: request.SemanticType,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"table": dto.AdaptTable(table)})
}

func (h DataModelHandler) ListFields(c *gin.Context) {
	tableID, ok := parseUUIDParam(c, "tableId")
	if !ok {
		return
	}
	fields, err := h.fieldService.ListByTable(c.Request.Context(), tableID)
	if err != nil {
		writeError(c, err)
		return
	}
	response := make([]dto.FieldResponse, len(fields))
	for i, field := range fields {
		response[i] = dto.AdaptField(field)
	}
	c.JSON(http.StatusOK, gin.H{"fields": response})
}

func (h DataModelHandler) UpdateTable(c *gin.Context) {
	tableID, ok := parseUUIDParam(c, "tableId")
	if !ok {
		return
	}
	var request dto.UpdateTableRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		writeBadRequest(c, err.Error())
		return
	}
	table, err := h.tableService.Update(c.Request.Context(), service.UpdateTableInput{
		TableID:      tableID,
		Description:  request.Description,
		Alias:        request.Alias,
		SemanticType: request.SemanticType,
		CaptionField: request.CaptionField,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"table": dto.AdaptTable(table)})
}

func (h DataModelHandler) DeleteTable(c *gin.Context) {
	tableID, ok := parseUUIDParam(c, "tableId")
	if !ok {
		return
	}
	report, err := h.tableService.Delete(c.Request.Context(), tableID, c.Query("dry_run") == "true")
	if err != nil {
		writeDeleteConflictAware(c, err, report)
		return
	}
	c.JSON(http.StatusOK, dto.AdaptDeleteReport(report))
}

func (h DataModelHandler) CreateField(c *gin.Context) {
	tableID, ok := parseUUIDParam(c, "tableId")
	if !ok {
		return
	}
	var request dto.CreateFieldRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		writeBadRequest(c, err.Error())
		return
	}
	dataType, err := datamodel.ParseDataType(request.DataType)
	if err != nil {
		writeBadRequest(c, err.Error())
		return
	}
	field, err := h.fieldService.Create(c.Request.Context(), service.CreateFieldInput{
		TableID:     tableID,
		Name:        request.Name,
		Description: request.Description,
		DataType:    dataType,
		Nullable:    request.Nullable,
		IsEnum:      request.IsEnum,
		IsUnique:    request.IsUnique,
		EnumValues:  adaptCreateFieldEnumValueSeeds(request.EnumValues),
	})
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"field": dto.AdaptField(field)})
}

func adaptCreateFieldEnumValueSeeds(values []dto.CreateFieldEnumValueRequest) []service.CreateFieldEnumValueSeed {
	seeds := make([]service.CreateFieldEnumValueSeed, len(values))
	for i, value := range values {
		seeds[i] = service.CreateFieldEnumValueSeed{
			Value:     value.Value,
			Label:     value.Label,
			SortOrder: value.SortOrder,
		}
	}
	return seeds
}

func (h DataModelHandler) UpdateField(c *gin.Context) {
	fieldID, ok := parseUUIDParam(c, "fieldId")
	if !ok {
		return
	}
	var request dto.UpdateFieldRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		writeBadRequest(c, err.Error())
		return
	}
	field, err := h.fieldService.Update(c.Request.Context(), service.UpdateFieldInput{
		FieldID:     fieldID,
		Description: request.Description,
		Nullable:    request.Nullable,
		IsEnum:      request.IsEnum,
		IsUnique:    request.IsUnique,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"field": dto.AdaptField(field)})
}

func (h DataModelHandler) DeleteField(c *gin.Context) {
	fieldID, ok := parseUUIDParam(c, "fieldId")
	if !ok {
		return
	}
	report, err := h.fieldService.Delete(c.Request.Context(), fieldID, c.Query("dry_run") == "true")
	if err != nil {
		writeDeleteConflictAware(c, err, report)
		return
	}
	c.JSON(http.StatusOK, dto.AdaptDeleteReport(report))
}

func (h DataModelHandler) CreateLink(c *gin.Context) {
	tenantID, ok := parseUUIDParam(c, "tenantId")
	if !ok {
		return
	}
	var request dto.CreateLinkRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		writeBadRequest(c, err.Error())
		return
	}
	link, err := h.linkService.Create(c.Request.Context(), service.CreateLinkInput{
		TenantID:    tenantID,
		Name:        request.Name,
		ParentTable: request.ParentTableID,
		ParentField: request.ParentFieldID,
		ChildTable:  request.ChildTableID,
		ChildField:  request.ChildFieldID,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"link": dto.AdaptLink(link)})
}

func (h DataModelHandler) ListLinks(c *gin.Context) {
	tenantID, ok := parseUUIDParam(c, "tenantId")
	if !ok {
		return
	}
	links, err := h.linkService.ListByTenant(c.Request.Context(), tenantID)
	if err != nil {
		writeError(c, err)
		return
	}
	response := make([]dto.LinkResponse, len(links))
	for i, link := range links {
		response[i] = dto.AdaptLink(link)
	}
	c.JSON(http.StatusOK, gin.H{"links": response})
}

func (h DataModelHandler) DeleteLink(c *gin.Context) {
	linkID, ok := parseUUIDParam(c, "linkId")
	if !ok {
		return
	}
	report, err := h.linkService.Delete(c.Request.Context(), linkID, c.Query("dry_run") == "true")
	if err != nil {
		writeDeleteConflictAware(c, err, report)
		return
	}
	c.JSON(http.StatusOK, dto.AdaptDeleteReport(report))
}

func (h DataModelHandler) CreatePivot(c *gin.Context) {
	tenantID, ok := parseUUIDParam(c, "tenantId")
	if !ok {
		return
	}
	var request dto.CreatePivotRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		writeBadRequest(c, err.Error())
		return
	}
	pivot, err := h.pivotService.Create(c.Request.Context(), service.CreatePivotInput{
		TenantID:    tenantID,
		BaseTableID: request.BaseTableID,
		FieldID:     request.FieldID,
		PathLinkIDs: request.PathLinkIDs,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"pivot": dto.AdaptPivot(pivot)})
}

func (h DataModelHandler) ListPivots(c *gin.Context) {
	tenantID, ok := parseUUIDParam(c, "tenantId")
	if !ok {
		return
	}
	pivots, err := h.pivotService.List(c.Request.Context(), tenantID)
	if err != nil {
		writeError(c, err)
		return
	}
	response := make([]dto.PivotResponse, len(pivots))
	for i, pivot := range pivots {
		response[i] = dto.AdaptPivot(pivot)
	}
	c.JSON(http.StatusOK, gin.H{"pivots": response})
}

func (h DataModelHandler) DeletePivot(c *gin.Context) {
	pivotID, ok := parseUUIDParam(c, "pivotId")
	if !ok {
		return
	}
	report, err := h.pivotService.Delete(c.Request.Context(), pivotID, c.Query("dry_run") == "true")
	if err != nil {
		writeDeleteConflictAware(c, err, report)
		return
	}
	c.JSON(http.StatusOK, dto.AdaptDeleteReport(report))
}

func (h DataModelHandler) GetOptions(c *gin.Context) {
	tableID, ok := parseUUIDParam(c, "tableId")
	if !ok {
		return
	}
	options, err := h.optionsService.Get(c.Request.Context(), tableID)
	if err != nil {
		writeError(c, err)
		return
	}
	fields, err := h.fieldService.ListByTable(c.Request.Context(), tableID)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, dto.AdaptTableOptions(options, fields))
}

func (h DataModelHandler) UpsertOptions(c *gin.Context) {
	tableID, ok := parseUUIDParam(c, "tableId")
	if !ok {
		return
	}
	var request dto.TableOptionsRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		writeBadRequest(c, err.Error())
		return
	}
	options, err := h.optionsService.Upsert(c.Request.Context(), datamodel.TableOptions{
		TableID:         tableID,
		DisplayedFields: request.DisplayedFields,
		FieldOrder:      request.FieldOrder,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	fields, err := h.fieldService.ListByTable(c.Request.Context(), tableID)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, dto.AdaptTableOptions(options, fields))
}

func (h DataModelHandler) ListNavigationOptions(c *gin.Context) {
	tableID, ok := parseUUIDParam(c, "tableId")
	if !ok {
		return
	}
	options, err := h.navigationOptionService.ListByTable(c.Request.Context(), tableID)
	if err != nil {
		writeError(c, err)
		return
	}
	response := make([]dto.NavigationOptionResponse, len(options))
	for i, option := range options {
		response[i] = dto.AdaptNavigationOption(option)
	}
	c.JSON(http.StatusOK, gin.H{"navigation_options": response})
}

func (h DataModelHandler) CreateNavigationOption(c *gin.Context) {
	tableID, ok := parseUUIDParam(c, "tableId")
	if !ok {
		return
	}
	var request dto.CreateNavigationOptionRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		writeBadRequest(c, err.Error())
		return
	}
	table, err := h.tableService.Get(c.Request.Context(), tableID)
	if err != nil {
		writeError(c, err)
		return
	}
	option, err := h.navigationOptionService.Create(c.Request.Context(), service.CreateNavigationOptionInput{
		TenantID:        table.TenantID,
		SourceTableID:   tableID,
		SourceFieldID:   request.SourceFieldID,
		TargetTableID:   request.TargetTableID,
		FilterFieldID:   request.FilterFieldID,
		OrderingFieldID: request.OrderingFieldID,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"navigation_option": dto.AdaptNavigationOption(option)})
}

func (h DataModelHandler) DeleteNavigationOption(c *gin.Context) {
	optionID, ok := parseUUIDParam(c, "navigationOptionId")
	if !ok {
		return
	}
	if err := h.navigationOptionService.Delete(c.Request.Context(), optionID); err != nil {
		writeError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h DataModelHandler) ListFieldEnumValues(c *gin.Context) {
	fieldID, ok := parseUUIDParam(c, "fieldId")
	if !ok {
		return
	}
	values, err := h.enumValueService.List(c.Request.Context(), fieldID)
	if err != nil {
		writeError(c, err)
		return
	}
	response := make([]dto.FieldEnumValueResponse, len(values))
	for i, value := range values {
		response[i] = dto.AdaptFieldEnumValue(value)
	}
	c.JSON(http.StatusOK, gin.H{"enum_values": response})
}

func (h DataModelHandler) CreateFieldEnumValue(c *gin.Context) {
	fieldID, ok := parseUUIDParam(c, "fieldId")
	if !ok {
		return
	}
	var request dto.CreateFieldEnumValueRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		writeBadRequest(c, err.Error())
		return
	}
	value, err := h.enumValueService.Create(c.Request.Context(), service.CreateFieldEnumValueInput{
		FieldID:   fieldID,
		Value:     request.Value,
		Label:     request.Label,
		SortOrder: request.SortOrder,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"enum_value": dto.AdaptFieldEnumValue(value)})
}

func (h DataModelHandler) UpdateFieldEnumValue(c *gin.Context) {
	enumValueID, ok := parseUUIDParam(c, "enumValueId")
	if !ok {
		return
	}
	var request dto.UpdateFieldEnumValueRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		writeBadRequest(c, err.Error())
		return
	}
	value, err := h.enumValueService.Update(c.Request.Context(), service.UpdateFieldEnumValueInput{
		EnumValueID: enumValueID,
		Value:       request.Value,
		Label:       request.Label,
		SortOrder:   request.SortOrder,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"enum_value": dto.AdaptFieldEnumValue(value)})
}

func (h DataModelHandler) DeleteFieldEnumValue(c *gin.Context) {
	enumValueID, ok := parseUUIDParam(c, "enumValueId")
	if !ok {
		return
	}
	if err := h.enumValueService.Delete(c.Request.Context(), enumValueID); err != nil {
		writeError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func parseUUIDParam(c *gin.Context, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param(name))
	if err != nil {
		writeBadRequest(c, "invalid "+name)
		return uuid.Nil, false
	}
	return id, true
}

func writeBadRequest(c *gin.Context, message string) {
	c.JSON(http.StatusBadRequest, gin.H{
		"error": gin.H{
			"code":    "bad_parameter",
			"message": message,
		},
	})
}

func writeError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"code": "not_found", "message": err.Error()}})
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "bad_parameter", "message": err.Error()}})
	}
}

func writeDeleteConflictAware(c *gin.Context, err error, report datamodel.DeleteReport) {
	if report.Conflicts.Reserved || len(report.Conflicts.Links) > 0 || len(report.Conflicts.Pivots) > 0 {
		c.JSON(http.StatusConflict, dto.AdaptDeleteReport(report))
		return
	}
	writeError(c, err)
}
