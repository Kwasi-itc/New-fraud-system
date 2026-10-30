package mysql

import (
	"context"
	"errors"
	"regexp"
	"sort"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/mysqljobs"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

var ErrEffectJobOwnerState = errors.New("effect is not eligible for initial delivery")

// DeliveryQueuePolicy is explicit installation configuration, not handler
// activation. Transport attempts never increment the domain attempt counter.
type DeliveryQueuePolicy struct {
	Name        string
	MaxAttempts int
}

type EffectQueuePolicy = DeliveryQueuePolicy

type effectQueueConfig map[ports.JobKind]EffectQueuePolicy

var effectQueueName = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,128}$`)

func effectJobKind(table string) (ports.JobKind, string, string, bool) {
	switch table {
	case "decision_engine_workflow_executions":
		return ports.JobWorkflow, "workflow_id", "pending_dispatch", true
	case "decision_engine_screening_executions":
		return ports.JobScreening, "screening_id", "pending", true
	case "decision_engine_scoring_requests":
		return ports.JobScoring, "scoring_id", "pending", true
	case "decision_engine_outbox_events":
		return ports.JobOutbox, "outbox_id", "pending", true
	}
	return "", "", "", false
}

func copyEffectQueues(policies map[ports.JobKind]EffectQueuePolicy) (effectQueueConfig, error) {
	if len(policies) != 4 {
		return nil, errors.New("all four implemented effect queues must be configured")
	}
	out := effectQueueConfig{}
	for _, kind := range []ports.JobKind{ports.JobWorkflow, ports.JobScreening, ports.JobScoring, ports.JobOutbox} {
		p, ok := policies[kind]
		if !ok || !effectQueueName.MatchString(p.Name) || p.MaxAttempts < 1 || p.MaxAttempts > 2147483647 {
			return nil, errors.New("valid explicit effect queues and bounded transport attempts required")
		}
		out[kind] = p
	}
	return out, nil
}

// enqueueCreated runs only inside the authorized owner mutation. It is not a
// public arbitrary-payload producer. Initial attempt/generation are allocated
// here; retries/redrives require a separate locked lifecycle operation.
// Fixed table/column identifiers come exclusively from effectJobKind.
func (r authoringRepository) enqueueCreated(ctx context.Context, table string, ids []string) error {
	if err := r.mutating(); err != nil {
		return err
	}
	if r.effectQueues == nil {
		return errors.New("required effect producers are not configured")
	}
	if len(ids) == 0 {
		return nil
	}
	if len(ids) > r.maxRows {
		return ErrProjectionLimit
	}
	kind, column, pending, ok := effectJobKind(table)
	if !ok {
		return errors.New("unsupported effect owner for required delivery")
	}
	policy, ok := r.effectQueues[kind]
	if !ok {
		return errors.New("required effect producer is not configured")
	}
	ordered := append([]string(nil), ids...)
	sort.Strings(ordered)
	for start := 0; start < len(ordered); start += 100 {
		batch := ordered[start:min(start+100, len(ordered))]
		args := []any{mysqlstore.UUID(r.tenant)}
		for _, id := range batch {
			parsed, err := uuid.Parse(id)
			if err != nil || parsed == uuid.Nil {
				return ErrTenantScope
			}
			args = append(args, mysqlstore.UUID(parsed))
		}
		marks := strings.TrimSuffix(strings.Repeat("?,", len(batch)), ",")
		rows, err := r.tx.QueryContext(ctx, "SELECT id,status FROM "+r.schema+"."+table+" WHERE tenant_id=? AND id IN ("+marks+") ORDER BY id FOR UPDATE", args...)
		if err != nil {
			return err
		}
		count := 0
		for rows.Next() {
			var id mysqlstore.UUID
			var status string
			if err := rows.Scan(&id, &status); err != nil {
				rows.Close()
				return err
			}
			if status != pending {
				rows.Close()
				return ErrEffectJobOwnerState
			}
			count++
		}
		err = rows.Err()
		closeErr := rows.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		if count != len(batch) {
			return ErrOwnerRelationshipConflict
		}
		// The resource lock serializes competing producers, including missing
		// receipt rows under READ COMMITTED. Receipts are exact tuples, not hashes.
		if err := r.enqueueInitialBatch(ctx, kind, column, policy, batch); err != nil {
			return err
		}
	}
	return nil
}

func (r authoringRepository) enqueueInitialBatch(ctx context.Context, kind ports.JobKind, column string, policy EffectQueuePolicy, ids []string) error {
	args := []any{mysqlstore.UUID(r.tenant), string(kind)}
	for _, id := range ids {
		args = append(args, mysqlstore.UUID(uuid.MustParse(id))) // Validated by enqueueCreated.
	}
	marks := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	rows, err := r.tx.QueryContext(ctx, "SELECT resource_id FROM "+r.schema+".decision_engine_effect_job_receipts WHERE tenant_id=? AND kind=? AND resource_id IN ("+marks+") AND domain_attempt=1 AND redrive_generation=0 FOR UPDATE", args...)
	if err != nil {
		return err
	}
	existing := make(map[uuid.UUID]bool)
	for rows.Next() {
		var id mysqlstore.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		existing[uuid.UUID(id)] = true
	}
	err = rows.Err()
	closeErr := rows.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	var receiptMarks, deliveryMarks []string
	var receiptArgs, deliveryArgs []any
	for _, raw := range ids {
		resource := uuid.MustParse(raw)
		if existing[resource] {
			continue // Includes retained receipts after delivery retention cleanup.
		}
		identity := mysqljobs.Identity{Version: 1, Kind: kind, TenantID: r.tenant, ResourceID: resource, DomainAttempt: 1}
		if err := identity.Validate(); err != nil {
			return err
		}
		id := mysqlstore.UUID(uuid.New())
		receiptMarks = append(receiptMarks, "(?,?,?,?,1,0,?,UTC_TIMESTAMP(6))")
		receiptArgs = append(receiptArgs, id, mysqlstore.UUID(r.tenant), mysqlstore.UUID(resource), string(kind), mysqlstore.UUID(resource))
		deliveryMarks = append(deliveryMarks, "(?,?,?,?,UTC_TIMESTAMP(6),UTC_TIMESTAMP(6),?)")
		deliveryArgs = append(deliveryArgs, id, mysqlstore.UUID(r.tenant), string(kind), policy.Name, policy.MaxAttempts)
	}
	if len(receiptMarks) == 0 {
		return nil
	}
	if _, err := r.tx.ExecContext(ctx, "INSERT INTO "+r.schema+".decision_engine_effect_job_receipts(id,tenant_id,resource_id,kind,domain_attempt,redrive_generation,"+column+",created_at) VALUES"+strings.Join(receiptMarks, ","), receiptArgs...); err != nil {
		return err
	}
	_, err = r.tx.ExecContext(ctx, "INSERT INTO "+r.schema+".decision_engine_effect_job_deliveries(id,tenant_id,kind,queue_name,available_at,created_at,max_attempts) VALUES"+strings.Join(deliveryMarks, ","), deliveryArgs...)
	return err
}
