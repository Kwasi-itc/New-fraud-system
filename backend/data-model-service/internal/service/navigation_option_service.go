package service

import (
	"context"
	"fmt"
	"slices"

	"github.com/google/uuid"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
)

type NavigationOptionService struct {
	tableRepository       ports.TableRepository
	fieldRepository       ports.FieldRepository
	linkRepository        ports.LinkRepository
	pivotRepository       ports.PivotRepository
	navigationRepository  ports.NavigationOptionRepository
	schemaChanges         ports.SchemaChangeRepository
	txManager             ports.TransactionManager
	idGenerator           ports.IDGenerator
	clock                 ports.Clock
}

type CreateNavigationOptionInput struct {
	TenantID        uuid.UUID
	SourceTableID   uuid.UUID
	SourceFieldID   uuid.UUID
	TargetTableID   uuid.UUID
	FilterFieldID   uuid.UUID
	OrderingFieldID uuid.UUID
}

func NewNavigationOptionService(
	tableRepository ports.TableRepository,
	fieldRepository ports.FieldRepository,
	linkRepository ports.LinkRepository,
	pivotRepository ports.PivotRepository,
	navigationRepository ports.NavigationOptionRepository,
	schemaChanges ports.SchemaChangeRepository,
	txManager ports.TransactionManager,
	idGenerator ports.IDGenerator,
	clock ports.Clock,
) NavigationOptionService {
	return NavigationOptionService{
		tableRepository:      tableRepository,
		fieldRepository:      fieldRepository,
		linkRepository:       linkRepository,
		pivotRepository:      pivotRepository,
		navigationRepository: navigationRepository,
		schemaChanges:        schemaChanges,
		txManager:            txManager,
		idGenerator:          idGenerator,
		clock:                clock,
	}
}

