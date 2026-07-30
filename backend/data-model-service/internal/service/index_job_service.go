package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/riverjobs"
)

type IndexJobService struct {
	tenantRepository ports.TenantRepository
	tableRepository  ports.TableRepository
	fieldRepository  ports.FieldRepository
	indexJobs        ports.IndexJobRepository
	schemaChanges    ports.SchemaChangeRepository
	txManager        ports.TransactionManager
	idGenerator      ports.IDGenerator
	clock            ports.Clock
	enqueuer         riverjobs.IndexJobEnqueuer
}

type CreateIndexJobInput struct {
	TenantID             uuid.UUID
	TableID              uuid.UUID
	IndexType            datamodel.IndexJobType
	Columns              []string
	RequestedByOperation string
	ScheduledAt          *time.Time
	Method               string
	IsUnique             bool
	IncludeColumns       []string
	OwnerService         string
	SubmittedByService   string
	Purpose              string
	ModelRevision        string
}

func NewIndexJobService(
	tenantRepository ports.TenantRepository,
	tableRepository ports.TableRepository,
	fieldRepository ports.FieldRepository,
	indexJobs ports.IndexJobRepository,
	schemaChanges ports.SchemaChangeRepository,
	txManager ports.TransactionManager,
	idGenerator ports.IDGenerator,
	clock ports.Clock,
	enqueuer riverjobs.IndexJobEnqueuer,
) IndexJobService {
	if enqueuer == nil {
		enqueuer = riverjobs.NoopIndexJobEnqueuer{}
	}
	return IndexJobService{
		tenantRepository: tenantRepository,
		tableRepository:  tableRepository,
		fieldRepository:  fieldRepository,
		indexJobs:        indexJobs,
		schemaChanges:    schemaChanges,
		txManager:        txManager,
		idGenerator:      idGenerator,
		clock:            clock,
		enqueuer:         enqueuer,
	}
}

func (s IndexJobService) Create(ctx context.Context, input CreateIndexJobInput) (datamodel.IndexJob, error) {
	if err := datamodel.ValidateIndexJobCreate(input.IndexType, input.Columns); err != nil {
		return datamodel.IndexJob{}, err
	}

	tenantRecord, err := s.tenantRepository.GetByID(ctx, input.TenantID)
	if err != nil {
		return datamodel.IndexJob{}, err
	}
	if tenantRecord.Status != tenant.StatusActive {
		return datamodel.IndexJob{}, fmt.Errorf("tenant must be active before creating index jobs")
	}

	table, err := s.tableRepository.GetByID(ctx, input.TableID)
	if err != nil {
		return datamodel.IndexJob{}, err
	}
	if table.TenantID != input.TenantID {
		return datamodel.IndexJob{}, fmt.Errorf("table does not belong to tenant")
	}

	fields, err := s.fieldRepository.ListByTable(ctx, input.TableID)
	if err != nil {
		return datamodel.IndexJob{}, err
	}
	fieldNames := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		fieldNames[field.Name] = struct{}{}
	}

	normalizedColumns := make([]string, len(input.Columns))
	for i, column := range input.Columns {
		normalized := datamodel.NormalizeName(column)
		if _, ok := fieldNames[normalized]; !ok {
			return datamodel.IndexJob{}, fmt.Errorf("field does not exist on table: %s", column)
		}
		normalizedColumns[i] = normalized
	}
	normalizedIncludes := make([]string, len(input.IncludeColumns))
	for i, column := range input.IncludeColumns {
		normalized := datamodel.NormalizeName(column)
		if _, ok := fieldNames[normalized]; !ok {
			return datamodel.IndexJob{}, fmt.Errorf("include field does not exist on table: %s", column)
		}
		normalizedIncludes[i] = normalized
	}

	method := strings.ToLower(strings.TrimSpace(input.Method))
	if method == "" {
		method = "btree"
	}
	if method != "btree" {
		return datamodel.IndexJob{}, fmt.Errorf("only btree managed indexes are supported")
	}
	ownerService := strings.TrimSpace(input.OwnerService)
	if ownerService == "" {
		ownerService = "legacy"
	}
	submittedByService := strings.TrimSpace(input.SubmittedByService)
	if submittedByService == "" {
		submittedByService = ownerService
	}
	purpose := strings.TrimSpace(input.Purpose)
	if purpose == "" {
		purpose = strings.TrimSpace(input.RequestedByOperation)
	}
	if purpose == "" {
		purpose = string(input.IndexType)
	}
	isUnique := input.IsUnique || input.IndexType == datamodel.IndexJobTypeUnique
	specHash := datamodel.BuildPhysicalIndexSpecHash(
		input.TenantID,
		table.ID,
		method,
		isUnique,
		normalizedColumns,
		normalizedIncludes,
	)

	canonicalRepo, canonicalSupported := s.indexJobs.(ports.CanonicalIndexJobRepository)
	if existing, err := getExistingIndexJob(ctx, canonicalRepo, canonicalSupported, specHash); err == nil {
		intent := datamodel.IndexIntent{
			ID:                 s.idGenerator.New(),
			IndexJobID:         existing.ID,
			TenantID:           input.TenantID,
			OwnerService:       ownerService,
			SubmittedByService: submittedByService,
			Purpose:            purpose,
			ModelRevision:      strings.TrimSpace(input.ModelRevision),
			Active:             true,
			RequestedAt:        s.clock.Now(),
		}
		if err := canonicalRepo.CreateIntent(ctx, intent); err != nil {
			return datamodel.IndexJob{}, err
		}
		return existing, nil
	} else if err != pgx.ErrNoRows {
		return datamodel.IndexJob{}, err
	}

	now := s.clock.Now()
	job := datamodel.IndexJob{
		ID:                   s.idGenerator.New(),
		TenantID:             input.TenantID,
		TableID:              &table.ID,
		TableName:            table.Name,
		IndexType:            input.IndexType,
		Columns:              normalizedColumns,
		Status:               datamodel.IndexJobStatusPending,
		RequestedByOperation: strings.TrimSpace(input.RequestedByOperation),
		AttemptCount:         0,
		RequestedAt:          now,
		ScheduledAt:          input.ScheduledAt,
		DedupeKey:            specHash,
		Method:               method,
		IsUnique:             isUnique,
		IncludeColumns:       normalizedIncludes,
		OwnerService:         ownerService,
		SubmittedByService:   submittedByService,
		Purpose:              purpose,
		ModelRevision:        strings.TrimSpace(input.ModelRevision),
		SpecHash:             specHash,
	}
	job.IndexName = managedIndexName(table.Name, specHash, isUnique)
	intent := datamodel.IndexIntent{
		ID:                 s.idGenerator.New(),
		IndexJobID:         job.ID,
		TenantID:           input.TenantID,
		OwnerService:       ownerService,
		SubmittedByService: submittedByService,
		Purpose:            purpose,
		ModelRevision:      job.ModelRevision,
		Active:             true,
		RequestedAt:        now,
	}

	if err := s.txManager.Run(ctx, func(store ports.MutationStore) error {
		if err := store.IndexJobs().Create(ctx, job); err != nil {
			return err
		}
		if repo, ok := store.IndexJobs().(ports.CanonicalIndexJobRepository); ok {
			if err := repo.CreateIntent(ctx, intent); err != nil {
				return err
			}
		}
		_ = store.SchemaChanges().Create(ctx, newSchemaChange(
			s.idGenerator.New(),
			job.TenantID,
			"request_index_job",
			"index_job",
			job.ID,
			now,
			map[string]any{
				"table_id":               table.ID,
				"table_name":             table.Name,
				"index_type":             job.IndexType,
				"columns":                job.Columns,
				"requested_by_operation": job.RequestedByOperation,
				"owner_service":          job.OwnerService,
				"purpose":                job.Purpose,
				"spec_hash":              job.SpecHash,
			},
		))
		if err := s.enqueuer.EnqueueTx(ctx, store.RawTx(), job.ID, job.ScheduledAt); err != nil {
			return err
		}
		return nil
	}); err != nil {
		return datamodel.IndexJob{}, err
	}

	return job, nil
}

