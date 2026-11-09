package mysql

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/migrations"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

// NativeIndexWork is trusted, retained owner input, never an HTTP DTO. Reference
// definitions must come from the isolated reference builder, not live drift.
// Preserve this input across retries, including the original epoch and version.
type NativeIndexWork struct {
	Previous      PhysicalDDLPlan
	Fields        []MappedPhysicalField
	Reference     []migrations.Step
	Physical      PhysicalTable
	ActorID       string
	RepairMissing []uuid.UUID
}

// RunNativeIndexJob executes one committed lease through typed planning, durable
// intent, pinned DDL, publication and fenced completion. The caller supervises
// lease renewal/cancellation. Failures retain the immutable intent and return an
// error; they never manufacture an applied result or adopt application drift.
// This staged composition supports native search indexes only. String indexes
// need a separately reviewed text strategy; repair/navigation are unsupported.
func (m *PhysicalPublicationManager) RunNativeIndexJob(ctx context.Context, execution *LeasedIndexJobExecutionStore, work NativeIndexWork, ids ports.IDGenerator, clock ports.Clock) error {
	return m.runIndexJob(ctx, execution, work, ids, clock, false, nil)
}

// RunManagedIndexJob accepts all owner index-job kinds. Text keys use bounded,
// nonunique prefixes; complete query values and exact unique guards are retained.
func (m *PhysicalPublicationManager) RunManagedIndexJob(ctx context.Context, execution *LeasedIndexJobExecutionStore, work NativeIndexWork, ids ports.IDGenerator, clock ports.Clock) error {
	return m.runIndexJob(ctx, execution, work, ids, clock, true, nil)
}

// The trusted host may use a temporary table-scoped installer for DDL. The
// callback executes only after the immutable intent and lease fence commit.
func (m *PhysicalPublicationManager) RunManagedIndexJobWithExecutor(ctx context.Context, execution *LeasedIndexJobExecutionStore, work NativeIndexWork, ids ports.IDGenerator, clock ports.Clock, execute func(context.Context, uuid.UUID, uuid.UUID) error) error {
	if execute == nil {
		return errors.New("scoped physical executor required")
	}
	return m.runIndexJob(ctx, execution, work, ids, clock, true, execute)
}

