package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type IndexJobExecutionStore struct{ transactions TransactionManager }

func NewIndexJobExecutionStore(db *pgxpool.Pool) IndexJobExecutionStore {
	return IndexJobExecutionStore{transactions: NewTransactionManager(db)}
}

func (s IndexJobExecutionStore) StartAttempt(ctx context.Context, id uuid.UUID, at time.Time) (datamodel.IndexJob, error) {
	if id == uuid.Nil || at.IsZero() {
		return datamodel.IndexJob{}, errors.New("job identity and start time required")
	}
	var job datamodel.IndexJob
	err := s.transactions.RunWithTx(ctx, func(store ports.MutationStore, tx pgx.Tx) error {
		var locked uuid.UUID
		if err := tx.QueryRow(ctx, "SELECT id FROM core.index_jobs WHERE id=$1 FOR UPDATE", id).Scan(&locked); err != nil {
			return err
		}
		var err error
		job, err = store.IndexJobs().GetByID(ctx, id)
		if err != nil {
			return err
		}
		switch job.Status {
		case datamodel.IndexJobStatusApplied, datamodel.IndexJobStatusFailed, datamodel.IndexJobStatusCancelled:
			return nil
		case datamodel.IndexJobStatusPending, datamodel.IndexJobStatusRunning:
			job, err = store.IndexJobs().StartAttempt(ctx, id, at)
			return err
		default:
			return errors.New("unknown index job state")
		}
	})
	if err != nil {
		return datamodel.IndexJob{}, err
	}
	return job, nil
}

func (s IndexJobExecutionStore) CompleteAttempt(ctx context.Context, outcome ports.IndexJobOutcome) error {
	if err := outcome.Validate(); err != nil {
		return err
	}
	return s.transactions.RunWithTx(ctx, func(store ports.MutationStore, tx pgx.Tx) error {
		var completedAt *time.Time
		if outcome.Change.Status != string(datamodel.IndexJobStatusPending) {
			completedAt = &outcome.Change.CreatedAt
		}
		var message *string
		if outcome.ErrorMessage != "" {
			message = &outcome.ErrorMessage
		}
		result, err := tx.Exec(ctx, `UPDATE core.index_jobs SET status=$1,error_message=$2,completed_at=$3
			WHERE tenant_id=$4 AND id=$5 AND status='running' AND attempt_count=$6`, outcome.Change.Status, message, completedAt, outcome.Job.TenantID, outcome.Job.ID, outcome.Job.AttemptCount)
		if err != nil {
			return err
		}
		if result.RowsAffected() != 1 {
			return ports.ErrStaleIndexJobAttempt
		}
		return store.SchemaChanges().Create(ctx, outcome.Change)
	})
}

var _ ports.IndexJobExecutionStore = IndexJobExecutionStore{}
