package mysqlruntime

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	store "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/installation"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/worker"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/ec2test"
	"github.com/google/uuid"
)

const localWorkerActor = "data-model-mysql-index-worker/v1"

func localIndexQueue(db *sql.DB) (*store.IndexDeliveryStore, error) {
	return store.NewIndexDeliveryStoreWithRetryPolicy(db, "fraud", "index_jobs", 3, time.Minute, worker.RiverDeliveryRetryPolicy{})
}

// RunWorker composes the actual MySQL delivery host for the explicit local
// administrator runtime. Target pool 8 + reference pool 4; one DDL slot, one
// heartbeat and one paced maintenance call, within the shared DB's 100 slots.
func (p *LocalCreation) RunWorker(ctx context.Context, logger *slog.Logger) error {
	if p == nil || p.Target == nil || p.Reference == nil || logger == nil {
		return errors.New("configured worker pools and logger required")
	}
	ctx = context.WithValue(ctx, localAuthorityKey{}, true)
	queue, err := localIndexQueue(p.Target)
	if err != nil {
		return err
	}
	supervisor, err := worker.NewLeaseSupervisor(time.Minute, 10*time.Second, 5*time.Second, 50*time.Second)
	if err != nil {
		return err
	}
	poller, err := worker.NewDeliveryPoller(1, time.Second, 5*time.Second, supervisor)
	if err != nil {
		return err
	}
	maintenance, err := worker.NewDeliveryMaintenance(100, 1, 5*time.Second, 5*time.Second)
	if err != nil {
		return err
	}
	run, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan error, 2)
	go func() { done <- maintenance.Run(run, queue) }()
	go func() {
		done <- poller.Run(run, func(c context.Context) (*worker.DeliveryTask, error) {
			if err := p.initializeUnusedDeliveryHistory(c); err != nil {
				return nil, err
			}
			lease, err := queue.Claim(c)
			if err != nil || lease == nil {
				return nil, err
			}
			return &worker.DeliveryTask{
				Heartbeat: func(c context.Context) error { return queue.Heartbeat(c, *lease) },
				Execute:   func(c context.Context) error { return p.executeDelivery(c, queue, *lease, logger) },
			}, nil
		}, func(_ context.Context, err error) error {
			logger.Error("index delivery failed; retained for retry/recovery", "error", err)
			return nil
		})
	}()
	first := <-done
	cancel()
	return errors.Join(first, <-done)
}

// The earlier local API did not populate v002 error_count. Only never-claimed,
// error-free local deliveries have provable zero history. Audit their explicit
// translation; attempted/ambiguous legacy history continues to fail closed.
func (p *LocalCreation) initializeUnusedDeliveryHistory(ctx context.Context) error {
	return mysqlstore.Mutation(ctx, p.Target, func(tx *sql.Tx) error {
		if err := localAuthority(ctx, tx); err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, "SELECT id,tenant_id,attempt,error_count,last_error_code FROM fraud.data_model_index_deliveries WHERE queue_name='index_jobs' AND state='available' AND available_at<=UTC_TIMESTAMP(6) ORDER BY available_at,id LIMIT 25 FOR UPDATE SKIP LOCKED")
		if err != nil {
			return err
		}
		type item struct{ id, tenant mysqlstore.UUID }
		var items []item
		for rows.Next() {
			var v item
			var attempt int
			var count sql.NullInt64
			var code string
			if err := rows.Scan(&v.id, &v.tenant, &attempt, &count, &code); err != nil {
				rows.Close()
				return err
			}
			if attempt == 0 && !count.Valid && code == "" {
				items = append(items, v)
			}
		}
		err = errors.Join(rows.Err(), rows.Close())
		if err != nil {
			return err
		}
		for _, v := range items {
			if _, err := tx.ExecContext(ctx, "UPDATE fraud.data_model_index_deliveries SET error_count=0 WHERE tenant_id=? AND id=? AND attempt=0 AND error_count IS NULL", v.tenant, v.id); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "INSERT INTO fraud.data_model_schema_change_log(id,tenant_id,operation,resource_type,resource_id,status,details,created_at) VALUES (?,?,'initialize_unused_delivery_history','index_delivery',?,'applied',JSON_OBJECT('source','never_claimed_delivery','attempt',0,'error_count',0),UTC_TIMESTAMP(6))", mysqlstore.UUID(uuid.New()), v.tenant, v.id); err != nil {
				return err
			}
		}
		return nil
	})
}

