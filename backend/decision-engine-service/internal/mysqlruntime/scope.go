// Package mysqlruntime is explicit, inactive application composition. It builds
// real owners, evaluators, APIs and workers; it supplies no permissive providers,
// migrates no database and does not select MySQL in the deployed commands.
package mysqlruntime

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"reflect"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/mysqlapi"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/mysqlexecution"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/workers"
	"github.com/google/uuid"
)

// AuthorityProvider must resolve a trusted request session OR a separately
// authenticated service principal. It may not derive authority from path/job
// tenant IDs. These mandatory callbacks recheck current permissions/revocation
// through local SQL on every caller transaction, before owner locks.
type Authorities struct {
	TenantID   uuid.UUID
	Authoring  mysql.AuthoringAuthorizer
	Reads      mysql.PlatformAuthorizer
	Persist    mysql.PlatformAuthorizer
	Executions mysql.ExecutionAuthorizer
	Worker     mysql.EffectWorkerAuthorizer
	// Optional only for service scope. Browser access requires a trusted user
	// session supplied by the real auth owner, not decoded from route JSON.
	Session *mysql.TrustedDecisionSession
}
type AuthorityProvider func(context.Context, uuid.UUID) (Authorities, error)

type Policy struct {
	MaxRows, MaxBytes, HTTPMaxBytes, MaxWorkerTenants int
	MaxWorkerSlots                                    int
	Evaluation                                        mysqlexecution.EvaluationPolicy
	Admission                                         mysqlexecution.AdmissionPolicy
	Execution                                         mysql.ExecutionPolicy
	Effects                                           map[ports.JobKind]mysql.EffectQueuePolicy
	Wait                                              mysqlapi.ExecutionWaitPolicy
	Lease                                             time.Duration
	Workers                                           map[ports.JobKind]mysqlexecution.EvaluationWorkerPolicy
	Scheduler                                         mysqlexecution.SchedulerPolicy
	ReadinessTimeout                                  time.Duration
	StartupTimeout                                    time.Duration
	RequestTimeout                                    time.Duration
	ReadHeaderTimeout, HTTPIdleTimeout                time.Duration
	HTTPMaxHeaderBytes                                int
	MaintenancePageSize, MaintenancePagesPerTick      int
	MaintenanceInterval, MaintenanceTimeout           time.Duration
}

type Dependencies struct {
	DB               *sql.DB
	Database         string
	Models           ports.TenantModelReader
	RequestAuthority AuthorityProvider
	Authenticate     func(*http.Request) (context.Context, error)
	ServiceAuthority AuthorityProvider
	IDs              ports.IDGenerator
	Clock            ports.Clock
	GeoIP            ports.GeoIPLookup
	Dispatch         map[ports.JobKind]ports.ReceiptDispatcher
	Readiness        *mysql.RuntimeReadiness
}

type Scope struct {
	Executions  *mysql.ExecutionStore
	Effects     *mysql.EffectStore
	Unit        *mysql.DecisionUnitOfWorkStore
	Preparer    *mysqlexecution.Preparer
	Publication *mysqlexecution.Publication
	browser     *mysql.BrowserWorkflowStore
	Endpoint    *mysqlapi.ExecutionEndpointScope
	worker      mysql.EffectWorkerAuthorizer
}

// Nil interface values, including typed nil providers, are unavailable. This is
// only a constructor check; actual authority remains per-transaction owner SQL.
func absent(v any) bool {
	if v == nil {
		return true
	}
	value := reflect.ValueOf(v)
	switch value.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Func, reflect.Slice:
		return value.IsNil()
	}
	return false
}

var allKinds = []ports.JobKind{ports.JobScheduled, ports.JobAsync, ports.JobCallback, ports.JobWorkflow, ports.JobScreening, ports.JobScoring, ports.JobOutbox}
var dispatchKinds = []ports.JobKind{ports.JobCallback, ports.JobWorkflow, ports.JobScreening, ports.JobScoring, ports.JobOutbox}

type Runtime struct {
	deps   Dependencies
	policy Policy
}

