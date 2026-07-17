package worker

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
)

type Clock interface {
	Now() time.Time
}

type Runner struct {
	logger        *slog.Logger
	tenants       ports.TenantRepository
	tables        ports.TableRepository
	indexJobs     ports.IndexJobRepository
	schemaChanges ports.SchemaChangeRepository
	schemaManager ports.SchemaManager
	idGenerator   ports.IDGenerator
	clock         Clock
	maxAttempts   int
}

func NewRunner(
	logger *slog.Logger,
	tenants ports.TenantRepository,
	tables ports.TableRepository,
	indexJobs ports.IndexJobRepository,
	schemaChanges ports.SchemaChangeRepository,
	schemaManager ports.SchemaManager,
	idGenerator ports.IDGenerator,
	clock Clock,
	maxAttempts int,
) Runner {
	return Runner{
		logger:        logger,
		tenants:       tenants,
		tables:        tables,
		indexJobs:     indexJobs,
		schemaChanges: schemaChanges,
		schemaManager: schemaManager,
		idGenerator:   idGenerator,
		clock:         clock,
		maxAttempts:   maxAttempts,
	}
}

func (r Runner) RunJob(ctx context.Context, id uuid.UUID) error {
	job, err := r.indexJobs.StartAttempt(ctx, id, r.clock.Now())
	if err != nil {
		return err
	}
	_, err = r.executeJob(ctx, job)
	return err
}

func (r Runner) executeJob(ctx context.Context, job datamodel.IndexJob) (bool, error) {
	if job.TableID == nil {
		r.failJob(ctx, job, "index job missing table_id")
		return true, nil
	}

	tenantRecord, err := r.tenants.GetByID(ctx, job.TenantID)
	if err != nil {
		r.failJob(ctx, job, err.Error())
		return true, nil
	}
	table, err := r.tables.GetByID(ctx, *job.TableID)
	if err != nil {
		r.failJob(ctx, job, err.Error())
		return true, nil
	}

	state, err := r.schemaManager.GetManagedIndexState(ctx, tenantRecord, table, job)
	if err != nil {
		r.failJob(ctx, job, err.Error())
		return true, nil
	}
	if state.Exists {
		return r.markApplied(ctx, job, table, state.Name)
	}

	if err := r.schemaManager.CreateManagedIndex(ctx, tenantRecord, table, job); err != nil {
		r.retryOrFail(ctx, job, err.Error())
		r.logger.Error("index job failed", "job_id", job.ID, "error", err)
		return true, err
	}

	return r.markApplied(ctx, job, table, state.Name)
}

func (r Runner) markApplied(ctx context.Context, job datamodel.IndexJob, table datamodel.Table, indexName string) (bool, error) {
	completedAt := r.clock.Now()
	if err := r.indexJobs.MarkApplied(ctx, job.ID, completedAt); err != nil {
		return true, err
	}
	r.recordSchemaChange(ctx, job, completedAt, "apply_index_job", "applied", map[string]any{
		"table_id":               uuidString(job.TableID),
		"table_name":             table.Name,
		"index_name":             indexName,
		"index_type":             job.IndexType,
		"columns":                job.Columns,
		"requested_by_operation": job.RequestedByOperation,
		"attempt_count":          job.AttemptCount,
	})
	r.logger.Info("index job applied",
		"job_id", job.ID,
		"tenant_id", job.TenantID,
		"table_name", job.TableName,
		"index_type", job.IndexType,
		"columns", job.Columns,
	)
	return true, nil
}

func (r Runner) retryOrFail(ctx context.Context, job datamodel.IndexJob, message string) {
	if job.AttemptCount < r.maxAttempts {
		if err := r.indexJobs.MarkPendingRetry(ctx, job.ID, message); err == nil {
			r.recordSchemaChange(ctx, job, r.clock.Now(), "reschedule_index_job", "pending", map[string]any{
				"table_id":               uuidString(job.TableID),
				"table_name":             job.TableName,
				"index_type":             job.IndexType,
				"columns":                job.Columns,
				"requested_by_operation": job.RequestedByOperation,
				"attempt_count":          job.AttemptCount,
				"error_message":          message,
			})
			return
		}
	}
	r.failJob(ctx, job, message)
}

func (r Runner) failJob(ctx context.Context, job datamodel.IndexJob, message string) {
	completedAt := r.clock.Now()
	_ = r.indexJobs.MarkFailed(ctx, job.ID, message, completedAt)
	r.recordSchemaChange(ctx, job, completedAt, "fail_index_job", "failed", map[string]any{
		"table_id":               uuidString(job.TableID),
		"table_name":             job.TableName,
		"index_type":             job.IndexType,
		"columns":                job.Columns,
		"requested_by_operation": job.RequestedByOperation,
		"attempt_count":          job.AttemptCount,
		"error_message":          message,
	})
}

func (r Runner) recordSchemaChange(
	ctx context.Context,
	job datamodel.IndexJob,
	createdAt time.Time,
	operation string,
	status string,
	details map[string]any,
) {
	if r.schemaChanges == nil || r.idGenerator == nil {
		return
	}

	payload, err := json.Marshal(details)
	if err != nil {
		payload = []byte(`{}`)
	}

	if err := r.schemaChanges.Create(ctx, datamodel.SchemaChange{
		ID:           r.idGenerator.New(),
		TenantID:     job.TenantID,
		Operation:    operation,
		ResourceType: "index_job",
		ResourceID:   job.ID,
		Status:       status,
		Details:      payload,
		CreatedAt:    createdAt,
	}); err != nil {
		r.logger.Error("failed to record index job schema change", "job_id", job.ID, "operation", operation, "error", err)
	}
}

func uuidString(value *uuid.UUID) string {
	if value == nil {
		return ""
	}
	return value.String()
}