func (p *LocalCreation) workerAuthority(ctx context.Context, tx *sql.Tx, tenant uuid.UUID) error {
	if err := localTenantAuthority(ctx, tx, tenant); err != nil {
		return err
	}
	var active bool
	if err := tx.QueryRowContext(ctx, "SELECT status='active' FROM fraud.data_model_tenants WHERE id=?", mysqlstore.UUID(tenant)).Scan(&active); err != nil {
		return err
	}
	if !active {
		return store.ErrModelUnavailable
	}
	return nil
}

func (p *LocalCreation) executeDelivery(ctx context.Context, queue *store.IndexDeliveryStore, lease store.DeliveryLease, logger *slog.Logger) (result error) {
	execution, err := store.NewLeasedIndexJobExecutionStore(queue, lease, localMaxBytes, 0)
	if err != nil {
		return err
	}
	execution, err = execution.WithAuthorizer(p.workerAuthority)
	if err != nil {
		return err
	}
	defer func() {
		if result != nil && ctx.Err() == nil {
			result = errors.Join(result, p.failDelivery(ctx, execution, lease, result))
		}
		if result != nil {
			logger.Error("MySQL index attempt failed", "tenant_id", lease.TenantID, "job_id", lease.JobID, "delivery_id", lease.ID, "delivery_attempt", lease.Attempt, "error", result)
		}
	}()
	var job datamodel.IndexJob
	err = mysqlstore.Snapshot(ctx, p.Target, func(tx *sql.Tx) error {
		if err := p.workerAuthority(ctx, tx, lease.TenantID); err != nil {
			return err
		}
		r, err := store.NewMetadataRepositories(tx, "fraud", lease.TenantID, localMaxRows, localMaxBytes)
		if err != nil {
			return err
		}
		job, err = r.Jobs.GetByID(ctx, lease.JobID)
		return err
	})
	if err != nil {
		return err
	}
	if job.Status == datamodel.IndexJobStatusApplied || job.Status == datamodel.IndexJobStatusFailed || job.Status == datamodel.IndexJobStatusCancelled {
		_, err := execution.StartAttempt(ctx, job.ID, localClock{}.Now())
		return err
	}
	if job.TableID == nil {
		return store.ErrPhysicalConflict
	}
	table := *job.TableID
	sum := sha256.Sum256([]byte("local-field/" + table.String()))
	return mysqlstore.WithDDLLock(ctx, p.Target, "dm_local_field_"+hex.EncodeToString(sum[:20]), func(_ *sql.Conn) error {
		work, err := p.loadIndexWork(ctx, lease, table)
		if err != nil {
			return err
		}
		metadata, err := store.NewTransactionManager(p.Target, "fraud", lease.TenantID, localMaxRows, localMaxBytes, p.workerAuthority, queue)
		if err != nil {
			return err
		}
		publication, err := store.NewPublicationStore(p.Target, "fraud", localMaxRows, localMaxRows, localMaxBytes)
		if err != nil {
			return err
		}
		manager, err := store.NewPhysicalPublicationManager(metadata, publication, work.Physical, localPhysicalPolicies(work.Physical), func(c context.Context, id uuid.UUID, request ports.PhysicalPublicationRequest) error {
			if id != lease.TenantID || request.TableID != table || request.OperationID != lease.JobID || request.ActorKind != "worker" || request.ActorID != localWorkerActor {
				return store.ErrPhysicalConflict
			}
			return localAuthority(c, nil)
		})
		if err != nil {
			return err
		}
		defs := []installation.Definer{}
		for _, name := range []string{"local_dm_guard", "local_dm_creation", "local_dm_result", "local_dm_record", "local_dm_contract"} {
			defs = append(defs, installation.Definer{User: name, Host: "localhost"})
		}
		ready, err := installation.NewExactKeyReadinessWithLookups(p.Target, p.Plan, defs[0], defs[1], defs[2], defs[3], defs[4])
		if err != nil {
			return err
		}
		installerHost, err := ec2test.InstallerHost()
		if err != nil {
			return err
		}
		installer := installation.RecordInstallerPolicy{Principal: installation.Definer{User: "lcn_" + strings.ReplaceAll(table.String(), "-", "")[:24], Host: installerHost}, Table: work.Physical.Name, NativeChanges: true}
		manager, err = manager.WithCreationReadiness(ready, installer)
		if err != nil {
			return err
		}
		err = manager.RunManagedIndexJobWithExecutor(ctx, execution, work, localIDs{}, localClock{}, func(c context.Context, table, op uuid.UUID) error {
			return p.executeNative(c, lease.TenantID, table, op, installer)
		})
		if err == nil {
			logger.Info("MySQL index job applied", "tenant_id", lease.TenantID, "job_id", lease.JobID, "index_type", job.IndexType)
		}
		return err
	})
}