// New validates configuration/dependency bindings without dialing a database or
// starting work. Start checks the full cold lineage/real provider readiness.
// Pools belong to the caller and are never silently opened with other roles.
func New(deps Dependencies, policy Policy) (*Runtime, error) {
	if deps.DB == nil || !deps.Readiness.MatchesRuntimePool(deps.DB, deps.Database) || absent(deps.Models) || deps.Authenticate == nil || deps.RequestAuthority == nil || deps.ServiceAuthority == nil || absent(deps.IDs) || absent(deps.Clock) {
		return nil, mysql.ErrOwnerContractUnavailable
	}
	if _, err := mysqlstore.Identifier(deps.Database); err != nil {
		return nil, err
	}
	if policy.MaxRows < 1 || policy.MaxRows > 10000 || policy.MaxBytes < 1024 || policy.MaxBytes > 64<<20 || policy.HTTPMaxBytes < 1024 || policy.HTTPMaxBytes > policy.MaxBytes || policy.MaxWorkerTenants < 1 || policy.MaxWorkerTenants > 64 || policy.ReadinessTimeout < time.Millisecond || policy.ReadinessTimeout > 3*time.Second || policy.Lease < time.Millisecond || policy.Lease > time.Hour || policy.Lease%time.Microsecond != 0 {
		return nil, errors.New("explicit bounded application, tenant-worker, lease and readiness budgets required")
	}
	if policy.StartupTimeout < time.Millisecond || policy.StartupTimeout > 5*time.Minute {
		return nil, errors.New("bounded cold-start timeout required")
	}
	if policy.MaxWorkerSlots < 1 || policy.MaxWorkerSlots > 1024 {
		return nil, errors.New("explicit total worker-slot budget in [1,1024] required")
	}
	if err := policy.Evaluation.Limits.Validate(); err != nil {
		return nil, err
	}
	if policy.Evaluation.Limits.MaxItems > policy.MaxRows || policy.Evaluation.Limits.MaxBytes > policy.MaxBytes {
		return nil, mysql.ErrProjectionLimit
	}
	if err := policy.Wait.Validate(); err != nil {
		return nil, err
	}
	if policy.RequestTimeout < time.Millisecond || policy.RequestTimeout > 5*time.Minute || policy.MaintenancePageSize > 100 || policy.MaintenancePagesPerTick > 100 || policy.MaintenanceInterval < 10*time.Millisecond || policy.MaintenanceInterval > time.Hour || policy.MaintenanceTimeout > time.Minute {
		return nil, errors.New("bounded request and maintenance budgets required")
	}
	if policy.ReadHeaderTimeout < time.Millisecond || policy.ReadHeaderTimeout > 10*time.Second || policy.HTTPIdleTimeout < time.Millisecond || policy.HTTPIdleTimeout > 5*time.Minute || policy.HTTPMaxHeaderBytes < 1024 || policy.HTTPMaxHeaderBytes > 64<<10 {
		return nil, errors.New("explicit bounded HTTP header/idle policy required")
	}
	if _, err := workers.NewMaintenanceScheduler[mysql.DeliveryMaintenanceCursor](policy.MaintenancePageSize, policy.MaintenancePagesPerTick, policy.MaintenanceInterval, policy.MaintenanceTimeout); err != nil {
		return nil, err
	}
	if len(deps.Dispatch) != len(dispatchKinds) || len(policy.Workers) != len(allKinds) {
		return nil, mysql.ErrOwnerContractUnavailable
	}
	copyDeps := deps
	copyDeps.Dispatch = make(map[ports.JobKind]ports.ReceiptDispatcher, len(dispatchKinds))
	for _, kind := range dispatchKinds {
		if absent(deps.Dispatch[kind]) {
			return nil, mysql.ErrOwnerContractUnavailable
		}
		copyDeps.Dispatch[kind] = deps.Dispatch[kind]
	}
	// Detach mutable configuration maps/slices before storing the composition.
	policy.Workers = cloneMap(policy.Workers)
	policy.Effects = cloneMap(policy.Effects)
	policy.Execution.Queues = cloneMap(policy.Execution.Queues)
	policy.Evaluation.Aggregates = append([]string(nil), policy.Evaluation.Aggregates...)
	for _, kind := range allKinds {
		if worker, ok := policy.Workers[kind]; !ok || worker.Concurrency < 1 || worker.Concurrency > policy.MaxWorkerSlots {
			return nil, mysql.ErrOwnerContractUnavailable
		}
	}
	return &Runtime{copyDeps, policy}, nil
}
func cloneMap[K comparable, V any](source map[K]V) map[K]V {
	out := make(map[K]V, len(source))
	for k, v := range source {
		out[k] = v
	}
	return out
}

