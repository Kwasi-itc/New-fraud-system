package worker

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	"github.com/google/uuid"
)

type Clock interface{ Now() time.Time }

var ErrManagedIndexUnverified = errors.New("managed index inspection did not confirm the expected index")

type Runner struct {
	logger        *slog.Logger
	tenants       ports.TenantRepository
	tables        ports.TableRepository
	execution     ports.IndexJobExecutionStore
	schemaManager ports.SchemaManager
	idGenerator   ports.IDGenerator
	clock         Clock
	maxAttempts   int
}

func NewRunner(logger *slog.Logger, tenants ports.TenantRepository, tables ports.TableRepository, execution ports.IndexJobExecutionStore, schemaManager ports.SchemaManager, idGenerator ports.IDGenerator, clock Clock, maxAttempts int) Runner {
	return Runner{logger, tenants, tables, execution, schemaManager, idGenerator, clock, maxAttempts}
}

func (r Runner) RunJob(ctx context.Context, id uuid.UUID) error {
	if r.logger == nil || r.tenants == nil || r.tables == nil || r.execution == nil || r.schemaManager == nil || r.idGenerator == nil || r.clock == nil || r.maxAttempts < 1 {
		return errors.New("index worker dependencies and positive attempt limit required")
	}
	job, err := r.execution.StartAttempt(ctx, id, r.timestamp())
	if err != nil {
		return err
	}
	if job.ID != id || job.TenantID == uuid.Nil {
		return errors.New("index worker claim identity mismatch")
	}
	switch job.Status {
	case datamodel.IndexJobStatusApplied, datamodel.IndexJobStatusFailed, datamodel.IndexJobStatusCancelled:
		return nil // Redelivery of an already durable terminal outcome.
	case datamodel.IndexJobStatusRunning:
	default:
		return errors.New("index worker did not receive a running attempt")
	}
	if job.AttemptCount < 1 {
		return errors.New("invalid claimed index job attempt")
	}
	if job.TableID == nil {
		return r.finish(ctx, job, datamodel.IndexJobStatusFailed, "index job missing table_id", "", false)
	}
	tenantRecord, err := r.tenants.GetByID(ctx, job.TenantID)
	if err != nil {
		return r.retryOrFail(ctx, job, err)
	}
	table, err := r.tables.GetByID(ctx, *job.TableID)
	if err != nil {
		return r.retryOrFail(ctx, job, err)
	}
	if tenantRecord.ID != job.TenantID || table.TenantID != job.TenantID || table.ID != *job.TableID || table.Name != job.TableName {
		return r.finish(ctx, job, datamodel.IndexJobStatusFailed, "index job tenant/table identity mismatch", "", false)
	}
	state, err := r.schemaManager.GetManagedIndexState(ctx, tenantRecord, table, job)
	if err != nil {
		return r.retryOrFail(ctx, job, err)
	}
	if state.Name == "" {
		return r.retryOrFail(ctx, job, ErrManagedIndexUnverified)
	}
	if !state.Exists {
		if job.AttemptCount > r.maxAttempts {
			return r.finish(ctx, job, datamodel.IndexJobStatusFailed, "attempt limit exceeded", "", false)
		}
		if err := r.schemaManager.CreateManagedIndex(ctx, tenantRecord, table, job); err != nil {
			return r.retryOrFail(ctx, job, err)
		}
		// Successful DDL execution is not a verified postcondition. An adapter
		// may return after an idempotent no-op or before readiness is established.
		// Persist no applied outcome unless inspection confirms the same identity.
		expectedName := state.Name
		state, err = r.schemaManager.GetManagedIndexState(ctx, tenantRecord, table, job)
		if err != nil {
			return r.retryOrFail(ctx, job, err)
		}
		if !state.Exists || state.Name != expectedName {
			return r.retryOrFail(ctx, job, ErrManagedIndexUnverified)
		}
	}
	if err := r.finish(ctx, job, datamodel.IndexJobStatusApplied, "", state.Name, false); err != nil {
		return err
	}
	r.logger.Info("index job applied", "job_id", job.ID, "tenant_id", job.TenantID, "table_name", job.TableName)
	return nil
}

func (r Runner) retryOrFail(ctx context.Context, job datamodel.IndexJob, cause error) error {
	status := datamodel.IndexJobStatusPending
	if job.AttemptCount >= r.maxAttempts {
		status = datamodel.IndexJobStatusFailed
	}
	// Preserve both execution failure and any failure to save its outcome.
	return errors.Join(cause, r.finish(ctx, job, status, cause.Error(), "", true))
}

func (r Runner) finish(ctx context.Context, job datamodel.IndexJob, status datamodel.IndexJobStatus, message, indexName string, retryDelivery bool) error {
	operation := "apply_index_job"
	if status == datamodel.IndexJobStatusPending {
		operation = "reschedule_index_job"
	}
	if status == datamodel.IndexJobStatusFailed {
		operation = "fail_index_job"
	}
	payload, err := json.Marshal(map[string]any{
		"table_id": uuidString(job.TableID), "table_name": job.TableName,
		"index_name": indexName, "index_type": job.IndexType, "columns": job.Columns,
		"requested_by_operation": job.RequestedByOperation, "attempt_count": job.AttemptCount,
		"error_message": message,
	})
	if err != nil {
		return err
	}
	return r.execution.CompleteAttempt(ctx, ports.IndexJobOutcome{Job: job, ErrorMessage: message, RetryDelivery: retryDelivery, Change: datamodel.SchemaChange{
		ID: r.idGenerator.New(), TenantID: job.TenantID, Operation: operation, ResourceType: "index_job",
		ResourceID: job.ID, Status: string(status), Details: payload, CreatedAt: r.timestamp(),
	}})
}

// Worker lifecycle timestamps have microsecond precision on both engines.
// This explicit boundary avoids relying on driver/database coercion.
func (r Runner) timestamp() time.Time { return r.clock.Now().UTC().Round(time.Microsecond) }

func uuidString(value *uuid.UUID) string {
	if value == nil {
		return ""
	}
	return value.String()
}
