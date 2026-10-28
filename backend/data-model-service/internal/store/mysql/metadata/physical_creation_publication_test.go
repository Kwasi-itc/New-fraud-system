package metadata_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/display"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/service"
	store "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/installation"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/migrations"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	driver "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
)

func creationInstallationDB(t *testing.T, ctx context.Context) (*sql.DB, string, migrations.Plan, installation.Definer) {
	t.Helper()
	db, database := metadataDB(t)
	if !strings.HasPrefix(database, "data_model_metadata_test_") {
		t.Fatal("disposable database required")
	}
	schema, _ := mysqlstore.Identifier(database)
	exec := func(q string) {
		t.Helper()
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	guard := installation.Definer{User: "sg_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:24], Host: "%"}
	account, _ := guard.SQLAccount()
	create, err := installation.CreateDefiner(guard)
	if err != nil {
		t.Fatal(err)
	}
	exec(create)
	t.Cleanup(func() {
		if _, err := db.Exec("DROP USER " + account); err != nil {
			t.Error(err)
		}
	})
	// metadataDB enforces the isolated address/password and generated namespace.
	// Capture reference observations there, then rebuild only that empty fixture.
	exec("DROP DATABASE " + schema)
	exec("CREATE DATABASE " + schema + " CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin")
	reference, err := installation.Statements(database, guard)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for i := range reference {
		s := &reference[i]
		s.Before, err = migrations.Definition(ctx, conn, database, s.Kind, s.Name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = conn.ExecContext(ctx, s.SQL); err != nil {
			t.Fatal(err)
		}
		s.After, err = migrations.Definition(ctx, conn, database, s.Kind, s.Name)
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	plan, err := installation.Plan(database, guard, reference)
	if err != nil {
		t.Fatal(err)
	}
	exec("DROP DATABASE " + schema)
	exec("CREATE DATABASE " + schema + " CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin")
	runner, err := migrations.New(db, plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.Apply(ctx); err != nil {
		t.Fatal(err)
	}
	grants, err := installation.DefinerGrants(database, guard)
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range grants {
		exec(g)
	}
	return db, database, plan, guard
}

func TestPhysicalCreationPublication(t *testing.T) {
	testPhysicalCreationPublication(t, false)
}

func TestPhysicalCreationAllocated(t *testing.T) {
	testPhysicalCreationPublication(t, true)
}

func testPhysicalCreationPublication(t *testing.T, allocated bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 110*time.Second)
	defer cancel()
	var db *sql.DB
	var database string
	var installed migrations.Plan
	var resolver, receiptGuard, resultGuard installation.Definer
	if allocated {
		db, database, installed, resolver, receiptGuard = creationReceiptInstallationDB(t, ctx)
		installed, resultGuard = installDisplayResultFixture(t, ctx, db, database, installed)
	} else {
		db, database, installed, resolver = creationInstallationDB(t, ctx)
	}
	db.SetMaxOpenConns(1)
	schema, _ := mysqlstore.Identifier(database)
	var shared *installation.ExactKeyReadiness
	var err error
	if allocated {
		shared, err = installation.NewExactKeyReadinessWithOwnerResults(db, installed, resolver, receiptGuard, resultGuard)
	} else {
		shared, err = installation.NewExactKeyReadiness(db, installed, resolver)
	}
	if err != nil {
		t.Fatal(err)
	}
	if allocated {
		verifyOwnerResultReadinessFixture(t, ctx, db, database, shared, resultGuard)
	}
	publication, err := store.NewPublicationStore(db, database, 100, 100, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	scenarios := []string{"lost_insert_ack", "lost_update_ack", "service_commit", "service_drift", "service_inactive", "service_audit_failure", "service_revision_failure", "publish_replay", "lost_commit", "owner_rollback", "metadata_mismatch", "extra_field", "missing_mapping", "authorization", "trigger_drift", "extra_trigger", "dirty_journal", "resolver_drift", "shared_grant", "record_grant", "record_unlocked", "writer_grant", "private_consumer", "installer_active", "installer_capability", "installer_session", "installer_extra_privilege", "missing_reference_privilege", "missing_definer_privilege", "missing_trigger_privilege"}
	if allocated {
		scenarios = []string{"service_allocated", "service_allocated_lost_commit", "service_allocated_native_guard_before", "service_allocated_native_guard_after", "service_allocated_native_owner_rollback", "service_allocated_native_active_installer", "service_allocated_native_missing_alter", "service_allocated_native_lost_ack", "service_allocated_native_lost_commit", "service_allocated_native_metadata_mismatch", "service_allocated_native_extra_field", "service_allocated_native_missing_mapping"}
	}
	if allocated {
		scenarios = append(scenarios, "service_allocated_native_field_result_concurrent_commit", "service_allocated_native_field_result_commit", "service_allocated_native_field_result_lost_commit", "service_allocated_native_field_result_receipt_limit", "service_allocated_native_field_result_drift", "service_allocated_native_nullability_result_commit", "service_allocated_native_nullability_result_lost_commit", "service_allocated_native_nullability_result_receipt_limit", "service_allocated_native_nullability_result_drift", "service_allocated_native_unique_result_commit", "service_allocated_native_unique_result_lost_commit", "service_allocated_native_unique_result_receipt_limit", "service_allocated_native_unique_result_drift", "service_allocated_native_field_commit", "service_allocated_native_field_audit_failure", "service_allocated_native_field_revision_failure", "service_allocated_native_field_drift", "service_allocated_native_field_inactive", "service_allocated_native_field_lost_commit")
	}
	if allocated {
		scenarios = append(scenarios, "service_allocated_display_result_owner_commit", "service_allocated_display_result_owner_concurrent", "service_allocated_display_result_owner_lost_commit", "service_allocated_display_result_owner_restricted", "service_allocated_display_result_owner_revision_failure", "service_allocated_display_result_replay", "service_allocated_display_result_concurrent", "service_allocated_display_result_rollback", "service_allocated_display_result_lost_commit", "service_allocated_display_result_legacy", "service_allocated_display_commit", "service_allocated_display_lost_commit", "service_allocated_display_missing_queue", "service_allocated_display_restricted")
	}
	if allocated {
		scenarios = append(scenarios, "service_allocated_presentation_result_commit", "service_allocated_presentation_result_concurrent", "service_allocated_presentation_result_lost_commit", "service_allocated_presentation_result_revision_failure", "service_allocated_presentation_result_closed_gate", "service_allocated_presentation_result_restricted", "service_allocated_field_presentation_result_commit", "service_allocated_field_presentation_result_concurrent", "service_allocated_field_presentation_result_lost_commit", "service_allocated_field_presentation_result_revision_failure", "service_allocated_field_presentation_result_closed_gate", "service_allocated_field_presentation_result_restricted", "service_allocated_presentation_commit", "service_allocated_presentation_concurrent", "service_allocated_presentation_lost_commit", "service_allocated_presentation_revision_failure", "service_allocated_presentation_closed_gate", "service_allocated_presentation_semantic_change", "service_allocated_presentation_caption_change", "service_allocated_presentation_archive")
	}
	if allocated {
		scenarios = append(scenarios, "service_allocated_field_presentation_commit", "service_allocated_field_presentation_concurrent", "service_allocated_field_presentation_lost_commit", "service_allocated_field_presentation_revision_failure", "service_allocated_field_presentation_closed_gate")
	}
	if allocated {
		scenarios = append(scenarios, "service_allocated_native_nullability_commit", "service_allocated_native_nullability_rollback", "service_allocated_native_nullability_drift", "service_allocated_native_nullability_lost_commit", "service_allocated_native_nullability_populated", "service_allocated_native_nullability_relax_commit")
	}
	if allocated {
		scenarios = append(scenarios, "service_allocated_native_unique_commit", "service_allocated_native_unique_rollback", "service_allocated_native_unique_drift", "service_allocated_native_unique_lost_commit", "service_allocated_native_unique_duplicates", "service_allocated_native_unique_repair_commit", "service_allocated_native_unique_float_commit")
	}
	for _, scenario := range scenarios {
		t.Run(scenario, func(t *testing.T) {
			exec := func(q string, args ...any) {
				t.Helper()
				if _, err := db.ExecContext(ctx, q, args...); err != nil {
					t.Fatal(err)
				}
			}
			newAccount := func(prefix string, locked bool) installation.Definer {
				d := installation.Definer{User: prefix + strings.ReplaceAll(uuid.NewString(), "-", "")[:24], Host: "%"}
				a, _ := d.SQLAccount()
				q := "CREATE USER " + a + " IDENTIFIED BY 'isolated-creation-only'"
				if locked {
					q += " ACCOUNT LOCK"
				}
				exec(q)
				t.Cleanup(func() {
					if _, err := db.Exec("DROP USER " + a); err != nil {
						t.Error(err)
					}
				})
				return d
			}
			tenantID, tableID, objectID, updatedID, operation := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
			actor := uuid.New()
			var allocationManager *store.TableCreationManager
			var allocationRequest ports.TableCreationRequest
			at := time.Now().UTC().Truncate(time.Microsecond)
			fields := []datamodel.Field{
				{ID: objectID, TenantID: tenantID, TableID: tableID, Name: "object_id", DataType: datamodel.DataTypeString, IsUnique: true, CreatedAt: at, UpdatedAt: at},
				{ID: updatedID, TenantID: tenantID, TableID: tableID, Name: "updated_at", DataType: datamodel.DataTypeTimestamp, CreatedAt: at, UpdatedAt: at},
			}
			var mappings []store.PhysicalField
			err := mysqlstore.Mutation(ctx, db, func(tx *sql.Tx) error {
				r, err := store.NewMetadataRepositories(tx, database, tenantID, 100, 1<<20)
				if err != nil {
					return err
				}
				if err := r.Tenants.Create(ctx, tenant.New(tenantID, at, "creation", nil)); err != nil {
					return err
				}
				if allocated {
					return nil
				}
				if err := r.Tables.Create(ctx, datamodel.Table{ID: tableID, TenantID: tenantID, Name: "records", CreatedAt: at, UpdatedAt: at}); err != nil {
					return err
				}
				for _, f := range fields {
					if err := r.Fields.Create(ctx, f); err != nil {
						return err
					}
				}
				registry, err := store.NewPhysicalRegistry(tx, database, tenantID, 100)
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
			if allocated {
				exec("UPDATE "+schema+".data_model_tenants SET status='active' WHERE id=?", mysqlstore.UUID(tenantID))
				metadata, e := store.NewTransactionManager(db, database, tenantID, 100, 1<<20, func(context.Context, *sql.Tx, uuid.UUID) error { return nil }, nil)
				if e != nil {
					t.Fatal(e)
				}
				allocationManager, e = store.NewTableCreationManager(metadata, func(ctx context.Context, r ports.TableCreationRequest) error {
					if r.ActorID != actor || r.TenantID != tenantID {
						return errors.New("denied")
					}
					return db.PingContext(ctx)
				})
				if e != nil {
					t.Fatal(e)
				}
				ids := &creationOwnerIDs{}
				owner := service.NewTableService(nil, nil, nil, nil, nil, nil, nil, nil, ids, creationOwnerClock{})
				input := service.CreateTableInput{TenantID: tenantID, Name: "records"}
				saved, e := owner.PrepareCreation(ctx, input, operation, actor, allocationManager)
				if e != nil {
					t.Fatal(e)
				}
				replay, e := owner.PrepareCreation(ctx, input, operation, actor, allocationManager)
				if e != nil || replay != saved || ids.calls != 3 {
					t.Fatal("allocation replay", e, ids.calls)
				}
				if _, e := owner.RecoverCreation(ctx, input, operation, actor, allocationManager); !errors.Is(e, store.ErrModelUnavailable) {
					t.Fatal("unfinished allocation succeeded", e)
				}
				tableID = saved.Snapshot.Table.ID
				fields = append([]datamodel.Field(nil), saved.Snapshot.Fields[:]...)
				objectID, updatedID = fields[0].ID, fields[1].ID
				hash, e := service.TableCreationRequestHash(actor, input)
				if e != nil {
					t.Fatal(e)
				}
				allocationRequest = ports.TableCreationRequest{TenantID: tenantID, OperationID: operation, ActorID: actor, RequestHash: hash}
				if e := mysqlstore.Mutation(ctx, db, func(tx *sql.Tx) error {
					registry, e := store.NewPhysicalRegistry(tx, database, tenantID, 100)
					if e != nil {
						return e
					}
					mappings, e = registry.Fields(ctx, tableID)
					return e
				}); e != nil {
					t.Fatal(e)
				}
			}
			var name string
			if err := db.QueryRowContext(ctx, "SELECT physical_name FROM "+schema+".data_model_physical_tables WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(tableID)).Scan(&name); err != nil {
				t.Fatal(err)
			}
			var mapped []store.MappedPhysicalField
			for _, f := range fields {
				m := store.MappedPhysicalField{Field: f}
				for _, v := range mappings {
					if v.FieldID == f.ID {
						m.Components = append(m.Components, v)
					}
				}
				mapped = append(mapped, m)
			}
			guard := newAccount("rg_", true)
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
			if err := conn.Close(); err != nil {
				t.Fatal(err)
			}
			exec("DROP TABLE " + schema + ".`" + name + "`")
			plan, err := creation.Bind(steps)
			if err != nil {
				t.Fatal(err)
			}
			shape, err := json.Marshal(plan)
			if err != nil {
				t.Fatal(err)
			}
			request := store.PhysicalPublicationRequest{TableID: tableID, Operation: store.PublicationOperation{ID: operation, RequestHash: sha256.Sum256([]byte(scenario)), Condition: store.PublicationCondition{Mode: store.PublicationMustBeAbsent}}, ActorKind: "api", ActorID: actor.String()}
			if strings.HasPrefix(scenario, "service_") {
				request.Operation.RequestHash, err = service.PublicationRequestHash(service.PublicationRequestContext{TenantID: tenantID, TableID: tableID, ActorID: uuid.MustParse(request.ActorID), Condition: request.Operation.Condition}, service.CreateTableInput{TenantID: tenantID, Name: "records"})
				if err != nil {
					t.Fatal(err)
				}
			}
			intents, err := store.NewDDLIntentStore(db, database, tenantID, 1<<20)
			if err != nil {
				t.Fatal(err)
			}
			if allocated {
				if _, e := allocationManager.BindPlan(ctx, allocationRequest, plan); e != nil {
					t.Fatal(e)
				}
				if replay, e := allocationManager.BindPlan(ctx, allocationRequest, plan); e != nil || !replay.Replayed {
					t.Fatal("binding replay", e)
				}
			} else {
				if _, err := intents.Prepare(ctx, store.DDLIntent{TableID: tableID, OperationID: operation, RequestHash: request.Operation.RequestHash, ActorKind: request.ActorKind, ActorID: request.ActorID, Shape: shape}); err != nil {
					t.Fatal(err)
				}
			}
			installer := installation.RecordInstallerPolicy{Principal: newAccount("ci_", false), Table: name}
			installerAccount, _ := installer.Principal.SQLAccount()
			grants, err := installer.ActiveGrants(database)
			if err != nil {
				t.Fatal(err)
			}
			for _, g := range grants {
				exec(g)
			}
			inspection, err := db.Conn(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if err := installer.VerifyActive(ctx, inspection, database); err != nil {
				t.Fatal(err)
			}
			if err := inspection.Close(); err != nil {
				t.Fatal(err)
			}
			login, err := mysqlstore.Open(ctx, mysqlstore.Config{Address: os.Getenv("DATA_MODEL_MYSQL_TEST_ADDRESS"), Database: database, User: installer.Principal.User, Password: "isolated-creation-only", AllowInsecureTransport: true, MaxOpen: 1, MaxIdle: 1, ConnectTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute})
			if err != nil {
				t.Fatal(err)
			}
			defer login.Close()
			restricted, err := store.NewPhysicalExecutor(login, database, tenantID)
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "lost_insert_ack" || scenario == "lost_update_ack" {
				index := 1
				if scenario == "lost_update_ack" {
					index = 2
				}
				var observed atomic.Bool
				faultDB := physicalFaultDBAs(t, database, steps[index].SQL, &observed, installer.Principal.User, "isolated-creation-only")
				fault, err := store.NewPhysicalExecutor(faultDB, database, tenantID)
				if err != nil {
					t.Fatal(err)
				}
				if err := fault.ExecuteSteps(ctx, tableID, operation); err == nil || !observed.Load() {
					t.Fatal("restricted lost DDL acknowledgement", err, observed.Load())
				}
				if err := faultDB.Close(); err != nil {
					t.Fatal(err)
				}
				sum := sha256.Sum256([]byte(database + ":" + tenantID.String() + ":" + tableID.String()))
				lock := fmt.Sprintf("data_model_ddl_%x", sum[:24])
				var acquired int
				if err := login.QueryRowContext(ctx, "SELECT GET_LOCK(?,5)", lock).Scan(&acquired); err != nil || acquired != 1 {
					t.Fatal("restricted recovery lock", err)
				}
				if err := login.QueryRowContext(ctx, "SELECT RELEASE_LOCK(?)", lock).Scan(&acquired); err != nil || acquired != 1 {
					t.Fatal("restricted recovery release", err)
				}
			}
			denial := strings.HasPrefix(scenario, "missing_") && scenario != "missing_mapping"
			if denial {
				switch scenario {
				case "missing_reference_privilege":
					exec("REVOKE REFERENCES ON " + schema + ".data_model_exact_keys FROM " + installerAccount)
				case "missing_definer_privilege":
					exec("REVOKE SET_ANY_DEFINER ON *.* FROM " + installerAccount)
				case "missing_trigger_privilege":
					exec("REVOKE TRIGGER ON " + schema + ".`" + name + "` FROM " + installerAccount)
				}
				if err := restricted.ExecuteSteps(ctx, tableID, operation); err == nil {
					t.Fatal("restricted DDL unexpectedly succeeded")
				}
				for _, g := range grants {
					exec(g)
				}
			}
			if err := restricted.ExecuteSteps(ctx, tableID, operation); err != nil {
				t.Fatal("restricted creation", err)
			}
			if err := restricted.ExecuteSteps(ctx, tableID, operation); err != nil {
				t.Fatal("restricted replay", err)
			}
			// Restricted installer cannot directly mutate records or the dictionary.
			for _, q := range []string{"DELETE FROM " + schema + ".`" + name + "`", "SELECT * FROM " + schema + ".data_model_exact_keys", "CALL " + schema + ".data_model_resolve_exact_key(_binary'x',_binary'',_binary'y',@out)"} {
				if _, err := login.ExecContext(ctx, q); err == nil {
					t.Fatal("installer bypass accepted", q)
				}
			}
			if scenario != "installer_session" {
				if err := login.Close(); err != nil {
					t.Fatal(err)
				}
			}
			if scenario != "installer_active" {
				exec("ALTER USER " + installerAccount + " ACCOUNT LOCK")
			}
			if scenario != "installer_capability" {
				exec("REVOKE SET_ANY_DEFINER ON *.* FROM " + installerAccount)
			}
			guardGrants, err := installation.GrantStatements(database, creation.DefinerPolicy())
			if err != nil {
				t.Fatal(err)
			}
			for _, g := range guardGrants {
				exec(g)
			}
			var policies []store.PhysicalAccessPolicy
			for _, mode := range []string{"writer", "ddl"} {
				d := newAccount(mode+"_", false)
				privileges := []string{"SELECT", "INSERT", "UPDATE"}
				if mode == "ddl" {
					privileges = []string{"SELECT", "CREATE", "ALTER", "INDEX"}
				}
				p := installation.AccountPolicy{User: d.User, Host: d.Host, Objects: []installation.ObjectGrant{{Kind: "TABLE", Name: name, Privileges: privileges}}}
				grants, err := installation.GrantStatements(database, p)
				if err != nil {
					t.Fatal(err)
				}
				for _, g := range grants {
					exec(g)
				}
				policies = append(policies, store.PhysicalAccessPolicy{Mode: mode, Account: p})
			}
			base, err := store.NewPhysicalExecutor(db, database, tenantID)
			if err != nil {
				t.Fatal(err)
			}
			publisher, err := base.WithCreationReadiness(shared, installer)
			if err != nil {
				t.Fatal(err)
			}
			if strings.HasPrefix(scenario, "service_") {
				verifyPreparedCreationOwner(t, ctx, db, database, tenantID, store.PhysicalTable{TableID: tableID, Name: name}, request, policies, shared, installer, scenario)
				if allocated {
					original, e := allocationManager.Recover(ctx, allocationRequest)
					if e != nil || original.ID != tableID {
						t.Fatal("original result", e)
					}
					if strings.HasPrefix(scenario, "service_allocated_presentation_") || strings.HasPrefix(scenario, "service_allocated_field_presentation_") {
						verifyTablePresentationOwner(t, ctx, db, database, tenantID, tableID, request, scenario)
					} else if strings.HasPrefix(scenario, "service_allocated_display_") {
						settings, e := store.NewDisplayStore(db, database, tenantID, 100, 1<<20)
						if e != nil {
							t.Fatal(e)
						}
						before, e := settings.Get(ctx, tableID)
						if e != nil {
							t.Fatal(e)
						}
						initial, e := publication.RecoverPublication(ctx, tenantID, tableID, request.Operation)
						if e != nil {
							t.Fatal(e)
						}
						input := display.SaveRequest{Version: &before.Version, Caption: "object_id", Displayed: []uuid.UUID{plan.Creation.ObjectFieldID}, Order: []uuid.UUID{plan.Creation.ObjectFieldID}}
						if strings.HasPrefix(scenario, "service_allocated_display_result_owner_") {
							verifyDisplayOwner(t, ctx, db, database, settings, publication, tenantID, tableID, uuid.MustParse(request.ActorID), uuid.New(), initial, input, "service_"+strings.TrimPrefix(scenario, "service_allocated_display_"))
						} else if strings.HasPrefix(scenario, "service_allocated_display_result_") {
							verifyDisplayResult(t, ctx, db, database, settings, publication, tenantID, tableID, uuid.MustParse(request.ActorID), initial, input, strings.TrimPrefix(scenario, "service_allocated_display_result_"))
						} else {
							verifyDisplayOwner(t, ctx, db, database, settings, publication, tenantID, tableID, uuid.MustParse(request.ActorID), uuid.New(), initial, input, "service_"+strings.TrimPrefix(scenario, "service_allocated_display_"))
						}
					} else if scenario != "service_allocated_lost_commit" {
						verifyGuardedNativeChange(t, ctx, db, database, tenantID, tableID, name, plan, mapped, shared, installer, policies, scenario)
					}
					exec("UPDATE "+schema+".data_model_tables SET alias='later edit' WHERE tenant_id=? AND id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(tableID))
					replay, e := allocationManager.Recover(ctx, allocationRequest)
					if e != nil || replay != original || replay.Alias != "" {
						t.Fatal("historical result changed", e, replay)
					}
				}
				return
			}
			calls := 0
			denied := false
			authorize := func(context.Context, uuid.UUID, store.PhysicalPublicationRequest) error {
				if denied || scenario == "authorization" {
					return errors.New("denied")
				}
				return nil
			}
			owner := func(tx *sql.Tx) error {
				calls++
				sum := sha256.Sum256([]byte(database + ":" + tenantID.String() + ":" + tableID.String()))
				var owns bool
				if err := tx.QueryRowContext(ctx, "SELECT IS_USED_LOCK(?)=CONNECTION_ID()", fmt.Sprintf("data_model_ddl_%x", sum[:24])).Scan(&owns); err != nil || !owns {
					return fmt.Errorf("owner lost execution lock: %v", err)
				}
				if _, err := tx.ExecContext(ctx, "UPDATE "+schema+".data_model_tables SET alias='published' WHERE tenant_id=? AND id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(tableID)); err != nil {
					return err
				}
				switch scenario {
				case "owner_rollback":
					return errors.New("owner failure")
				case "metadata_mismatch":
					_, err := tx.ExecContext(ctx, "UPDATE "+schema+".data_model_fields SET nullable=TRUE WHERE tenant_id=? AND id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(objectID))
					return err
				case "extra_field":
					r, err := store.NewMetadataRepositories(tx, database, tenantID, 100, 1<<20)
					if err != nil {
						return err
					}
					return r.Fields.Create(ctx, datamodel.Field{ID: uuid.New(), TenantID: tenantID, TableID: tableID, Name: "extra", DataType: datamodel.DataTypeString, CreatedAt: at, UpdatedAt: at})
				case "missing_mapping":
					_, err := tx.ExecContext(ctx, "DELETE FROM "+schema+".data_model_physical_fields WHERE tenant_id=? AND table_id=? AND field_id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(tableID), mysqlstore.UUID(objectID))
					return err
				}
				return nil
			}
			var restore func()
			switch scenario {
			case "trigger_drift":
				exec("DROP TRIGGER " + schema + ".`" + steps[1].Name + "`")
			case "extra_trigger":
				exec("CREATE TRIGGER " + schema + ".extra_" + strings.ReplaceAll(tableID.String(), "-", "") + " BEFORE INSERT ON " + schema + ".`" + name + "` FOR EACH ROW SET @unexpected=1")
			case "dirty_journal":
				exec("UPDATE "+schema+".data_model_schema_change_steps SET state='dirty',applied_at=NULL WHERE tenant_id=? AND operation_id=? AND step_no=3", mysqlstore.UUID(tenantID), mysqlstore.UUID(operation))
			case "resolver_drift":
				exec("ALTER PROCEDURE " + schema + ".data_model_resolve_exact_key COMMENT 'drift'")
				restore = func() {
					for _, s := range installed.Steps {
						if s.Name == "data_model_resolve_exact_key" {
							exec("DROP PROCEDURE " + schema + ".data_model_resolve_exact_key")
							exec(s.SQL)
						}
					}
					grants, _ := installation.DefinerGrants(database, resolver)
					for _, g := range grants {
						exec(g)
					}
					for _, g := range guardGrants {
						exec(g)
					}
				}
			case "shared_grant":
				a, _ := resolver.SQLAccount()
				exec("REVOKE EXECUTE ON FUNCTION " + schema + ".data_model_exact_key_digest FROM " + a)
				restore = func() { exec("GRANT EXECUTE ON FUNCTION " + schema + ".data_model_exact_key_digest TO " + a) }
			case "record_grant":
				a, _ := guard.SQLAccount()
				exec("REVOKE EXECUTE ON PROCEDURE " + schema + ".data_model_resolve_exact_key FROM " + a)
			case "record_unlocked":
				a, _ := guard.SQLAccount()
				exec("ALTER USER " + a + " ACCOUNT UNLOCK")
			case "installer_extra_privilege":
				exec("GRANT INSERT ON " + schema + ".`" + name + "` TO " + installerAccount)
			case "private_consumer":
				a, _ := (installation.Definer{User: policies[0].Account.User, Host: "%"}).SQLAccount()
				exec("GRANT SELECT ON " + schema + ".data_model_exact_keys TO " + a)
				policies[0].Account.Objects = append(policies[0].Account.Objects, installation.ObjectGrant{Kind: "TABLE", Name: "data_model_exact_keys", Privileges: []string{"SELECT"}})
			case "writer_grant":
				a, _ := (installation.Definer{User: policies[0].Account.User, Host: "%"}).SQLAccount()
				exec("REVOKE UPDATE ON " + schema + ".`" + name + "` FROM " + a)
			}
			if restore != nil {
				defer restore()
			}
			selected := publisher
			var observed atomic.Bool
			if scenario == "lost_commit" {
				fault, err := store.NewPhysicalExecutor(deliveryFaultDB(t, database, &observed), database, tenantID)
				if err != nil {
					t.Fatal(err)
				}
				selected, err = fault.WithCreationReadiness(shared, installer)
				if err != nil {
					t.Fatal(err)
				}
			}
			result, publishErr := selected.PublishVerifiedOperation(ctx, publication, request, policies, authorize, owner)
			if scenario == "lost_commit" {
				var ambiguous *mysqlstore.CommitError
				if !errors.As(publishErr, &ambiguous) || !observed.Load() || result.Version != uuid.Nil || calls != 1 {
					t.Fatal("missing ambiguous commit", publishErr, observed.Load(), calls)
				}
				sum := sha256.Sum256([]byte(database + ":" + tenantID.String() + ":" + tableID.String()))
				lock := fmt.Sprintf("data_model_ddl_%x", sum[:24])
				probe, err := db.Conn(ctx)
				if err != nil {
					t.Fatal(err)
				}
				var acquired int
				if err := probe.QueryRowContext(ctx, "SELECT GET_LOCK(?,5)", lock).Scan(&acquired); err != nil || acquired != 1 {
					t.Fatal("previous session remains active", err)
				}
				if err := probe.QueryRowContext(ctx, "SELECT RELEASE_LOCK(?)", lock).Scan(&acquired); err != nil || acquired != 1 {
					t.Fatal("release recovery probe", err)
				}
				if err := probe.Close(); err != nil {
					t.Fatal(err)
				}
				replay, err := publisher.PublishVerifiedOperation(ctx, publication, request, policies, authorize, owner)
				if err != nil || replay.Version == uuid.Nil || calls != 1 {
					t.Fatal("lost commit replay", err, calls)
				}
				return
			}
			success := scenario == "publish_replay" || scenario == "lost_insert_ack" || scenario == "lost_update_ack" || denial
			if success {
				if publishErr != nil || result.Version == uuid.Nil || calls != 1 {
					t.Fatalf("publication %v %v calls=%d", result, publishErr, calls)
				}
				if scenario == "publish_replay" {
					writer, err := mysqlstore.Open(ctx, mysqlstore.Config{Address: os.Getenv("DATA_MODEL_MYSQL_TEST_ADDRESS"), Database: database, User: policies[0].Account.User, Password: "isolated-creation-only", AllowInsecureTransport: true, MaxOpen: 1, MaxIdle: 1, ConnectTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute})
					if err != nil {
						t.Fatal(err)
					}
					defer writer.Close()
					insert := "INSERT INTO " + schema + ".`" + name + "` (id,`" + plan.Creation.ObjectColumn + "`,`" + plan.Creation.UpdatedColumn + "`,record_object_key_id) VALUES (?,?,?,?)"
					value := strings.Repeat("long", 2000) + "\x00Case "
					id, forged := uuid.New(), uuid.New()
					if _, err := writer.ExecContext(ctx, insert, mysqlstore.UUID(id), value, at, mysqlstore.UUID(forged)); err != nil {
						t.Fatal("published restricted writer", err)
					}
					var key mysqlstore.UUID
					if err := writer.QueryRowContext(ctx, "SELECT record_object_key_id FROM "+schema+".`"+name+"` WHERE id=?", mysqlstore.UUID(id)).Scan(&key); err != nil || uuid.UUID(key) == forged {
						t.Fatal("forged key was not overwritten", err)
					}
					_, err = writer.ExecContext(ctx, insert, mysqlstore.UUID(uuid.New()), value, at, mysqlstore.UUID(uuid.New()))
					var duplicate *driver.MySQLError
					if !errors.As(err, &duplicate) || duplicate.Number != 1062 {
						t.Fatal("published duplicate was not rejected", err)
					}
				}
				replay, err := publisher.PublishVerifiedOperation(ctx, publication, request, policies, authorize, owner)
				if err != nil || replay.Version != result.Version || calls != 1 {
					t.Fatal("replay", err, calls)
				}
				denied = true
				if _, err := publisher.PublishVerifiedOperation(ctx, publication, request, policies, authorize, owner); err == nil {
					t.Fatal("revoked replay accepted")
				}
			} else {
				if publishErr == nil || result.Version != uuid.Nil {
					t.Fatal("unsafe creation published", publishErr)
				}
				var gate, alias string
				var receipts int
				if err := db.QueryRowContext(ctx, "SELECT p.write_state,t.alias,(SELECT COUNT(*) FROM "+schema+".data_model_publication_operations o WHERE o.tenant_id=p.tenant_id AND o.table_id=p.table_id) FROM "+schema+".data_model_physical_tables p JOIN "+schema+".data_model_tables t ON t.tenant_id=p.tenant_id AND t.id=p.table_id WHERE p.tenant_id=? AND p.table_id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(tableID)).Scan(&gate, &alias, &receipts); err != nil {
					t.Fatal(err)
				}
				if gate != "closed" || alias != "" || receipts != 0 {
					t.Fatal("partial publication", gate, alias, receipts)
				}
				ownerFailure := scenario == "owner_rollback" || scenario == "metadata_mismatch" || scenario == "extra_field" || scenario == "missing_mapping"
				if (ownerFailure && calls != 1) || (!ownerFailure && calls != 0) {
					t.Fatal("incorrect owner callback count", calls, publishErr)
				}
			}
		})
	}
}
