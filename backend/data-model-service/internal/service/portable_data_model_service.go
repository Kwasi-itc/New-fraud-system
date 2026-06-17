package service

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
)

const portableDataModelVersion = "v1"

type PortableDataModelService struct {
	readService             DataModelReadService
	tableService            TableService
	fieldService            FieldService
	linkService             LinkService
	pivotService            PivotService
	optionsService          OptionsService
	navigationOptionService NavigationOptionService
}

type PortableDataModelDocument struct {
	Version    string
	RevisionID string
	Tables     []PortableTable
	Links      []PortableLink
	Pivots     []PortablePivot
}

type PortableTable struct {
	Name              string
	Description       string
	Alias             string
	SemanticType      string
	CaptionField      string
	Fields            []PortableField
	Options           *PortableTableOptions
	NavigationOptions []PortableNavigationOption
}

type PortableField struct {
	Name        string
	Description string
	DataType    string
	Nullable    bool
	IsEnum      bool
	IsUnique    bool
	EnumValues  []CreateFieldEnumValueSeed
}

type PortableTableOptions struct {
	DisplayedFields []string
	FieldOrder      []string
}

type PortableNavigationOption struct {
	SourceField   string
	TargetTable   string
	FilterField   string
	OrderingField string
}

type PortableLink struct {
	Name        string
	ParentTable string
	ParentField string
	ChildTable  string
	ChildField  string
}

type PortablePivot struct {
	BaseTable string
	Field     string
	PathLinks []string
}

type PortableImportResult struct {
	TablesCreated            int
	FieldsCreated            int
	LinksCreated             int
	PivotsCreated            int
	TableOptionsUpserted     int
	NavigationOptionsCreated int
	RevisionID               string
}

func NewPortableDataModelService(
	readService DataModelReadService,
	tableService TableService,
	fieldService FieldService,
	linkService LinkService,
	pivotService PivotService,
	optionsService OptionsService,
	navigationOptionService NavigationOptionService,
) PortableDataModelService {
	return PortableDataModelService{
		readService:             readService,
		tableService:            tableService,
		fieldService:            fieldService,
		linkService:             linkService,
		pivotService:            pivotService,
		optionsService:          optionsService,
		navigationOptionService: navigationOptionService,
	}
}