func getExistingIndexJob(
	ctx context.Context,
	repository ports.CanonicalIndexJobRepository,
	supported bool,
	specHash string,
) (datamodel.IndexJob, error) {
	if !supported {
		return datamodel.IndexJob{}, pgx.ErrNoRows
	}
	return repository.GetBySpecHash(ctx, specHash)
}

func managedIndexName(tableName, specHash string, unique bool) string {
	prefix := "idx"
	if unique {
		prefix = "uniq"
	}
	hash := specHash
	if len(hash) > 12 {
		hash = hash[:12]
	}
	return fmt.Sprintf("%s_%s_%s", prefix, tableName, hash)
}

func (s IndexJobService) Get(ctx context.Context, id uuid.UUID) (datamodel.IndexJob, error) {
	return s.indexJobs.GetByID(ctx, id)
}

func (s IndexJobService) List(ctx context.Context, tenantID uuid.UUID) ([]datamodel.IndexJob, error) {
	return s.indexJobs.ListByTenant(ctx, tenantID)
}

func (s IndexJobService) Retry(ctx context.Context, id uuid.UUID) (datamodel.IndexJob, error) {
	job, err := s.indexJobs.GetByID(ctx, id)
	if err != nil {
		return datamodel.IndexJob{}, err
	}
	if job.Status != datamodel.IndexJobStatusFailed && job.Status != datamodel.IndexJobStatusCancelled {
		return datamodel.IndexJob{}, fmt.Errorf("only failed or cancelled index jobs can be retried")
	}
	scheduledAt := s.clock.Now()
	if err := s.txManager.Run(ctx, func(store ports.MutationStore) error {
		if err := store.IndexJobs().Retry(ctx, id, scheduledAt); err != nil {
			return err
		}
		if err := s.enqueuer.EnqueueTx(ctx, store.RawTx(), id, &scheduledAt); err != nil {
			return err
		}
		return nil
	}); err != nil {
		return datamodel.IndexJob{}, err
	}
	job, err = s.indexJobs.GetByID(ctx, id)
	if err != nil {
		return datamodel.IndexJob{}, err
	}
	return job, nil
}
