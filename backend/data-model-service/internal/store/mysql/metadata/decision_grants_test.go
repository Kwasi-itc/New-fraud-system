package metadata_test

import (
	"context"
	"database/sql"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/decisionaccess"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	store "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/installation"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
	"testing"
	"time"
)

func verifyDecisionPreparationGrants(t *testing.T, db *sql.DB, database string, tenant, table uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	schema, _ := mysqlstore.Identifier(database)
	account := func(worker bool) *sql.DB {
		user := "dp_" + uuid.New().String()[:8]
		quoted, _ := mysqlstore.Identifier(user)
		if _, e := db.ExecContext(ctx, "CREATE USER "+quoted+"@'%' IDENTIFIED BY 'isolated-grant-test-only'"); e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() {
			if _, e := db.ExecContext(context.Background(), "DROP USER "+quoted+"@'%'"); e != nil {
				t.Error(e)
			}
		})
		p := decisionaccess.PreparationPolicy(user, "%")
		if worker {
			p = decisionaccess.ExecutionPolicy(user, "%")
		}
		statements, e := installation.GrantStatements(database, p)
		if e != nil {
			t.Fatal(e)
		}
		for _, q := range statements {
			if _, e = db.ExecContext(ctx, q); e != nil {
				t.Fatal(e)
			}
		}
		conn, e := db.Conn(ctx)
		if e != nil {
			t.Fatal(e)
		}
		e = installation.VerifyAccountPolicy(ctx, conn, database, p)
		conn.Close()
		if e != nil {
			t.Fatal(e)
		}
		restricted, e := mysqlstore.Open(ctx, mysqlstore.Config{Address: "mysql:3306", Database: database, User: user, Password: "isolated-grant-test-only", AllowInsecureTransport: true, MaxOpen: 2, MaxIdle: 1, ConnectTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute})
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { restricted.Close() })
		for _, q := range []string{"CREATE TABLE " + schema + ".unauthorized_ddl(id INT)", "UPDATE " + schema + ".identity_tenants SET active=FALSE", "UPDATE " + schema + ".data_model_fields SET archived=TRUE"} {
			if _, e = restricted.ExecContext(ctx, q); e == nil {
				t.Fatalf("restricted account accepted %s", q)
			}
		}
		return restricted
	}
	api, worker := account(false), account(true)
	// Service authority is explicit fixture input. The decision HTTP journey
	// separately verifies the real identity owner; these checks isolate SQL grants.
	auth := func(ctx context.Context, tx *sql.Tx, id uuid.UUID) error {
		if id != tenant {
			return sql.ErrNoRows
		}
		var n int
		return tx.QueryRowContext(ctx, "SELECT 1").Scan(&n)
	}
	owner, e := decisionaccess.New(decisionaccess.Config{DB: api, Database: database, Tenant: tenant, MaxRows: 500, MaxBytes: 2 << 20, Queue: "grant_worker", MaxAttempts: 3, Lease: time.Minute, Read: auth, Write: auth, IDs: &registrationIDs{}, Clock: registrationClock{time.Now().UTC().Truncate(time.Microsecond)}})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = owner.ModelJSON(ctx, tenant.String()); e != nil {
		t.Fatal(e)
	}
	job, e := owner.Create(ctx, tenant.String(), table.String(), "search", []string{"object_id"}, "grant_test")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = owner.List(ctx, tenant.String(), []string{table.String()}, 100); e != nil {
		t.Fatal(e)
	}
	queue, e := store.NewIndexDeliveryStore(worker, database, "grant_worker", 3, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	lease, e := queue.Claim(ctx)
	if e != nil || lease == nil {
		t.Fatal(lease, e)
	}
	execution, e := store.NewLeasedIndexJobExecutionStore(queue, *lease, 1<<20, 0)
	if e != nil {
		t.Fatal(e)
	}
	at := time.Now().UTC().Truncate(time.Microsecond)
	running, e := execution.StartAttempt(ctx, uuid.MustParse(job.ID), at)
	if e != nil {
		t.Fatal(e)
	}
	e = execution.CompleteAttempt(ctx, ports.IndexJobOutcome{Job: running, ErrorMessage: "grant retry fixture", Change: datamodel.SchemaChange{ID: uuid.New(), TenantID: tenant, ResourceID: running.ID, ResourceType: "index_job", Operation: "fail_index_job", Status: "failed", Details: []byte(`{}`), CreatedAt: at}})
	if e != nil {
		t.Fatal(e)
	}
	if e = owner.Retry(ctx, job.ID); e != nil {
		t.Fatal(e)
	}
	if lease, e = queue.Claim(ctx); e != nil || lease == nil || lease.JobID != running.ID {
		t.Fatal(lease, e)
	}
}
