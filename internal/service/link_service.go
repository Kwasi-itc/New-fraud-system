package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/Kwasi-itc/marble-datamodel-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/marble-datamodel-service/internal/ports"
)

type LinkService struct {
	tableRepository ports.TableRepository
	fieldRepository ports.FieldRepository
	linkRepository  ports.LinkRepository
	pivotRepository ports.PivotRepository
	schemaChanges   ports.SchemaChangeRepository
	txManager       ports.TransactionManager
	idGenerator     ports.IDGenerator
	clock           ports.Clock
}

type CreateLinkInput struct {
	TenantID    uuid.UUID
	Name        string
	ParentTable uuid.UUID
	ParentField uuid.UUID
	ChildTable  uuid.UUID
	ChildField  uuid.UUID
}

func NewLinkService(
	tableRepository ports.TableRepository,
	fieldRepository ports.FieldRepository,
	linkRepository ports.LinkRepository,
	pivotRepository ports.PivotRepository,
	schemaChanges ports.SchemaChangeRepository,
	txManager ports.TransactionManager,
	idGenerator ports.IDGenerator,
	clock ports.Clock,
) LinkService {
	return LinkService{
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

func (s LinkService) Create(ctx context.Context, input CreateLinkInput) (datamodel.Link, error) {
	if err := datamodel.ValidateLinkName(input.Name); err != nil {
		return datamodel.Link{}, err
	}

	parentTable, err := s.tableRepository.GetByID(ctx, input.ParentTable)
	if err != nil {
		return datamodel.Link{}, err
	}
	childTable, err := s.tableRepository.GetByID(ctx, input.ChildTable)
	if err != nil {
		return datamodel.Link{}, err
	}
	parentField, err := s.fieldRepository.GetByID(ctx, input.ParentField)
	if err != nil {
		return datamodel.Link{}, err
	}
	childField, err := s.fieldRepository.GetByID(ctx, input.ChildField)
	if err != nil {
		return datamodel.Link{}, err
	}

	if parentTable.TenantID != input.TenantID || childTable.TenantID != input.TenantID {
		return datamodel.Link{}, fmt.Errorf("tables do not belong to tenant")
	}
	if !parentField.IsUnique {
		return datamodel.Link{}, fmt.Errorf("parent field must be unique")
	}
	if parentField.DataType != datamodel.DataTypeString || childField.DataType != datamodel.DataTypeString {
		return datamodel.Link{}, fmt.Errorf("parent and child link fields must be string")
	}

	link := datamodel.Link{
		ID:          s.idGenerator.New(),
		TenantID:    input.TenantID,
		Name:        datamodel.NormalizeName(input.Name),
		ParentTable: input.ParentTable,
		ParentField: input.ParentField,
		ChildTable:  input.ChildTable,
		ChildField:  input.ChildField,
		CreatedAt:   s.clock.Now(),
	}
	if err := s.txManager.Run(ctx, func(store ports.MutationStore) error {
		if err := store.Links().Create(ctx, link); err != nil {
			return err
		}
		_ = store.SchemaChanges().Create(ctx, newSchemaChange(
			s.idGenerator.New(),
			link.TenantID,
			"create_link",
			"link",
			link.ID,
			link.CreatedAt,
			map[string]any{
				"name":            link.Name,
				"parent_table_id": link.ParentTable,
				"parent_field_id": link.ParentField,
				"child_table_id":  link.ChildTable,
				"child_field_id":  link.ChildField,
			},
		))
		return nil
	}); err != nil {
		return datamodel.Link{}, err
	}
	return link, nil
}

func (s LinkService) Delete(ctx context.Context, linkID uuid.UUID, dryRun bool) (datamodel.DeleteReport, error) {
	report := datamodel.NewDeleteReport()

	link, err := s.linkRepository.GetByID(ctx, linkID)
	if err != nil {
		return report, err
	}
	pivots, err := s.pivotRepository.ListByTenant(ctx, link.TenantID)
	if err != nil {
		return report, err
	}
	for _, pivot := range pivots {
		for _, pathLinkID := range pivot.PathLinkIDs {
			if pathLinkID == linkID {
				report.Conflicts.Pivots = append(report.Conflicts.Pivots, pivot.ID)
				break
			}
		}
	}

	if len(report.Conflicts.Pivots) > 0 {
		return report, fmt.Errorf("link has internal conflicts")
	}
	if dryRun {
		return report, nil
	}
	if err := s.txManager.Run(ctx, func(store ports.MutationStore) error {
		if err := store.Links().Delete(ctx, linkID); err != nil {
			return err
		}
		_ = store.SchemaChanges().Create(ctx, newSchemaChange(
			s.idGenerator.New(),
			link.TenantID,
			"delete_link",
			"link",
			link.ID,
			s.clock.Now(),
			map[string]any{
				"name":            link.Name,
				"parent_table_id": link.ParentTable,
				"parent_field_id": link.ParentField,
				"child_table_id":  link.ChildTable,
				"child_field_id":  link.ChildField,
			},
		))
		return nil
	}); err != nil {
		return report, err
	}
	report.Performed = true
	return report, nil
}