func (m *PhysicalPublicationManager) runIndexJob(ctx context.Context, execution *LeasedIndexJobExecutionStore, work NativeIndexWork, ids ports.IDGenerator, clock ports.Clock, managed bool, execute func(context.Context, uuid.UUID, uuid.UUID) error) error {
	if m == nil || m.executor == nil || m.executor.creation == nil || execution == nil || execution.delivery == nil || ids == nil || clock == nil || work.ActorID == "" || work.Physical.TableID != m.table || work.Physical.Version == uuid.Nil {
		return errors.New("publication readiness, leased execution, retained physical snapshot, actor, IDs and clock required")
	}
	if execution.delivery.db != m.metadata.db || execution.delivery.schema != m.executor.schema || execution.lease.TenantID != m.metadata.tenant {
		return errors.New("native worker requires the same owner pool, database and tenant")
	}
	boundExecution := *execution
	boundExecution.authorize = m.metadata.authorize
	execution = &boundExecution
	var job datamodel.IndexJob
	if err := mysqlstore.Mutation(ctx, m.metadata.db, func(tx *sql.Tx) error {
		if err := m.metadata.authorize(ctx, tx, m.metadata.tenant); err != nil {
			return err
		}
		r := IndexJobRepository{metadataRepository{tx, m.executor.schema, m.metadata.tenant, 1000, execution.maxBytes}}
		var err error
		job, err = r.GetByID(ctx, execution.lease.JobID)
		return err
	}); err != nil {
		return err
	}
	if job.TableID == nil || *job.TableID != m.table || job.TenantID != m.metadata.tenant || (!managed && job.IndexType != "search") {
		return ErrPhysicalPlanningUnsupported
	}
	if err := datamodel.ValidateIndexJobCreate(job.IndexType, job.Columns); err != nil {
		return err
	}
	planner, err := NewPhysicalPlanner(m.metadata.database, m.metadata.tenant, work.Physical, work.Fields)
	if err != nil {
		return err
	}
	byName := make(map[string]uuid.UUID, len(work.Fields))
	for _, field := range work.Fields {
		if !field.Field.Archived {
			byName[field.Field.Name] = field.Field.ID
		}
	}
	columns := make([]uuid.UUID, len(job.Columns))
	for i, name := range job.Columns {
		id, ok := byName[name]
		if !ok {
			return ErrPhysicalConflict
		}
		columns[i] = id
	}
	statement, err := planner.CreateIndex(job.ID, columns, false)
	if managed {
		statement, err = planner.CreateManagedIndex(job.ID, columns)
	}
	if err != nil {
		return err
	}
	statements := []PhysicalStatement{statement}
	var plan PhysicalDDLPlan
	if managed && len(work.RepairMissing) > 0 {
		restore, e := planner.RestoreManagedIndexes(work.Previous.FinalTableDefinition(), work.RepairMissing)
		if e != nil {
			return e
		}
		statements = append(restore, statement)
		plan, err = planner.BindManagedRepair(work.Previous, work.RepairMissing, statements, work.Reference)
	} else {
		plan, err = planner.BindRecordChanges(work.Previous, statements, work.Reference)
	}
	if err != nil {
		return err
	}
	raw, err := json.Marshal(plan)
	if err != nil {
		return err
	}
	// Job identity determines both physical index and durable operation. The
	// intent separately binds actor, epoch, version and exact desired shape.
	request := ports.PhysicalPublicationRequest{TableID: m.table, OperationID: job.ID, RequestHash: sha256.Sum256(raw), Condition: PublicationCondition{Mode: PublicationExactVersion, Version: work.Physical.Version}, ActorKind: "worker", ActorID: work.ActorID}
	if err := m.authorize(ctx, m.metadata.tenant, request); err != nil {
		return err
	}
	job, err = execution.StartAttempt(ctx, job.ID, clock.Now().UTC().Truncate(time.Microsecond))
	if err != nil {
		return err
	}
	if terminalIndexJob(job.Status) {
		return nil
	}
	intents, err := NewDDLIntentStore(m.metadata.db, m.metadata.database, m.metadata.tenant, 1<<20)
	if err != nil {
		return err
	}
	var prepared DDLIntentResult
	err = mysqlstore.Mutation(ctx, m.metadata.db, func(tx *sql.Tx) error {
		if err := m.metadata.authorize(ctx, tx, m.metadata.tenant); err != nil {
			return err
		}
		// Publication and preparation share model -> registry -> job -> delivery
		// ordering. No database row lock is held while executing physical DDL.
		var found mysqlstore.UUID
		if err := tx.QueryRowContext(ctx, "SELECT id FROM "+m.executor.schema+".data_model_tables WHERE tenant_id=? AND id=? AND name=? AND archived=FALSE FOR SHARE", mysqlstore.UUID(job.TenantID), mysqlstore.UUID(m.table), job.TableName).Scan(&found); err != nil {
			return err
		}
		var err error
		prepared, err = intents.PrepareTx(ctx, tx, DDLIntent{TableID: m.table, OperationID: job.ID, RequestHash: request.RequestHash, ActorKind: request.ActorKind, ActorID: request.ActorID, ExpectedEpoch: work.Physical.Epoch, ExpectedVersion: &work.Physical.Version, Shape: raw})
		if err != nil {
			return err
		}
		return m.lockNativeIndexAttempt(ctx, tx, execution, job)
	})
	if err != nil {
		return err
	}
	// A committed publication may have lost its acknowledgement. Reconcile its
	// receipt instead of trying to execute an already published intent again.
	if prepared.State != "published" {
		run := m.executor.ExecuteSteps
		if execute != nil {
			run = execute
		}
		if err := run(ctx, m.table, job.ID); err != nil {
			return err
		}
	}
	publication, err := m.Publish(ctx, request, func(s ports.MutationStore) error {
		store, ok := s.(metadataMutationStore)
		if !ok {
			return errors.New("owner publication transaction required")
		}
		return m.lockNativeIndexAttempt(ctx, store.tx, execution, job)
	})
	if err != nil {
		return err
	}
	fenced, err := execution.WithPublishedNativeIndex(m.table, publication.Version)
	if managed {
		fenced, err = execution.WithPublishedManagedIndex(m.table, publication.Version)
	}
	if err != nil {
		return err
	}
	details, err := json.Marshal(map[string]any{"table_id": m.table, "index_job_id": job.ID, "publication_version": publication.Version, "attempt_count": job.AttemptCount})
	if err != nil {
		return err
	}
	return fenced.CompleteAttempt(ctx, ports.IndexJobOutcome{Job: job, Change: datamodel.SchemaChange{ID: ids.New(), TenantID: job.TenantID, ResourceType: "index_job", ResourceID: job.ID, Operation: "apply_index_job", Status: "applied", Details: details, CreatedAt: clock.Now().UTC().Truncate(time.Microsecond)}})
}

func (m *PhysicalPublicationManager) lockNativeIndexAttempt(ctx context.Context, tx *sql.Tx, execution *LeasedIndexJobExecutionStore, job datamodel.IndexJob) error {
	if err := execution.delivery.lockJob(ctx, tx, job.TenantID, job.ID, false); err != nil {
		return err
	}
	r := IndexJobRepository{metadataRepository{tx, m.executor.schema, job.TenantID, 1000, execution.maxBytes}}
	current, err := r.GetByID(ctx, job.ID)
	if err != nil {
		return err
	}
	if current.Status != datamodel.IndexJobStatusRunning || current.AttemptCount != job.AttemptCount {
		return ports.ErrStaleIndexJobAttempt
	}
	return execution.checkLease(ctx, tx)
}
