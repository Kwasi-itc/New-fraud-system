package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
)

type DataModelReadRepository struct {
	tableRepository            TableRepository
	fieldRepository            FieldRepository
	fieldEnumValueRepository   FieldEnumValueRepository
	linkRepository             LinkRepository
	pivotRepository            PivotRepository
	tableOptionsRepository     TableOptionsRepository
	navigationOptionRepository NavigationOptionRepository
}

func NewDataModelReadRepository(db *pgxpool.Pool) DataModelReadRepository {
	return DataModelReadRepository{
		tableRepository:            NewTableRepository(db),
		fieldRepository:            NewFieldRepository(db),
		fieldEnumValueRepository:   NewFieldEnumValueRepository(db),
		linkRepository:             NewLinkRepository(db),
		pivotRepository:            NewPivotRepository(db),
		tableOptionsRepository:     NewTableOptionsRepository(db),
		navigationOptionRepository: NewNavigationOptionRepository(db),
	}
}

func (r DataModelReadRepository) GetAssembledDataModel(ctx context.Context, tenantID uuid.UUID) (datamodel.AssembledDataModel, error) {
	tables, err := r.tableRepository.ListByTenant(ctx, tenantID)
	if err != nil {
		return datamodel.AssembledDataModel{}, err
	}
	links, err := r.linkRepository.ListByTenant(ctx, tenantID)
	if err != nil {
		return datamodel.AssembledDataModel{}, err
	}
	pivots, err := r.pivotRepository.ListByTenant(ctx, tenantID)
	if err != nil {
		return datamodel.AssembledDataModel{}, err
	}
	navigationOptions, err := r.navigationOptionRepository.ListByTenant(ctx, tenantID)
	if err != nil {
		return datamodel.AssembledDataModel{}, err
	}

	tableByID := make(map[uuid.UUID]datamodel.Table, len(tables))
	fieldByID := make(map[uuid.UUID]datamodel.Field)
	result := datamodel.AssembledDataModel{
		Tables: map[string]datamodel.AssembledTable{},
		Pivots: make([]datamodel.AssembledPivot, 0, len(pivots)),
	}

	for _, table := range tables {
		tableByID[table.ID] = table
		options, err := r.tableOptionsRepository.GetByTableID(ctx, table.ID)
		if err != nil {
			return datamodel.AssembledDataModel{}, err
		}
		result.Tables[table.Name] = datamodel.AssembledTable{
			ID:                table.ID,
			Name:              table.Name,
			Description:       table.Description,
			Alias:             table.Alias,
			SemanticType:      table.SemanticType,
			CaptionField:      table.CaptionField,
			Archived:          table.Archived,
			Fields:            map[string]datamodel.AssembledField{},
			LinksToSingle:     map[string]datamodel.AssembledLink{},
			NavigationOptions: []datamodel.NavigationOption{},
			Options:           options,
		}
		fields, err := r.fieldRepository.ListByTable(ctx, table.ID)
		if err != nil {
			return datamodel.AssembledDataModel{}, err
		}
		for _, field := range fields {
			fieldByID[field.ID] = field
			assembledTable := result.Tables[table.Name]
			assembledTable.Fields[field.Name] = datamodel.AssembledField{
				ID:          field.ID,
				Name:        field.Name,
				Description: field.Description,
				DataType:    field.DataType,
				Nullable:    field.Nullable,
				IsEnum:      field.IsEnum,
				IsUnique:    field.IsUnique,
				Archived:    field.Archived,
				EnumValues:  []datamodel.FieldEnumValue{},
			}
			if field.IsEnum {
				enumValues, err := r.fieldEnumValueRepository.ListByField(ctx, field.ID)
				if err != nil {
					return datamodel.AssembledDataModel{}, err
				}
				assembledField := assembledTable.Fields[field.Name]
				assembledField.EnumValues = enumValues
				assembledTable.Fields[field.Name] = assembledField
			}
			result.Tables[table.Name] = assembledTable
		}
	}

	for _, link := range links {
		parentTable, ok := tableByID[link.ParentTable]
		if !ok {
			return datamodel.AssembledDataModel{}, fmt.Errorf("parent table not found while assembling links")
		}
		childTable, ok := tableByID[link.ChildTable]
		if !ok {
			return datamodel.AssembledDataModel{}, fmt.Errorf("child table not found while assembling links")
		}
		parentField := fieldByID[link.ParentField]
		childField := fieldByID[link.ChildField]

		assembledChildTable := result.Tables[childTable.Name]
		assembledChildTable.LinksToSingle[link.Name] = datamodel.AssembledLink{
			ID:              link.ID,
			Name:            link.Name,
			ParentTableID:   link.ParentTable,
			ParentFieldID:   link.ParentField,
			ChildTableID:    link.ChildTable,
			ChildFieldID:    link.ChildField,
			ParentTableName: parentTable.Name,
			ParentFieldName: parentField.Name,
			ChildTableName:  childTable.Name,
			ChildFieldName:  childField.Name,
		}
		result.Tables[childTable.Name] = assembledChildTable
	}

	for _, pivot := range pivots {
		baseTable := tableByID[pivot.BaseTableID]
		assembledPivot := datamodel.AssembledPivot{
			ID:          pivot.ID,
			BaseTableID: pivot.BaseTableID,
			BaseTable:   baseTable.Name,
			FieldID:     pivot.FieldID,
			PathLinkIDs: pivot.PathLinkIDs,
			PathLinks:   make([]string, 0, len(pivot.PathLinkIDs)),
		}
		if pivot.FieldID != nil {
			field := fieldByID[*pivot.FieldID]
			assembledPivot.Field = field.Name
		} else if len(pivot.PathLinkIDs) > 0 {
			for _, pathLinkID := range pivot.PathLinkIDs {
				for _, link := range links {
					if link.ID == pathLinkID {
						assembledPivot.PathLinks = append(assembledPivot.PathLinks, link.Name)
						assembledPivot.Field = fieldByID[link.ParentField].Name
						break
					}
				}
			}
		}
		result.Pivots = append(result.Pivots, assembledPivot)
	}

	for _, option := range navigationOptions {
		sourceTable, ok := tableByID[option.SourceTableID]
		if !ok {
			return datamodel.AssembledDataModel{}, fmt.Errorf("source table not found while assembling navigation options")
		}
		targetTable := tableByID[option.TargetTableID]
		sourceField := fieldByID[option.SourceFieldID]
		filterField := fieldByID[option.FilterFieldID]
		orderingField := fieldByID[option.OrderingFieldID]

		option.SourceTableName = sourceTable.Name
		option.SourceFieldName = sourceField.Name
		option.TargetTableName = targetTable.Name
		option.FilterFieldName = filterField.Name
		option.OrderingFieldName = orderingField.Name

		assembledTable := result.Tables[sourceTable.Name]
		assembledTable.NavigationOptions = append(assembledTable.NavigationOptions, option)
		result.Tables[sourceTable.Name] = assembledTable
	}

	return result, nil
}
