package mysqlruntime

import (
	"bytes"
	"context"
	"errors"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/localtenants"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/mysqlexecution"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	store "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/workers"
	"github.com/google/uuid"
)

type scopedDeliveryWorker interface {
	Claim(context.Context) (*workers.DeliveryTask, error)
	Run(context.Context, func(context.Context, error) error) error
}

// Fixed slots multiplex tenant-scoped owners; adding a tenant creates neither
// a pool nor a permanently running set of fifteen tenant goroutines.
func (r *Runtime) localWorkerLoops(registry *localtenants.Registry) ([]workerLoop, error) {
	var loops []workerLoop
	for _, kind := range allKinds {
		policy := r.policy.Workers[kind]
		if policy.Concurrency != 1 {
			return nil, errors.New("local tenant sweep requires one claim slot per kind")
		}
		supervisor, err := workers.NewLeaseSupervisor(r.policy.Lease, policy.HeartbeatInterval, policy.HeartbeatTimeout, policy.WorkTimeout)
		if err != nil {
			return nil, err
		}
		poller, err := workers.NewDeliveryPoller(1, policy.PollInterval, policy.ClaimTimeout, supervisor)
		if err != nil {
			return nil, err
		}
		poller, err = poller.WithPollJitter(policy.PollJitter)
		if err != nil {
			return nil, err
		}
		loops = append(loops, func(ctx context.Context, report func(context.Context, error) error) error {
			var after uuid.UUID
			claim := func(ctx context.Context) (*workers.DeliveryTask, error) {
				return r.localClaimTask(ctx, kind, &after)
			}
			return poller.Run(ctx, claim, report)
		})
		maintenance, err := workers.NewMaintenanceScheduler[localMaintenanceCursor](r.policy.MaintenancePageSize, r.policy.MaintenancePagesPerTick, r.policy.MaintenanceInterval, r.policy.MaintenanceTimeout)
		if err != nil {
			return nil, err
		}
		loops = append(loops, func(ctx context.Context, _ func(context.Context, error) error) error {
			return maintenance.Run(ctx, &localMaintenance{r, registry, kind})
		})
	}
	loops = append(loops, func(ctx context.Context, report func(context.Context, error) error) error {
		var tenant, after uuid.UUID
		var cursor *store.ScheduleCursor
		for {
			tick, cancel := context.WithTimeout(ctx, r.policy.Scheduler.TickTimeout)
			var err error
			if tenant == uuid.Nil {
				tenant, after, err = registry.Next(tick, after)
			}
			if err == nil && tenant != uuid.Nil {
				var scope *Scope
				scope, err = r.Scope(tick, tenant, true)
				if err == nil {
					var scheduler *mysqlexecution.Scheduler
					scheduler, err = mysqlexecution.NewScheduler(scope.Executions, r.policy.Scheduler)
					if err == nil {
						cursor, err = scheduler.Tick(tick, cursor, report)
					}
				}
				if cursor == nil || err == store.ErrDecisionAuthorizationDenied {
					tenant = uuid.Nil
					cursor = nil
				}
			}
			cancel()
			if err != nil && err != store.ErrDecisionAuthorizationDenied {
				return err
			}
			if err := localPause(ctx, r.policy.Scheduler.Interval); err != nil {
				return err
			}
		}
	})
	if len(loops) > r.policy.MaxWorkerSlots {
		return nil, errors.New("local worker loops exceed the shared slot budget")
	}
	return loops, nil
}

// Empty registry pages are not an idle queue. Continue across at most eight
// indexed pages (160 tenant IDs), wrapping once to visit each tenant fairly.
// The poller sleeps only after a complete sweep or this explicit page budget.
func (r *Runtime) localClaimTask(ctx context.Context, kind ports.JobKind, after *uuid.UUID) (*workers.DeliveryTask, error) {
	start := *after
	wrapped := false
	for range 8 {
		page, err := r.localClaimCandidates(ctx, kind, *after)
		if err != nil {
			return nil, err
		}
		for _, entry := range page {
			if wrapped && bytes.Compare(entry.ID[:], start[:]) > 0 {
				*after = start
				return nil, nil
			}
			*after = entry.ID
			if entry.Active && entry.Ready {
				scope, err := r.Scope(ctx, entry.ID, true)
				if err != nil {
					return nil, err
				}
				worker, _, err := r.kindWorker(scope, entry.ID, kind)
				if err != nil {
					return nil, err
				}
				task, err := worker.Claim(ctx)
				if err != nil && err != store.ErrDecisionAuthorizationDenied {
					return nil, err
				}
				if task != nil {
					return task, nil
				}
			}
			if wrapped && entry.ID == start {
				return nil, nil
			}
		}
		if len(page) < 20 {
			*after = uuid.Nil
			if start == uuid.Nil || wrapped {
				return nil, nil
			}
			wrapped = true
		}
	}
	return nil, nil
}

