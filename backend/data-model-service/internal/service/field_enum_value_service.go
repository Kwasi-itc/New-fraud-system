package service

import (
	"context"

	"github.com/google/uuid"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
)

type FieldEnumValueService struct {
	fieldRepository     ports.FieldRepository
	enumValueRepository ports.FieldEnumValueRepository
	schemaChanges       ports.SchemaChangeRepository
	txManager           ports.TransactionManager
	idGenerator         ports.IDGenerator
	clock               ports.Clock
}

type CreateFieldEnumValueInput struct {
	FieldID    uuid.UUID
	Value      string
	Label      string
	SortOrder  int
}

type UpdateFieldEnumValueInput struct {
	EnumValueID uuid.UUID
	Value       *string
	Label       *string
	SortOrder   *int
}

func NewFieldEnumValueService(
	fieldRepository ports.FieldRepository,
	enumValueRepository ports.FieldEnumValueRepository,
	schemaChanges ports.SchemaChangeRepository,
	txManager ports.TransactionManager,
	idGenerator ports.IDGenerator,
	clock ports.Clock,
) FieldEnumValueService {
	return FieldEnumValueService{
		fieldRepository:     fieldRepository,
		enumValueRepository: enumValueRepository,
		schemaChanges:       schemaChanges,
		txManager:           txManager,
		idGenerator:         idGenerator,
		clock:               clock,
	}
}

func (s FieldEnumValueService) List(ctx context.Context, fieldID uuid.UUID) ([]datamodel.FieldEnumValue, error) {
	field, err := s.fieldRepository.GetByID(ctx, fieldID)
	if err != nil {
		return nil, err
	}
	if !field.IsEnum {
		return []datamodel.FieldEnumValue{}, nil
	}
	return s.enumValueRepository.ListByField(ctx, fieldID)
}

func (s FieldEnumValueService) Create(ctx context.Context, input CreateFieldEnumValueInput) (datamodel.FieldEnumValue, error) {
	field, err := s.fieldRepository.GetByID(ctx, input.FieldID)
	if err != nil {
		return datamodel.FieldEnumValue{}, err
	}
	if err := datamodel.ValidateEnumValueCreate(field, input.Value, input.Label); err != nil {
		return datamodel.FieldEnumValue{}, err
	}

	now := s.clock.Now()
	value := datamodel.FieldEnumValue{
		ID:        s.idGenerator.New(),
		TenantID:  field.TenantID,
		FieldID:   field.ID,
		Value:     input.Value,
		Label:     input.Label,
		SortOrder: input.SortOrder,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.txManager.Run(ctx, func(store ports.MutationStore) error {
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
		return nil
	}); err != nil {
		return datamodel.FieldEnumValue{}, err
	}
	return value, nil
}

func (s FieldEnumValueService) Update(ctx context.Context, input UpdateFieldEnumValueInput) (datamodel.FieldEnumValue, error) {
	value, err := s.enumValueRepository.GetByID(ctx, input.EnumValueID)
	if err != nil {
		return datamodel.FieldEnumValue{}, err
	}
	field, err := s.fieldRepository.GetByID(ctx, value.FieldID)
	if err != nil {
		return datamodel.FieldEnumValue{}, err
	}
	if err := datamodel.ValidateEnumValueUpdate(field, input.Value, input.Label); err != nil {
		return datamodel.FieldEnumValue{}, err
	}
	if input.Value != nil {
		value.Value = *input.Value
	}
	if input.Label != nil {
		value.Label = *input.Label
	}
	if input.SortOrder != nil {
		value.SortOrder = *input.SortOrder
	}
	value.UpdatedAt = s.clock.Now()

	if err := s.txManager.Run(ctx, func(store ports.MutationStore) error {
		if err := store.FieldEnumValues().Update(ctx, value); err != nil {
			return err
		}
		_ = store.SchemaChanges().Create(ctx, newSchemaChange(
			s.idGenerator.New(),
			field.TenantID,
			"update_field_enum_value",
			"field_enum_value",
			value.ID,
			value.UpdatedAt,
			map[string]any{
				"field_id":   field.ID,
				"value":      value.Value,
				"label":      value.Label,
				"sort_order": value.SortOrder,
			},
		))
		recordTenantSchemaMigration(ctx, store.TenantSchemaMigrations(), s.idGenerator, field.TenantID, schemaMigrationVersion("update_field_enum_value", "field_enum_value"), value.UpdatedAt)
		return nil
	}); err != nil {
		return datamodel.FieldEnumValue{}, err
	}
	return value, nil
}

func (s FieldEnumValueService) Delete(ctx context.Context, enumValueID uuid.UUID) error {
	value, err := s.enumValueRepository.GetByID(ctx, enumValueID)
	if err != nil {
		return err
	}
	field, err := s.fieldRepository.GetByID(ctx, value.FieldID)
	if err != nil {
		return err
	}
	now := s.clock.Now()
	return s.txManager.Run(ctx, func(store ports.MutationStore) error {
		if err := store.FieldEnumValues().Delete(ctx, enumValueID); err != nil {
			return err
		}
		_ = store.SchemaChanges().Create(ctx, newSchemaChange(
			s.idGenerator.New(),
			field.TenantID,
			"delete_field_enum_value",
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
		recordTenantSchemaMigration(ctx, store.TenantSchemaMigrations(), s.idGenerator, field.TenantID, schemaMigrationVersion("delete_field_enum_value", "field_enum_value"), now)
		return nil
	})
}
