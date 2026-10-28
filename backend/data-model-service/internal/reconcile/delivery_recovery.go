package reconcile

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/riverjobs"
	storepostgres "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/postgres"
)

// DeliveryRecovery is an operator-only PostgreSQL bridge for legacy repair jobs.
// It does not retry running/terminal jobs or remove any River evidence.
type DeliveryRecovery struct {
	transactions storepostgres.TransactionManager
}

type DeliveryRecoveryRequest struct {
	TenantID uuid.UUID
	JobID    uuid.UUID
	Apply    bool
	Actor    string
	Reason   string
}

type DeliveryRecoveryResult struct {
	TenantID       uuid.UUID                `json:"tenant_id"`
	JobID          uuid.UUID                `json:"job_id"`
	Status         datamodel.IndexJobStatus `json:"status"`
	DeliveryExists bool                     `json:"delivery_exists"`
	Eligible       bool                     `json:"eligible"`
	Recovered      bool                     `json:"recovered"`
	Reason         string                   `json:"reason"`
}

func NewDeliveryRecovery(db *pgxpool.Pool, enqueuer riverjobs.IndexJobEnqueuer) (DeliveryRecovery, error) {
	if db == nil {
		return DeliveryRecovery{}, errors.New("recovery database required")
	}
	if enqueuer == nil {
		client, err := river.NewClient(riverpgxv5.New(db), &river.Config{})
		if err != nil {
			return DeliveryRecovery{}, err
		}
		enqueuer = riverjobs.NewRiverIndexJobEnqueuer(client, 10, riverjobs.IndexJobQueueName)
	}
	return DeliveryRecovery{storepostgres.NewTransactionManager(db).WithIndexJobEnqueuer(enqueuer)}, nil
}

func (s DeliveryRecovery) Run(ctx context.Context, request DeliveryRecoveryRequest) (DeliveryRecoveryResult, error) {
	if request.TenantID == uuid.Nil || request.JobID == uuid.Nil {
		return DeliveryRecoveryResult{}, errors.New("tenant and job UUIDs required")
	}
	if request.Apply && (strings.TrimSpace(request.Actor) == "" || strings.TrimSpace(request.Reason) == "" || len(request.Actor) > 256 || len(request.Reason) > 2048) {
		return DeliveryRecoveryResult{}, errors.New("apply requires operator identity (1..256 bytes) and reason (1..2048 bytes)")
	}
	result := DeliveryRecoveryResult{TenantID: request.TenantID, JobID: request.JobID}
	err := s.transactions.RunWithTx(ctx, func(store ports.MutationStore, tx pgx.Tx) error {
		// The exact tenant/job lock also serializes worker claims and owner retry
		// updates. No network calls or physical DDL occur under this lock.
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT id FROM core.index_jobs WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, request.TenantID, request.JobID).Scan(&id); err != nil {
			return err
		}
		job, err := store.IndexJobs().GetByID(ctx, id)
		if err != nil {
			return err
		}
		result.Status = job.Status
		args, err := json.Marshal(riverjobs.IndexJobArgs{IndexJobID: id})
		if err != nil {
			return err
		}
		// Include every queue and state: retained terminal evidence, legacy rows
		// without unique_key, and active deliveries all prohibit this recovery.
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM public.river_job WHERE kind=$1 AND args @> $2::jsonb)`, (riverjobs.IndexJobArgs{}).Kind(), args).Scan(&result.DeliveryExists); err != nil {
			return err
		}
		switch {
		case job.Status != datamodel.IndexJobStatusPending:
			result.Reason = "job_not_pending"
		case job.IndexType != datamodel.IndexJobTypeRepair || job.RequestedByOperation != "reconcile_repair":
			result.Reason = "not_reconciliation_repair"
		case result.DeliveryExists:
			result.Reason = "delivery_evidence_exists"
		case job.TableID == nil:
			result.Reason = "table_identity_missing"
		default:
			var valid bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM core.model_tables WHERE tenant_id=$1 AND id=$2 AND name=$3 AND NOT archived)`, job.TenantID, *job.TableID, job.TableName).Scan(&valid); err != nil {
				return err
			}
			if !valid {
				result.Reason = "table_identity_invalid"
				break
			}
			result.Eligible, result.Reason = true, "missing_delivery"
		}
		if !request.Apply || !result.Eligible {
			return nil
		}
		var principal string
		if err := tx.QueryRow(ctx, `SELECT session_user`).Scan(&principal); err != nil {
			return err
		}
		// Preserve the original schedule and monotonic attempt counter. The
		// worker will inspect physical state before executing any repair DDL.
		if err := store.EnqueueIndexJob(ctx, id, job.ScheduledAt); err != nil {
			return err
		}
		details, err := json.Marshal(map[string]any{"actor": request.Actor, "database_principal": principal, "reason": request.Reason, "scheduled_at": job.ScheduledAt, "attempt_count": job.AttemptCount})
		if err != nil {
			return err
		}
		if err := store.SchemaChanges().Create(ctx, datamodel.SchemaChange{
			ID: uuid.New(), TenantID: job.TenantID, Operation: "recover_index_job_delivery", ResourceType: "index_job", ResourceID: id,
			Status: "applied", Details: details, CreatedAt: time.Now().UTC().Truncate(time.Microsecond),
		}); err != nil {
			return err
		}
		result.Recovered = true
		return nil
	})
	if err != nil {
		return DeliveryRecoveryResult{}, err
	}
	return result, nil
}