// Load only the exact published predecessor, or the immutable inputs of this
// operation after interruption. No target SHOW CREATE is adopted as expected SQL.
func (p *LocalCreation) loadIndexWork(ctx context.Context, lease store.DeliveryLease, table uuid.UUID) (work store.NativeIndexWork, result error) {
	work.ActorID = localWorkerActor
	work.Physical.TableID = table
	var raw []byte
	var expected mysqlstore.UUID
	var epoch int64
	err := p.Target.QueryRowContext(ctx, "SELECT i.expected_epoch,i.expected_version,i.desired_shape FROM fraud.data_model_schema_change_intents i WHERE i.tenant_id=? AND i.table_id=? AND i.operation_id=? AND i.actor_kind='worker' AND i.actor_id=?", mysqlstore.UUID(lease.TenantID), mysqlstore.UUID(table), mysqlstore.UUID(lease.JobID), localWorkerActor).Scan(&epoch, &expected, &raw)
	replay := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return work, err
	}
	var gate string
	if err := p.Target.QueryRowContext(ctx, "SELECT physical_name,epoch,published_version,write_state FROM fraud.data_model_physical_tables WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(lease.TenantID), mysqlstore.UUID(table)).Scan(&work.Physical.Name, &work.Physical.Epoch, (*mysqlstore.UUID)(&work.Physical.Version), &gate); err != nil {
		return work, err
	}
	if replay {
		var saved store.PhysicalDDLPlan
		if err := json.Unmarshal(raw, &saved); err != nil {
			return work, err
		}
		if saved.Native == nil {
			return work, store.ErrPhysicalConflict
		}
		work.Physical.Epoch, work.Physical.Version = epoch, uuid.UUID(expected)
		work.Fields, work.Reference = saved.Native.Fields, saved.Steps
		work.RepairMissing = saved.RepairMissing
	} else {
		if gate != "open" {
			return work, store.ErrModelUnavailable
		}
		if err := mysqlstore.Snapshot(ctx, p.Target, func(tx *sql.Tx) error {
			if err := p.workerAuthority(ctx, tx, lease.TenantID); err != nil {
				return err
			}
			r, err := store.NewMetadataRepositories(tx, "fraud", lease.TenantID, localMaxRows, localMaxBytes)
			if err != nil {
				return err
			}
			fields, err := r.Fields.ListByTable(ctx, table)
			if err != nil {
				return err
			}
			registry, err := store.NewPhysicalRegistry(tx, "fraud", lease.TenantID, localMaxRows)
			if err != nil {
				return err
			}
			columns, err := registry.Fields(ctx, table)
			if err != nil {
				return err
			}
			byID := map[uuid.UUID][]store.PhysicalField{}
			for _, c := range columns {
				byID[c.FieldID] = append(byID[c.FieldID], c)
			}
			for _, f := range fields {
				work.Fields = append(work.Fields, store.MappedPhysicalField{Field: f, Components: byID[f.ID]})
			}
			return nil
		}); err != nil {
			return work, err
		}
	}
	if err := p.Target.QueryRowContext(ctx, "SELECT desired_shape FROM fraud.data_model_schema_change_intents WHERE tenant_id=? AND table_id=? AND expected_epoch=? AND state='published'", mysqlstore.UUID(lease.TenantID), mysqlstore.UUID(table), work.Physical.Epoch-1).Scan(&raw); err != nil {
		return work, err
	}
	if err := json.Unmarshal(raw, &work.Previous); err != nil {
		return work, err
	}
	if !replay {
		var names []byte
		if err := p.Target.QueryRowContext(ctx, "SELECT columns_json FROM fraud.data_model_index_jobs WHERE tenant_id=? AND id=? AND table_id=?", mysqlstore.UUID(lease.TenantID), mysqlstore.UUID(lease.JobID), mysqlstore.UUID(table)).Scan(&names); err != nil {
			return work, err
		}
		var columns []string
		if err := json.Unmarshal(names, &columns); err != nil {
			return work, err
		}
		ids := []uuid.UUID{}
		for _, name := range columns {
			for _, f := range work.Fields {
				if f.Field.Name == name && !f.Field.Archived {
					ids = append(ids, f.Field.ID)
				}
			}
		}
		if len(ids) != len(columns) {
			return work, store.ErrPhysicalConflict
		}
		planner, err := store.NewPhysicalPlanner("fraud", lease.TenantID, work.Physical, work.Fields)
		if err != nil {
			return work, err
		}
		statement, err := planner.CreateManagedIndex(lease.JobID, ids)
		if err != nil {
			return work, err
		}
		// Compare the target only to identify absent owner-managed index names.
		// Expected table/guard definitions always come from retained publication.
		actual := map[string]bool{}
		rows, err := p.Target.QueryContext(ctx, "SELECT DISTINCT INDEX_NAME FROM information_schema.statistics WHERE TABLE_SCHEMA='fraud' AND TABLE_NAME=? LIMIT 65", work.Physical.Name)
		if err != nil {
			return work, err
		}
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				rows.Close()
				return work, err
			}
			actual[name] = true
		}
		err = errors.Join(rows.Err(), rows.Close())
		if err != nil {
			return work, err
		}
		work.RepairMissing, err = store.MissingManagedIndexes(work.Previous.FinalTableDefinition(), actual)
		if err != nil {
			return work, err
		}
		statements := []store.PhysicalStatement{statement}
		baseline := work.Previous
		if len(work.RepairMissing) > 0 {
			restore, e := planner.RestoreManagedIndexes(work.Previous.FinalTableDefinition(), work.RepairMissing)
			if e != nil {
				return work, e
			}
			statements = append(restore, statement)
			before, e := store.ManagedRepairBefore(work.Previous.FinalTableDefinition(), work.RepairMissing)
			if e != nil {
				return work, e
			}
			baseline.Steps = append(baseline.Steps[:0:0], baseline.Steps...)
			for i := len(baseline.Steps) - 1; i >= 0; i-- {
				if baseline.Steps[i].Kind == "TABLE" {
					baseline.Steps[i].After = before
					break
				}
			}
		}
		work.Reference, err = p.nativeReferences(ctx, work.Physical, baseline, statements)
		if err != nil {
			return work, err
		}
	}
	return work, nil
}

