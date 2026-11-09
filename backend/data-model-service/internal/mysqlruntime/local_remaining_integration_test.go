package mysqlruntime

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	"encoding/json"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	domainTenant "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/httpapi/dto"
	store "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/installation"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/metadata"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	deliveryworkers "github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/workers"
	"github.com/google/uuid"
	"reflect"
	"sync"
)

// This fixture is only the final metadata phase. Physical creation and portable
// routes and physical effects are exercised by mysql-remaining-endpoints-smoke.mjs.
func TestRemainingRelationshipTransactions(t *testing.T) {
	address := os.Getenv("DATA_MODEL_REMAINING_TEST_ADDRESS")
	if address == "" {
		address = os.Getenv("DATA_MODEL_MYSQL_TEST_ADDRESS")
	}
	if address == "" {
		if os.Getenv("DATA_MODEL_REQUIRE_MYSQL_TEST") == "1" {
			t.Fatal("required relationship MySQL fixture missing")
		}
		t.Skip("disposable relationship MySQL fixture not configured")
	}
	if address != "mysql:3306" || os.Getenv("DATA_MODEL_MYSQL_TEST_ROOT_PASSWORD") != "isolated-test-root-only" {
		t.Fatal("only isolated Compose fixture allowed")
	}
	ctx, cancel := context.WithTimeout(context.WithValue(context.Background(), localAuthorityKey{}, true), 60*time.Second)
	defer cancel()
	open := func(database string) *sql.DB {
		db, e := mysqlstore.Open(ctx, mysqlstore.Config{Address: address, Database: database, User: "root", Password: "isolated-test-root-only", AllowInsecureTransport: true, MaxOpen: 8, MaxIdle: 4, ConnectTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute})
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { db.Close() })
		return db
	}
	admin := open("data_model_publication_test")
	// No IF NOT EXISTS: refuse to touch a pre-existing application schema.
	if _, e := admin.ExecContext(ctx, "CREATE DATABASE fraud CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin"); e != nil {
		t.Fatal(e)
	}
	// Only remove the database this test successfully created, on the guarded
	// disposable Compose server. Register before open so its pool closes first.
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		if _, e := admin.ExecContext(cleanup, "DROP DATABASE fraud"); e != nil {
			t.Error(e)
		}
	})
	db := open("fraud")
	defs, e := metadata.Definitions("fraud")
	if e != nil {
		t.Fatal(e)
	}
	for _, q := range defs {
		if _, e = db.ExecContext(ctx, q); e != nil {
			t.Fatal(e)
		}
	}
	upgrade, err := installation.DeliveryRetryUpgradeStatement("fraud")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, upgrade.SQL); err != nil {
		t.Fatal(err)
	}
	tenant, parent, child, pf, cf := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	e = mysqlstore.Mutation(ctx, db, func(tx *sql.Tx) error {
		r, e := store.NewMetadataRepositories(tx, "fraud", tenant, 1000, 4<<20)
		if e != nil {
			return e
		}
		now := localClock{}.Now()
		if e = r.Tenants.Create(ctx, domainTenant.New(tenant, now, "portable", nil)); e != nil {
			return e
		}
		if e = r.Tenants.UpdateStatus(ctx, tenant, domainTenant.StatusActive); e != nil {
			return e
		}
		registry, e := store.NewPhysicalRegistry(tx, "fraud", tenant, 1000)
		if e != nil {
			return e
		}
		for i, id := range []uuid.UUID{parent, child} {
			name := []string{"parent", "child"}[i]
			if e = r.Tables.Create(ctx, datamodel.Table{ID: id, TenantID: tenant, Name: name, CreatedAt: now, UpdatedAt: now}); e != nil {
				return e
			}
			if e = registry.RegisterTable(ctx, id); e != nil {
				return e
			}
			if _, e = tx.ExecContext(ctx, "UPDATE fraud.data_model_physical_tables SET published_version=?,write_state='open' WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(uuid.New()), mysqlstore.UUID(tenant), mysqlstore.UUID(id)); e != nil {
				return e
			}
		}
		for i, id := range []uuid.UUID{pf, cf} {
			table := parent
			if i == 1 {
				table = child
			}
			if e = r.Fields.Create(ctx, datamodel.Field{ID: id, TenantID: tenant, TableID: table, Name: "reference", DataType: datamodel.DataTypeString, IsUnique: i == 0, CreatedAt: now, UpdatedAt: now}); e != nil {
				return e
			}
		}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	worker := LocalCreation{Target: db}

	input := relationshipCommand{Kind: "link_create", ID: tenant, Link: dto.CreateLinkRequest{Name: "owner", ParentTableID: parent, ParentFieldID: pf, ChildTableID: child, ChildFieldID: cf}}
	key := uuid.New()
	if _, e := worker.relationship(context.Background(), input, key); e == nil {
		t.Fatal("unauthorized mutation")
	}
	canceled, stop := context.WithCancel(ctx)
	stop()
	if _, e := worker.relationship(canceled, input, key); e == nil {
		t.Fatal("cancellation ignored")
	}
	// This trigger is installed only in the disposable database allocated above.
	if _, e = db.ExecContext(ctx, "CREATE TRIGGER fraud.fail_relationship_receipt BEFORE INSERT ON fraud.data_model_schema_change_log FOR EACH ROW BEGIN IF NEW.operation='local_relationship' THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='injected receipt failure'; END IF; END"); e != nil {
		t.Fatal(e)
	}
	if _, e = worker.relationship(ctx, input, key); e == nil {
		t.Fatal("receipt failure ignored")
	}
	for _, table := range []string{"data_model_links", "data_model_schema_change_log", "data_model_tenant_schema_migrations"} {
		var n int
		if e = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM fraud."+table+" WHERE tenant_id=?", mysqlstore.UUID(tenant)).Scan(&n); e != nil || n != 0 {
			t.Fatalf("rollback %s: %d %v", table, n, e)
		}
	}
	if _, e = db.ExecContext(ctx, "DROP TRIGGER fraud.fail_relationship_receipt"); e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	results := make([][]byte, 2)
	errs := make([]error, 2)
	for i := range results {
		wg.Add(1)
		go func(i int) { defer wg.Done(); results[i], errs[i] = worker.relationship(ctx, input, key) }(i)
	}
	wg.Wait()
	var a, b any
	_ = json.Unmarshal(results[0], &a)
	_ = json.Unmarshal(results[1], &b)
	if errs[0] != nil || errs[1] != nil || !reflect.DeepEqual(a, b) {
		t.Fatalf("concurrent replay: %v %v %s %s", errs[0], errs[1], results[0], results[1])
	}
	changed := input
	changed.Link.Name = "changed"
	if _, e = worker.relationship(ctx, changed, key); !errors.Is(e, store.ErrPublicationOperationConflict) {
		t.Fatalf("body conflict: %v", e)
	}
	foreign := input
	foreign.ID = uuid.New()
	if _, e = worker.relationship(ctx, foreign, uuid.New()); !errors.Is(e, sql.ErrNoRows) {
		t.Fatalf("tenant isolation: %v", e)
	}
	var n int
	if e = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM fraud.data_model_links WHERE tenant_id=?", mysqlstore.UUID(tenant)).Scan(&n); e != nil || n != 1 {
		t.Fatalf("duplicate effects: %d %v", n, e)
	}
	// A cancelled waiter must not change either options or its receipt.
	locked, e := db.BeginTx(ctx, nil)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = locked.ExecContext(ctx, "SELECT id FROM fraud.data_model_tables WHERE id=? FOR UPDATE", mysqlstore.UUID(parent)); e != nil {
		t.Fatal(e)
	}
	short, stop := context.WithTimeout(ctx, 100*time.Millisecond)
	_, e = worker.relationship(short, relationshipCommand{Kind: "options", ID: parent, Options: dto.TableOptionsRequest{DisplayedFields: []uuid.UUID{pf}}}, uuid.New())
	stop()
	locked.Rollback()
	if e == nil {
		t.Fatal("lock waiter ignored cancellation")
	}
	if e = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM fraud.data_model_table_options WHERE tenant_id=?", mysqlstore.UUID(tenant)).Scan(&n); e != nil || n != 0 {
		t.Fatalf("cancelled options persisted: %d %v", n, e)
	}
	t.Run("worker_failure_outcomes_commit_retry_and_exhaustion", func(t *testing.T) {
		queue, e := store.NewIndexDeliveryStoreWithRetryPolicy(db, "fraud", "worker_error_test", 3, time.Minute, deliveryworkers.RiverDeliveryRetryPolicy{})
		if e != nil {
			t.Fatal(e)
		}
		jobID := uuid.New()
		e = mysqlstore.Mutation(ctx, db, func(tx *sql.Tx) error {
			r, e := store.NewMetadataRepositories(tx, "fraud", tenant, 1000, 4<<20)
			if e != nil {
				return e
			}
			if e := r.Jobs.Create(ctx, datamodel.IndexJob{ID: jobID, TenantID: tenant, TableID: &parent, TableName: "parent", IndexType: "search", Columns: []string{"reference"}, Status: "pending", RequestedAt: localClock{}.Now()}); e != nil {
				return e
			}
			_, e = queue.EnqueueTx(ctx, tx, tenant, jobID, nil)
			return e
		})
		if e != nil {
			t.Fatal(e)
		}
		for i := 0; i < 2; i++ {
			lease, e := queue.Claim(ctx)
			if e != nil || lease == nil {
				t.Fatal(lease, e)
			}
			execution, e := store.NewLeasedIndexJobExecutionStore(queue, *lease, 4<<20, 0)
			if e != nil {
				t.Fatal(e)
			}
			execution, e = execution.WithAuthorizer(worker.workerAuthority)
			if e != nil {
				t.Fatal(e)
			}
			if _, e := execution.StartAttempt(ctx, jobID, localClock{}.Now()); e != nil {
				t.Fatal(e)
			}
			if e := worker.failDelivery(ctx, execution, *lease, errors.New("injected execution failure")); e != nil {
				t.Fatal(e)
			}
			var status, state string
			var count int
			if e := db.QueryRowContext(ctx, "SELECT j.status,d.state,d.error_count FROM fraud.data_model_index_jobs j JOIN fraud.data_model_index_deliveries d ON d.tenant_id=j.tenant_id AND d.job_id=j.id WHERE j.id=?", mysqlstore.UUID(jobID)).Scan(&status, &state, &count); e != nil {
				t.Fatal(e)
			}
			if i == 0 {
				if status != "pending" || state != "available" || count != 1 {
					t.Fatal(status, state, count)
				}
				if _, e := db.ExecContext(ctx, "UPDATE fraud.data_model_index_deliveries SET attempt=2,available_at=UTC_TIMESTAMP(6) WHERE id=?", mysqlstore.UUID(lease.ID)); e != nil {
					t.Fatal(e)
				}
			} else if status != "failed" || state != "discarded" {
				t.Fatal(status, state)
			}
		}
		if e := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM fraud.data_model_schema_change_log WHERE resource_id=? AND operation IN ('reschedule_index_job','fail_index_job')", mysqlstore.UUID(jobID)).Scan(&n); e != nil || n != 2 {
			t.Fatal(n, e)
		}
	})
	t.Run("worker_history_translation_is_explicit_and_bounded", func(t *testing.T) {
		queue, e := store.NewIndexDeliveryStore(db, "fraud", "index_jobs", 3, time.Minute)
		if e != nil {
			t.Fatal(e)
		}
		deliveries := make([]uuid.UUID, 3)
		e = mysqlstore.Mutation(ctx, db, func(tx *sql.Tx) error {
			r, e := store.NewMetadataRepositories(tx, "fraud", tenant, 1000, 4<<20)
			if e != nil {
				return e
			}
			for i := range deliveries {
				id := uuid.New()
				if e := r.Jobs.Create(ctx, datamodel.IndexJob{ID: id, TenantID: tenant, TableID: &parent, TableName: "parent", IndexType: "search", Columns: []string{"object_id"}, Status: "pending", RequestedAt: localClock{}.Now()}); e != nil {
					return e
				}
				deliveries[i], e = queue.EnqueueTx(ctx, tx, tenant, id, nil)
				if e != nil {
					return e
				}
			}
			return nil
		})
		if e != nil {
			t.Fatal(e)
		}
		if _, e = db.ExecContext(ctx, "UPDATE fraud.data_model_index_deliveries SET attempt=1 WHERE id=?", mysqlstore.UUID(deliveries[1])); e != nil {
			t.Fatal(e)
		}
		if _, e = db.ExecContext(ctx, "UPDATE fraud.data_model_index_deliveries SET last_error_code='unknown_history' WHERE id=?", mysqlstore.UUID(deliveries[2])); e != nil {
			t.Fatal(e)
		}
		for range 2 {
			if e := worker.initializeUnusedDeliveryHistory(ctx); e != nil {
				t.Fatal(e)
			}
		}
		for i, id := range deliveries {
			var count sql.NullInt64
			if e := db.QueryRowContext(ctx, "SELECT error_count FROM fraud.data_model_index_deliveries WHERE id=?", mysqlstore.UUID(id)).Scan(&count); e != nil {
				t.Fatal(e)
			}
			if count.Valid != (i == 0) || count.Int64 != 0 {
				t.Fatal("guessed ambiguous history", i, count)
			}
		}
		if e := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM fraud.data_model_schema_change_log WHERE operation='initialize_unused_delivery_history' AND resource_id=?", mysqlstore.UUID(deliveries[0])).Scan(&n); e != nil || n != 1 {
			t.Fatal("translation audit/replay", n, e)
		}
	})
}
