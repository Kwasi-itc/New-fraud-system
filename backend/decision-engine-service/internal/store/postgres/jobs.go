package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/riverjobs"
	"github.com/google/uuid"
)

// JobEnqueuers belongs to PostgreSQL wiring, not service/domain ports. Every
// producer receives the transaction that persisted its corresponding resource.
type JobEnqueuers struct {
	Scheduled riverjobs.ScheduledExecutionEnqueuer
	Async     riverjobs.AsyncDecisionExecutionEnqueuer
	Callback  riverjobs.AsyncDecisionExecutionCallbackEnqueuer
	Workflow  riverjobs.WorkflowExecutionEnqueuer
	Screening riverjobs.ScreeningExecutionEnqueuer
	Scoring   riverjobs.ScoringRequestEnqueuer
	Outbox    riverjobs.OutboxEventEnqueuer
}

func (s mutationStore) Enqueue(ctx context.Context, job ports.JobRequest) error {
	if s.tx == nil {
		return errors.New("decision enqueue requires an active mutation transaction")
	}
	tenant, err := uuid.Parse(job.TenantID)
	if err != nil || tenant == uuid.Nil {
		return errors.New("decision enqueue requires a nonzero tenant identity")
	}
	id, err := uuid.Parse(job.ResourceID)
	if err != nil || id == uuid.Nil {
		return errors.New("decision enqueue requires a nonzero resource identity")
	}
	// These are fixed owner identifiers, never caller-provided SQL fragments.
	var table string
	var configured bool
	switch job.Kind {
	case ports.JobScheduled:
		table, configured = "core.scheduled_executions", s.jobs.Scheduled != nil
	case ports.JobAsync:
		table, configured = "core.async_decision_executions", s.jobs.Async != nil
	case ports.JobCallback:
		table, configured = "core.async_decision_executions", s.jobs.Callback != nil
	case ports.JobWorkflow:
		table, configured = "core.workflow_executions", s.jobs.Workflow != nil
	case ports.JobScreening:
		table, configured = "core.screening_executions", s.jobs.Screening != nil
	case ports.JobScoring:
		table, configured = "core.scoring_requests", s.jobs.Scoring != nil
	case ports.JobOutbox:
		table, configured = "core.outbox_events", s.jobs.Outbox != nil
	default:
		return fmt.Errorf("unsupported decision job kind %q", job.Kind)
	}
	if !configured {
		return fmt.Errorf("required %s producer is not configured", job.Kind)
	}
	// Bind tenant/resource under a short key-share lock. Inserting or updating
	// the resource earlier in this transaction is visible; no external work runs.
	var bound bool
	if err := s.tx.QueryRow(ctx, "SELECT true FROM "+table+" WHERE tenant_id=$1 AND id=$2 FOR KEY SHARE", tenant, id).Scan(&bound); err != nil {
		return fmt.Errorf("bind %s owner resource: %w", job.Kind, err)
	}
	t, resource, at := tenant.String(), id.String(), job.ScheduledAt
	switch job.Kind {
	case ports.JobScheduled:
		return s.jobs.Scheduled.EnqueueTx(ctx, s.tx, resource, at)
	case ports.JobAsync:
		return s.jobs.Async.EnqueueTx(ctx, s.tx, resource, at)
	case ports.JobCallback:
		return s.jobs.Callback.EnqueueTx(ctx, s.tx, t, resource, at)
	case ports.JobWorkflow:
		return s.jobs.Workflow.EnqueueTx(ctx, s.tx, t, resource, at)
	case ports.JobScreening:
		return s.jobs.Screening.EnqueueTx(ctx, s.tx, t, resource, at)
	case ports.JobScoring:
		return s.jobs.Scoring.EnqueueTx(ctx, s.tx, t, resource, at)
	case ports.JobOutbox:
		return s.jobs.Outbox.EnqueueTx(ctx, s.tx, t, resource, at)
	}
	return errors.New("unreachable decision job dispatch")
}
