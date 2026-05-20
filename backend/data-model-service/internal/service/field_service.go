package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
)

type FieldService struct {
	tenantRepository ports.TenantRepository
	tableRepository  ports.TableRepository
	fieldRepository  ports.FieldRepository
	fieldEnumValueRepository ports.FieldEnumValueRepository
	linkRepository   ports.LinkRepository
	pivotRepository  ports.PivotRepository
	schemaChanges    ports.SchemaChangeRepository
	schemaManager    ports.SchemaManager
	txManager        ports.TransactionManager
	idGenerator      ports.IDGenerator
	clock            ports.Clock
}

type CreateFieldInput struct {
	TableID     uuid.UUID
	Name        string
	Description string
	DataType    datamodel.DataType
	Nullable    bool
	IsEnum      bool
	IsUnique    bool
	EnumValues  []CreateFieldEnumValueSeed
}

type CreateFieldEnumValueSeed struct {
	Value     string
	Label     string
	SortOrder int
}

type UpdateFieldInput struct {
	FieldID     uuid.UUID
	Description *string
	Nullable    *bool
	IsEnum      *bool
	IsUnique    *bool
}

func NewFieldService(
	tenantRepository ports.TenantRepository,
	tableRepository ports.TableRepository,
	fieldRepository ports.FieldRepository,
	fieldEnumValueRepository ports.FieldEnumValueRepository,
	linkRepository ports.LinkRepository,
	pivotRepository ports.PivotRepository,
	schemaChanges ports.SchemaChangeRepository,
	schemaManager ports.SchemaManager,
	txManager ports.TransactionManager,
	idGenerator ports.IDGenerator,
	clock ports.Clock,
) FieldService {
	return FieldService{
		tenantRepository: tenantRepository,
		tableRepository:  tableRepository,
		fieldRepository:  fieldRepository,
		fieldEnumValueRepository: fieldEnumValueRepository,
		linkRepository:   linkRepository,
		pivotRepository:  pivotRepository,
		schemaChanges:    schemaChanges,
		schemaManager:    schemaManager,
		txManager:        txManager,
		idGenerator:      idGenerator,
		clock:            clock,
	}
}

func (s FieldService) ListByTable(ctx context.Context, tableID uuid.UUID) ([]datamodel.Field, error) {
	return s.fieldRepository.ListByTable(ctx, tableID)
}

