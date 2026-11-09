package metadata_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/service"
	store "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/installation"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/migrations"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/integrationtest"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

// Called after actual creation and field publication, with retained reference
// artifacts. Privileged fixture setup is isolated; restricted worker grants and
// live identity composition remain separate checklist gates.
func verifyNativeIndexWorker(t *testing.T, ctx context.Context, db *sql.DB, database string, tenant, table uuid.UUID, name string, previous store.PhysicalDDLPlan, fields []store.MappedPhysicalField, shared *installation.ExactKeyReadiness, installer installation.RecordInstallerPolicy, policies []store.PhysicalAccessPolicy, scenario string, installed migrations.Plan) {
	t.Helper()
	schema, _ := mysqlstore.Identifier(database)
	denied := false
	var jobID uuid.UUID
	authorize := func(ctx context.Context, tx *sql.Tx, id uuid.UUID) error {
		if strings.HasSuffix(scenario, "_completion_denied") {
			var published bool
			if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM "+schema+".data_model_schema_change_intents WHERE tenant_id=? AND operation_id=? AND state='published')", mysqlstore.UUID(tenant), mysqlstore.UUID(jobID)).Scan(&published); err != nil {
				return err
			}
			if published {
				return errors.New("fixture completion authority revoked")
			}
		}
		if denied || id != tenant {
			return errors.New("fixture authority denied")
		}
		var probe int
		return tx.QueryRowContext(ctx, "SELECT 1").Scan(&probe)
	}
	queue, err := store.NewIndexDeliveryStore(db, database, "native_worker", 5, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	ids, clock := &registrationIDs{}, registrationClock{time.Now().UTC().Truncate(time.Microsecond)}
	app, err := store.NewIndexJobApplication(db, database, tenant, 100, 1<<20, authorize, authorize, queue, ids, clock)
	if err != nil {
		t.Fatal(err)
	}
	column := "score"
	managed := strings.Contains(scenario, "_managed_")
	if strings.HasSuffix(scenario, "_text") {
		column = "object_id"
	}
	var journey *integrationtest.Preparation
	var preparedID uuid.UUID
	if strings.HasSuffix(scenario, "_decision") {
		var logicalName string
		if err := db.QueryRowContext(ctx, "SELECT name FROM "+schema+".data_model_tables WHERE tenant_id=? AND id=?", mysqlstore.UUID(tenant), mysqlstore.UUID(table)).Scan(&logicalName); err != nil {
			t.Fatal(err)
		}
		journey, preparedID = integrationtest.Begin(t, db, database, tenant, table, logicalName, column)
	}
	var job datamodel.IndexJob
	jobType := datamodel.IndexJobTypeSearch
	unsupportedKind := !managed && (strings.HasSuffix(scenario, "_navigation") || strings.HasSuffix(scenario, "_repair"))
	if strings.HasSuffix(scenario, "_navigation") {
		jobType = datamodel.IndexJobTypeNavigation
	} else if strings.HasSuffix(scenario, "_repair") {
		jobType = datamodel.IndexJobTypeRepair
	}
	if journey != nil {
		job, err = app.Get(ctx, preparedID)
	} else {
		job, err = app.Create(ctx, service.CreateIndexJobInput{TenantID: tenant, TableID: table, IndexType: jobType, Columns: []string{column}, RequestedByOperation: "native_worker_fixture"})
	}
	if err != nil {
		t.Fatal(err)
	}
	jobID = job.ID
	physical := store.PhysicalTable{TableID: table, Name: name}
	var version mysqlstore.UUID
	if err := db.QueryRowContext(ctx, "SELECT epoch,published_version FROM "+schema+".data_model_physical_tables WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(tenant), mysqlstore.UUID(table)).Scan(&physical.Epoch, &version); err != nil {
		t.Fatal(err)
	}
	physical.Version = uuid.UUID(version)
	planner, err := store.NewPhysicalPlanner(database, tenant, physical, fields)
	if err != nil {
		t.Fatal(err)
	}
	var selected uuid.UUID
	for _, f := range fields {
		if f.Field.Name == column {
			selected = f.Field.ID
		}
	}
	var reference []migrations.Step
	if column != "object_id" || managed {
		statement, err := planner.CreateIndex(job.ID, []uuid.UUID{selected}, false)
		if managed {
			statement, err = planner.CreateManagedIndex(job.ID, []uuid.UUID{selected})
		}
		if err != nil {
			t.Fatal(err)
		}
		step := statement.ReferenceStep()
		conn, err := db.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		step.Before, err = migrations.Definition(ctx, conn, database, "TABLE", name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = conn.ExecContext(ctx, step.SQL); err != nil {
			t.Fatal(err)
		}
		step.After, err = migrations.Definition(ctx, conn, database, "TABLE", name)
		if err != nil {
			t.Fatal(err)
		}
		drop, err := planner.DropIndex(job.ID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = conn.ExecContext(ctx, drop.ReferenceStep().SQL); err != nil {
			t.Fatal(err)
		}
		if err = conn.Close(); err != nil {
			t.Fatal(err)
		}
		reference = []migrations.Step{step}
	}
	publication, err := store.NewPublicationStore(db, database, 100, 100, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := store.NewTransactionManager(db, database, tenant, 100, 1<<20, authorize, queue)
	if err != nil {
		t.Fatal(err)
	}
	actor := uuid.NewString()
	publisher, err := store.NewPhysicalPublicationManager(metadata, publication, physical, policies, func(_ context.Context, id uuid.UUID, request ports.PhysicalPublicationRequest) error {
		if strings.HasSuffix(scenario, "_publication_expired") {
			var verified bool
			if err := db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM "+schema+".data_model_schema_change_intents WHERE tenant_id=? AND operation_id=? AND state='verifying')", mysqlstore.UUID(tenant), mysqlstore.UUID(job.ID)).Scan(&verified); err != nil {
				return err
			}
			if verified {
				_, err := db.ExecContext(ctx, "UPDATE "+schema+".data_model_index_deliveries SET lease_until=UTC_TIMESTAMP(6)-INTERVAL 1 SECOND WHERE tenant_id=? AND job_id=?", mysqlstore.UUID(tenant), mysqlstore.UUID(job.ID))
				if err != nil {
					return err
				}
			}
		}
		if denied || id != tenant || request.ActorID != actor || request.ActorKind != "worker" || request.OperationID != job.ID {
			return errors.New("fixture publication authority denied")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	publisher, err = publisher.WithCreationReadiness(shared, installer)
	if err != nil {
		t.Fatal(err)
	}
	lease, err := queue.Claim(ctx)
	if err != nil || lease == nil {
		t.Fatal(lease, err)
	}
	execution, err := store.NewLeasedIndexJobExecutionStore(queue, *lease, 1<<20, 0)
	if err != nil {
		t.Fatal(err)
	}
	if journey != nil {
		running, e := execution.StartAttempt(ctx, job.ID, clock.Now())
		if e != nil {
			t.Fatal(e)
		}
		e = execution.CompleteAttempt(ctx, ports.IndexJobOutcome{Job: running, ErrorMessage: "injected owner failure", Change: datamodel.SchemaChange{ID: uuid.New(), TenantID: tenant, ResourceID: job.ID, ResourceType: "index_job", Operation: "fail_index_job", Status: "failed", Details: []byte(`{}`), CreatedAt: clock.Now()}})
		if e != nil {
			t.Fatal(e)
		}
		journey.RetryAfterFailure()
		// Reconstruct the worker's queue and execution adapter from durable rows.
		queue, e = store.NewIndexDeliveryStore(db, database, "native_worker", 5, 5*time.Minute)
		if e != nil {
			t.Fatal(e)
		}
		lease, e = queue.Claim(ctx)
		if e != nil || lease == nil {
			t.Fatal(lease, e)
		}
		execution, e = store.NewLeasedIndexJobExecutionStore(queue, *lease, 1<<20, 0)
		if e != nil {
			t.Fatal(e)
		}
	}
	work := store.NativeIndexWork{Previous: previous, Fields: fields, Reference: reference, Physical: physical, ActorID: actor}
	if strings.HasSuffix(scenario, "_worker_denied") {
		denied = true
	}
	if strings.HasSuffix(scenario, "_worker_expired") {
		if _, err := db.ExecContext(ctx, "UPDATE "+schema+".data_model_index_deliveries SET lease_until=UTC_TIMESTAMP(6)-INTERVAL 1 SECOND WHERE id=?", mysqlstore.UUID(lease.ID)); err != nil {
			t.Fatal(err)
		}
	}
	if strings.HasSuffix(scenario, "_foreign_tenant") {
		foreign := *lease
		foreign.TenantID = uuid.New()
		execution, err = store.NewLeasedIndexJobExecutionStore(queue, foreign, 1<<20, 0)
		if err != nil {
			t.Fatal(err)
		}
	}
	if strings.HasSuffix(scenario, "_other_pool") {
		other, err := sql.Open("mysql", "")
		if err != nil {
			t.Fatal(err)
		}
		defer other.Close()
		otherQueue, err := store.NewIndexDeliveryStore(other, database, "native_worker", 5, 5*time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		execution, err = store.NewLeasedIndexJobExecutionStore(otherQueue, *lease, 1<<20, 0)
		if err != nil {
			t.Fatal(err)
		}
	}
	if strings.HasSuffix(scenario, "_audit_failure") || journey != nil {
		if _, err := db.ExecContext(ctx, "CREATE TRIGGER "+schema+".fixture_native_audit_failure BEFORE INSERT ON "+schema+".data_model_schema_change_log FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='injected index audit failure'"); err != nil {
			t.Fatal(err)
		}
	}
	runIndex := publisher.RunNativeIndexJob
	if managed {
		runIndex = publisher.RunManagedIndexJob
	}
	err = runIndex(ctx, execution, work, ids, clock)
	if strings.HasSuffix(scenario, "_completion_denied") || strings.HasSuffix(scenario, "_publication_expired") {
		if err == nil {
			t.Fatal("worker committed after authority/lease loss")
		}
		assertNativeWorkerState(t, ctx, db, schema, tenant, job.ID, "running", "running", 1, 0)
		wantState, wantGate := "published", "open"
		if strings.HasSuffix(scenario, "_publication_expired") {
			if !errors.Is(err, store.ErrStaleDeliveryLease) {
				t.Fatal("unexpected publication failure", err)
			}
			wantState, wantGate = "verifying", "closed"
		} else if !strings.Contains(err.Error(), "fixture completion authority revoked") {
			t.Fatal("unexpected completion failure", err)
		}
		var state, gate string
		if e := db.QueryRowContext(ctx, "SELECT i.state,p.write_state FROM "+schema+".data_model_schema_change_intents i JOIN "+schema+".data_model_physical_tables p ON p.tenant_id=i.tenant_id AND p.table_id=i.table_id WHERE i.tenant_id=? AND i.operation_id=?", mysqlstore.UUID(tenant), mysqlstore.UUID(job.ID)).Scan(&state, &gate); e != nil || state != wantState || gate != wantGate {
			t.Fatal("incorrect recovery state", state, gate, e)
		}
		return
	}
	if unsupportedKind || strings.HasSuffix(scenario, "_denied") || strings.HasSuffix(scenario, "_expired") || (!managed && strings.HasSuffix(scenario, "_text")) || strings.HasSuffix(scenario, "_foreign_tenant") || strings.HasSuffix(scenario, "_other_pool") {
		if err == nil {
			t.Fatal("invalid native worker accepted")
		}
		if (unsupportedKind || strings.HasSuffix(scenario, "_text")) && !errors.Is(err, store.ErrPhysicalPlanningUnsupported) {
			t.Fatal("unsupported index strategy did not fail explicitly", err)
		}
		assertNativeWorkerState(t, ctx, db, schema, tenant, job.ID, "pending", "running", 0, 0)
		var count int
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.statistics WHERE TABLE_SCHEMA=? AND TABLE_NAME=? AND INDEX_NAME=?", database, name, "record_index_"+strings.ReplaceAll(job.ID.String(), "-", "")).Scan(&count); err != nil || count != 0 {
			t.Fatal("invalid worker performed DDL", count, err)
		}
		return
	}
	if strings.HasSuffix(scenario, "_audit_failure") || journey != nil {
		if err == nil {
			t.Fatal("audit failure accepted")
		}
		assertNativeWorkerState(t, ctx, db, schema, tenant, job.ID, "running", "running", 1, 0)
		if _, err := db.ExecContext(ctx, "DROP TRIGGER "+schema+".fixture_native_audit_failure"); err != nil {
			t.Fatal(err)
		}
		// Fresh execution adapter simulates restart after publication committed
		// but atomic job/audit/ack failed. The same immutable operation replays.
		execution, err = store.NewLeasedIndexJobExecutionStore(queue, *lease, 1<<20, 0)
		if err != nil {
			t.Fatal(err)
		}
		err = runIndex(ctx, execution, work, ids, clock)
	}
	if err != nil {
		t.Fatal("native worker", err)
	}
	assertNativeWorkerState(t, ctx, db, schema, tenant, job.ID, "applied", "completed", 1, 1)
	var ready bool
	if err := db.QueryRowContext(ctx, "SELECT p.write_state='open' AND p.active_operation IS NULL AND p.published_version=o.version FROM "+schema+".data_model_physical_tables p JOIN "+schema+".data_model_publication_operations o ON o.tenant_id=p.tenant_id AND o.table_id=p.table_id WHERE p.tenant_id=? AND p.table_id=? AND o.operation_id=?", mysqlstore.UUID(tenant), mysqlstore.UUID(table), mysqlstore.UUID(job.ID)).Scan(&ready); err != nil || !ready {
		t.Fatal("publication receipt and registry disagree", ready, err)
	}
	var state string
	if err := db.QueryRowContext(ctx, "SELECT state FROM "+schema+".data_model_schema_change_intents WHERE tenant_id=? AND operation_id=?", mysqlstore.UUID(tenant), mysqlstore.UUID(job.ID)).Scan(&state); err != nil || state != "published" {
		t.Fatal("unpublished operation", state, err)
	}
	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+schema+".data_model_schema_change_steps WHERE tenant_id=? AND operation_id=?", mysqlstore.UUID(tenant), mysqlstore.UUID(job.ID)).Scan(&count); err != nil || count != 1 {
		t.Fatal("physical step replayed or missing", count, err)
	}
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.statistics WHERE TABLE_SCHEMA=? AND TABLE_NAME=? AND INDEX_NAME=?", database, name, "record_index_"+strings.ReplaceAll(job.ID.String(), "-", "")).Scan(&count); err != nil || count != 1 {
		t.Fatal("index not physically created", count, err)
	}
	if journey != nil {
		journey.Finish()
		journey.VerifyPinnedModelChange()
		installDecisionRecordLookup(t, ctx, db, database, installed)
		journey.VerifyAggregateParity()
		journey.VerifyBrowserCopy()
		journey.VerifyScenarioCopyParity()
		verifyDecisionPreparationGrants(t, db, database, tenant, table)
	}
}

func assertNativeWorkerState(t *testing.T, ctx context.Context, db *sql.DB, schema string, tenant, job uuid.UUID, wantJob, wantDelivery string, wantIntents, wantAudit int) {
	t.Helper()
	var status, delivery string
	if err := db.QueryRowContext(ctx, "SELECT j.status,d.state FROM "+schema+".data_model_index_jobs j JOIN "+schema+".data_model_index_delivery_reservations r ON r.tenant_id=j.tenant_id AND r.job_id=j.id JOIN "+schema+".data_model_index_deliveries d ON d.tenant_id=r.tenant_id AND d.id=r.delivery_id WHERE j.tenant_id=? AND j.id=?", mysqlstore.UUID(tenant), mysqlstore.UUID(job)).Scan(&status, &delivery); err != nil || status != wantJob || delivery != wantDelivery {
		t.Fatal("worker state", status, delivery, err)
	}
	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+schema+".data_model_schema_change_intents WHERE tenant_id=? AND operation_id=?", mysqlstore.UUID(tenant), mysqlstore.UUID(job)).Scan(&count); err != nil || count != wantIntents {
		t.Fatal("worker intent count", count, err)
	}
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+schema+".data_model_schema_change_log WHERE tenant_id=? AND resource_id=? AND operation='apply_index_job'", mysqlstore.UUID(tenant), mysqlstore.UUID(job)).Scan(&count); err != nil || count != wantAudit {
		t.Fatal("worker audit count", count, err)
	}
}
