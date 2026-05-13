package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
)

type OptionsService struct {
	tableRepository   ports.TableRepository
	fieldRepository   ports.FieldRepository
	optionsRepository ports.TableOptionsRepository
	schemaChanges     ports.SchemaChangeRepository
	txManager         ports.TransactionManager
	idGenerator       ports.IDGenerator
	clock             ports.Clock
}

func NewOptionsService(
	tableRepository ports.TableRepository,
	fieldRepository ports.FieldRepository,
	optionsRepository ports.TableOptionsRepository,
	schemaChanges ports.SchemaChangeRepository,
	txManager ports.TransactionManager,
	idGenerator ports.IDGenerator,
	clock ports.Clock,
) OptionsService {
	return OptionsService{
		tableRepository:   tableRepository,
		fieldRepository:   fieldRepository,
		optionsRepository: optionsRepository,
		schemaChanges:     schemaChanges,
		txManager:         txManager,
		idGenerator:       idGenerator,
		clock:             clock,
	}
}

func (s OptionsService) Get(ctx context.Context, tableID uuid.UUID) (datamodel.TableOptions, error) {
	table, err := s.tableRepository.GetByID(ctx, tableID)
	if err != nil {
		return datamodel.TableOptions{}, err
	}
	fields, err := s.fieldRepository.ListByTable(ctx, table.ID)
	if err != nil {
		return datamodel.TableOptions{}, err
	}
	options, err := s.optionsRepository.GetByTableID(ctx, tableID)
	if err != nil {
		return datamodel.TableOptions{}, err
	}
	if options == nil {
		options = &datamodel.TableOptions{
			ID:              s.idGenerator.New(),
			TenantID:        table.TenantID,
			TableID:         tableID,
			DisplayedFields: []uuid.UUID{},
			FieldOrder:      []uuid.UUID{},
			UpdatedAt:       s.clock.Now(),
		}
	}
	options.FieldOrder = datamodel.SortFieldOrder(fields, *options)
	return *options, nil
}

func (s OptionsService) Upsert(ctx context.Context, input datamodel.TableOptions) (datamodel.TableOptions, error) {
	table, err := s.tableRepository.GetByID(ctx, input.TableID)
	if err != nil {
		return datamodel.TableOptions{}, err
	}
	fields, err := s.fieldRepository.ListByTable(ctx, table.ID)
	if err != nil {
		return datamodel.TableOptions{}, err
	}

	validFieldIDs := make(map[uuid.UUID]struct{}, len(fields))
	for _, field := range fields {
		validFieldIDs[field.ID] = struct{}{}
	}
	for _, fieldID := range input.DisplayedFields {
		if _, ok := validFieldIDs[fieldID]; !ok {
			return datamodel.TableOptions{}, fmt.Errorf("displayed field does not belong to table")
		}
	}
	for _, fieldID := range input.FieldOrder {
		if _, ok := validFieldIDs[fieldID]; !ok {
			return datamodel.TableOptions{}, fmt.Errorf("ordered field does not belong to table")
		}
	}

	if input.ID == uuid.Nil {
		input.ID = s.idGenerator.New()
	}
	input.TenantID = table.TenantID
	input.UpdatedAt = s.clock.Now()

	if err := s.txManager.Run(ctx, func(store ports.MutationStore) error {
		if err := store.TableOptions().Upsert(ctx, input); err != nil {
			return err
		}
		_ = store.SchemaChanges().Create(ctx, newSchemaChange(
			s.idGenerator.New(),
			input.TenantID,
			"upsert_table_options",
			"table_options",
			input.TableID,
			input.UpdatedAt,
			map[string]any{
				"displayed_fields": input.DisplayedFields,
				"field_order":      input.FieldOrder,
			},
		))
		recordTenantSchemaMigration(ctx, store.TenantSchemaMigrations(), s.idGenerator, input.TenantID, schemaMigrationVersion("upsert_table_options", "table_options"), input.UpdatedAt)
		return nil
	}); err != nil {
		return datamodel.TableOptions{}, err
	}

	input.FieldOrder = datamodel.SortFieldOrder(fields, input)
	return input, nil
}