func (s PortableDataModelService) Export(ctx context.Context, tenantID uuid.UUID) (PortableDataModelDocument, error) {
	published, err := s.readService.Get(ctx, tenantID)
	if err != nil {
		return PortableDataModelDocument{}, err
	}

	tableNames := make([]string, 0, len(published.Model.Tables))
	for name := range published.Model.Tables {
		tableNames = append(tableNames, name)
	}
	sort.Strings(tableNames)

	document := PortableDataModelDocument{
		Version:    portableDataModelVersion,
		RevisionID: published.RevisionID,
		Tables:     make([]PortableTable, 0, len(tableNames)),
		Links:      make([]PortableLink, 0),
		Pivots:     make([]PortablePivot, 0, len(published.Model.Pivots)),
	}

	seenLinks := make(map[uuid.UUID]struct{})
	for _, tableName := range tableNames {
		table := published.Model.Tables[tableName]
		fieldNames := make([]string, 0, len(table.Fields))
		for name := range table.Fields {
			if isManagedPortableField(name) {
				continue
			}
			fieldNames = append(fieldNames, name)
		}
		sort.Strings(fieldNames)

		fields := make([]PortableField, 0, len(fieldNames))
		for _, fieldName := range fieldNames {
			field := table.Fields[fieldName]
			enumValues := make([]CreateFieldEnumValueSeed, len(field.EnumValues))
			for i, value := range field.EnumValues {
				enumValues[i] = CreateFieldEnumValueSeed{
					Value:     value.Value,
					Label:     value.Label,
					SortOrder: value.SortOrder,
				}
			}
			slices.SortFunc(enumValues, func(lhs, rhs CreateFieldEnumValueSeed) int {
				if lhs.SortOrder == rhs.SortOrder {
					return strings.Compare(lhs.Value, rhs.Value)
				}
				return lhs.SortOrder - rhs.SortOrder
			})
			fields = append(fields, PortableField{
				Name:        field.Name,
				Description: field.Description,
				DataType:    string(field.DataType),
				Nullable:    field.Nullable,
				IsEnum:      field.IsEnum,
				IsUnique:    field.IsUnique,
				EnumValues:  enumValues,
			})
		}

		var options *PortableTableOptions
		if table.Options != nil {
			options = &PortableTableOptions{
				DisplayedFields: fieldNamesForIDs(table.Options.DisplayedFields, table.Fields),
				FieldOrder:      fieldNamesForIDs(table.Options.FieldOrder, table.Fields),
			}
		}

		navigationOptions := make([]PortableNavigationOption, len(table.NavigationOptions))
		for i, option := range table.NavigationOptions {
			navigationOptions[i] = PortableNavigationOption{
				SourceField:   option.SourceFieldName,
				TargetTable:   option.TargetTableName,
				FilterField:   option.FilterFieldName,
				OrderingField: option.OrderingFieldName,
			}
		}
		slices.SortFunc(navigationOptions, func(lhs, rhs PortableNavigationOption) int {
			if lhs.TargetTable == rhs.TargetTable {
				if lhs.SourceField == rhs.SourceField {
					if lhs.FilterField == rhs.FilterField {
						return strings.Compare(lhs.OrderingField, rhs.OrderingField)
					}
					return strings.Compare(lhs.FilterField, rhs.FilterField)
				}
				return strings.Compare(lhs.SourceField, rhs.SourceField)
			}
			return strings.Compare(lhs.TargetTable, rhs.TargetTable)
		})

		document.Tables = append(document.Tables, PortableTable{
			Name:              table.Name,
			Description:       table.Description,
			Alias:             table.Alias,
			SemanticType:      table.SemanticType,
			CaptionField:      table.CaptionField,
			Fields:            fields,
			Options:           options,
			NavigationOptions: navigationOptions,
		})

		linkNames := make([]string, 0, len(table.LinksToSingle))
		for linkName := range table.LinksToSingle {
			linkNames = append(linkNames, linkName)
		}
		sort.Strings(linkNames)
		for _, linkName := range linkNames {
			link := table.LinksToSingle[linkName]
			if _, ok := seenLinks[link.ID]; ok {
				continue
			}
			seenLinks[link.ID] = struct{}{}
			document.Links = append(document.Links, PortableLink{
				Name:        link.Name,
				ParentTable: link.ParentTableName,
				ParentField: link.ParentFieldName,
				ChildTable:  link.ChildTableName,
				ChildField:  link.ChildFieldName,
			})
		}
	}
	slices.SortFunc(document.Links, func(lhs, rhs PortableLink) int {
		if lhs.Name == rhs.Name {
			if lhs.ChildTable == rhs.ChildTable {
				return strings.Compare(lhs.ParentTable, rhs.ParentTable)
			}
			return strings.Compare(lhs.ChildTable, rhs.ChildTable)
		}
		return strings.Compare(lhs.Name, rhs.Name)
	})

	for _, pivot := range published.Model.Pivots {
		document.Pivots = append(document.Pivots, PortablePivot{
			BaseTable: pivot.BaseTable,
			Field:     pivot.Field,
			PathLinks: append([]string(nil), pivot.PathLinks...),
		})
	}
	slices.SortFunc(document.Pivots, func(lhs, rhs PortablePivot) int {
		if lhs.BaseTable == rhs.BaseTable {
			if lhs.Field == rhs.Field {
				return strings.Compare(strings.Join(lhs.PathLinks, ","), strings.Join(rhs.PathLinks, ","))
			}
			return strings.Compare(lhs.Field, rhs.Field)
		}
		return strings.Compare(lhs.BaseTable, rhs.BaseTable)
	})

	return document, nil
}

