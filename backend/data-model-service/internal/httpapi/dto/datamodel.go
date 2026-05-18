package dto

import (
	"time"

	"github.com/google/uuid"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
)

type CreateTableRequest struct {
	Name         string `json:"name" binding:"required"`
	Description  string `json:"description"`
	Alias        string `json:"alias"`
	SemanticType string `json:"semantic_type"`
}

type UpdateTableRequest struct {
	Description  *string `json:"description"`
	Alias        *string `json:"alias"`
	SemanticType *string `json:"semantic_type"`
	CaptionField *string `json:"caption_field"`
}

type TableResponse struct {
	ID           uuid.UUID `json:"id"`
	Name         string    `json:"name"`
	Description  string    `json:"description"`
	Alias        string    `json:"alias"`
	SemanticType string    `json:"semantic_type"`
	CaptionField string    `json:"caption_field"`
	Archived     bool      `json:"archived"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func AdaptTable(table datamodel.Table) TableResponse {
	return TableResponse{
		ID:           table.ID,
		Name:         table.Name,
		Description:  table.Description,
		Alias:        table.Alias,
		SemanticType: table.SemanticType,
		CaptionField: table.CaptionField,
		Archived:     table.Archived,
		CreatedAt:    table.CreatedAt,
		UpdatedAt:    table.UpdatedAt,
	}
}

type CreateFieldRequest struct {
	Name        string `json:"name" binding:"required"`
	Description string `json:"description"`
	DataType    string `json:"data_type" binding:"required"`
	Nullable    bool   `json:"nullable"`
	IsEnum      bool   `json:"is_enum"`
	IsUnique    bool   `json:"is_unique"`
}

type UpdateFieldRequest struct {
	Description *string `json:"description"`
	Nullable    *bool   `json:"nullable"`
	IsEnum      *bool   `json:"is_enum"`
	IsUnique    *bool   `json:"is_unique"`
}

type FieldResponse struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	DataType    string    `json:"data_type"`
	Nullable    bool      `json:"nullable"`
	IsEnum      bool      `json:"is_enum"`
	IsUnique    bool      `json:"is_unique"`
	Archived    bool      `json:"archived"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type CreateFieldEnumValueRequest struct {
	Value     string `json:"value" binding:"required"`
	Label     string `json:"label" binding:"required"`
	SortOrder int    `json:"sort_order"`
}

type UpdateFieldEnumValueRequest struct {
	Value     *string `json:"value"`
	Label     *string `json:"label"`
	SortOrder *int    `json:"sort_order"`
}

type FieldEnumValueResponse struct {
	ID        uuid.UUID `json:"id"`
	FieldID   uuid.UUID `json:"field_id"`
	Value     string    `json:"value"`
	Label     string    `json:"label"`
	SortOrder int       `json:"sort_order"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func AdaptField(field datamodel.Field) FieldResponse {
	return FieldResponse{
		ID:          field.ID,
		Name:        field.Name,
		Description: field.Description,
		DataType:    string(field.DataType),
		Nullable:    field.Nullable,
		IsEnum:      field.IsEnum,
		IsUnique:    field.IsUnique,
		Archived:    field.Archived,
		CreatedAt:   field.CreatedAt,
		UpdatedAt:   field.UpdatedAt,
	}
}

func AdaptFieldEnumValue(value datamodel.FieldEnumValue) FieldEnumValueResponse {
	return FieldEnumValueResponse{
		ID:        value.ID,
		FieldID:   value.FieldID,
		Value:     value.Value,
		Label:     value.Label,
		SortOrder: value.SortOrder,
		CreatedAt: value.CreatedAt,
		UpdatedAt: value.UpdatedAt,
	}
}

type CreateLinkRequest struct {
	Name          string    `json:"name" binding:"required"`
	ParentTableID uuid.UUID `json:"parent_table_id" binding:"required"`
	ParentFieldID uuid.UUID `json:"parent_field_id" binding:"required"`
	ChildTableID  uuid.UUID `json:"child_table_id" binding:"required"`
	ChildFieldID  uuid.UUID `json:"child_field_id" binding:"required"`
}

type LinkResponse struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	ParentTable uuid.UUID `json:"parent_table_id"`
	ParentField uuid.UUID `json:"parent_field_id"`
	ChildTable  uuid.UUID `json:"child_table_id"`
	ChildField  uuid.UUID `json:"child_field_id"`
	CreatedAt   time.Time `json:"created_at"`
}

func AdaptLink(link datamodel.Link) LinkResponse {
	return LinkResponse{
		ID:          link.ID,
		Name:        link.Name,
		ParentTable: link.ParentTable,
		ParentField: link.ParentField,
		ChildTable:  link.ChildTable,
		ChildField:  link.ChildField,
		CreatedAt:   link.CreatedAt,
	}
}

type CreatePivotRequest struct {
	BaseTableID uuid.UUID   `json:"base_table_id" binding:"required"`
	FieldID     *uuid.UUID  `json:"field_id"`
	PathLinkIDs []uuid.UUID `json:"path_link_ids"`
}

type PivotResponse struct {
	ID          uuid.UUID   `json:"id"`
	BaseTableID uuid.UUID   `json:"base_table_id"`
	FieldID     *uuid.UUID  `json:"field_id"`
	PathLinkIDs []uuid.UUID `json:"path_link_ids"`
	CreatedAt   time.Time   `json:"created_at"`
}

func AdaptPivot(pivot datamodel.Pivot) PivotResponse {
	return PivotResponse{
		ID:          pivot.ID,
		BaseTableID: pivot.BaseTableID,
		FieldID:     pivot.FieldID,
		PathLinkIDs: pivot.PathLinkIDs,
		CreatedAt:   pivot.CreatedAt,
	}
}

type TableOptionsRequest struct {
	DisplayedFields []uuid.UUID `json:"displayed_fields"`
	FieldOrder      []uuid.UUID `json:"field_order"`
}

type CreateNavigationOptionRequest struct {
	SourceFieldID   uuid.UUID `json:"source_field_id" binding:"required"`
	TargetTableID   uuid.UUID `json:"target_table_id" binding:"required"`
	FilterFieldID   uuid.UUID `json:"filter_field_id" binding:"required"`
	OrderingFieldID uuid.UUID `json:"ordering_field_id" binding:"required"`
}

type TableOptionsResponse struct {
	ID              uuid.UUID   `json:"id"`
	TableID         uuid.UUID   `json:"table_id"`
	DisplayedFields []uuid.UUID `json:"displayed_fields"`
	FieldOrder      []uuid.UUID `json:"field_order"`
	UpdatedAt       time.Time   `json:"updated_at"`
}

func AdaptTableOptions(options datamodel.TableOptions) TableOptionsResponse {
	return TableOptionsResponse{
		ID:              options.ID,
		TableID:         options.TableID,
		DisplayedFields: options.DisplayedFields,
		FieldOrder:      options.FieldOrder,
		UpdatedAt:       options.UpdatedAt,
	}
}

type NavigationOptionResponse struct {
	ID                uuid.UUID `json:"id"`
	TenantID          uuid.UUID `json:"tenant_id"`
	SourceTableID     uuid.UUID `json:"source_table_id"`
	SourceFieldID     uuid.UUID `json:"source_field_id"`
	TargetTableID     uuid.UUID `json:"target_table_id"`
	FilterFieldID     uuid.UUID `json:"filter_field_id"`
	OrderingFieldID   uuid.UUID `json:"ordering_field_id"`
	SourceTableName   string    `json:"source_table_name"`
	SourceFieldName   string    `json:"source_field_name"`
	TargetTableName   string    `json:"target_table_name"`
	FilterFieldName   string    `json:"filter_field_name"`
	OrderingFieldName string    `json:"ordering_field_name"`
	CreatedAt         time.Time `json:"created_at"`
}

func AdaptNavigationOption(option datamodel.NavigationOption) NavigationOptionResponse {
	return NavigationOptionResponse{
		ID:                option.ID,
		TenantID:          option.TenantID,
		SourceTableID:     option.SourceTableID,
		SourceFieldID:     option.SourceFieldID,
		TargetTableID:     option.TargetTableID,
		FilterFieldID:     option.FilterFieldID,
		OrderingFieldID:   option.OrderingFieldID,
		SourceTableName:   option.SourceTableName,
		SourceFieldName:   option.SourceFieldName,
		TargetTableName:   option.TargetTableName,
		FilterFieldName:   option.FilterFieldName,
		OrderingFieldName: option.OrderingFieldName,
		CreatedAt:         option.CreatedAt,
	}
}

type DeleteReportResponse struct {
	Performed bool `json:"performed"`
	Conflicts struct {
		Reserved bool        `json:"reserved"`
		Links    []uuid.UUID `json:"links"`
		Pivots   []uuid.UUID `json:"pivots"`
	} `json:"conflicts"`
}

func AdaptDeleteReport(report datamodel.DeleteReport) DeleteReportResponse {
	response := DeleteReportResponse{
		Performed: report.Performed,
	}
	response.Conflicts.Reserved = report.Conflicts.Reserved
	response.Conflicts.Links = report.Conflicts.Links
	response.Conflicts.Pivots = report.Conflicts.Pivots
	return response
}

type AssembledDataModelResponse struct {
	Tables map[string]AssembledTableResponse `json:"tables"`
	Pivots []AssembledPivotResponse          `json:"pivots"`
}

type AssembledTableResponse struct {
	ID                uuid.UUID                         `json:"id"`
	Name              string                            `json:"name"`
	Description       string                            `json:"description"`
	Alias             string                            `json:"alias"`
	SemanticType      string                            `json:"semantic_type"`
	CaptionField      string                            `json:"caption_field"`
	Fields            map[string]AssembledFieldResponse `json:"fields"`
	LinksToSingle     map[string]AssembledLinkResponse  `json:"links_to_single"`
	NavigationOptions []datamodel.NavigationOption      `json:"navigation_options"`
	Options           *TableOptionsResponse             `json:"options,omitempty"`
}

type AssembledFieldResponse struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	DataType    string    `json:"data_type"`
	Nullable    bool      `json:"nullable"`
	IsEnum      bool      `json:"is_enum"`
	IsUnique    bool      `json:"is_unique"`
	EnumValues  []FieldEnumValueResponse `json:"enum_values"`
}

type AssembledLinkResponse struct {
	ID              uuid.UUID `json:"id"`
	Name            string    `json:"name"`
	ParentTableID   uuid.UUID `json:"parent_table_id"`
	ParentFieldID   uuid.UUID `json:"parent_field_id"`
	ChildTableID    uuid.UUID `json:"child_table_id"`
	ChildFieldID    uuid.UUID `json:"child_field_id"`
	ParentTableName string    `json:"parent_table_name"`
	ParentFieldName string    `json:"parent_field_name"`
	ChildTableName  string    `json:"child_table_name"`
	ChildFieldName  string    `json:"child_field_name"`
}

type AssembledPivotResponse struct {
	ID          uuid.UUID   `json:"id"`
	BaseTableID uuid.UUID   `json:"base_table_id"`
	BaseTable   string      `json:"base_table"`
	FieldID     *uuid.UUID  `json:"field_id"`
	Field       string      `json:"field"`
	PathLinkIDs []uuid.UUID `json:"path_link_ids"`
	PathLinks   []string    `json:"path_links"`
}

func AdaptAssembledDataModel(model datamodel.AssembledDataModel) AssembledDataModelResponse {
	response := AssembledDataModelResponse{
		Tables: make(map[string]AssembledTableResponse, len(model.Tables)),
		Pivots: make([]AssembledPivotResponse, len(model.Pivots)),
	}
	for key, table := range model.Tables {
		var options *TableOptionsResponse
		if table.Options != nil {
			adapted := AdaptTableOptions(*table.Options)
			options = &adapted
		}
		fields := make(map[string]AssembledFieldResponse, len(table.Fields))
		for fieldKey, field := range table.Fields {
			fields[fieldKey] = AssembledFieldResponse{
				ID:          field.ID,
				Name:        field.Name,
				Description: field.Description,
				DataType:    string(field.DataType),
				Nullable:    field.Nullable,
				IsEnum:      field.IsEnum,
				IsUnique:    field.IsUnique,
				EnumValues:  adaptFieldEnumValues(field.EnumValues),
			}
		}
		links := make(map[string]AssembledLinkResponse, len(table.LinksToSingle))
		for linkKey, link := range table.LinksToSingle {
			links[linkKey] = AssembledLinkResponse{
				ID:              link.ID,
				Name:            link.Name,
				ParentTableID:   link.ParentTableID,
				ParentFieldID:   link.ParentFieldID,
				ChildTableID:    link.ChildTableID,
				ChildFieldID:    link.ChildFieldID,
				ParentTableName: link.ParentTableName,
				ParentFieldName: link.ParentFieldName,
				ChildTableName:  link.ChildTableName,
				ChildFieldName:  link.ChildFieldName,
			}
		}
		response.Tables[key] = AssembledTableResponse{
			ID:                table.ID,
			Name:              table.Name,
			Description:       table.Description,
			Alias:             table.Alias,
			SemanticType:      table.SemanticType,
			CaptionField:      table.CaptionField,
			Fields:            fields,
			LinksToSingle:     links,
			NavigationOptions: table.NavigationOptions,
			Options:           options,
		}
	}
	for i, pivot := range model.Pivots {
		response.Pivots[i] = AssembledPivotResponse{
			ID:          pivot.ID,
			BaseTableID: pivot.BaseTableID,
			BaseTable:   pivot.BaseTable,
			FieldID:     pivot.FieldID,
			Field:       pivot.Field,
			PathLinkIDs: pivot.PathLinkIDs,
			PathLinks:   pivot.PathLinks,
		}
	}
	return response
}

func adaptFieldEnumValues(values []datamodel.FieldEnumValue) []FieldEnumValueResponse {
	response := make([]FieldEnumValueResponse, len(values))
	for i, value := range values {
		response[i] = AdaptFieldEnumValue(value)
	}
	return response
}
