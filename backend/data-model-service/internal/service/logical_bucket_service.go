package service

import (
	"context"
	"fmt"
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/google/uuid"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
)

const (
	initialBucketSealDelay     = 48 * time.Hour
	defaultBucketRetiringGrace = 5 * time.Minute
	maxActiveBucketsPerTable   = 3
)

type LogicalBucketService struct {
	tenants     ports.TenantRepository
	tables      ports.TableRepository
	fields      ports.FieldRepository
	buckets     ports.LogicalBucketRepository
	indexJobs   IndexJobService
	txManager   ports.TransactionManager
	schema      ports.SchemaManager
	idGenerator ports.IDGenerator
	clock       ports.Clock
}

type CreateLogicalBucketInput struct {
	TenantID         uuid.UUID
	TableID          uuid.UUID
	TimestampFieldID uuid.UUID
	Timezone         string
}

func NewLogicalBucketService(
	tenants ports.TenantRepository,
	tables ports.TableRepository,
	fields ports.FieldRepository,
	buckets ports.LogicalBucketRepository,
	indexJobs IndexJobService,
	txManager ports.TransactionManager,
	schema ports.SchemaManager,
	idGenerator ports.IDGenerator,
	clock ports.Clock,
) LogicalBucketService {
	return LogicalBucketService{
		tenants: tenants, tables: tables, fields: fields, buckets: buckets,
		indexJobs: indexJobs, txManager: txManager, schema: schema,
		idGenerator: idGenerator, clock: clock,
	}
}