func (s PortableDataModelService) Import(ctx context.Context, tenantID uuid.UUID, document PortableDataModelDocument) (PortableImportResult, error) {
	if err := validatePortableDocument(document); err != nil {
		return PortableImportResult{}, err
	}

	result := PortableImportResult{}
	tableIDs := make(map[string]uuid.UUID, len(document.Tables))
	fieldIDs := make(map[string]uuid.UUID)
	linkIDs := make(map[string]uuid.UUID, len(document.Links))

	for _, table := range document.Tables {
		createdTable, err := s.tableService.Create(ctx, CreateTableInput{
			TenantID:     tenantID,
			Name:         table.Name,
			Description:  table.Description,
			Alias:        table.Alias,
			SemanticType: table.SemanticType,
		})
		if err != nil {
			return result, fmt.Errorf("create table %s: %w", table.Name, err)
		}
		result.TablesCreated++
		tableIDs[createdTable.Name] = createdTable.ID

		if err := s.refreshFieldMap(ctx, createdTable.ID, createdTable.Name, fieldIDs); err != nil {
			return result, err
		}

		for _, field := range table.Fields {
			dataType, err := datamodel.ParseDataType(field.DataType)
			if err != nil {
				return result, fmt.Errorf("parse field %s.%s data type: %w", table.Name, field.Name, err)
			}
			createdField, err := s.fieldService.Create(ctx, CreateFieldInput{
				TableID:     createdTable.ID,
				Name:        field.Name,
				Description: field.Description,
				DataType:    dataType,
				Nullable:    field.Nullable,
				IsEnum:      field.IsEnum,
				IsUnique:    field.IsUnique,
				EnumValues:  field.EnumValues,
			})
			if err != nil {
				return result, fmt.Errorf("create field %s.%s: %w", table.Name, field.Name, err)
			}
			result.FieldsCreated++
			fieldIDs[fieldKey(createdTable.Name, createdField.Name)] = createdField.ID
		}
	}

	for _, table := range document.Tables {
		if strings.TrimSpace(table.CaptionField) == "" {
			continue
		}
		tableID, ok := tableIDs[datamodel.NormalizeName(table.Name)]
		if !ok {
			return result, fmt.Errorf("table not found while applying caption field: %s", table.Name)
		}
		captionField := datamodel.NormalizeName(table.CaptionField)
		if _, ok := fieldIDs[fieldKey(datamodel.NormalizeName(table.Name), captionField)]; !ok {
			return result, fmt.Errorf("caption field %s not found on table %s", table.CaptionField, table.Name)
		}
		if _, err := s.tableService.Update(ctx, UpdateTableInput{
			TableID:      tableID,
			CaptionField: &captionField,
		}); err != nil {
			return result, fmt.Errorf("update table %s caption field: %w", table.Name, err)
		}
	}

	for _, link := range document.Links {
		parentTable := datamodel.NormalizeName(link.ParentTable)
		childTable := datamodel.NormalizeName(link.ChildTable)
		createdLink, err := s.linkService.Create(ctx, CreateLinkInput{
			TenantID:    tenantID,
			Name:        link.Name,
			ParentTable: tableIDs[parentTable],
			ParentField: fieldIDs[fieldKey(parentTable, link.ParentField)],
			ChildTable:  tableIDs[childTable],
			ChildField:  fieldIDs[fieldKey(childTable, link.ChildField)],
		})
		if err != nil {
			return result, fmt.Errorf("create link %s: %w", link.Name, err)
		}
		result.LinksCreated++
		linkIDs[createdLink.Name] = createdLink.ID
	}

	for _, pivot := range document.Pivots {
		baseTable := datamodel.NormalizeName(pivot.BaseTable)
		input := CreatePivotInput{
			TenantID:    tenantID,
			BaseTableID: tableIDs[baseTable],
		}
		if strings.TrimSpace(pivot.Field) != "" {
			fieldID, ok := fieldIDs[fieldKey(baseTable, pivot.Field)]
			if !ok {
				return result, fmt.Errorf("pivot field %s.%s not found", pivot.BaseTable, pivot.Field)
			}
			input.FieldID = &fieldID
		}
		if len(pivot.PathLinks) > 0 {
			input.PathLinkIDs = make([]uuid.UUID, len(pivot.PathLinks))
			for i, linkName := range pivot.PathLinks {
				linkID, ok := linkIDs[datamodel.NormalizeName(linkName)]
				if !ok {
					return result, fmt.Errorf("pivot path link %s not found", linkName)
				}
				input.PathLinkIDs[i] = linkID
			}
		}
		if _, err := s.pivotService.Create(ctx, input); err != nil {
			return result, fmt.Errorf("create pivot on table %s: %w", pivot.BaseTable, err)
		}
		result.PivotsCreated++
	}

	for _, table := range document.Tables {
		tableName := datamodel.NormalizeName(table.Name)
		tableID := tableIDs[tableName]

		if table.Options != nil {
			optionsInput := datamodel.TableOptions{
				TableID:         tableID,
				DisplayedFields: make([]uuid.UUID, len(table.Options.DisplayedFields)),
				FieldOrder:      make([]uuid.UUID, len(table.Options.FieldOrder)),
			}
			for i, fieldName := range table.Options.DisplayedFields {
				fieldID, ok := fieldIDs[fieldKey(tableName, fieldName)]
				if !ok {
					return result, fmt.Errorf("displayed field %s.%s not found", table.Name, fieldName)
				}
				optionsInput.DisplayedFields[i] = fieldID
			}
			for i, fieldName := range table.Options.FieldOrder {
				fieldID, ok := fieldIDs[fieldKey(tableName, fieldName)]
				if !ok {
					return result, fmt.Errorf("field order entry %s.%s not found", table.Name, fieldName)
				}
				optionsInput.FieldOrder[i] = fieldID
			}
			if _, err := s.optionsService.Upsert(ctx, optionsInput); err != nil {
				return result, fmt.Errorf("upsert options for table %s: %w", table.Name, err)
			}
			result.TableOptionsUpserted++
		}

		for _, option := range table.NavigationOptions {
			targetTable := datamodel.NormalizeName(option.TargetTable)
			if _, err := s.navigationOptionService.Create(ctx, CreateNavigationOptionInput{
				TenantID:        tenantID,
				SourceTableID:   tableID,
				SourceFieldID:   fieldIDs[fieldKey(tableName, option.SourceField)],
				TargetTableID:   tableIDs[targetTable],
				FilterFieldID:   fieldIDs[fieldKey(targetTable, option.FilterField)],
				OrderingFieldID: fieldIDs[fieldKey(targetTable, option.OrderingField)],
			}); err != nil {
				return result, fmt.Errorf("create navigation option on table %s: %w", table.Name, err)
			}
			result.NavigationOptionsCreated++
		}
	}

	published, err := s.readService.Get(ctx, tenantID)
	if err != nil {
		return result, fmt.Errorf("read revision after import: %w", err)
	}
	result.RevisionID = published.RevisionID

	return result, nil
}

