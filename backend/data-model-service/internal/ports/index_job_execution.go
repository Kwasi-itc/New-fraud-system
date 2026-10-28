package ports

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/google/uuid"
)

var ErrStaleIndexJobAttempt = errors.New("index job attempt is no longer current")

// IndexJobExecutionStore owns short metadata transactions, never physical DDL.
// StartAttempt returns terminal jobs unchanged. CompleteAttempt must fence by
// tenant, job ID, running status and attempt number, and append history atomically.
// Attempt fencing does not fence DDL; MySQL executors still require P-01.
type IndexJobExecutionStore interface {
	StartAttempt(context.Context, uuid.UUID, time.Time) (datamodel.IndexJob, error)
	CompleteAttempt(context.Context, IndexJobOutcome) error
}

type IndexJobOutcome struct {
	Job          datamodel.IndexJob
	Change       datamodel.SchemaChange
	ErrorMessage string
	// RetryDelivery preserves an execution-error transport outcome even when
	// the domain attempt limit produces a failed job. Pending outcomes always
	// retry. Direct terminal validation failures leave this false. Delivery
	// budgets remain authoritative and distinct from the domain attempt count.
	RetryDelivery bool
}

func (o IndexJobOutcome) Validate() error {
	if o.Job.ID == uuid.Nil || o.Job.TenantID == uuid.Nil || o.Job.AttemptCount < 1 || o.Job.Status != datamodel.IndexJobStatusRunning {
		return errors.New("running job identity and positive attempt required")
	}
	if o.Change.ID == uuid.Nil || o.Change.TenantID != o.Job.TenantID || o.Change.ResourceID != o.Job.ID || o.Change.ResourceType != "index_job" || o.Change.CreatedAt.IsZero() || !json.Valid(o.Change.Details) {
		return errors.New("valid job-scoped outcome history required")
	}
	var operation string
	switch o.Change.Status {
	case string(datamodel.IndexJobStatusApplied):
		operation = "apply_index_job"
		if o.ErrorMessage != "" || o.RetryDelivery {
			return errors.New("applied job cannot have an error or request delivery retry")
		}
	case string(datamodel.IndexJobStatusPending):
		operation = "reschedule_index_job"
	case string(datamodel.IndexJobStatusFailed):
		operation = "fail_index_job"
	default:
		return errors.New("unsupported index job outcome")
	}
	if o.Change.Operation != operation {
		return errors.New("outcome operation/status mismatch")
	}
	return nil
}
