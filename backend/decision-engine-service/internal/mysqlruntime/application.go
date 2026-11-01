package mysqlruntime

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/mysqlapi"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/mysqlexecution"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/workers"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"
)

// Application is explicit composition, not live activation. Commands do not
// construct it until the separately approved release/provider gates are met.
// Handler exposes only implemented routes; it never mounts PostgreSQL routes
// over a MySQL pool or advertises unsupported endpoints as successful no-ops.
type Application struct {
	runtime       *Runtime
	handler       *gin.Engine
	tenants       []uuid.UUID
	workers       []workerLoop
	ready         atomic.Bool
	workerRunning atomic.Bool
	stopped       atomic.Bool
	shutdownMu    sync.Mutex
	workerCancel  context.CancelFunc
	workerDone    chan struct{}
}
type workerLoop func(context.Context, func(context.Context, error) error) error

// Start completes cold shape/provider checks before returning an HTTP handler.
// Worker tenant scope is an explicit bounded service deployment inventory, not
// authority inferred from a queued payload. No worker starts until RunWorkers.
func (r *Runtime) Start(ctx context.Context, tenants []uuid.UUID) (*Application, error) {
	if r == nil || len(tenants) < 1 || len(tenants) > r.policy.MaxWorkerTenants {
		return nil, mysql.ErrTenantScope
	}
	seen := map[uuid.UUID]bool{}
	slotsPerTenant := len(allKinds) + 1 // bounded maintenance loops plus scheduler
	for _, kind := range allKinds {
		slotsPerTenant += r.policy.Workers[kind].Concurrency
	}
	if slotsPerTenant*len(tenants) > r.policy.MaxWorkerSlots {
		return nil, errors.New("tenant worker inventory exceeds the application slot budget")
	}
	for _, tenant := range tenants {
		if tenant == uuid.Nil || seen[tenant] {
			return nil, mysql.ErrTenantScope
		}
		seen[tenant] = true
	}
	ctx, cancel := context.WithTimeout(ctx, r.policy.StartupTimeout)
	defer cancel()
	if _, err := r.deps.Readiness.Check(ctx); err != nil {
		return nil, err
	}
	a := &Application{runtime: r, tenants: append([]uuid.UUID(nil), tenants...)}
	// Build EVERY required loop before returning; a missing receipt provider or
	// invalid policy must not leave a partially active worker set.
	for _, tenant := range tenants {
		scope, err := r.Scope(ctx, tenant, true)
		if err != nil {
			return nil, err
		}
		loops, err := r.workerLoops(scope, tenant)
		if err != nil {
			return nil, err
		}
		a.workers = append(a.workers, loops...)
	}
	h, err := mysqlapi.NewExecutionHTTP(func(ctx context.Context, tenant uuid.UUID) (*mysqlapi.ExecutionEndpointScope, error) {
		if !a.ready.Load() || a.stopped.Load() {
			return nil, mysql.ErrOwnerContractUnavailable
		}
		if !seen[tenant] {
			return nil, mysql.ErrDecisionAuthorizationDenied
		}
		scope, err := r.Scope(ctx, tenant, false)
		if err != nil {
			return nil, err
		}
		return scope.Endpoint, nil
	}, int64(r.policy.HTTPMaxBytes))
	if err != nil {
		return nil, err
	}
	engine := gin.New()
	engine.Use(gin.Recovery())
	engine.GET("/healthz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "alive"}) })
	engine.GET("/readyz", func(c *gin.Context) {
		if !a.ready.Load() || a.stopped.Load() {
			c.AbortWithStatusJSON(503, gin.H{"status": "not_ready"})
			return
		}
		check, cancel := context.WithTimeout(c.Request.Context(), r.policy.ReadinessTimeout)
		defer cancel()
		if err := r.deps.Readiness.CheckHot(check); err != nil {
			c.AbortWithStatusJSON(503, gin.H{"status": "not_ready"})
			return
		}
		c.JSON(200, gin.H{"status": "ready"})
	})
	v1 := engine.Group("/v1")
	v1.Use(func(c *gin.Context) {
		trusted, err := r.deps.Authenticate(c.Request)
		if err != nil || trusted == nil {
			c.AbortWithStatusJSON(401, gin.H{"error": "authentication_required"})
			return
		}
		bounded, cancel := context.WithTimeout(trusted, r.policy.RequestTimeout)
		defer cancel()
		// Preserve client cancellation even if a faulty verifier drops it.
		stop := context.AfterFunc(c.Request.Context(), cancel)
		defer stop()
		c.Request = c.Request.WithContext(bounded)
		c.Next()
	})
	h.Register(v1)
	a.handler = engine
	a.ready.Store(true)
	return a, nil
}

func (a *Application) Handler() http.Handler {
	if a == nil || a.handler == nil {
		return http.NotFoundHandler()
	}
	return a.handler
}

// HTTPServer configures socket-level deadlines as well as the handler's work
// deadline. Context cancellation alone cannot bound a client stalled mid-body.
// It does not listen; the caller owns serving/graceful HTTP shutdown and pools.
func (a *Application) HTTPServer(address string) (*http.Server, error) {
	if a == nil || a.runtime == nil || a.handler == nil || a.stopped.Load() {
		return nil, mysql.ErrOwnerContractUnavailable
	}
	_, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, errors.New("explicit HTTP listen address/port required")
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return nil, errors.New("HTTP port must be in 1..65535")
	}
	p := a.runtime.policy
	return &http.Server{Addr: address, Handler: a.handler, ReadHeaderTimeout: p.ReadHeaderTimeout, ReadTimeout: p.ReadHeaderTimeout + p.RequestTimeout, WriteTimeout: p.ReadHeaderTimeout + p.RequestTimeout, IdleTimeout: p.HTTPIdleTimeout, MaxHeaderBytes: p.HTTPMaxHeaderBytes}, nil
}