func (s PortableDataModelService) refreshFieldMap(ctx context.Context, tableID uuid.UUID, tableName string, fieldIDs map[string]uuid.UUID) error {
	fields, err := s.fieldService.ListByTable(ctx, tableID)
	if err != nil {
		return fmt.Errorf("list fields for table %s: %w", tableName, err)
	}
	for _, field := range fields {
		fieldIDs[fieldKey(tableName, field.Name)] = field.ID
	}
	return nil
}

func validatePortableDocument(document PortableDataModelDocument) error {
	if version := strings.TrimSpace(document.Version); version != "" && version != portableDataModelVersion {
		return fmt.Errorf("unsupported portable data model version: %s", document.Version)
	}

	tableNames := make(map[string]struct{}, len(document.Tables))
	fieldNamesByTable := make(map[string]map[string]struct{}, len(document.Tables))
	linkNames := make(map[string]struct{}, len(document.Links))

	for _, table := range document.Tables {
		tableName := datamodel.NormalizeName(table.Name)
		if tableName == "" {
			return fmt.Errorf("table name is required")
		}
		if _, ok := tableNames[tableName]; ok {
			return fmt.Errorf("duplicate table name: %s", table.Name)
		}
		tableNames[tableName] = struct{}{}

		fieldNames := map[string]struct{}{
			"object_id":  {},
			"updated_at": {},
		}
		for _, field := range table.Fields {
			fieldName := datamodel.NormalizeName(field.Name)
			if fieldName == "" {
				return fmt.Errorf("field name is required on table %s", table.Name)
			}
			if _, ok := fieldNames[fieldName]; ok {
				return fmt.Errorf("duplicate field name %s on table %s", field.Name, table.Name)
			}
			fieldNames[fieldName] = struct{}{}
		}
		fieldNamesByTable[tableName] = fieldNames
	}

	for _, link := range document.Links {
		name := datamodel.NormalizeName(link.Name)
		if name == "" {
			return fmt.Errorf("link name is required")
		}
		if _, ok := linkNames[name]; ok {
			return fmt.Errorf("duplicate link name: %s", link.Name)
		}
		linkNames[name] = struct{}{}
		if err := requirePortableFieldReference(tableNames, fieldNamesByTable, link.ParentTable, link.ParentField, "link parent"); err != nil {
			return err
		}
		if err := requirePortableFieldReference(tableNames, fieldNamesByTable, link.ChildTable, link.ChildField, "link child"); err != nil {
			return err
		}
	}

	for _, pivot := range document.Pivots {
		baseTable := datamodel.NormalizeName(pivot.BaseTable)
		if _, ok := tableNames[baseTable]; !ok {
			return fmt.Errorf("pivot base table not found: %s", pivot.BaseTable)
		}
		if strings.TrimSpace(pivot.Field) != "" && len(pivot.PathLinks) > 0 {
			return fmt.Errorf("pivot on table %s cannot define both field and path_links", pivot.BaseTable)
		}
		if strings.TrimSpace(pivot.Field) != "" {
			if err := requirePortableFieldReference(tableNames, fieldNamesByTable, pivot.BaseTable, pivot.Field, "pivot"); err != nil {
				return err
			}
		}
		for _, linkName := range pivot.PathLinks {
			if _, ok := linkNames[datamodel.NormalizeName(linkName)]; !ok {
				return fmt.Errorf("pivot path link not found: %s", linkName)
			}
		}
	}

	for _, table := range document.Tables {
		tableName := datamodel.NormalizeName(table.Name)
		for _, option := range table.NavigationOptions {
			if err := requirePortableFieldReference(tableNames, fieldNamesByTable, table.Name, option.SourceField, "navigation option source"); err != nil {
				return err
			}
			if err := requirePortableFieldReference(tableNames, fieldNamesByTable, option.TargetTable, option.FilterField, "navigation option filter"); err != nil {
				return err
			}
			if err := requirePortableFieldReference(tableNames, fieldNamesByTable, option.TargetTable, option.OrderingField, "navigation option ordering"); err != nil {
				return err
			}
		}
		if table.Options != nil {
			for _, fieldName := range table.Options.DisplayedFields {
				if _, ok := fieldNamesByTable[tableName][datamodel.NormalizeName(fieldName)]; !ok {
					return fmt.Errorf("displayed field not found on table %s: %s", table.Name, fieldName)
				}
			}
			for _, fieldName := range table.Options.FieldOrder {
				if _, ok := fieldNamesByTable[tableName][datamodel.NormalizeName(fieldName)]; !ok {
					return fmt.Errorf("field order entry not found on table %s: %s", table.Name, fieldName)
				}
			}
		}
	}

	return nil
}

