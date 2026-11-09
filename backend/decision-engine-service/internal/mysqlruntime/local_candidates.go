package mysqlruntime

import (
	"context"
	"fmt"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/workers"
	"github.com/google/uuid"
)

type localWorkCandidate struct {
	ID            uuid.UUID
	Active, Ready bool
}

// This is discovery only. The maintenance owner still checks authority, lease
// identity and retention in its own transaction. A race that creates work after
// this check is picked up on the next bounded registry sweep.
func (r *Runtime) localMaintenanceQuery(tenant uuid.UUID, kind ports.JobKind, cleanup bool) (string, []any, error) {
	schema, err := mysqlstore.Identifier(r.deps.Database)
	if err != nil {
		return "", nil, err
	}
	table, prefix, queue := "decision_engine_effect_job_deliveries", "de_effect_delivery", r.policy.Effects[kind].Name
	switch kind {
	case ports.JobScheduled, ports.JobAsync, ports.JobCallback:
		table, prefix, queue = "decision_engine_execution_job_deliveries", "de_execution_delivery", r.policy.Execution.Queues[kind].Name
	case ports.JobWorkflow, ports.JobScreening, ports.JobScoring, ports.JobOutbox:
	default:
		return "", nil, fmt.Errorf("unsupported local maintenance kind %q", kind)
	}
	base := "EXISTS(SELECT 1 FROM " + schema + "." + table + " d FORCE INDEX(" + prefix
	args := []any{}
	if !cleanup {
		return "SELECT " + base + "_rescue) WHERE d.tenant_id=? AND d.kind=? AND d.queue_name=? AND d.state='running' AND d.lease_until<=UTC_TIMESTAMP(6))", []any{mysqlstore.UUID(tenant), string(kind), queue}, nil
	}
	var parts []string
	completed, err := workers.ResolveDeliveryOutcome(1, 1, false)
	if err != nil {
		return "", nil, err
	}
	discarded, err := workers.ResolveDeliveryOutcome(1, 1, true)
	if err != nil {
		return "", nil, err
	}
	cancelled, _, err := workers.ResolveCancellation(workers.Cancelled)
	if err != nil {
		return "", nil, err
	}
	for _, outcome := range []workers.DeliveryOutcome{completed, cancelled, discarded} {
		parts = append(parts, base+"_cleanup) WHERE d.tenant_id=? AND d.kind=? AND d.queue_name=? AND d.state=? AND d.finished_at<=TIMESTAMPADD(MICROSECOND,?,UTC_TIMESTAMP(6)))")
		args = append(args, mysqlstore.UUID(tenant), string(kind), queue, string(outcome.State), -outcome.Retention.Microseconds())
	}
	return "SELECT " + strings.Join(parts, " OR "), args, nil
}

func (r *Runtime) localMaintenanceReady(ctx context.Context, tenant uuid.UUID, kind ports.JobKind, cleanup bool) (bool, error) {
	query, args, err := r.localMaintenanceQuery(tenant, kind, cleanup)
	if err != nil {
		return false, err
	}
	var ready bool
	err = r.deps.DB.QueryRowContext(ctx, query, args...).Scan(&ready)
	return ready, err
}

// Discovery reads tenant IDs and indexed queue existence only. It grants no
// authority and claims nothing. The scoped owner rechecks activation, locks and
// lease identity when claiming. Idle tenants require no owner transactions.
func (r *Runtime) localCandidateQuery(kind ports.JobKind, after uuid.UUID) (string, []any, error) {
	schema, err := mysqlstore.Identifier(r.deps.Database)
	if err != nil {
		return "", nil, err
	}
	table, prefix, queue := "decision_engine_effect_job_deliveries", "de_effect_delivery", r.policy.Effects[kind].Name
	switch kind {
	case ports.JobScheduled, ports.JobAsync, ports.JobCallback:
		table, prefix, queue = "decision_engine_execution_job_deliveries", "de_execution_delivery", r.policy.Execution.Queues[kind].Name
	case ports.JobWorkflow, ports.JobScreening, ports.JobScoring, ports.JobOutbox:
	default:
		return "", nil, fmt.Errorf("unsupported local delivery kind %q", kind)
	}
	due := "EXISTS(SELECT 1 FROM " + schema + "." + table + " d FORCE INDEX(" + prefix + "_claim) WHERE d.tenant_id=t.id AND d.kind=? AND d.queue_name=? AND d.state='available' AND d.available_at<=UTC_TIMESTAMP(6) AND d.attempt<d.max_attempts)"
	args := []any{string(kind), queue}
	if kind != ports.JobScheduled && kind != ports.JobAsync {
		due += " OR EXISTS(SELECT 1 FROM " + schema + "." + table + " d FORCE INDEX(" + prefix + "_rescue) WHERE d.tenant_id=t.id AND d.kind=? AND d.queue_name=? AND d.state='running' AND d.lease_until<=UTC_TIMESTAMP(6) AND d.attempt>=d.max_attempts AND EXISTS(SELECT 1 FROM " + schema + ".decision_engine_dispatch_intents i WHERE i.receipt_id=d.id AND i.tenant_id=t.id))"
		args = append(args, string(kind), queue)
	}
	query := "SELECT t.id,t.status='active',(" + due + ") FROM " + schema + ".data_model_tenants t FORCE INDEX(PRIMARY) WHERE t.id>? ORDER BY t.id LIMIT 20"
	return query, append(args, mysqlstore.UUID(after)), nil
}
func (r *Runtime) localClaimCandidates(ctx context.Context, kind ports.JobKind, after uuid.UUID) ([]localWorkCandidate, error) {
	query, args, err := r.localCandidateQuery(kind, after)
	if err != nil {
		return nil, err
	}
	rows, err := r.deps.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]localWorkCandidate, 0, 20)
	for rows.Next() {
		var id mysqlstore.UUID
		var active, ready bool
		if err := rows.Scan(&id, &active, &ready); err != nil {
			return nil, err
		}
		out = append(out, localWorkCandidate{uuid.UUID(id), active, ready})
	}
	return out, rows.Err()
}