func (s NavigationOptionService) Create(ctx context.Context, input CreateNavigationOptionInput) (datamodel.NavigationOption, error) {
	sourceTable, err := s.tableRepository.GetByID(ctx, input.SourceTableID)
	if err != nil {
		return datamodel.NavigationOption{}, err
	}
	targetTable, err := s.tableRepository.GetByID(ctx, input.TargetTableID)
	if err != nil {
		return datamodel.NavigationOption{}, err
	}
	if sourceTable.TenantID != input.TenantID || targetTable.TenantID != input.TenantID {
		return datamodel.NavigationOption{}, fmt.Errorf("tables do not belong to tenant")
	}

	sourceFields, err := s.fieldRepository.ListByTable(ctx, sourceTable.ID)
	if err != nil {
		return datamodel.NavigationOption{}, err
	}
	targetFields, err := s.fieldRepository.ListByTable(ctx, targetTable.ID)
	if err != nil {
		return datamodel.NavigationOption{}, err
	}

	sourceField, ok := findFieldByID(sourceFields, input.SourceFieldID)
	if !ok {
		return datamodel.NavigationOption{}, fmt.Errorf("source field does not belong to source table")
	}
	filterField, ok := findFieldByID(targetFields, input.FilterFieldID)
	if !ok {
		return datamodel.NavigationOption{}, fmt.Errorf("filter field does not belong to target table")
	}
	orderingField, ok := findFieldByID(targetFields, input.OrderingFieldID)
	if !ok {
		return datamodel.NavigationOption{}, fmt.Errorf("ordering field does not belong to target table")
	}
	if sourceField.DataType != datamodel.DataTypeString {
		return datamodel.NavigationOption{}, fmt.Errorf("source field must be a string field")
	}
	if input.FilterFieldID == input.OrderingFieldID {
		return datamodel.NavigationOption{}, fmt.Errorf("filter and ordering fields must be different")
	}
	if sourceTable.ID == targetTable.ID && sourceField.ID != filterField.ID {
		return datamodel.NavigationOption{}, fmt.Errorf("if source and target tables are the same, source and filter fields must match")
	}

	links, err := s.linkRepository.ListByTenant(ctx, input.TenantID)
	if err != nil {
		return datamodel.NavigationOption{}, err
	}
	pivots, err := s.pivotRepository.ListByTenant(ctx, input.TenantID)
	if err != nil {
		return datamodel.NavigationOption{}, err
	}

	canCreate := false
	for _, link := range links {
		if link.ParentTable == sourceTable.ID &&
			link.ParentField == sourceField.ID &&
			link.ChildTable == targetTable.ID &&
			link.ChildField == filterField.ID {
			canCreate = true
			break
		}
	}
	if !canCreate && sourceTable.ID == targetTable.ID {
		for _, pivot := range pivots {
			if pivot.BaseTableID == sourceTable.ID && pivot.FieldID != nil && *pivot.FieldID == sourceField.ID {
				canCreate = true
				break
			}
		}
	}
	if !canCreate {
		return datamodel.NavigationOption{}, fmt.Errorf("navigation option must be backed by a reverse link or self-table pivot")
	}

	now := s.clock.Now()
	option := datamodel.NavigationOption{
		ID:                s.idGenerator.New(),
		TenantID:          input.TenantID,
		SourceTableID:     sourceTable.ID,
		SourceFieldID:     sourceField.ID,
		TargetTableID:     targetTable.ID,
		FilterFieldID:     filterField.ID,
		OrderingFieldID:   orderingField.ID,
		SourceTableName:   sourceTable.Name,
		SourceFieldName:   sourceField.Name,
		TargetTableName:   targetTable.Name,
		FilterFieldName:   filterField.Name,
		OrderingFieldName: orderingField.Name,
		CreatedAt:         now,
	}

	indexJob := datamodel.IndexJob{
		ID:                   s.idGenerator.New(),
		TenantID:             input.TenantID,
		TableID:              &targetTable.ID,
		TableName:            targetTable.Name,
		IndexType:            datamodel.IndexJobTypeNavigation,
		Columns:              []string{filterField.Name, orderingField.Name},
		Status:               datamodel.IndexJobStatusPending,
		RequestedByOperation: "create_navigation_option",
		RequestedAt:          now,
		DedupeKey:            datamodel.BuildIndexJobDedupeKey(input.TenantID, targetTable.ID, datamodel.IndexJobTypeNavigation, []string{filterField.Name, orderingField.Name}),
	}

	if err := s.txManager.Run(ctx, func(store ports.MutationStore) error {
		if err := store.NavigationOptions().Create(ctx, option); err != nil {
			return err
		}
		if err := store.IndexJobs().Create(ctx, indexJob); err != nil {
			return err
		}
		_ = store.SchemaChanges().Create(ctx, newSchemaChange(
			s.idGenerator.New(),
			input.TenantID,
			"create_navigation_option",
			"navigation_option",
			option.ID,
			now,
			map[string]any{
				"source_table_id":   sourceTable.ID,
				"source_field_id":   sourceField.ID,
				"target_table_id":   targetTable.ID,
				"filter_field_id":   filterField.ID,
				"ordering_field_id": orderingField.ID,
				"index_job_id":      indexJob.ID,
			},
		))
		return nil
	}); err != nil {
		return datamodel.NavigationOption{}, err
	}

	return option, nil
}

func (s NavigationOptionService) ListByTable(ctx context.Context, tableID uuid.UUID) ([]datamodel.NavigationOption, error) {
	return s.navigationRepository.ListBySourceTable(ctx, tableID)
}

func (s NavigationOptionService) Delete(ctx context.Context, id uuid.UUID) error {
	option, err := s.navigationRepository.GetByID(ctx, id)
	if err != nil {
		return err
	}
	return s.txManager.Run(ctx, func(store ports.MutationStore) error {
		if err := store.NavigationOptions().Delete(ctx, id); err != nil {
			return err
		}
		_ = store.SchemaChanges().Create(ctx, newSchemaChange(
			s.idGenerator.New(),
			option.TenantID,
			"delete_navigation_option",
			"navigation_option",
			id,
			s.clock.Now(),
			map[string]any{},
		))
		return nil
	})
}

func findFieldByID(fields []datamodel.Field, fieldID uuid.UUID) (datamodel.Field, bool) {
	index := slices.IndexFunc(fields, func(field datamodel.Field) bool {
		return field.ID == fieldID
	})
	if index == -1 {
		return datamodel.Field{}, false
	}
	return fields[index], true
}