func (p *LocalCreation) failDelivery(ctx context.Context, execution *store.LeasedIndexJobExecutionStore, lease store.DeliveryLease, cause error) error {
	var job datamodel.IndexJob
	err := mysqlstore.Snapshot(ctx, p.Target, func(tx *sql.Tx) error {
		if err := p.workerAuthority(ctx, tx, lease.TenantID); err != nil {
			return err
		}
		r, err := store.NewMetadataRepositories(tx, "fraud", lease.TenantID, localMaxRows, localMaxBytes)
		if err != nil {
			return err
		}
		job, err = r.Jobs.GetByID(ctx, lease.JobID)
		return err
	})
	if err != nil {
		return err
	}
	if job.Status != datamodel.IndexJobStatusRunning {
		job, err = execution.StartAttempt(ctx, job.ID, localClock{}.Now())
		if err != nil {
			return err
		}
		if job.Status != datamodel.IndexJobStatusRunning {
			return nil
		}
	}
	status := "pending"
	operation := "reschedule_index_job"
	if lease.Attempt >= 3 {
		status = "failed"
		operation = "fail_index_job"
	}
	message := cause.Error()
	if len(message) > 2048 {
		message = message[:2048]
	}
	return execution.CompleteAttempt(ctx, ports.IndexJobOutcome{Job: job, ErrorMessage: message, RetryDelivery: true, Change: datamodel.SchemaChange{ID: uuid.New(), TenantID: lease.TenantID, ResourceType: "index_job", ResourceID: job.ID, Operation: operation, Status: status, Details: []byte(`{"code":"index_execution_failed"}`), CreatedAt: localClock{}.Now()}})
}
