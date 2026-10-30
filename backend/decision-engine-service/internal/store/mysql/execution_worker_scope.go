package mysql

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/execution"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
)

// ValidateEvaluationWorkerScope rejects composition across pools, databases,
// tenants, policies or budgets. Identity authorization remains mandatory on each
// operation; matching configuration alone is never worker authority.
func (s *ExecutionStore) ValidateEvaluationWorkerScope(unit *DecisionUnitOfWorkStore, delivery *ExecutionDeliveryStore, kind ports.JobKind) (time.Duration, error) {
	if s == nil || s.configuration == nil || unit == nil || unit.executions == nil || delivery == nil || delivery.transport == nil || (kind != ports.JobScheduled && kind != ports.JobAsync) {
		return 0, ErrDecisionAuthorizationUnavailable
	}
	a, b, d := s.configuration, unit.executions.configuration, delivery.transport
	if b == nil || a.db != b.db || a.schema != b.schema || a.tenant != b.tenant || a.maxRows != b.maxRows || a.maxBytes != b.maxBytes || d.db != a.db || d.schema != a.schema || d.tenant != a.tenant || d.kind != kind || !d.executionKinds {
		return 0, ErrTenantScope
	}
	for _, k := range []ports.JobKind{ports.JobScheduled, ports.JobAsync, ports.JobCallback} {
		if s.policy.Queues[k] != unit.executions.policy.Queues[k] {
			return 0, ErrExecutionState
		}
	}
	if s.policy.CallbackMaxAttempts != unit.executions.policy.CallbackMaxAttempts || d.queue != s.policy.Queues[kind].Name {
		return 0, ErrExecutionState
	}
	return d.lease, nil
}

// EvaluationCompletionCommitted reconciles only the exact durable evaluation
// outcome after an ambiguous COMMIT. One statement reads owner, permanent
// receipt and optional retained delivery together. Cleanup may remove completed
// delivery rows but never receipts/owners. A missing, stale, failed or changed
// owner is NOT evidence of success and must not trigger an automatic replay.
// This uses run authority, not browser read authority or a fabricated service
// identity. It does not renew, fail, retry, acknowledge or create any work.
func (s *ExecutionStore) EvaluationCompletionCommitted(ctx context.Context, lease ExecutionLease) (bool, error) {
	if s == nil || s.configuration == nil || s.authorize == nil || (lease.Identity.Kind != ports.JobScheduled && lease.Identity.Kind != ports.JobAsync) {
		return false, ErrExecutionLease
	}
	c := s.configuration
	var committed bool
	err := mysqlstore.Mutation(ctx, c.db, func(tx *sql.Tx) error {
		if err := s.authorize(ctx, tx, c.tenant, ExecutionRun); err != nil {
			return err
		}
		r := s.repositories(tx, ExecutionRun, nil).Scheduled.executionRepository
		if !r.validLease(lease, lease.Identity.Kind) {
			return ErrExecutionLease
		}
		var status string
		var generation, attempt int
		var delivery sql.NullString
		err := tx.QueryRowContext(ctx, "SELECT o.status,o.generation,o.attempt_count,d.state FROM "+c.schema+"."+executionOwner(lease.Identity.Kind)+" o JOIN "+c.schema+".decision_engine_execution_job_receipts r ON r.tenant_id=o.tenant_id AND r.resource_id=o.id LEFT JOIN "+c.schema+".decision_engine_execution_job_deliveries d ON d.tenant_id=r.tenant_id AND d.id=r.id WHERE o.tenant_id=? AND o.id=? AND r.id=? AND r.kind=? AND r.domain_attempt=? AND r.redrive_generation=?", mysqlstore.UUID(c.tenant), mysqlstore.UUID(lease.Identity.ResourceID), mysqlstore.UUID(lease.ID), string(lease.Identity.Kind), lease.Identity.DomainAttempt, lease.Identity.RedriveGeneration).Scan(&status, &generation, &attempt, &delivery)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		committed = execution.Status(status) == execution.StatusCompleted && generation == lease.Identity.RedriveGeneration && attempt == lease.Identity.DomainAttempt && (!delivery.Valid || delivery.String == "completed")
		return nil
	})
	if err != nil {
		return false, err
	}
	return committed, nil
}
