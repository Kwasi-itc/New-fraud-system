package metadata_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
	store "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/migrations"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	driver "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
)

func TestPhysicalExecutionRecovery(t *testing.T) {
	db, database := metadataDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	schema, _ := mysqlstore.Identifier(database)
	for _, scenario := range []string{"fresh_and_replay", "dirty_before", "dirty_after", "unrecorded_object", "applied_drift", "competing_executor", "wrong_tenant", "lost_create_ack", "lost_alter_ack"} {
		t.Run(scenario, func(t *testing.T) {
			tenantID, tableID, operation := uuid.New(), uuid.New(), uuid.New()
			at := time.Now().UTC().Truncate(time.Microsecond)
			err := mysqlstore.Mutation(ctx, db, func(tx *sql.Tx) error {
				r, err := store.NewMetadataRepositories(tx, database, tenantID, 1000, 1<<20)
				if err != nil {
					return err
				}
				if err := r.Tenants.Create(ctx, tenant.New(tenantID, at, "execution", nil)); err != nil {
					return err
				}
				if err := r.Tables.Create(ctx, datamodel.Table{ID: tableID, TenantID: tenantID, Name: "records", CreatedAt: at, UpdatedAt: at}); err != nil {
					return err
				}
				registry, err := store.NewPhysicalRegistry(tx, database, tenantID, 1000)
				if err != nil {
					return err
				}
				return registry.RegisterTable(ctx, tableID)
			})
			if err != nil {
				t.Fatal(err)
			}
			var name string
			if err := db.QueryRowContext(ctx, "SELECT physical_name FROM data_model_physical_tables WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(tableID)).Scan(&name); err != nil {
				t.Fatal(err)
			}
			quoted, _ := mysqlstore.Identifier(name)
			steps := []migrations.Step{
				{ID: "create_records", Kind: "TABLE", Name: name, SQL: "CREATE TABLE " + schema + "." + quoted + " (id BINARY(16) NOT NULL PRIMARY KEY) ENGINE=InnoDB ROW_FORMAT=DYNAMIC"},
				{ID: "add_counter_index", Kind: "TABLE", Name: name, SQL: "ALTER TABLE " + schema + "." + quoted + " ADD COLUMN counter BIGINT NULL, ADD INDEX record_counter (counter)"},
			}
			conn, err := db.Conn(ctx)
			if err != nil {
				t.Fatal(err)
			}
			for i := range steps {
				steps[i].Before, err = migrations.Definition(ctx, conn, database, "TABLE", name)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = conn.ExecContext(ctx, steps[i].SQL); err != nil {
					t.Fatal(err)
				}
				steps[i].After, err = migrations.Definition(ctx, conn, database, "TABLE", name)
				if err != nil {
					t.Fatal(err)
				}
			}
			if _, err = conn.ExecContext(ctx, "DROP TABLE "+schema+"."+quoted); err != nil {
				t.Fatal(err)
			}
			if err = conn.Close(); err != nil {
				t.Fatal(err)
			}
			shape, err := json.Marshal(store.PhysicalDDLPlan{Version: 1, Steps: steps})
			if err != nil {
				t.Fatal(err)
			}
			intents, err := store.NewDDLIntentStore(db, database, tenantID, 1<<20)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = intents.Prepare(ctx, store.DDLIntent{TableID: tableID, OperationID: operation, RequestHash: sha256.Sum256([]byte(scenario)), ActorKind: "migrate", ActorID: "fixture-owner", Shape: shape}); err != nil {
				t.Fatal(err)
			}
			execute, err := store.NewPhysicalExecutor(db, database, tenantID)
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "dirty_before" || scenario == "dirty_after" {
				data, _ := json.Marshal(struct {
					Database string
					Step     migrations.Step
				}{database, steps[0]})
				sum := sha256.Sum256(data)
				if _, err := db.ExecContext(ctx, "INSERT INTO data_model_schema_change_steps(tenant_id,table_id,operation_id,step_no,checksum,state,started_at) VALUES (?,?,?,1,?,'dirty',UTC_TIMESTAMP(6))", mysqlstore.UUID(tenantID), mysqlstore.UUID(tableID), mysqlstore.UUID(operation), sum[:]); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "dirty_after" || scenario == "unrecorded_object" {
				if _, err := db.ExecContext(ctx, steps[0].SQL); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "competing_executor" {
				owner, err := db.Conn(ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer owner.Close()
				sum := sha256.Sum256([]byte(database + ":" + tenantID.String() + ":" + tableID.String()))
				lock := fmt.Sprintf("data_model_ddl_%x", sum[:24])
				var acquired int
				if err := owner.QueryRowContext(ctx, "SELECT GET_LOCK(?,0)", lock).Scan(&acquired); err != nil || acquired != 1 {
					t.Fatal("lock", err)
				}
				if err := execute.ExecuteSteps(ctx, tableID, operation); !errors.Is(err, mysqlstore.ErrDDLLockBusy) {
					t.Fatalf("competing executor: %v", err)
				}
				if err := owner.QueryRowContext(ctx, "SELECT RELEASE_LOCK(?)", lock).Scan(&acquired); err != nil || acquired != 1 {
					t.Fatal("unlock", err)
				}
			}
			if scenario == "wrong_tenant" {
				other, err := store.NewPhysicalExecutor(db, database, uuid.New())
				if err != nil {
					t.Fatal(err)
				}
				if err := other.ExecuteSteps(ctx, tableID, operation); !errors.Is(err, sql.ErrNoRows) {
					t.Fatalf("tenant isolation: %v", err)
				}
			}
			if scenario == "lost_create_ack" || scenario == "lost_alter_ack" {
				index := 0
				if scenario == "lost_alter_ack" {
					index = 1
				}
				var observed atomic.Bool
				faultDB := physicalFaultDB(t, database, steps[index].SQL, &observed)
				fault, err := store.NewPhysicalExecutor(faultDB, database, tenantID)
				if err != nil {
					t.Fatal(err)
				}
				if err := fault.ExecuteSteps(ctx, tableID, operation); err == nil || !observed.Load() {
					t.Fatalf("lost DDL acknowledgement observed=%v err=%v", observed.Load(), err)
				}
				var state string
				if err := db.QueryRowContext(ctx, "SELECT state FROM data_model_schema_change_steps WHERE tenant_id=? AND operation_id=? AND step_no=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(operation), index+1).Scan(&state); err != nil || state != "dirty" {
					t.Fatalf("dirty DDL evidence: %s %v", state, err)
				}
				// Closing the fault socket does not synchronously prove that MySQL
				// has retired the old session. Recovery must respect its named lock.
				// Wait for actual server release; never retry DDL on a timer alone.
				probe, err := db.Conn(ctx)
				if err != nil {
					t.Fatal(err)
				}
				sum := sha256.Sum256([]byte(database + ":" + tenantID.String() + ":" + tableID.String()))
				lock := fmt.Sprintf("data_model_ddl_%x", sum[:24])
				var acquired int
				lockErr := probe.QueryRowContext(ctx, "SELECT GET_LOCK(?,5)", lock).Scan(&acquired)
				if lockErr != nil || acquired != 1 {
					probe.Close()
					t.Fatalf("old session still owns DDL lock: %d %v", acquired, lockErr)
				}
				lockErr = probe.QueryRowContext(ctx, "SELECT RELEASE_LOCK(?)", lock).Scan(&acquired)
				closeErr := probe.Close()
				if lockErr != nil || closeErr != nil || acquired != 1 {
					t.Fatalf("probe lock cleanup: %d %v %v", acquired, lockErr, closeErr)
				}
			}
			if scenario == "unrecorded_object" {
				if err := execute.ExecuteSteps(ctx, tableID, operation); !errors.Is(err, migrations.ErrDrift) {
					t.Fatalf("unjournaled object adopted: %v", err)
				}
			} else {
				_, pendingState, err := intents.Read(ctx, operation)
				if err != nil {
					t.Fatal(err)
				}
				recovered, err := execute.ReconcilePage(ctx, pendingState, uuid.Nil, 1, 5*time.Second)
				if err != nil || len(recovered.Results) != 1 || recovered.Results[0].Outcome != "awaiting_publication" {
					t.Fatalf("durable recovery pass: %+v %v", recovered, err)
				}
				if err := execute.ExecuteSteps(ctx, tableID, operation); err != nil {
					t.Fatal("replay", err)
				}
				if scenario == "applied_drift" {
					if _, err := db.ExecContext(ctx, "ALTER TABLE "+schema+"."+quoted+" DROP INDEX record_counter"); err != nil {
						t.Fatal(err)
					}
					if err := execute.ExecuteSteps(ctx, tableID, operation); !errors.Is(err, migrations.ErrDrift) {
						t.Fatalf("index drift accepted: %v", err)
					}
				}
			}
			_, state, err := intents.Read(ctx, operation)
			if err != nil {
				t.Fatal(err)
			}
			want := "verifying"
			if scenario == "unrecorded_object" || scenario == "applied_drift" {
				want = "blocked"
			}
			if state != want {
				t.Fatalf("state %s want %s", state, want)
			}
			var gate string
			var version mysqlstore.NullUUID
			if err := db.QueryRowContext(ctx, "SELECT write_state,published_version FROM data_model_physical_tables WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(tableID)).Scan(&gate, &version); err != nil {
				t.Fatal(err)
			}
			if gate != "closed" || version.Valid {
				t.Fatal("execution prematurely published")
			}
			page, recoverErr := execute.ReconcilePage(ctx, want, uuid.Nil, 1, 5*time.Second)
			if want == "blocked" {
				if !errors.Is(recoverErr, migrations.ErrDrift) || len(page.Results) != 1 || page.Results[0].Outcome != "schema_drift" {
					t.Fatalf("reconciliation hid drift: %+v %v", page, recoverErr)
				}
			} else if recoverErr != nil || len(page.Results) != 1 || page.Results[0].Outcome != "awaiting_publication" {
				t.Fatalf("reconciliation: %+v %v", page, recoverErr)
			}
			if !page.Exhausted || page.After != operation || page.Results[0].TableID != tableID {
				t.Fatalf("invalid recovery cursor: %+v", page)
			}
			next, err := execute.ReconcilePage(ctx, want, page.After, 1, time.Second)
			if err != nil || !next.Exhausted || len(next.Results) != 0 {
				t.Fatalf("cursor replay: %+v %v", next, err)
			}
			foreign, err := store.NewPhysicalExecutor(db, database, uuid.New())
			if err != nil {
				t.Fatal(err)
			}
			next, err = foreign.ReconcilePage(ctx, want, uuid.Nil, 1000, time.Second)
			if err != nil || len(next.Results) != 0 {
				t.Fatalf("foreign recovery: %+v %v", next, err)
			}
		})
	}
}

func physicalFaultDB(t *testing.T, database, statement string, observed *atomic.Bool) *sql.DB {
	t.Helper()
	return physicalFaultDBAs(t, database, statement, observed, "root", "isolated-test-root-only")
}

func physicalFaultDBAs(t *testing.T, database, statement string, observed *atomic.Bool, user, password string) *sql.DB {
	t.Helper()
	network := "physical_ddl_fault_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	driver.RegisterDialContext(network, func(ctx context.Context, address string) (net.Conn, error) {
		conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", address)
		if err != nil {
			return nil, err
		}
		return &migrationLostDDL{Conn: conn, statement: statement, observed: observed}, nil
	})
	t.Cleanup(func() { driver.DeregisterDialContext(network) })
	cfg := driver.NewConfig()
	cfg.Net = network
	cfg.Addr = "mysql:3306"
	cfg.DBName = database
	cfg.User = user
	cfg.Passwd = password
	cfg.ParseTime = true
	cfg.Timeout = 5 * time.Second
	cfg.ReadTimeout = 5 * time.Second
	cfg.WriteTimeout = 5 * time.Second
	connector, err := driver.NewConnector(cfg)
	if err != nil {
		t.Fatal(err)
	}
	db := sql.OpenDB(connector)
	t.Cleanup(func() { db.Close() })
	return db
}