func (s LogicalBucketService) Create(ctx context.Context, input CreateLogicalBucketInput) (datamodel.LogicalBucketDefinition, error) {
	tenantRecord, err := s.tenants.GetByID(ctx, input.TenantID)
	if err != nil {
		return datamodel.LogicalBucketDefinition{}, err
	}
	if tenantRecord.Status != tenant.StatusActive {
		return datamodel.LogicalBucketDefinition{}, fmt.Errorf("tenant must be active before creating logical buckets")
	}
	table, err := s.tables.GetByID(ctx, input.TableID)
	if err != nil {
		return datamodel.LogicalBucketDefinition{}, err
	}
	if table.TenantID != input.TenantID {
		return datamodel.LogicalBucketDefinition{}, fmt.Errorf("table does not belong to tenant")
	}
	field, err := s.fields.GetByID(ctx, input.TimestampFieldID)
	if err != nil {
		return datamodel.LogicalBucketDefinition{}, err
	}
	if field.TableID != table.ID || field.TenantID != input.TenantID {
		return datamodel.LogicalBucketDefinition{}, fmt.Errorf("timestamp field does not belong to table")
	}
	if field.DataType != datamodel.DataTypeTimestamp {
		return datamodel.LogicalBucketDefinition{}, fmt.Errorf("logical bucket field must be a timestamp")
	}
	if field.Nullable {
		return datamodel.LogicalBucketDefinition{}, fmt.Errorf("logical bucket timestamp field must be non-nullable")
	}
	timezone := strings.TrimSpace(input.Timezone)
	if timezone == "" {
		return datamodel.LogicalBucketDefinition{}, fmt.Errorf("timezone is required")
	}
	if _, err := time.LoadLocation(timezone); err != nil {
		return datamodel.LogicalBucketDefinition{}, fmt.Errorf("timezone must be a valid IANA timezone: %w", err)
	}

	existing, err := s.buckets.ListByTable(ctx, table.ID)
	if err != nil {
		return datamodel.LogicalBucketDefinition{}, err
	}
	version := 1
	for _, item := range existing {
		if item.TimestampFieldID == field.ID && item.DefinitionVersion >= version {
			version = item.DefinitionVersion + 1
		}
	}
	now := s.clock.Now()
	item := datamodel.LogicalBucketDefinition{
		ID:                 s.idGenerator.New(),
		TenantID:           input.TenantID,
		TableID:            table.ID,
		TimestampFieldID:   field.ID,
		TimestampFieldName: field.Name,
		Grain:              "daily",
		Timezone:           timezone,
		SealDelay:          initialBucketSealDelay,
		DefinitionVersion:  version,
		Status:             datamodel.LogicalBucketStatusPendingIndex,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	if err := s.txManager.Run(ctx, func(store ports.MutationStore) error {
		buckets := s.buckets
		if extended, ok := store.(interface {
			LogicalBuckets() ports.LogicalBucketRepository
		}); ok {
			buckets = extended.LogicalBuckets()
		}
		if err := buckets.LockTable(ctx, table.ID); err != nil {
			return err
		}
		items, err := buckets.ListByTable(ctx, table.ID)
		if err != nil {
			return err
		}
		activeCount := 0
		for _, existing := range items {
			switch existing.Status {
			case datamodel.LogicalBucketStatusPendingIndex,
				datamodel.LogicalBucketStatusActivating,
				datamodel.LogicalBucketStatusActive,
				datamodel.LogicalBucketStatusBlockedData:
				activeCount++
				if existing.TimestampFieldID == field.ID {
					return fmt.Errorf("timestamp field already has an active logical bucket definition")
				}
			}
		}
		if activeCount >= maxActiveBucketsPerTable {
			return fmt.Errorf("table already has the maximum of %d active logical bucket definitions", maxActiveBucketsPerTable)
		}
		if err := buckets.Create(ctx, item); err != nil {
			return err
		}
		recordTenantSchemaMigration(
			ctx,
			store.TenantSchemaMigrations(),
			s.idGenerator,
			item.TenantID,
			schemaMigrationVersion("create_logical_bucket", "logical_bucket"),
			now,
		)
		return nil
	}); err != nil {
		return datamodel.LogicalBucketDefinition{}, err
	}

	job, err := s.indexJobs.Create(ctx, CreateIndexJobInput{
		TenantID:             input.TenantID,
		TableID:              table.ID,
		IndexType:            datamodel.IndexJobTypeLogicalBucket,
		Columns:              []string{field.Name},
		RequestedByOperation: "logical_bucket_activation",
		Method:               "btree",
		OwnerService:         "ingestion",
		SubmittedByService:   "data-model",
		Purpose:              "logical_bucket",
	})
	if err != nil {
		return item, fmt.Errorf("logical bucket created but index request failed: %w", err)
	}
	if err := s.buckets.AttachIndexJob(ctx, item.ID, job.ID, s.clock.Now()); err != nil {
		return item, fmt.Errorf("attach logical bucket index job: %w", err)
	}
	item.IndexJobID = &job.ID
	if current, getErr := s.indexJobs.Get(ctx, job.ID); getErr == nil &&
		current.Status == datamodel.IndexJobStatusApplied {
		return s.activateIndexedDefinition(ctx, item, table)
	}
	return item, nil
}

func (s LogicalBucketService) ListByTable(ctx context.Context, tableID uuid.UUID) ([]datamodel.LogicalBucketDefinition, error) {
	if err := s.buckets.PromoteLifecycle(ctx, s.clock.Now()); err != nil {
		return nil, err
	}
	return s.buckets.ListByTable(ctx, tableID)
}

func (s LogicalBucketService) Get(ctx context.Context, id uuid.UUID) (datamodel.LogicalBucketDefinition, error) {
	if err := s.buckets.PromoteLifecycle(ctx, s.clock.Now()); err != nil {
		return datamodel.LogicalBucketDefinition{}, err
	}
	return s.buckets.GetByID(ctx, id)
}

func (s LogicalBucketService) Retire(ctx context.Context, id uuid.UUID) (datamodel.LogicalBucketDefinition, error) {
	item, err := s.buckets.GetByID(ctx, id)
	if err != nil {
		return datamodel.LogicalBucketDefinition{}, err
	}
	switch item.Status {
	case datamodel.LogicalBucketStatusRetiring, datamodel.LogicalBucketStatusRetired:
		return item, nil
	}
	now := s.clock.Now()
	until := now.Add(defaultBucketRetiringGrace)
	if err := s.buckets.MarkRetiring(ctx, id, until, now); err != nil {
		return datamodel.LogicalBucketDefinition{}, err
	}
	item.Status = datamodel.LogicalBucketStatusRetiring
	item.MaintenanceUntil = &until
	item.UpdatedAt = now
	return item, nil
}

func (s LogicalBucketService) RetryActivation(ctx context.Context, id uuid.UUID) (datamodel.LogicalBucketDefinition, error) {
	item, err := s.buckets.GetByID(ctx, id)
	if err != nil {
		return datamodel.LogicalBucketDefinition{}, err
	}
	if item.Status == datamodel.LogicalBucketStatusPendingIndex {
		table, err := s.tables.GetByID(ctx, item.TableID)
		if err != nil {
			return datamodel.LogicalBucketDefinition{}, err
		}
		var job datamodel.IndexJob
		if item.IndexJobID == nil {
			job, err = s.indexJobs.Create(ctx, CreateIndexJobInput{
				TenantID:             item.TenantID,
				TableID:              item.TableID,
				IndexType:            datamodel.IndexJobTypeLogicalBucket,
				Columns:              []string{item.TimestampFieldName},
				RequestedByOperation: "logical_bucket_activation_retry",
				Method:               "btree",
				OwnerService:         "ingestion",
				SubmittedByService:   "data-model",
				Purpose:              "logical_bucket",
			})
			if err != nil {
				return datamodel.LogicalBucketDefinition{}, err
			}
			if err := s.buckets.AttachIndexJob(ctx, item.ID, job.ID, s.clock.Now()); err != nil {
				return datamodel.LogicalBucketDefinition{}, err
			}
			item.IndexJobID = &job.ID
		} else {
			job, err = s.indexJobs.Get(ctx, *item.IndexJobID)
			if err != nil {
				return datamodel.LogicalBucketDefinition{}, err
			}
		}
		switch job.Status {
		case datamodel.IndexJobStatusFailed, datamodel.IndexJobStatusCancelled:
			if _, err := s.indexJobs.Retry(ctx, job.ID); err != nil {
				return datamodel.LogicalBucketDefinition{}, err
			}
			return item, nil
		case datamodel.IndexJobStatusApplied:
			return s.activateIndexedDefinition(ctx, item, table)
		default:
			return item, nil
		}
	}
	if item.Status != datamodel.LogicalBucketStatusBlockedData {
		return datamodel.LogicalBucketDefinition{}, fmt.Errorf("only pending_index or blocked_data definitions can retry activation")
	}
	table, err := s.tables.GetByID(ctx, item.TableID)
	if err != nil {
		return datamodel.LogicalBucketDefinition{}, err
	}
	return s.activateIndexedDefinition(ctx, item, table)
}

func (s LogicalBucketService) activateIndexedDefinition(
	ctx context.Context,
	item datamodel.LogicalBucketDefinition,
	table datamodel.Table,
) (datamodel.LogicalBucketDefinition, error) {
	tenantRecord, err := s.tenants.GetByID(ctx, item.TenantID)
	if err != nil {
		return datamodel.LogicalBucketDefinition{}, err
	}
	inspector, ok := s.schema.(ports.TenantDataInspector)
	if !ok {
		return datamodel.LogicalBucketDefinition{}, fmt.Errorf("tenant data inspector is not configured")
	}
	hasNull, err := inspector.HasNullValue(ctx, tenantRecord, table, item.TimestampFieldName)
	if err != nil {
		return datamodel.LogicalBucketDefinition{}, err
	}
	if hasNull {
		now := s.clock.Now()
		if err := s.buckets.MarkBlockedData(ctx, item.ID, now); err != nil {
			return datamodel.LogicalBucketDefinition{}, err
		}
		item.Status = datamodel.LogicalBucketStatusBlockedData
		item.CacheEligibleAt = nil
		item.UpdatedAt = now
		return datamodel.LogicalBucketDefinition{}, fmt.Errorf("timestamp field still contains null values")
	}
	now := s.clock.Now()
	eligibleAt := now.Add(defaultBucketRetiringGrace)
	if err := s.buckets.MarkActivating(ctx, item.ID, eligibleAt, now); err != nil {
		return datamodel.LogicalBucketDefinition{}, err
	}
	item.Status = datamodel.LogicalBucketStatusActivating
	item.CacheEligibleAt = &eligibleAt
	item.UpdatedAt = now
	return item, nil
}
