package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
)

type PivotService struct {
	tableRepository ports.TableRepository
	fieldRepository ports.FieldRepository
	linkRepository  ports.LinkRepository
	pivotRepository ports.PivotRepository
	schemaChanges   ports.SchemaChangeRepository
	txManager       ports.TransactionManager
	idGenerator     ports.IDGenerator
	clock           ports.Clock
}

type CreatePivotInput struct {
	TenantID    uuid.UUID
	BaseTableID uuid.UUID
	FieldID     *uuid.UUID
	PathLinkIDs []uuid.UUID
}

func NewPivotService(
	tableRepository ports.TableRepository,
	fieldRepository ports.FieldRepository,
	linkRepository ports.LinkRepository,
	pivotRepository ports.PivotRepository,
	schemaChanges ports.SchemaChangeRepository,
	txManager ports.TransactionManager,
	idGenerator ports.IDGenerator,
	clock ports.Clock,
) PivotService {
	return PivotService{
		tableRepository: tableRepository,
		fieldRepository: fieldRepository,
		linkRepository:  linkRepository,
		pivotRepository: pivotRepository,
		schemaChanges:   schemaChanges,
		txManager:       txManager,
		idGenerator:     idGenerator,
		clock:           clock,
	}
}

func (s PivotService) Create(ctx context.Context, input CreatePivotInput) (datamodel.Pivot, error) {
	if err := datamodel.ValidatePivot(input.FieldID, input.PathLinkIDs); err != nil {
		return datamodel.Pivot{}, err
	}

	baseTable, err := s.tableRepository.GetByID(ctx, input.BaseTableID)
	if err != nil {
		return datamodel.Pivot{}, err
	}
	if baseTable.TenantID != input.TenantID {
		return datamodel.Pivot{}, fmt.Errorf("base table does not belong to tenant")
	}

	if input.FieldID != nil {
		field, err := s.fieldRepository.GetByID(ctx, *input.FieldID)
		if err != nil {
			return datamodel.Pivot{}, err
		}
		if field.DataType != datamodel.DataTypeString {
			return datamodel.Pivot{}, fmt.Errorf("pivot field must be a string field")
		}
	}

	if len(input.PathLinkIDs) > 0 {
		links, err := s.linkRepository.ListByTenant(ctx, input.TenantID)
		if err != nil {
			return datamodel.Pivot{}, err
		}
		linkMap := make(map[uuid.UUID]datamodel.Link, len(links))
		for _, link := range links {
			linkMap[link.ID] = link
		}
		first, ok := linkMap[input.PathLinkIDs[0]]
		if !ok || first.ChildTable != input.BaseTableID {
			return datamodel.Pivot{}, fmt.Errorf("first pivot path link must start from the base table")
		}
		for i := 1; i < len(input.PathLinkIDs); i++ {
			prev := linkMap[input.PathLinkIDs[i-1]]
			cur, ok := linkMap[input.PathLinkIDs[i]]
			if !ok {
				return datamodel.Pivot{}, fmt.Errorf("pivot path contains unknown link")
			}
			if prev.ParentTable != cur.ChildTable {
				return datamodel.Pivot{}, fmt.Errorf("pivot path links are not chained consistently")
			}
		}
		last := linkMap[input.PathLinkIDs[len(input.PathLinkIDs)-1]]
		field, err := s.fieldRepository.GetByID(ctx, last.ParentField)
		if err != nil {
			return datamodel.Pivot{}, err
		}
		if field.DataType != datamodel.DataTypeString {
			return datamodel.Pivot{}, fmt.Errorf("pivot field must be a string field")
		}
	}

	pivot := datamodel.Pivot{
		ID:          s.idGenerator.New(),
		TenantID:    input.TenantID,
		BaseTableID: input.BaseTableID,
		FieldID:     input.FieldID,
		PathLinkIDs: input.PathLinkIDs,
		CreatedAt:   s.clock.Now(),
	}
	if err := s.txManager.Run(ctx, func(store ports.MutationStore) error {
		if err := store.Pivots().Create(ctx, pivot); err != nil {
			return err
		}
		_ = store.SchemaChanges().Create(ctx, newSchemaChange(
			s.idGenerator.New(),
			pivot.TenantID,
			"create_pivot",
			"pivot",
			pivot.ID,
			pivot.CreatedAt,
			map[string]any{
				"base_table_id": pivot.BaseTableID,
				"field_id":      pivot.FieldID,
				"path_link_ids": pivot.PathLinkIDs,
			},
		))
		recordTenantSchemaMigration(ctx, store.TenantSchemaMigrations(), s.idGenerator, pivot.TenantID, schemaMigrationVersion("create_pivot", "pivot"), pivot.CreatedAt)
		return nil
	}); err != nil {
		return datamodel.Pivot{}, err
	}
	return pivot, nil
}

func (s PivotService) List(ctx context.Context, tenantID uuid.UUID) ([]datamodel.Pivot, error) {
	return s.pivotRepository.ListByTenant(ctx, tenantID)
}

func (s PivotService) Delete(ctx context.Context, pivotID uuid.UUID, dryRun bool) (datamodel.DeleteReport, error) {
	report := datamodel.NewDeleteReport()
	pivot, err := s.pivotRepository.GetByID(ctx, pivotID)
	if err != nil {
		return report, err
	}
	if dryRun {
		return report, nil
	}
	if err := s.txManager.Run(ctx, func(store ports.MutationStore) error {
		if err := store.Pivots().Delete(ctx, pivotID); err != nil {
			return err
		}
		_ = store.SchemaChanges().Create(ctx, newSchemaChange(
			s.idGenerator.New(),
			pivot.TenantID,
			"delete_pivot",
			"pivot",
			pivot.ID,
			s.clock.Now(),
			map[string]any{
				"base_table_id": pivot.BaseTableID,
				"field_id":      pivot.FieldID,
				"path_link_ids": pivot.PathLinkIDs,
			},
		))
		recordTenantSchemaMigration(ctx, store.TenantSchemaMigrations(), s.idGenerator, pivot.TenantID, schemaMigrationVersion("delete_pivot", "pivot"), s.clock.Now())
		return nil
	}); err != nil {
		return report, err
	}
	report.Performed = true
	return report, nil
}