func (r *Runtime) workerLoops(scope *Scope, tenant uuid.UUID) ([]workerLoop, error) {
	p := r.policy
	var loops []workerLoop
	for _, kind := range allKinds {
		var dispatch *mysql.DispatchStore
		if kind == ports.JobScheduled || kind == ports.JobAsync || kind == ports.JobCallback {
			transport, err := mysql.NewExecutionDeliveryStore(r.deps.DB, r.deps.Database, tenant, kind, p.Execution.Queues[kind].Name, p.Lease, scope.worker)
			if err != nil {
				return nil, err
			}
			maintenance, err := mysql.NewReceiptAwareExecutionMaintenance(transport, scope.Executions)
			if err != nil {
				return nil, err
			}
			maintenanceLoop, err := r.maintenanceLoop(maintenance)
			if err != nil {
				return nil, err
			}
			loops = append(loops, maintenanceLoop)
			switch kind {
			case ports.JobScheduled:
				w, err := mysqlexecution.NewScheduledWorker(scope.Executions, transport, scope.Preparer, scope.Unit, p.Workers[kind])
				if err != nil {
					return nil, err
				}
				loops = append(loops, w.Run)
				continue
			case ports.JobAsync:
				w, err := mysqlexecution.NewAsyncWorker(scope.Executions, transport, scope.Preparer, scope.Unit, p.Workers[kind])
				if err != nil {
					return nil, err
				}
				loops = append(loops, w.Run)
				continue
			case ports.JobCallback:
				dispatch, err = mysql.NewCallbackDispatchStore(scope.Executions, transport)
				if err != nil {
					return nil, err
				}
			}
		} else {
			transport, err := mysql.NewEffectDeliveryStore(r.deps.DB, r.deps.Database, tenant, kind, p.Effects[kind].Name, p.Lease, scope.worker)
			if err != nil {
				return nil, err
			}
			dispatch, err = mysql.NewEffectDispatchStore(scope.Effects, transport)
			if err != nil {
				return nil, err
			}
			maintenance, err := mysql.NewReceiptAwareEffectMaintenance(transport)
			if err != nil {
				return nil, err
			}
			maintenanceLoop, err := r.maintenanceLoop(maintenance)
			if err != nil {
				return nil, err
			}
			loops = append(loops, maintenanceLoop)
		}
		w, err := mysqlexecution.NewDispatchWorker(dispatch, r.deps.Dispatch[kind], p.Workers[kind])
		if err != nil {
			return nil, err
		}
		loops = append(loops, w.Run)
	}
	scheduler, err := mysqlexecution.NewScheduler(scope.Executions, p.Scheduler)
	if err != nil {
		return nil, err
	}
	loops = append(loops, scheduler.Run)
	return loops, nil
}

func (r *Runtime) maintenanceLoop(store *mysql.DeliveryMaintenanceStore) (workerLoop, error) {
	p := r.policy
	scheduler, err := workers.NewMaintenanceScheduler[mysql.DeliveryMaintenanceCursor](p.MaintenancePageSize, p.MaintenancePagesPerTick, p.MaintenanceInterval, p.MaintenanceTimeout)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context, _ func(context.Context, error) error) error {
		return scheduler.Run(ctx, store)
	}, nil
}

// RunWorkers is single-use: unexpected completion/failure cancels all siblings
// and makes the application not ready. Shutdown waits for all supervised work.
// Report is mandatory and called concurrently; it must be bounded/thread-safe.
func (a *Application) RunWorkers(ctx context.Context, report func(context.Context, error) error) error {
	if a == nil || report == nil || len(a.workers) == 0 || a.stopped.Load() || !a.workerRunning.CompareAndSwap(false, true) {
		return mysql.ErrOwnerContractUnavailable
	}
	a.shutdownMu.Lock()
	if a.stopped.Load() {
		a.shutdownMu.Unlock()
		return mysql.ErrOwnerContractUnavailable
	}
	ctx, cancel := context.WithCancel(ctx)
	a.workerCancel = cancel
	a.workerDone = make(chan struct{})
	done := a.workerDone
	a.shutdownMu.Unlock()
	defer func() { a.ready.Store(false); a.stopped.Store(true); cancel(); close(done) }()
	group, work := errgroup.WithContext(ctx)
	for _, loop := range a.workers {
		group.Go(func() error {
			err := loop(work, report)
			if work.Err() != nil {
				return context.Cause(work)
			}
			if err == nil {
				return errors.New("required decision worker exited unexpectedly")
			}
			return err
		})
	}
	return group.Wait()
}

// Shutdown stops admission first. The HTTP server/listener and SQL pools remain
// caller-owned: its graceful shutdown should finish before closing these pools.
func (a *Application) Shutdown(ctx context.Context) error {
	if a == nil {
		return nil
	}
	a.ready.Store(false)
	a.stopped.Store(true)
	a.shutdownMu.Lock()
	cancel, done := a.workerCancel, a.workerDone
	a.shutdownMu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return context.Cause(ctx)
	}
}