// Scope constructs one same-database, same-pool, same-tenant graph. It retains
// configured stores, never transactions or mutable request authority caches.
func (r *Runtime) Scope(ctx context.Context, tenant uuid.UUID, service bool) (*Scope, error) {
	if r == nil || tenant == uuid.Nil {
		return nil, mysql.ErrTenantScope
	}
	provider := r.deps.RequestAuthority
	if service {
		provider = r.deps.ServiceAuthority
	}
	if provider == nil {
		return nil, mysql.ErrDecisionAuthorizationUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, context.Cause(ctx)
	}
	a, err := provider(ctx, tenant)
	if err != nil {
		return nil, err
	}
	if a.TenantID != tenant {
		return nil, mysql.ErrTenantScope
	}
	if a.Authoring == nil || a.Reads == nil || a.Persist == nil || a.Executions == nil || service && a.Worker == nil {
		return nil, mysql.ErrDecisionAuthorizationUnavailable
	}
	db, database, p := r.deps.DB, r.deps.Database, r.policy
	executions, err := mysql.NewExecutionStore(db, database, tenant, p.MaxRows, p.MaxBytes, a.Executions, p.Execution)
	if err != nil {
		return nil, err
	}
	reader, err := mysql.NewTenantDataReader(db, database, tenant, p.MaxRows, p.MaxBytes, r.deps.Models, a.Reads)
	if err != nil {
		return nil, err
	}
	configuration, err := mysql.NewConfigurationStore(db, database, tenant, p.MaxRows, p.MaxBytes, a.Authoring)
	if err != nil {
		return nil, err
	}
	history, err := mysql.NewDecisionStore(db, database, tenant, p.MaxRows, p.MaxBytes, a.Reads)
	if err != nil {
		return nil, err
	}
	platform, err := mysql.NewPlatformStore(db, database, tenant, p.MaxRows, p.MaxBytes, a.Reads)
	if err != nil {
		return nil, err
	}
	lists, err := mysql.NewCustomListStore(db, database, tenant, p.MaxRows, p.MaxBytes, a.Authoring)
	if err != nil {
		return nil, err
	}
	reads, err := mysql.NewEvaluationReaders(executions, configuration, history, platform, lists)
	if err != nil {
		return nil, err
	}
	preparer, err := mysqlexecution.NewTargetPreparer(executions, reader, reads, r.deps.IDs, r.deps.Clock, r.deps.GeoIP, p.Evaluation)
	if err != nil {
		return nil, err
	}
	source, err := mysql.NewScreeningSourceContract(database, p.MaxBytes)
	if err != nil {
		return nil, err
	}
	effects, err := mysql.NewQueuedEffectStore(db, database, tenant, p.MaxRows, p.MaxBytes, a.Persist, &source, p.Effects)
	if err != nil {
		return nil, err
	}
	// List reads have their own current authorization. This grants no list
	// mutation merely because a caller may persist an evaluation.
	effects, err = effects.WithCustomLists(a.Authoring, false)
	if err != nil {
		return nil, err
	}
	unit, err := mysql.NewDecisionUnitOfWorkStore(effects, executions)
	if err != nil {
		return nil, err
	}
	admission, err := mysqlexecution.NewAdmission(preparer, unit, r.deps.IDs, r.deps.Clock, p.Admission)
	if err != nil {
		return nil, err
	}
	read, err := mysqlapi.NewExecutionAdapter(executions)
	if err != nil {
		return nil, err
	}
	submission, err := mysqlapi.NewExecutionSubmission(admission, read, p.Wait)
	if err != nil {
		return nil, err
	}
	schedules, err := mysqlexecution.NewScheduleAdmin(preparer, p.Admission.ScheduledMaxAttempts)
	if err != nil {
		return nil, err
	}
	authoring, err := mysql.NewAuthoringStore(db, database, tenant, p.MaxRows, p.MaxBytes, a.Authoring)
	if err != nil {
		return nil, err
	}
	publication, err := mysqlexecution.NewPublication(authoring, reader, p.Evaluation.Limits)
	if err != nil {
		return nil, err
	}
	var browser *mysql.BrowserWorkflowStore
	if !service && a.Session != nil {
		if a.Session.TenantID != tenant {
			return nil, mysql.ErrTenantScope
		}
		browser, err = mysql.NewBrowserWorkflowStore(db, database, *a.Session, p.MaxRows, p.MaxBytes)
		if err != nil {
			return nil, err
		}
	}
	return &Scope{Executions: executions, Effects: effects, Unit: unit, Preparer: preparer, Publication: publication, browser: browser, Endpoint: &mysqlapi.ExecutionEndpointScope{Read: read, Submission: submission, Schedules: schedules}, worker: a.Worker}, nil
}

func (s *Scope) BrowserWorkflows() (*mysql.BrowserWorkflowStore, error) {
	if s == nil || s.browser == nil {
		return nil, mysql.ErrOwnerContractUnavailable
	}
	return s.browser, nil
}