func (s FieldService) Create(ctx context.Context, input CreateFieldInput) (datamodel.Field, error) {
	if err := datamodel.ValidateFieldCreate(input.Name, input.DataType, input.IsEnum, input.IsUnique); err != nil {
		return datamodel.Field{}, err
	}
	if len(input.EnumValues) > 0 && !input.IsEnum {
		return datamodel.Field{}, fmt.Errorf("enum_values can only be provided when is_enum=true")
	}

	table, err := s.tableRepository.GetByID(ctx, input.TableID)
	if err != nil {
		return datamodel.Field{}, err
	}
	tenantRecord, err := s.tenantRepository.GetByID(ctx, table.TenantID)
	if err != nil {
		return datamodel.Field{}, err
	}

	now := s.clock.Now()
	field := datamodel.Field{
		ID:          s.idGenerator.New(),
		TenantID:    table.TenantID,
		TableID:     input.TableID,
		Name:        datamodel.NormalizeName(input.Name),
		Description: input.Description,
		DataType:    input.DataType,
		Nullable:    input.Nullable,
		IsEnum:      input.IsEnum,
		IsUnique:    input.IsUnique,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := s.txManager.Run(ctx, func(store ports.MutationStore) error {
		if err := store.Fields().Create(ctx, field); err != nil {
			return err
		}
		if err := store.SchemaManager().AddField(ctx, tenantRecord, table, field); err != nil {
			return err
		}
		if field.IsUnique {
			if err := store.SchemaManager().CreateUniqueIndex(ctx, tenantRecord, table, []string{field.Name}); err != nil {
				return err
			}
		}
		for _, seed := range input.EnumValues {
			if err := datamodel.ValidateEnumValueCreate(field, seed.Value, seed.Label); err != nil {
				return err
			}
			value := datamodel.FieldEnumValue{
				ID:        s.idGenerator.New(),
				TenantID:  field.TenantID,
				FieldID:   field.ID,
				Value:     seed.Value,
				Label:     seed.Label,
				SortOrder: seed.SortOrder,
				CreatedAt: now,
				UpdatedAt: now,
			}
			if err := store.FieldEnumValues().Create(ctx, value); err != nil {
				return err
			}
			_ = store.SchemaChanges().Create(ctx, newSchemaChange(
				s.idGenerator.New(),
				field.TenantID,
				"create_field_enum_value",
				"field_enum_value",
				value.ID,
				now,
				map[string]any{
					"field_id":   field.ID,
					"value":      value.Value,
					"label":      value.Label,
					"sort_order": value.SortOrder,
				},
			))
			recordTenantSchemaMigration(ctx, store.TenantSchemaMigrations(), s.idGenerator, field.TenantID, schemaMigrationVersion("create_field_enum_value", "field_enum_value"), now)
		}
		_ = store.SchemaChanges().Create(ctx, newSchemaChange(
			s.idGenerator.New(),
			field.TenantID,
			"create_field",
			"field",
			field.ID,
			now,
			map[string]any{
				"table_id":    field.TableID,
				"name":        field.Name,
				"data_type":   field.DataType,
				"nullable":    field.Nullable,
				"is_enum":     field.IsEnum,
				"is_unique":   field.IsUnique,
				"description": field.Description,
			},
		))
		recordTenantSchemaMigration(ctx, store.TenantSchemaMigrations(), s.idGenerator, field.TenantID, schemaMigrationVersion("create_field", "field"), now)
		return nil
	}); err != nil {
		return datamodel.Field{}, err
	}

	return field, nil
}

func (s FieldService) Update(ctx context.Context, input UpdateFieldInput) (datamodel.Field, error) {
	field, err := s.fieldRepository.GetByID(ctx, input.FieldID)
	if err != nil {
		return datamodel.Field{}, err
	}

	if err := datamodel.ValidateFieldUpdate(field, field.DataType, input.IsEnum, input.IsUnique, input.Nullable); err != nil {
		return datamodel.Field{}, err
	}

	table, err := s.tableRepository.GetByID(ctx, field.TableID)
	if err != nil {
		return datamodel.Field{}, err
	}
	tenantRecord, err := s.tenantRepository.GetByID(ctx, table.TenantID)
	if err != nil {
		return datamodel.Field{}, err
	}

	previousUnique := field.IsUnique
	if input.Description != nil {
		field.Description = *input.Description
	}
	if input.Nullable != nil {
		field.Nullable = *input.Nullable
	}
	if input.IsEnum != nil {
		if field.IsEnum && !*input.IsEnum {
			enumValues, err := s.fieldEnumValueRepository.ListByField(ctx, field.ID)
			if err != nil {
				return datamodel.Field{}, err
			}
			if len(enumValues) > 0 {
				return datamodel.Field{}, fmt.Errorf("remove enum values before unsetting is_enum")
			}
		}
		field.IsEnum = *input.IsEnum
	}
	if input.IsUnique != nil {
		field.IsUnique = *input.IsUnique
	}
	field.UpdatedAt = s.clock.Now()

	if err := s.txManager.Run(ctx, func(store ports.MutationStore) error {
		if err := store.Fields().Update(ctx, field); err != nil {
			return err
		}

		if !previousUnique && field.IsUnique {
			if err := store.SchemaManager().CreateUniqueIndex(ctx, tenantRecord, table, []string{field.Name}); err != nil {
				return err
			}
		}
		if previousUnique && !field.IsUnique {
			if err := store.SchemaManager().DropUniqueIndex(ctx, tenantRecord, table, []string{field.Name}); err != nil {
				return err
			}
		}
		_ = store.SchemaChanges().Create(ctx, newSchemaChange(
			s.idGenerator.New(),
			field.TenantID,
			"update_field",
			"field",
			field.ID,
			field.UpdatedAt,
			map[string]any{
				"table_id":    field.TableID,
				"name":        field.Name,
				"nullable":    field.Nullable,
				"is_enum":     field.IsEnum,
				"is_unique":   field.IsUnique,
				"description": field.Description,
			},
		))
		recordTenantSchemaMigration(ctx, store.TenantSchemaMigrations(), s.idGenerator, field.TenantID, schemaMigrationVersion("update_field", "field"), field.UpdatedAt)
		return nil
	}); err != nil {
		return datamodel.Field{}, err
	}

	return field, nil
}

func (s FieldService) Delete(ctx context.Context, fieldID uuid.UUID, dryRun bool) (datamodel.DeleteReport, error) {
	report := datamodel.NewDeleteReport()

	field, err := s.fieldRepository.GetByID(ctx, fieldID)
	if err != nil {
		return report, err
	}
	table, err := s.tableRepository.GetByID(ctx, field.TableID)
	if err != nil {
		return report, err
	}
	tenantRecord, err := s.tenantRepository.GetByID(ctx, table.TenantID)
	if err != nil {
		return report, err
	}

	if field.Name == "object_id" || field.Name == "updated_at" {
		report.Conflicts.Reserved = true
		return report, fmt.Errorf("field is reserved")
	}

	links, err := s.linkRepository.ListByTenant(ctx, table.TenantID)
	if err != nil {
		return report, err
	}
	for _, link := range links {
		if link.ParentField == fieldID || link.ChildField == fieldID {
			report.Conflicts.Links = append(report.Conflicts.Links, link.ID)
		}
	}

	pivots, err := s.pivotRepository.ListByTenant(ctx, table.TenantID)
	if err != nil {
		return report, err
	}
	for _, pivot := range pivots {
		if pivot.FieldID != nil && *pivot.FieldID == fieldID {
			report.Conflicts.Pivots = append(report.Conflicts.Pivots, pivot.ID)
		}
	}

	if report.Conflicts.Reserved || len(report.Conflicts.Links) > 0 || len(report.Conflicts.Pivots) > 0 {
		return report, fmt.Errorf("field has internal conflicts")
	}
	if dryRun {
		return report, nil
	}

	if err := s.txManager.Run(ctx, func(store ports.MutationStore) error {
		if field.IsUnique {
			if err := store.SchemaManager().DropUniqueIndex(ctx, tenantRecord, table, []string{field.Name}); err != nil {
				return err
			}
		}
		if err := store.SchemaManager().DropField(ctx, tenantRecord, table, field); err != nil {
			return err
		}
		if err := store.Fields().Delete(ctx, fieldID); err != nil {
			return err
		}
		_ = store.SchemaChanges().Create(ctx, newSchemaChange(
			s.idGenerator.New(),
			field.TenantID,
			"delete_field",
			"field",
			field.ID,
			s.clock.Now(),
			map[string]any{
				"table_id":  field.TableID,
				"name":      field.Name,
				"data_type": field.DataType,
			},
		))
		recordTenantSchemaMigration(ctx, store.TenantSchemaMigrations(), s.idGenerator, field.TenantID, schemaMigrationVersion("delete_field", "field"), s.clock.Now())
		return nil
	}); err != nil {
		return report, err
	}
	report.Performed = true
	return report, nil
}
