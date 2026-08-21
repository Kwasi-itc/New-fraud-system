package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
)

type EventSchemaRepository struct {
	db *pgxpool.Pool
}

func NewEventSchemaRepository(db *pgxpool.Pool) EventSchemaRepository {
	return EventSchemaRepository{db: db}
}

func (r EventSchemaRepository) Lock(ctx context.Context, tenantID, tableID uuid.UUID, expectedRevision string, lockedAt time.Time) (datamodel.Table, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return datamodel.Table{}, fmt.Errorf("begin event schema lock: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var storedRevision string
	var storedLockedAt *time.Time
	var storageClass datamodel.StorageClass
	var storedTenantID uuid.UUID
	if err := tx.QueryRow(ctx, `
		SELECT tenant_id, storage_class, event_schema_revision, event_schema_locked_at
		FROM core.model_tables
		WHERE id = $1
		FOR UPDATE
	`, tableID).Scan(&storedTenantID, &storageClass, &storedRevision, &storedLockedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return datamodel.Table{}, err
		}
		return datamodel.Table{}, fmt.Errorf("lock event table metadata: %w", err)
	}
	if storedTenantID != tenantID {
		return datamodel.Table{}, pgx.ErrNoRows
	}
	if storageClass != datamodel.StorageClassEvent {
		return datamodel.Table{}, fmt.Errorf("table is not configured for event storage")
	}
	if storedLockedAt != nil {
		if storedRevision != expectedRevision {
			return datamodel.Table{}, datamodel.ErrEventSchemaRevisionMismatch
		}
		table, err := NewTableRepository(tx).GetByID(ctx, tableID)
		if err != nil {
			return datamodel.Table{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return datamodel.Table{}, fmt.Errorf("commit existing event schema lock: %w", err)
		}
		return table, nil
	}

	tableRepository := NewTableRepository(tx)
	fieldRepository := NewFieldRepository(tx)
	enumRepository := NewFieldEnumValueRepository(tx)
	table, err := tableRepository.GetByID(ctx, tableID)
	if err != nil {
		return datamodel.Table{}, err
	}
	fields, err := fieldRepository.ListByTable(ctx, tableID)
	if err != nil {
		return datamodel.Table{}, err
	}
	assembled := datamodel.AssembledTable{
		ID: table.ID, Name: table.Name, StorageClass: table.StorageClass,
		EventTimeField: table.EventTimeField, Fields: make(map[string]datamodel.AssembledField, len(fields)),
	}
	for _, field := range fields {
		enumValues, err := enumRepository.ListByField(ctx, field.ID)
		if err != nil {
			return datamodel.Table{}, err
		}
		assembled.Fields[field.Name] = datamodel.AssembledField{
			ID: field.ID, Name: field.Name, DataType: field.DataType, Nullable: field.Nullable,
			IsEnum: field.IsEnum, IsUnique: field.IsUnique, IsProjection: field.IsProjection,
			AggregationMode: field.AggregationMode, AggregationColdBehavior: field.AggregationColdBehavior,
			AggregationDefaultValue: field.AggregationDefaultValue,
			Archived:                field.Archived, EnumValues: enumValues,
		}
	}
	actualRevision := datamodel.BuildEventSchemaRevision(assembled)
	if actualRevision != expectedRevision {
		return datamodel.Table{}, datamodel.ErrEventSchemaRevisionMismatch
	}
	if _, err := tx.Exec(ctx, `
		UPDATE core.model_tables
		SET event_schema_revision = $2, event_schema_locked_at = $3
		WHERE id = $1
	`, tableID, expectedRevision, lockedAt); err != nil {
		return datamodel.Table{}, fmt.Errorf("persist event schema lock: %w", err)
	}
	table.EventSchemaRevision = expectedRevision
	table.EventSchemaLockedAt = &lockedAt
	if err := tx.Commit(ctx); err != nil {
		return datamodel.Table{}, fmt.Errorf("commit event schema lock: %w", err)
	}
	return table, nil
}
