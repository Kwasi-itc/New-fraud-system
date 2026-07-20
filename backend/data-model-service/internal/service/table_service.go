package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/riverjobs"
)

type TableService struct {
	tenantRepository ports.TenantRepository
	tableRepository  ports.TableRepository
	fieldRepository  ports.FieldRepository
	linkRepository   ports.LinkRepository
	pivotRepository  ports.PivotRepository
	schemaChanges    ports.SchemaChangeRepository
	schemaManager    ports.SchemaManager
	txManager        ports.TransactionManager
	idGenerator      ports.IDGenerator
	clock            ports.Clock
	indexJobEnqueuer riverjobs.IndexJobEnqueuer
}

type CreateTableInput struct {
	TenantID     uuid.UUID
	Name         string
	Description  string
	Alias        string
	SemanticType string
}

type UpdateTableInput struct {
	TableID      uuid.UUID
	Description  *string
	Alias        *string
	SemanticType *string
	CaptionField *string
}

func NewTableService(
	tenantRepository ports.TenantRepository,
	tableRepository ports.TableRepository,
	fieldRepository ports.FieldRepository,
	linkRepository ports.LinkRepository,
	pivotRepository ports.PivotRepository,
	schemaChanges ports.SchemaChangeRepository,
	schemaManager ports.SchemaManager,
	txManager ports.TransactionManager,
	idGenerator ports.IDGenerator,
	clock ports.Clock,
) TableService {
	return TableService{
		tenantRepository: tenantRepository,
		tableRepository:  tableRepository,
		fieldRepository:  fieldRepository,
		linkRepository:   linkRepository,
		pivotRepository:  pivotRepository,
		schemaChanges:    schemaChanges,
		schemaManager:    schemaManager,
		txManager:        txManager,
		idGenerator:      idGenerator,
		clock:            clock,
	}
}

func (s TableService) WithIndexJobEnqueuer(enqueuer riverjobs.IndexJobEnqueuer) TableService {
	if enqueuer == nil {
		enqueuer = riverjobs.NoopIndexJobEnqueuer{}
	}
	s.indexJobEnqueuer = enqueuer
	return s
}

func (s TableService) Get(ctx context.Context, tableID uuid.UUID) (datamodel.Table, error) {
	return s.tableRepository.GetByID(ctx, tableID)
}

func (s TableService) ListByTenant(ctx context.Context, tenantID uuid.UUID) ([]datamodel.Table, error) {
	return s.tableRepository.ListByTenant(ctx, tenantID)
}

