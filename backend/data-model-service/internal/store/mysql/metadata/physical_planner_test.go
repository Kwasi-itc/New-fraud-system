package metadata_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
	store "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/migrations"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func TestTypedPhysicalOperations(t *testing.T) {
	db, database := metadataDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	schema, _ := mysqlstore.Identifier(database)
	for _, scenario := range []string{"nullable_types_and_indexes", "not_null_empty_table", "not_null_populated_table", "tighten_populated_table"} {
		t.Run(scenario, func(t *testing.T) {
			tenantID, tableID, operation := uuid.New(), uuid.New(), uuid.New()
			at := time.Now().UTC().Truncate(time.Microsecond)
			var fields []datamodel.Field
			var ids []uuid.UUID
			var mappings []store.PhysicalField
			err := mysqlstore.Mutation(ctx, db, func(tx *sql.Tx) error {
				r, err := store.NewMetadataRepositories(tx, database, tenantID, 1000, 1<<20)
				if err != nil {
					return err
				}
				if err := r.Tenants.Create(ctx, tenant.New(tenantID, at, "planner", nil)); err != nil {
					return err
				}
				if err := r.Tables.Create(ctx, datamodel.Table{ID: tableID, TenantID: tenantID, Name: "records", CreatedAt: at, UpdatedAt: at}); err != nil {
					return err
				}
				registry, err := store.NewPhysicalRegistry(tx, database, tenantID, 1000)
				if err != nil {
					return err
				}
				if err := registry.RegisterTable(ctx, tableID); err != nil {
					return err
				}
				for i, typ := range datamodel.SupportedDataTypes() {
					f := datamodel.Field{ID: uuid.New(), TenantID: tenantID, TableID: tableID, Name: fmt.Sprintf("column_%d", i), DataType: typ, Nullable: scenario == "nullable_types_and_indexes" || scenario == "tighten_populated_table", CreatedAt: at, UpdatedAt: at}
					if err := r.Fields.Create(ctx, f); err != nil {
						return err
					}
					fields = append(fields, f)
					ids = append(ids, f.ID)
				}
				if err := registry.RegisterFields(ctx, tableID, ids); err != nil {
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
			columns := map[uuid.UUID]map[string]string{}
			for _, f := range fields {
				value := store.MappedPhysicalField{Field: f}
				columns[f.ID] = map[string]string{}
				for _, m := range mappings {
					if m.FieldID == f.ID {
						value.Components = append(value.Components, m)
						quoted, _ := mysqlstore.Identifier(m.Name)
						columns[f.ID][m.Component] = quoted
					}
				}
				mapped = append(mapped, value)
			}
			planner, err := store.NewPhysicalPlanner(database, tenantID, store.PhysicalTable{TableID: tableID, Name: name}, mapped)
			if err != nil {
				t.Fatal(err)
			}
			add, err := planner.AddFields(ids)
			if err != nil {
				t.Fatal(err)
			}
			indexID := uuid.New()
			index, err := planner.CreateIndex(indexID, []uuid.UUID{ids[1], ids[5]}, false)
			if err != nil {
				t.Fatal(err)
			}
			drop, err := planner.DropIndex(indexID)
			if err != nil {
				t.Fatal(err)
			}
			unique, err := planner.CreateIndex(uuid.New(), []uuid.UUID{ids[1]}, true)
			if err != nil {
				t.Fatal(err)
			}
			fieldUnique, err := planner.CreateFieldUniqueIndex(ids[1])
			if err != nil {
				t.Fatal(err)
			}
			fieldDrop, err := planner.DropFieldUniqueIndex(ids[1])
			if err != nil {
				t.Fatal(err)
			}
			statements := []store.PhysicalStatement{add, index, drop, fieldUnique, fieldDrop, unique}
			if scenario == "tighten_populated_table" {
				statements = nil // Existing nullable columns precede this operation.
			}
			if scenario == "nullable_types_and_indexes" || scenario == "tighten_populated_table" {
				for _, id := range ids {
					tighten, err := planner.SetFieldNullable(id, false)
					if err != nil {
						t.Fatal(err)
					}
					statements = append(statements, tighten)
				}
				if scenario == "nullable_types_and_indexes" {
					for _, id := range ids {
						relax, err := planner.SetFieldNullable(id, true)
						if err != nil {
							t.Fatal(err)
						}
						statements = append(statements, relax)
					}
				}
			}
			quoted, _ := mysqlstore.Identifier(name)
			qualified := schema + "." + quoted
			base := "CREATE TABLE " + qualified + " (id BINARY(16) NOT NULL PRIMARY KEY) ENGINE=InnoDB ROW_FORMAT=DYNAMIC"
			exec := func(q string, args ...any) {
				t.Helper()
				if _, err := db.ExecContext(ctx, q, args...); err != nil {
					t.Fatal(err)
				}
			}
			exec(base)
			if scenario == "tighten_populated_table" {
				exec(add.ReferenceStep().SQL)
			}
			conn, err := db.Conn(ctx)
			if err != nil {
				t.Fatal(err)
			}
			var reference []migrations.Step
			for _, statement := range statements {
				step := statement.ReferenceStep()
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
				reference = append(reference, step)
			}
			if err := conn.Close(); err != nil {
				t.Fatal(err)
			}
			plan, err := planner.Bind(statements, reference)
			if err != nil {
				t.Fatal(err)
			}
			exec("DROP TABLE " + qualified)
			exec(base)
			if scenario == "tighten_populated_table" {
				exec(add.ReferenceStep().SQL)
			}
			existing := uuid.New()
			if scenario == "not_null_populated_table" || scenario == "tighten_populated_table" {
				exec("INSERT INTO "+qualified+"(id) VALUES (?)", mysqlstore.UUID(existing))
			}
			shape, err := json.Marshal(plan)
			if err != nil {
				t.Fatal(err)
			}
			intents, err := store.NewDDLIntentStore(db, database, tenantID, 1<<20)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := intents.Prepare(ctx, store.DDLIntent{TableID: tableID, OperationID: operation, RequestHash: sha256.Sum256([]byte(scenario)), ActorKind: "migrate", ActorID: "fixture-planner", Shape: shape}); err != nil {
				t.Fatal(err)
			}
			executor, err := store.NewPhysicalExecutor(db, database, tenantID)
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "not_null_populated_table" || scenario == "tighten_populated_table" {
				if err := executor.ExecuteSteps(ctx, tableID, operation); !errors.Is(err, store.ErrPhysicalPrecondition) {
					t.Fatalf("implicit zero-fill not prevented: %v", err)
				}
				var count int
				if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM data_model_schema_change_steps WHERE tenant_id=? AND operation_id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(operation)).Scan(&count); err != nil || count != 0 {
					t.Fatalf("DDL began before preflight: %d %v", count, err)
				}
				_, state, err := intents.Read(ctx, operation)
				if err != nil || state != "blocked" {
					t.Fatalf("preflight state %s %v", state, err)
				}
				// Explicit fixture correction only, not a production backfill strategy.
				exec("DELETE FROM "+qualified+" WHERE id=?", mysqlstore.UUID(existing))
			}
			if err := executor.ExecuteSteps(ctx, tableID, operation); err != nil {
				t.Fatal(err)
			}
			if err := executor.ExecuteSteps(ctx, tableID, operation); err != nil {
				t.Fatal("replay", err)
			}
			if scenario != "nullable_types_and_indexes" {
				_, err := db.ExecContext(ctx, "INSERT INTO "+qualified+"(id) VALUES (?)", mysqlstore.UUID(uuid.New()))
				mysqlError(t, err, 1364)
				return
			}
			row := uuid.New()
			exec("INSERT INTO "+qualified+"(id) VALUES (?)", mysqlstore.UUID(row))
			scalar := func(i int) string { return columns[ids[i]]["value"] }
			ip := columns[ids[5]]
			address := []byte{192, 0, 2, 129}
			exec("UPDATE "+qualified+" SET "+scalar(0)+"=?,"+scalar(1)+"=?,"+scalar(2)+"=?,"+scalar(3)+"=?,"+scalar(4)+"=?,"+ip["address"]+"=?,"+ip["family"]+"=?,"+ip["prefix"]+"=? WHERE id=?", true, 2147483647, 1.25, "Unicode \u00df  ", mysqlstore.DateTime{Time: at}, address, 4, 24, mysqlstore.UUID(row))
			var actual []byte
			var text string
			if err := db.QueryRowContext(ctx, "SELECT "+ip["address"]+","+scalar(3)+" FROM "+qualified+" WHERE id=?", mysqlstore.UUID(row)).Scan(&actual, &text); err != nil || !bytes.Equal(actual, address) || text != "Unicode \u00df  " {
				t.Fatalf("value preservation: %x %q %v", actual, text, err)
			}
			for _, set := range []string{scalar(0) + "=2", ip["prefix"] + "=NULL", ip["family"] + "=5", ip["prefix"] + "=33"} {
				_, err := db.ExecContext(ctx, "UPDATE "+qualified+" SET "+set+" WHERE id=?", mysqlstore.UUID(row))
				mysqlError(t, err, 3819)
			}
			_, err = db.ExecContext(ctx, "UPDATE "+qualified+" SET "+scalar(1)+"=2147483648 WHERE id=?", mysqlstore.UUID(row))
			mysqlError(t, err, 1264)
			_, err = db.ExecContext(ctx, "INSERT INTO "+qualified+"(id,"+scalar(1)+") VALUES (?,2147483647)", mysqlstore.UUID(uuid.New()))
			mysqlError(t, err, 1062)
			for i := 0; i < 2; i++ {
				exec("INSERT INTO "+qualified+"(id) VALUES (?)", mysqlstore.UUID(uuid.New()))
			}
			verifyPhysicalAccess(t, db, database, tenantID, tableID, operation, name, executor)
			t.Run("archived_snapshot_preserves_names_columns_and_values", func(t *testing.T) {
				activeIndexID := uuid.New()
				before, err := planner.CreateIndex(activeIndexID, []uuid.UUID{ids[1]}, false)
				if err != nil {
					t.Fatal(err)
				}
				// Fixture metadata transition only: this does not implement the
				// owner archive/display/publication transaction.
				err = mysqlstore.Mutation(ctx, db, func(tx *sql.Tx) error {
					if _, err := tx.ExecContext(ctx, "UPDATE data_model_fields SET archived=true,name=CONCAT(name,' renamed') WHERE tenant_id=? AND table_id=? AND id<>?", mysqlstore.UUID(tenantID), mysqlstore.UUID(tableID), mysqlstore.UUID(ids[1])); err != nil {
						return err
					}
					r, err := store.NewMetadataRepositories(tx, database, tenantID, 1000, 1<<20)
					if err != nil {
						return err
					}
					logical, err := r.Fields.ListByTable(ctx, tableID)
					if err != nil {
						return err
					}
					registry, err := store.NewPhysicalRegistry(tx, database, tenantID, 1000)
					if err != nil {
						return err
					}
					physical, err := registry.Fields(ctx, tableID)
					if err != nil {
						return err
					}
					if len(logical) != len(fields) || !reflect.DeepEqual(physical, mappings) {
						return errors.New("archive lost logical fields or changed physical mappings")
					}
					byID := make(map[uuid.UUID][]store.PhysicalField)
					for _, component := range physical {
						byID[component.FieldID] = append(byID[component.FieldID], component)
					}
					var snapshot []store.MappedPhysicalField
					for _, f := range logical {
						if f.ID != ids[1] && (!f.Archived || !strings.HasSuffix(f.Name, " renamed")) {
							return errors.New("archived logical name or visibility lost")
						}
						snapshot = append(snapshot, store.MappedPhysicalField{Field: f, Components: byID[f.ID]})
					}
					after, err := store.NewPhysicalPlanner(database, tenantID, store.PhysicalTable{TableID: tableID, Name: name}, snapshot)
					if err != nil {
						return err
					}
					statement, err := after.CreateIndex(activeIndexID, []uuid.UUID{ids[1]}, false)
					if err != nil || statement.ReferenceStep() != before.ReferenceStep() {
						return fmt.Errorf("active field plan changed after archive: %v", err)
					}
					if _, err := after.SetFieldNullable(ids[5], true); !errors.Is(err, store.ErrPhysicalConflict) {
						return fmt.Errorf("archived IP mutation not rejected: %v", err)
					}
					return nil
				})
				if err != nil {
					t.Fatal(err)
				}
				if err := db.QueryRowContext(ctx, "SELECT "+ip["address"]+","+scalar(3)+" FROM "+qualified+" WHERE id=?", mysqlstore.UUID(row)).Scan(&actual, &text); err != nil || !bytes.Equal(actual, address) || text != "Unicode \u00df  " {
					t.Fatalf("archive lost physical values: %x %q %v", actual, text, err)
				}
			})
		})
	}
}
