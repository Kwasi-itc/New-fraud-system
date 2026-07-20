package service

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/riverjobs"
)

func ensureManagedIndexJobTx(
	ctx context.Context,
	store ports.MutationStore,
	enqueuer riverjobs.IndexJobEnqueuer,
	idGenerator ports.IDGenerator,
	tenantID uuid.UUID,
	table datamodel.Table,
	jobType datamodel.IndexJobType,
	columns []string,
	requestedByOperation string,
	now time.Time,
) (datamodel.IndexJob, bool, error) {
	if enqueuer == nil {
		enqueuer = riverjobs.NoopIndexJobEnqueuer{}
	}

	dedupeKey := datamodel.BuildIndexJobDedupeKey(tenantID, table.ID, jobType, columns)
	existingJobs, err := store.IndexJobs().ListByTenant(ctx, tenantID)
	if err != nil {
		return datamodel.IndexJob{}, false, err
	}

	for _, existing := range existingJobs {
		if existing.DedupeKey != dedupeKey {
			continue
		}
		switch existing.Status {
		case datamodel.IndexJobStatusPending, datamodel.IndexJobStatusRunning, datamodel.IndexJobStatusApplied:
			return existing, false, nil
		case datamodel.IndexJobStatusFailed:
			if err := store.IndexJobs().Retry(ctx, existing.ID, now); err != nil {
				return datamodel.IndexJob{}, false, err
			}
			if err := enqueuer.EnqueueTx(ctx, store.RawTx(), existing.ID, &now); err != nil {
				return datamodel.IndexJob{}, false, err
			}
			existing.Status = datamodel.IndexJobStatusPending
			existing.ScheduledAt = &now
			existing.ErrorMessage = nil
			return existing, true, nil
		}
	}

	job := datamodel.IndexJob{
		ID:                   idGenerator.New(),
		TenantID:             tenantID,
		TableID:              &table.ID,
		TableName:            table.Name,
		IndexType:            jobType,
		Columns:              columns,
		Status:               datamodel.IndexJobStatusPending,
		RequestedByOperation: requestedByOperation,
		AttemptCount:         0,
		RequestedAt:          now,
		DedupeKey:            dedupeKey,
	}
	if err := store.IndexJobs().Create(ctx, job); err != nil {
		return datamodel.IndexJob{}, false, err
	}
	if err := enqueuer.EnqueueTx(ctx, store.RawTx(), job.ID, job.ScheduledAt); err != nil {
		return datamodel.IndexJob{}, false, err
	}
	return job, true, nil
}