type localMaintenanceCursor struct {
	Tenant, After uuid.UUID
	Inner         *store.DeliveryMaintenanceCursor
}
type localMaintenance struct {
	runtime  *Runtime
	registry *localtenants.Registry
	kind     ports.JobKind
}

func (m *localMaintenance) MaintainPage(ctx context.Context, limit int, cleanup bool, after *localMaintenanceCursor) (int, *localMaintenanceCursor, error) {
	cursor := localMaintenanceCursor{}
	if after != nil {
		cursor = *after
	}
	if cursor.Tenant == uuid.Nil {
		var err error
		cursor.Tenant, cursor.After, err = m.registry.Next(ctx, cursor.After)
		if err != nil {
			return 0, nil, err
		}
	}
	if cursor.Tenant == uuid.Nil {
		return 0, &cursor, nil
	}
	if cursor.Inner == nil {
		ready, err := m.runtime.localMaintenanceReady(ctx, cursor.Tenant, m.kind, cleanup)
		if err != nil {
			return 0, nil, err
		}
		if !ready {
			cursor.Tenant = uuid.Nil
			return 0, &cursor, nil
		}
	}
	scope, err := m.runtime.Scope(ctx, cursor.Tenant, true)
	if err != nil {
		return 0, nil, err
	}
	_, maintenance, err := m.runtime.kindWorker(scope, cursor.Tenant, m.kind)
	if err != nil {
		return 0, nil, err
	}
	n, next, err := maintenance.MaintainPage(ctx, limit, cleanup, cursor.Inner)
	if err == store.ErrDecisionAuthorizationDenied {
		cursor.Tenant = uuid.Nil
		cursor.Inner = nil
		return n, &cursor, nil
	}
	if err != nil {
		return n, nil, err
	}
	cursor.Inner = next
	if next == nil {
		cursor.Tenant = uuid.Nil
	}
	return n, &cursor, nil
}
func localPause(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return context.Cause(ctx)
	case <-timer.C:
		return nil
	}
}

func (r *Runtime) kindWorker(scope *Scope, tenant uuid.UUID, kind ports.JobKind) (scopedDeliveryWorker, *store.DeliveryMaintenanceStore, error) {
	p := r.policy
	var dispatch *store.DispatchStore
	var maintenance *store.DeliveryMaintenanceStore
	var err error
	if kind == ports.JobScheduled || kind == ports.JobAsync || kind == ports.JobCallback {
		transport, e := store.NewExecutionDeliveryStore(r.deps.DB, r.deps.Database, tenant, kind, p.Execution.Queues[kind].Name, p.Lease, scope.worker)
		if e != nil {
			return nil, nil, e
		}
		maintenance, err = store.NewReceiptAwareExecutionMaintenance(transport, scope.Executions)
		if err != nil {
			return nil, nil, err
		}
		if kind == ports.JobScheduled {
			w, err := mysqlexecution.NewScheduledWorker(scope.Executions, transport, scope.Preparer, scope.Unit, p.Workers[kind])
			return w, maintenance, err
		}
		if kind == ports.JobAsync {
			w, err := mysqlexecution.NewAsyncWorker(scope.Executions, transport, scope.Preparer, scope.Unit, p.Workers[kind])
			return w, maintenance, err
		}
		dispatch, err = store.NewCallbackDispatchStore(scope.Executions, transport)
	} else {
		transport, e := store.NewEffectDeliveryStore(r.deps.DB, r.deps.Database, tenant, kind, p.Effects[kind].Name, p.Lease, scope.worker)
		if e != nil {
			return nil, nil, e
		}
		dispatch, err = store.NewEffectDispatchStore(scope.Effects, transport)
		if err != nil {
			return nil, nil, err
		}
		maintenance, err = store.NewReceiptAwareEffectMaintenance(transport)
	}
	if err != nil {
		return nil, nil, err
	}
	w, err := mysqlexecution.NewDispatchWorker(dispatch, r.deps.Dispatch[kind], p.Workers[kind])
	return w, maintenance, err
}
