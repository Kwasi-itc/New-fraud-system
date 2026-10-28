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

func TestPhysicalPublication(t *testing.T) {
	db, database := metadataDB(t)
	db.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	schema, _ := mysqlstore.Identifier(database)
	at := time.Now().UTC().Truncate(time.Microsecond)
	publication, err := store.NewPublicationStore(db, database, 100, 100, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"port_commit", "port_owner_error", "port_permission", "port_missing_queue", "port_foreign_scope", "port_legacy_ddl", "publish_replay_and_new_barrier", "exact_version_next_operation", "owner_rollback", "receipt_rollback", "gate_rollback", "authorization", "identity_conflict", "actor_conflict", "archive_rejected", "unverified", "schema_drift", "grant_drift", "orphan_receipt", "lost_commit"} {
		t.Run(scenario, func(t *testing.T) {
			tenantID, tableID, operation := uuid.New(), uuid.New(), uuid.New()
			request := store.PhysicalPublicationRequest{TableID: tableID, Operation: store.PublicationOperation{ID: operation, RequestHash: sha256.Sum256([]byte(scenario)), Condition: store.PublicationCondition{Mode: store.PublicationMustBeAbsent}}, ActorKind: "api", ActorID: uuid.NewString()}
			err := mysqlstore.Mutation(ctx, db, func(tx *sql.Tx) error {
				r, e := store.NewMetadataRepositories(tx, database, tenantID, 100, 1<<20)
				if e != nil {
					return e
				}
				if e = r.Tenants.Create(ctx, tenant.New(tenantID, at, "publication", nil)); e != nil {
					return e
				}
				if e = r.Tables.Create(ctx, datamodel.Table{ID: tableID, TenantID: tenantID, Name: "records", CreatedAt: at, UpdatedAt: at}); e != nil {
					return e
				}
				registry, e := store.NewPhysicalRegistry(tx, database, tenantID, 100)
				if e != nil {
					return e
				}
				return registry.RegisterTable(ctx, tableID)
			})
			if err != nil {
				t.Fatal(err)
			}
			var name string
			if err = db.QueryRowContext(ctx, "SELECT physical_name FROM data_model_physical_tables WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(tableID)).Scan(&name); err != nil {
				t.Fatal(err)
			}
			object, _ := mysqlstore.Identifier(name)
			qualified := schema + "." + object
			// Reference shape is generated only in this validated disposable database.
			step := migrations.Step{ID: "create_records", Kind: "TABLE", Name: name, SQL: "CREATE TABLE " + qualified + " (id BINARY(16) NOT NULL PRIMARY KEY) ENGINE=InnoDB ROW_FORMAT=DYNAMIC"}
			if _, err = db.ExecContext(ctx, step.SQL); err != nil {
				t.Fatal(err)
			}
			conn, err := db.Conn(ctx)
			if err != nil {
				t.Fatal(err)
			}
			step.After, err = migrations.Definition(ctx, conn, database, "TABLE", name)
			closeErr := conn.Close()
			if err != nil || closeErr != nil {
				t.Fatal(errors.Join(err, closeErr))
			}
			if _, err = db.ExecContext(ctx, "DROP TABLE "+qualified); err != nil {
				t.Fatal(err)
			}
			shape, err := json.Marshal(store.PhysicalDDLPlan{Version: 1, Steps: []migrations.Step{step}})
			if err != nil {
				t.Fatal(err)
			}
			intents, err := store.NewDDLIntentStore(db, database, tenantID, 1<<20)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = intents.Prepare(ctx, store.DDLIntent{TableID: tableID, OperationID: operation, RequestHash: request.Operation.RequestHash, ActorKind: request.ActorKind, ActorID: request.ActorID, Shape: shape}); err != nil {
				t.Fatal(err)
			}
			executor, err := store.NewPhysicalExecutor(db, database, tenantID)
			if err != nil {
				t.Fatal(err)
			}
			if err = executor.ExecuteSteps(ctx, tableID, operation); err != nil {
				t.Fatal(err)
			}
			var policies []store.PhysicalAccessPolicy
			for _, mode := range []string{"writer", "ddl"} {
				user := mode + "_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:24]
				account := "`" + user + "`@`%`"
				if _, err = db.ExecContext(ctx, "CREATE USER "+account+" IDENTIFIED BY 'isolated-physical-only'"); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
					defer stop()
					if _, e := db.ExecContext(cleanup, "DROP USER "+account); e != nil {
						t.Error(e)
					}
				})
				privileges := []string{"SELECT", "INSERT", "UPDATE"}
				if mode == "ddl" {
					privileges = []string{"SELECT", "CREATE", "ALTER", "INDEX"}
				}
				policy := installation.AccountPolicy{User: user, Host: "%", Objects: []installation.ObjectGrant{{Kind: "TABLE", Name: name, Privileges: privileges}}}
				grants, e := installation.GrantStatements(database, policy)
				if e != nil {
					t.Fatal(e)
				}
				for _, grant := range grants {
					if _, e = db.ExecContext(ctx, grant); e != nil {
						t.Fatal(e)
					}
				}
				policies = append(policies, store.PhysicalAccessPolicy{Mode: mode, Account: policy})
			}
			if strings.HasPrefix(scenario, "port_") {
				verifyPhysicalPublicationPort(t, ctx, db, database, tenantID, store.PhysicalTable{TableID: tableID, Name: name}, request, policies, scenario)
				return
			}
			calls, authCalls := 0, 0
			forbidden := errors.New("current permission denied")
			deny := false
			authorize := func(ctx context.Context, tenant uuid.UUID, r store.PhysicalPublicationRequest) error {
				authCalls++
				if tenant != tenantID || r.TableID != tableID || r.ActorID != request.ActorID {
					return forbidden
				}
				if deny {
					return forbidden
				}
				// Authorization happens before the execution lock/connection is acquired.
				sum := sha256.Sum256([]byte(database + ":" + tenantID.String() + ":" + tableID.String()))
				var free int
				if e := db.QueryRowContext(ctx, "SELECT IS_FREE_LOCK(?)", fmt.Sprintf("data_model_ddl_%x", sum[:24])).Scan(&free); e != nil {
					return e
				}
				if free != 1 {
					return errors.New("authorization ran while execution lock was held")
				}
				return nil
			}
			owner := func(tx *sql.Tx) error {
				calls++
				sum := sha256.Sum256([]byte(database + ":" + tenantID.String() + ":" + tableID.String()))
				var owns bool
				if err := tx.QueryRowContext(ctx, "SELECT IS_USED_LOCK(?)=CONNECTION_ID()", fmt.Sprintf("data_model_ddl_%x", sum[:24])).Scan(&owns); err != nil {
					return err
				}
				if !owns {
					return errors.New("owner transaction lost pinned execution lock")
				}
				r, e := store.NewMetadataRepositories(tx, database, tenantID, 100, 1<<20)
				if e != nil {
					return e
				}
				table, e := r.Tables.GetByID(ctx, tableID)
				if e != nil {
					return e
				}
				table.Alias = "published"
				if e = r.Tables.Update(ctx, table); e != nil {
					return e
				}
				if e = r.History.Create(ctx, datamodel.SchemaChange{ID: uuid.New(), TenantID: tenantID, Operation: "physical_publication_test", ResourceType: "table", ResourceID: tableID, Status: "applied", Details: []byte(`{}`), CreatedAt: at}); e != nil {
					return e
				}
				if scenario == "owner_rollback" {
					return errors.New("owner failure")
				}
				if scenario == "archive_rejected" {
					_, err := tx.ExecContext(ctx, "UPDATE data_model_tables SET archived=TRUE WHERE tenant_id=? AND id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(tableID))
					return err
				}
				return nil
			}
			checkClosed := func() {
				t.Helper()
				var gate, state, alias string
				var receipts, audit int
				if e := db.QueryRowContext(ctx, "SELECT write_state FROM data_model_physical_tables WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(tableID)).Scan(&gate); e != nil {
					t.Fatal(e)
				}
				if e := db.QueryRowContext(ctx, "SELECT state FROM data_model_schema_change_intents WHERE tenant_id=? AND operation_id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(operation)).Scan(&state); e != nil {
					t.Fatal(e)
				}
				if e := db.QueryRowContext(ctx, "SELECT alias FROM data_model_tables WHERE tenant_id=? AND id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(tableID)).Scan(&alias); e != nil {
					t.Fatal(e)
				}
				if e := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM data_model_publication_operations WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(tableID)).Scan(&receipts); e != nil {
					t.Fatal(e)
				}
				if e := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM data_model_schema_change_log WHERE tenant_id=?", mysqlstore.UUID(tenantID)).Scan(&audit); e != nil {
					t.Fatal(e)
				}
				if gate != "closed" || state == "published" || alias != "" || receipts != 0 || audit != 0 {
					t.Fatalf("partial publication: %s %s %s receipts=%d audit=%d", gate, state, alias, receipts, audit)
				}
			}
			switch scenario {
			case "unverified":
				if _, err = db.ExecContext(ctx, "UPDATE data_model_schema_change_intents SET state='prepared' WHERE tenant_id=? AND operation_id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(operation)); err != nil {
					t.Fatal(err)
				}
			case "authorization":
				deny = true
			case "identity_conflict":
				request.Operation.RequestHash = sha256.Sum256([]byte("changed"))
			case "actor_conflict":
				request.ActorID = uuid.NewString()
			case "schema_drift":
				if _, err = db.ExecContext(ctx, "ALTER TABLE "+qualified+" ADD COLUMN unexpected INT"); err != nil {
					t.Fatal(err)
				}
			case "grant_drift":
				if _, err = db.ExecContext(ctx, "REVOKE UPDATE ON "+qualified+" FROM `"+policies[0].Account.User+"`@`%`"); err != nil {
					t.Fatal(err)
				}
			case "receipt_rollback":
				if _, err = db.ExecContext(ctx, "CREATE TRIGGER fail_physical_receipt BEFORE INSERT ON data_model_publication_operations FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='receipt failure'"); err != nil {
					t.Fatal(err)
				}
			case "gate_rollback":
				if _, err = db.ExecContext(ctx, "CREATE TRIGGER fail_physical_gate BEFORE UPDATE ON data_model_physical_tables FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='gate failure'"); err != nil {
					t.Fatal(err)
				}
			case "orphan_receipt":
				if _, err = publication.MutateAndPublishOperation(ctx, tenantID, tableID, request.Operation, func(*sql.Tx) error { return nil }); err != nil {
					t.Fatal(err)
				}
			}
			selected := executor
			var observed atomic.Bool
			if scenario == "lost_commit" {
				selected, err = store.NewPhysicalExecutor(deliveryFaultDB(t, database, &observed), database, tenantID)
				if err != nil {
					t.Fatal(err)
				}
			}
			result, publishErr := selected.PublishVerifiedOperation(ctx, publication, request, policies, authorize, owner)
			if scenario == "receipt_rollback" {
				if _, e := db.ExecContext(ctx, "DROP TRIGGER fail_physical_receipt"); e != nil {
					t.Fatal(e)
				}
			}
			if scenario == "gate_rollback" {
				if _, e := db.ExecContext(ctx, "DROP TRIGGER fail_physical_gate"); e != nil {
					t.Fatal(e)
				}
			}
			switch scenario {
			case "publish_replay_and_new_barrier", "exact_version_next_operation":
				if publishErr != nil || result.Version == uuid.Nil || calls != 1 {
					t.Fatalf("publish %v %v calls=%d", result, publishErr, calls)
				}
				if e := mysqlstore.Mutation(ctx, db, func(tx *sql.Tx) error {
					r, e := store.NewPhysicalRegistry(tx, database, tenantID, 100)
					if e != nil {
						return e
					}
					v, e := r.LockPublished(ctx, tableID, result.Version)
					if e == nil && v.Version != result.Version {
						return errors.New("registry version mismatch")
					}
					return e
				}); e != nil {
					t.Fatal(e)
				}
				if scenario == "exact_version_next_operation" {
					// Build a second trusted fixture plan from a temporary reference
					// change, then restore before execution through the durable journal.
					conn, e := db.Conn(ctx)
					if e != nil {
						t.Fatal(e)
					}
					before, e := migrations.Definition(ctx, conn, database, "TABLE", name)
					if e != nil {
						conn.Close()
						t.Fatal(e)
					}
					alter := "ALTER TABLE " + qualified + " ADD INDEX record_id (id)"
					if _, e = conn.ExecContext(ctx, alter); e != nil {
						conn.Close()
						t.Fatal(e)
					}
					after, e := migrations.Definition(ctx, conn, database, "TABLE", name)
					if e != nil {
						conn.Close()
						t.Fatal(e)
					}
					if _, e = conn.ExecContext(ctx, "ALTER TABLE "+qualified+" DROP INDEX record_id"); e != nil {
						conn.Close()
						t.Fatal(e)
					}
					if e = conn.Close(); e != nil {
						t.Fatal(e)
					}
					nextShape, e := json.Marshal(store.PhysicalDDLPlan{Version: 1, Steps: []migrations.Step{{ID: "add_index", Kind: "TABLE", Name: name, SQL: alter, Before: before, After: after}}})
					if e != nil {
						t.Fatal(e)
					}
					nextRequest := request
					nextRequest.Operation = store.PublicationOperation{ID: uuid.New(), RequestHash: sha256.Sum256([]byte("next exact")), Condition: store.PublicationCondition{Mode: store.PublicationExactVersion, Version: result.Version}}
					if _, e = intents.Prepare(ctx, store.DDLIntent{TableID: tableID, OperationID: nextRequest.Operation.ID, RequestHash: nextRequest.Operation.RequestHash, ActorKind: request.ActorKind, ActorID: request.ActorID, ExpectedEpoch: 1, ExpectedVersion: &result.Version, Shape: nextShape}); e != nil {
						t.Fatal(e)
					}
					if e = executor.ExecuteSteps(ctx, tableID, nextRequest.Operation.ID); e != nil {
						t.Fatal(e)
					}
					second, e := executor.PublishVerifiedOperation(ctx, publication, nextRequest, policies, authorize, owner)
					if e != nil || second.Version != result.Version || calls != 2 {
						t.Fatalf("exact publication %v %v calls=%d", second, e, calls)
					}
					return
				}
				deny = true
				if _, e := executor.PublishVerifiedOperation(ctx, publication, request, policies, authorize, owner); !errors.Is(e, forbidden) {
					t.Fatalf("replay bypassed auth: %v", e)
				}
				deny = false
				// Historical replay must not reopen a subsequently prepared operation.
				next := uuid.New()
				if _, e := intents.Prepare(ctx, store.DDLIntent{TableID: tableID, OperationID: next, RequestHash: sha256.Sum256([]byte("next")), ActorKind: request.ActorKind, ActorID: request.ActorID, ExpectedEpoch: 1, ExpectedVersion: &result.Version, Shape: shape}); e != nil {
					t.Fatal(e)
				}
				replay, e := executor.PublishVerifiedOperation(ctx, publication, request, policies, authorize, owner)
				if e != nil || replay.Version != result.Version || calls != 1 || authCalls != 3 {
					t.Fatalf("historical replay %v %v %d %d", replay, e, calls, authCalls)
				}
				var gate string
				var active mysqlstore.UUID
				if e = db.QueryRowContext(ctx, "SELECT write_state,active_operation FROM data_model_physical_tables WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(tableID)).Scan(&gate, &active); e != nil || gate != "closed" || uuid.UUID(active) != next {
					t.Fatalf("new barrier changed %s %v", gate, e)
				}
			case "lost_commit":
				var ambiguous *mysqlstore.CommitError
				if !errors.As(publishErr, &ambiguous) || !observed.Load() || result.Version != uuid.Nil {
					t.Fatalf("fault %v %v", result, publishErr)
				}
				sum := sha256.Sum256([]byte(database + ":" + tenantID.String() + ":" + tableID.String()))
				deadline := time.Now().Add(5 * time.Second)
				for {
					var free int
					if e := db.QueryRowContext(ctx, "SELECT IS_FREE_LOCK(?)", fmt.Sprintf("data_model_ddl_%x", sum[:24])).Scan(&free); e != nil {
						t.Fatal(e)
					}
					if free == 1 {
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("previous lock remains owned")
					}
					time.Sleep(time.Millisecond)
				}
				replay, e := executor.PublishVerifiedOperation(ctx, publication, request, policies, authorize, owner)
				if e != nil || replay.Version == uuid.Nil || calls != 1 {
					t.Fatalf("recovery %v %v calls=%d", replay, e, calls)
				}
			case "orphan_receipt":
				if !errors.Is(publishErr, store.ErrPhysicalConflict) || calls != 0 {
					t.Fatalf("orphan adopted %v calls=%d", publishErr, calls)
				}
				var gate string
				if e := db.QueryRowContext(ctx, "SELECT write_state FROM data_model_physical_tables WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(tableID)).Scan(&gate); e != nil || gate != "closed" {
					t.Fatalf("orphan gate %s %v", gate, e)
				}
			default:
				if publishErr == nil || result.Version != uuid.Nil {
					t.Fatalf("failure returned success: %v %v", result, publishErr)
				}
				checkClosed()
				if scenario != "owner_rollback" && scenario != "receipt_rollback" && scenario != "gate_rollback" && scenario != "archive_rejected" && calls != 0 {
					t.Fatalf("invalid readiness reached owner %d", calls)
				}
			}
		})
	}
}