func (s TableService) Create(ctx context.Context, input CreateTableInput) (datamodel.Table, error) {
	if err := datamodel.ValidateTableCreate(input.Name); err != nil {
		return datamodel.Table{}, err
	}
	if err := datamodel.ValidateSemanticType(input.SemanticType); err != nil {
		return datamodel.Table{}, err
	}

	tenantRecord, err := s.tenantRepository.GetByID(ctx, input.TenantID)
	if err != nil {
		return datamodel.Table{}, err
	}
	if tenantRecord.Status != tenant.StatusActive {
		return datamodel.Table{}, fmt.Errorf("tenant must be active before creating tables")
	}

	now := s.clock.Now()
	table := datamodel.Table{
		ID:           s.idGenerator.New(),
		TenantID:     input.TenantID,
		Name:         datamodel.NormalizeName(input.Name),
		Description:  input.Description,
		Alias:        input.Alias,
		SemanticType: datamodel.NormalizeName(input.SemanticType),
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	defaultFields := []datamodel.Field{
		{
			ID:          s.idGenerator.New(),
			TenantID:    input.TenantID,
			TableID:     table.ID,
			Name:        "object_id",
			Description: fmt.Sprintf("required id on all objects in the %s table", table.Name),
			DataType:    datamodel.DataTypeString,
			Nullable:    false,
			IsEnum:      false,
			IsUnique:    true,
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		{
			ID:          s.idGenerator.New(),
			TenantID:    input.TenantID,
			TableID:     table.ID,
			Name:        "updated_at",
			Description: fmt.Sprintf("required timestamp on all objects in the %s table", table.Name),
			DataType:    datamodel.DataTypeTimestamp,
			Nullable:    false,
			IsEnum:      false,
			IsUnique:    false,
			CreatedAt:   now,
			UpdatedAt:   now,
		},
	}

	if err := s.txManager.Run(ctx, func(store ports.MutationStore) error {
		if err := store.Tables().Create(ctx, table); err != nil {
			return err
		}
		if err := store.SchemaManager().CreateTable(ctx, tenantRecord, table); err != nil {
			return err
		}
		for _, field := range defaultFields {
			if err := store.Fields().Create(ctx, field); err != nil {
				return err
			}
		}
		if err := store.SchemaManager().CreateUniqueIndex(ctx, tenantRecord, table, []string{"object_id"}); err != nil {
			return err
		}
		_ = store.SchemaChanges().Create(ctx, newSchemaChange(
			s.idGenerator.New(),
			table.TenantID,
			"create_table",
			"table",
			table.ID,
			now,
			map[string]any{
				"name":          table.Name,
				"description":   table.Description,
				"alias":         table.Alias,
				"semantic_type": table.SemanticType,
				"default_fields": []string{
					"object_id",
					"updated_at",
				},
			},
		))
		recordTenantSchemaMigration(ctx, store.TenantSchemaMigrations(), s.idGenerator, table.TenantID, schemaMigrationVersion("create_table", "table"), now)
		return nil
	}); err != nil {
		return datamodel.Table{}, err
	}

	return table, nil
}

func (s TableService) Update(ctx context.Context, input UpdateTableInput) (datamodel.Table, error) {
	table, err := s.tableRepository.GetByID(ctx, input.TableID)
	if err != nil {
		return datamodel.Table{}, err
	}

	fields, err := s.fieldRepository.ListByTable(ctx, table.ID)
	if err != nil {
		return datamodel.Table{}, err
	}

	if input.Description != nil {
		table.Description = *input.Description
	}
	if input.Alias != nil {
		table.Alias = *input.Alias
	}
	if input.SemanticType != nil {
		if err := datamodel.ValidateSemanticType(*input.SemanticType); err != nil {
			return datamodel.Table{}, err
		}
		table.SemanticType = datamodel.NormalizeName(*input.SemanticType)
	}
	if input.CaptionField != nil {
		captionField := datamodel.NormalizeName(*input.CaptionField)
		if captionField != "" {
			field, ok := findTableFieldByName(fields, captionField)
			if !ok {
				return datamodel.Table{}, fmt.Errorf("caption field %s not found on table %s", captionField, table.Name)
			}
			if field.DataType != datamodel.DataTypeString {
				return datamodel.Table{}, fmt.Errorf("caption field must be a string field")
			}
		}
		table.CaptionField = captionField
	}
	table.UpdatedAt = s.clock.Now()

	if err := s.txManager.Run(ctx, func(store ports.MutationStore) error {
		if err := store.Tables().Update(ctx, table); err != nil {
			return err
		}
		details := map[string]any{
			"description":   table.Description,
			"alias":         table.Alias,
			"semantic_type": table.SemanticType,
			"caption_field": table.CaptionField,
		}
		if table.CaptionField != "" {
			indexJob, requested, err := ensureManagedIndexJobTx(
				ctx,
				store,
				s.indexJobEnqueuer,
				s.idGenerator,
				table.TenantID,
				table,
				datamodel.IndexJobTypeSearch,
				[]string{table.CaptionField},
				"update_table_caption_field",
				table.UpdatedAt,
			)
			if err != nil {
				return err
			}
			if requested {
				details["search_index_job_id"] = indexJob.ID
				details["search_index_requested"] = true
			}
		}
		_ = store.SchemaChanges().Create(ctx, newSchemaChange(
			s.idGenerator.New(),
			table.TenantID,
			"update_table",
			"table",
			table.ID,
			table.UpdatedAt,
			details,
		))
		return nil
	}); err != nil {
		return datamodel.Table{}, err
	}

	return table, nil
}

func (s TableService) Delete(ctx context.Context, tableID uuid.UUID, dryRun bool) (datamodel.DeleteReport, error) {
	report := datamodel.NewDeleteReport()

	table, err := s.tableRepository.GetByID(ctx, tableID)
	if err != nil {
		return report, err
	}
	tenantRecord, err := s.tenantRepository.GetByID(ctx, table.TenantID)
	if err != nil {
		return report, err
	}

	links, err := s.linkRepository.ListByTenant(ctx, table.TenantID)
	if err != nil {
		return report, err
	}
	for _, link := range links {
		if link.ParentTable == tableID || link.ChildTable == tableID {
			report.Conflicts.Links = append(report.Conflicts.Links, link.ID)
		}
	}

	pivots, err := s.pivotRepository.ListByTenant(ctx, table.TenantID)
	if err != nil {
		return report, err
	}
	for _, pivot := range pivots {
		if pivot.BaseTableID == tableID {
			report.Conflicts.Pivots = append(report.Conflicts.Pivots, pivot.ID)
		}
	}

	if len(report.Conflicts.Links) > 0 || len(report.Conflicts.Pivots) > 0 {
		return report, fmt.Errorf("table has internal conflicts")
	}
	if dryRun {
		return report, nil
	}

	if err := s.txManager.Run(ctx, func(store ports.MutationStore) error {
		if err := store.SchemaManager().DropTable(ctx, tenantRecord, table); err != nil {
			return err
		}
		if err := store.Tables().Delete(ctx, tableID); err != nil {
			return err
		}
		_ = store.SchemaChanges().Create(ctx, newSchemaChange(
			s.idGenerator.New(),
			table.TenantID,
			"delete_table",
			"table",
			table.ID,
			s.clock.Now(),
			map[string]any{
				"name": table.Name,
			},
		))
		recordTenantSchemaMigration(ctx, store.TenantSchemaMigrations(), s.idGenerator, table.TenantID, schemaMigrationVersion("delete_table", "table"), s.clock.Now())
		return nil
	}); err != nil {
		return report, err
	}
	report.Performed = true
	return report, nil
}

func findTableFieldByName(fields []datamodel.Field, name string) (datamodel.Field, bool) {
	for _, field := range fields {
		if field.Name == name {
			return field, true
		}
	}

	return datamodel.Field{}, false
}