func requirePortableFieldReference(
	tableNames map[string]struct{},
	fieldNamesByTable map[string]map[string]struct{},
	tableName string,
	fieldName string,
	contextLabel string,
) error {
	normalizedTable := datamodel.NormalizeName(tableName)
	if _, ok := tableNames[normalizedTable]; !ok {
		return fmt.Errorf("%s table not found: %s", contextLabel, tableName)
	}
	normalizedField := datamodel.NormalizeName(fieldName)
	if _, ok := fieldNamesByTable[normalizedTable][normalizedField]; !ok {
		return fmt.Errorf("%s field not found: %s.%s", contextLabel, tableName, fieldName)
	}
	return nil
}

func fieldNamesForIDs(fieldIDs []uuid.UUID, fields map[string]datamodel.AssembledField) []string {
	if len(fieldIDs) == 0 {
		return []string{}
	}
	idToName := make(map[uuid.UUID]string, len(fields))
	for _, field := range fields {
		idToName[field.ID] = field.Name
	}
	names := make([]string, 0, len(fieldIDs))
	for _, fieldID := range fieldIDs {
		name, ok := idToName[fieldID]
		if !ok {
			continue
		}
		names = append(names, name)
	}
	return names
}

func fieldKey(tableName, fieldName string) string {
	return datamodel.NormalizeName(tableName) + "." + datamodel.NormalizeName(fieldName)
}

func isManagedPortableField(name string) bool {
	switch datamodel.NormalizeName(name) {
	case "id", "object_id", "updated_at", "valid_from", "valid_until":
		return true
	default:
		return false
	}
}
