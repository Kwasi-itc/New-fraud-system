package metadata_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
	store "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/installation"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/migrations"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func TestPhysicalCreationRecovery(t *testing.T) {
	db, database := metadataDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	schema, _ := mysqlstore.Identifier(database)
	guard := installation.Definer{User: "cg_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:24], Host: "localhost"}
	account, _ := guard.SQLAccount()
	create, _ := installation.CreateDefiner(guard)
	if _, err := db.ExecContext(ctx, create); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := db.Exec("DROP USER " + account); err != nil {
			t.Error(err)
		}
	})
	for _, scenario := range []string{"fresh", "dirty_insert_before", "dirty_insert_after", "dirty_update_before", "dirty_update_after", "unrecorded_trigger", "earlier_table_drift", "earlier_trigger_drift", "extra_trigger", "lost_insert_ack", "lost_update_ack", "applied_trigger_drift", "wrong_tenant"} {
		t.Run(scenario, func(t *testing.T) {
			tenantID, tableID, operation := uuid.New(), uuid.New(), uuid.New()
			objectID, updatedID := uuid.New(), uuid.New()
			at := time.Now().UTC().Truncate(time.Microsecond)
			fields := []datamodel.Field{
				{ID: objectID, TenantID: tenantID, TableID: tableID, Name: "object_id", DataType: datamodel.DataTypeString, IsUnique: true, CreatedAt: at, UpdatedAt: at},
				{ID: updatedID, TenantID: tenantID, TableID: tableID, Name: "updated_at", DataType: datamodel.DataTypeTimestamp, CreatedAt: at, UpdatedAt: at},
			}
			var mappings []store.PhysicalField
			err := mysqlstore.Mutation(ctx, db, func(tx *sql.Tx) error {
				r, err := store.NewMetadataRepositories(tx, database, tenantID, 1000, 1<<20)
				if err != nil {
					return err
				}
				if err := r.Tenants.Create(ctx, tenant.New(tenantID, at, "creation", nil)); err != nil {
					return err
				}
				if err := r.Tables.Create(ctx, datamodel.Table{ID: tableID, TenantID: tenantID, Name: "records", CreatedAt: at, UpdatedAt: at}); err != nil {
					return err
				}
				for _, f := range fields {
					if err := r.Fields.Create(ctx, f); err != nil {
						return err
					}
				}
				registry, err := store.NewPhysicalRegistry(tx, database, tenantID, 1000)
				if err != nil {
					return err
				}
				if err := registry.RegisterTable(ctx, tableID); err != nil {
					return err
				}
				if err := registry.RegisterFields(ctx, tableID, []uuid.UUID{objectID, updatedID}); err != nil {
					return err
				}
				mappings, err = registry.Fields(ctx, tableID)
				return err
			})
			if err != nil {
				t.Fatal(err)
			}
			var name string
			if err := db.QueryRowContext(ctx, "SELECT physical_name FROM data_model_physical_tables WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(tableID)).Scan(&name); err != nil {
				t.Fatal(err)
			}
			var mapped []store.MappedPhysicalField
			for _, f := range fields {
				m := store.MappedPhysicalField{Field: f}
				for _, component := range mappings {
					if component.FieldID == f.ID {
						m.Components = append(m.Components, component)
					}
				}
				mapped = append(mapped, m)
			}
			planner, err := store.NewPhysicalPlanner(database, tenantID, store.PhysicalTable{TableID: tableID, Name: name}, mapped)
			if err != nil {
				t.Fatal(err)
			}
			creation, err := planner.CreateTableReference(objectID, updatedID, guard)
			if err != nil {
				t.Fatal(err)
			}
			steps := creation.ReferenceSteps()
			conn, err := db.Conn(ctx)
			if err != nil {
				t.Fatal(err)
			}
			for i := range steps {
				if _, err := conn.ExecContext(ctx, steps[i].SQL); err != nil {
					t.Fatal(err)
				}
				steps[i].After, err = migrations.Definition(ctx, conn, database, steps[i].Kind, steps[i].Name)
				if err != nil {
					t.Fatal(err)
				}
			}
			qualified := schema + ".`" + name + "`"
			if _, err := conn.ExecContext(ctx, "DROP TABLE "+qualified); err != nil {
				t.Fatal(err)
			}
			if err := conn.Close(); err != nil {
				t.Fatal(err)
			}
			plan, err := creation.Bind(steps)
			if err != nil {
				t.Fatal(err)
			}
			shape, err := json.Marshal(plan)
			if err != nil {
				t.Fatal(err)
			}
			intents, err := store.NewDDLIntentStore(db, database, tenantID, 1<<20)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := intents.Prepare(ctx, store.DDLIntent{TableID: tableID, OperationID: operation, RequestHash: sha256.Sum256([]byte(scenario)), ActorKind: "migrate", ActorID: "fixture-owner", Shape: shape}); err != nil {
				t.Fatal(err)
			}
			execute, err := store.NewPhysicalExecutor(db, database, tenantID)
			if err != nil {
				t.Fatal(err)
			}
			exec := func(q string, args ...any) {
				t.Helper()
				if _, err := db.ExecContext(ctx, q, args...); err != nil {
					t.Fatal(err)
				}
			}
			seed := func(i int, state string, apply bool) {
				t.Helper()
				if apply {
					exec(steps[i].SQL)
				}
				data, _ := json.Marshal(struct {
					Database string
					Step     migrations.Step
				}{database, steps[i]})
				sum := sha256.Sum256(data)
				exec("INSERT INTO data_model_schema_change_steps(tenant_id,table_id,operation_id,step_no,checksum,state,started_at,applied_at) VALUES (?,?,?,?,?,?,UTC_TIMESTAMP(6),IF(?='applied',UTC_TIMESTAMP(6),NULL))", mysqlstore.UUID(tenantID), mysqlstore.UUID(tableID), mysqlstore.UUID(operation), i+1, sum[:], state, state)
			}
			drift := false
			switch scenario {
			case "dirty_insert_before", "dirty_insert_after":
				seed(0, "applied", true)
				seed(1, "dirty", scenario == "dirty_insert_after")
			case "dirty_update_before", "dirty_update_after":
				seed(0, "applied", true)
				seed(1, "applied", true)
				seed(2, "dirty", scenario == "dirty_update_after")
			case "unrecorded_trigger":
				seed(0, "applied", true)
				exec(steps[1].SQL)
				drift = true
			case "earlier_table_drift":
				seed(0, "applied", true)
				seed(1, "dirty", false)
				exec("ALTER TABLE " + qualified + " ADD COLUMN unexpected INT NULL")
				drift = true
			case "earlier_trigger_drift":
				seed(0, "applied", true)
				seed(1, "applied", true)
				seed(2, "dirty", false)
				exec("DROP TRIGGER " + schema + ".`" + steps[1].Name + "`")
				drift = true
			case "extra_trigger":
				seed(0, "applied", true)
				exec("CREATE TRIGGER " + schema + ".`extra_" + strings.ReplaceAll(tableID.String(), "-", "") + "` BEFORE DELETE ON " + qualified + " FOR EACH ROW SET @unexpected=1")
				drift = true
			case "wrong_tenant":
				other, err := store.NewPhysicalExecutor(db, database, uuid.New())
				if err != nil {
					t.Fatal(err)
				}
				if err := other.ExecuteSteps(ctx, tableID, operation); !errors.Is(err, sql.ErrNoRows) {
					t.Fatal("foreign tenant", err)
				}
			}
			if scenario == "lost_insert_ack" || scenario == "lost_update_ack" {
				index := 1
				if scenario == "lost_update_ack" {
					index = 2
				}
				var observed atomic.Bool
				faultDB := physicalFaultDB(t, database, steps[index].SQL, &observed)
				fault, err := store.NewPhysicalExecutor(faultDB, database, tenantID)
				if err != nil {
					t.Fatal(err)
				}
				if err := fault.ExecuteSteps(ctx, tableID, operation); err == nil || !observed.Load() {
					t.Fatal("lost trigger acknowledgement", err, observed.Load())
				}
				var state string
				if err := db.QueryRowContext(ctx, "SELECT state FROM data_model_schema_change_steps WHERE tenant_id=? AND operation_id=? AND step_no=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(operation), index+1).Scan(&state); err != nil || state != "dirty" {
					t.Fatal("dirty trigger evidence", state, err)
				}
				probe, err := db.Conn(ctx)
				if err != nil {
					t.Fatal(err)
				}
				hash := sha256.Sum256([]byte(database + ":" + tenantID.String() + ":" + tableID.String()))
				lock := fmt.Sprintf("data_model_ddl_%x", hash[:24])
				var acquired int
				if err := probe.QueryRowContext(ctx, "SELECT GET_LOCK(?,5)", lock).Scan(&acquired); err != nil || acquired != 1 {
					t.Fatal("old execution still owns lock", err)
				}
				if err := probe.QueryRowContext(ctx, "SELECT RELEASE_LOCK(?)", lock).Scan(&acquired); err != nil || acquired != 1 {
					t.Fatal("lock release", err)
				}
				if err := probe.Close(); err != nil {
					t.Fatal(err)
				}
			}
			err = execute.ExecuteSteps(ctx, tableID, operation)
			if drift {
				if !errors.Is(err, migrations.ErrDrift) {
					t.Fatal("drift accepted", err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if err := execute.ExecuteSteps(ctx, tableID, operation); err != nil {
					t.Fatal("replay", err)
				}
				called := false
				if err := execute.WithVerifiedPhysical(ctx, tableID, operation, nil, func(*sql.Conn) error { called = true; return nil }); !errors.Is(err, store.ErrPhysicalPlanningUnsupported) || called {
					t.Fatal("native readiness accepted creation", err, called)
				}
				if scenario == "applied_trigger_drift" {
					exec("DROP TRIGGER " + schema + ".`" + steps[1].Name + "`")
					if err := execute.ExecuteSteps(ctx, tableID, operation); !errors.Is(err, migrations.ErrDrift) {
						t.Fatal("applied trigger drift accepted", err)
					}
					drift = true
				}
			}
			_, state, err := intents.Read(ctx, operation)
			if err != nil {
				t.Fatal(err)
			}
			want := "verifying"
			if drift {
				want = "blocked"
			}
			if state != want {
				t.Fatal("state", state, want)
			}
			var gate string
			var version mysqlstore.NullUUID
			if err := db.QueryRowContext(ctx, "SELECT write_state,published_version FROM data_model_physical_tables WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(tableID)).Scan(&gate, &version); err != nil || gate != "closed" || version.Valid {
				t.Fatal("premature publication", gate, version, err)
			}
			page, err := execute.ReconcilePage(ctx, want, uuid.Nil, 100, 5*time.Second)
			// The page includes earlier scenarios in this tenant only (one operation).
			if len(page.Results) != 1 || (drift && !errors.Is(err, migrations.ErrDrift)) || (!drift && err != nil) {
				t.Fatal("reconcile", page, err)
			}
		})
	}
}
