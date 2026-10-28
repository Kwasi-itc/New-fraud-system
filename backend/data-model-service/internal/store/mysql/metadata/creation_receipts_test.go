package metadata_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
	store "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/installation"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/migrations"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func creationReceiptInstallationDB(t *testing.T, ctx context.Context) (*sql.DB, string, migrations.Plan, installation.Definer, installation.Definer) {
	t.Helper()
	db, database, base, resolver := creationInstallationDB(t, ctx)
	schema, _ := mysqlstore.Identifier(database)
	guard := installation.Definer{User: "cr_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:24], Host: "%"}
	account, _ := guard.SQLAccount()
	create, err := installation.CreateDefiner(guard)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, create); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := db.Exec("DROP USER " + account); err != nil {
			t.Error(err)
		}
	})
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	observe := func(s migrations.Step) migrations.Step {
		t.Helper()
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
		return s
	}
	retry, err := installation.DeliveryRetryUpgradeStatement(database)
	if err != nil {
		t.Fatal(err)
	}
	upgraded, err := installation.ExtendDeliveryRetryPlan(base, observe(retry))
	if err != nil {
		t.Fatal(err)
	}
	steps, err := installation.CreationReceiptUpgradeStatements(database, guard)
	if err != nil {
		t.Fatal(err)
	}
	for i, s := range steps {
		steps[i] = observe(s)
	}
	plan, err := installation.ExtendCreationReceiptPlan(upgraded, guard, steps)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Steps) != 85 {
		t.Fatal("wrong upgrade length", len(plan.Steps))
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	// Only the generated disposable namespace enforced by creationInstallationDB.
	if !strings.HasPrefix(database, "data_model_metadata_test_") {
		t.Fatal("unsafe database")
	}
	for _, q := range []string{"DROP DATABASE " + schema, "CREATE DATABASE " + schema + " CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin"} {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	runner, err := migrations.New(db, plan)
	if err != nil {
		t.Fatal(err)
	}
	grants, err := installation.DefinerGrants(database, resolver)
	if err != nil {
		t.Fatal(err)
	}
	prefix, err := migrations.New(db, upgraded)
	if err != nil {
		t.Fatal(err)
	}
	if err := prefix.Apply(ctx); err != nil {
		t.Fatal(err)
	}
	for _, q := range grants {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	canary := uuid.New()
	if err := mysqlstore.Mutation(ctx, db, func(tx *sql.Tx) error {
		repos, e := store.NewMetadataRepositories(tx, database, canary, 10, 1<<20)
		if e != nil {
			return e
		}
		return repos.Tenants.Create(ctx, tenant.New(canary, time.Now().UTC().Truncate(time.Microsecond), "preserve before v003", nil))
	}); err != nil {
		t.Fatal(err)
	}
	partial, err := migrations.New(db, migrations.Plan{Database: database, Steps: plan.Steps[:82]})
	if err != nil {
		t.Fatal(err)
	}
	if err := partial.Apply(ctx); err != nil {
		t.Fatal("partial receipt installation", err)
	}
	if err := runner.Apply(ctx); err != nil {
		t.Fatal("resume receipt guards", err)
	}
	if err := runner.Apply(ctx); err != nil {
		t.Fatal("upgrade replay", err)
	}
	var preserved string
	if err := db.QueryRowContext(ctx, "SELECT name FROM "+schema+".data_model_tenants WHERE id=?", mysqlstore.UUID(canary)).Scan(&preserved); err != nil || preserved != "preserve before v003" {
		t.Fatal("upgrade changed existing metadata", err)
	}
	extra, err := installation.GrantStatements(database, installation.CreationReceiptDefinerPolicy(guard))
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range append(grants, extra...) {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	return db, database, plan, resolver, guard
}

func TestCreationReceipts(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	db, database, plan, resolver, guard := creationReceiptInstallationDB(t, ctx)
	db.SetMaxOpenConns(1)
	scope := uuid.New()
	at := time.Now().UTC().Truncate(time.Microsecond)
	mutate := func(fn func(*sql.Tx, *store.CreationReceipts) error) error {
		return mysqlstore.Mutation(ctx, db, func(tx *sql.Tx) error {
			receipts, err := store.NewCreationReceipts(tx, database, scope)
			if err != nil {
				return err
			}
			return fn(tx, receipts)
		})
	}
	if err := mutate(func(tx *sql.Tx, _ *store.CreationReceipts) error {
		repos, err := store.NewMetadataRepositories(tx, database, scope, 100, 1<<20)
		if err != nil {
			return err
		}
		return repos.Tenants.Create(ctx, tenant.New(scope, at, "receipts", nil))
	}); err != nil {
		t.Fatal(err)
	}
	candidate := func() ports.TableCreationSnapshot {
		id := uuid.New()
		return ports.TableCreationSnapshot{Format: "dm-table-creation-result/v1", Table: datamodel.Table{ID: id, TenantID: scope, Name: "records", Description: "original", CreatedAt: at, UpdatedAt: at}, Fields: [2]datamodel.Field{
			{ID: uuid.New(), TenantID: scope, TableID: id, Name: "object_id", DataType: datamodel.DataTypeString, IsUnique: true, CreatedAt: at, UpdatedAt: at},
			{ID: uuid.New(), TenantID: scope, TableID: id, Name: "updated_at", DataType: datamodel.DataTypeTimestamp, CreatedAt: at, UpdatedAt: at},
		}}
	}
	pubHash := sha256.Sum256([]byte("allocated publication"))
	id := store.CreationReceiptIdentity{OperationID: uuid.New(), ActorID: uuid.New(), RequestHash: sha256.Sum256([]byte("original command"))}
	original := candidate()
	t.Run("reserve_replay_preserves_allocation", func(t *testing.T) {
		for i := 0; i < 2; i++ {
			next := original
			if i == 1 {
				next = candidate()
			}
			if err := mutate(func(_ *sql.Tx, r *store.CreationReceipts) error {
				got, err := r.Reserve(ctx, id, next, pubHash)
				if err != nil {
					return err
				}
				if got.Snapshot != original || got.Replayed != (i == 1) || got.PublicationHash != pubHash {
					t.Fatal("changed original allocation", got)
				}
				if table, err := r.PublishedTable(ctx, id); !errors.Is(err, store.ErrModelUnavailable) || table.ID != uuid.Nil {
					t.Fatal("pending creation reported success", table, err)
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		}
	})
	t.Run("owner_rollback", func(t *testing.T) {
		other := id
		other.OperationID = uuid.New()
		injected := errors.New("owner rollback")
		if err := mutate(func(_ *sql.Tx, r *store.CreationReceipts) error {
			if _, err := r.Reserve(ctx, other, candidate(), pubHash); err != nil {
				return err
			}
			return injected
		}); !errors.Is(err, injected) {
			t.Fatal(err)
		}
		if err := mutate(func(_ *sql.Tx, r *store.CreationReceipts) error { _, err := r.Read(ctx, other); return err }); !errors.Is(err, sql.ErrNoRows) {
			t.Fatal(err)
		}
	})
	t.Run("invalid_original_snapshot", func(t *testing.T) {
		for _, change := range []func(*ports.TableCreationSnapshot){
			func(v *ports.TableCreationSnapshot) { v.Format = "future" },
			func(v *ports.TableCreationSnapshot) { v.Table.TenantID = uuid.New() },
			func(v *ports.TableCreationSnapshot) { v.Table.Archived = true },
			func(v *ports.TableCreationSnapshot) { v.Table.CaptionField = "object_id" },
			func(v *ports.TableCreationSnapshot) { v.Table.CreatedAt = time.Time{} },
			func(v *ports.TableCreationSnapshot) { v.Fields[0].Nullable = true },
			func(v *ports.TableCreationSnapshot) { v.Fields[0].IsUnique = false },
			func(v *ports.TableCreationSnapshot) { v.Fields[1].IsUnique = true },
			func(v *ports.TableCreationSnapshot) { v.Fields[1].DataType = datamodel.DataTypeString },
			func(v *ports.TableCreationSnapshot) { v.Fields[1].ID = v.Fields[0].ID },
			func(v *ports.TableCreationSnapshot) { v.Fields[0].TableID = uuid.New() },
			func(v *ports.TableCreationSnapshot) { v.Table.Alias = string([]byte{0xff}) },
		} {
			next := candidate()
			change(&next)
			other := id
			other.OperationID = uuid.New()
			if err := mutate(func(_ *sql.Tx, r *store.CreationReceipts) error {
				_, err := r.Reserve(ctx, other, next, pubHash)
				return err
			}); err == nil {
				t.Fatal("invalid original snapshot retained")
			}
			if err := mutate(func(_ *sql.Tx, r *store.CreationReceipts) error { _, err := r.Read(ctx, other); return err }); !errors.Is(err, sql.ErrNoRows) {
				t.Fatal("partial invalid receipt", err)
			}
		}
	})
	for _, name := range []string{"actor", "hash", "table_collision", "foreign_tenant"} {
		t.Run(name, func(t *testing.T) {
			changed := id
			next := candidate()
			if name == "actor" {
				changed.ActorID = uuid.New()
			}
			if name == "hash" {
				changed.RequestHash = sha256.Sum256([]byte("changed"))
			}
			if name == "table_collision" {
				changed.OperationID = uuid.New()
				next = original
			}
			err := mutate(func(tx *sql.Tx, r *store.CreationReceipts) error {
				if name == "foreign_tenant" {
					foreign, e := store.NewCreationReceipts(tx, database, uuid.New())
					if e != nil {
						return e
					}
					_, e = foreign.Read(ctx, id)
					return e
				}
				_, err := r.Reserve(ctx, changed, next, pubHash)
				return err
			})
			want := store.ErrPublicationOperationConflict
			if name == "foreign_tenant" {
				want = sql.ErrNoRows
			}
			if !errors.Is(err, want) {
				t.Fatal(err)
			}
		})
	}
	t.Run("immutable_retained_evidence", func(t *testing.T) {
		for _, q := range []string{"UPDATE `" + database + "`.data_model_table_creation_operations SET request_hash=RANDOM_BYTES(32) WHERE tenant_id=? AND operation_id=?", "DELETE FROM `" + database + "`.data_model_table_creation_operations WHERE tenant_id=? AND operation_id=?"} {
			_, err := db.ExecContext(ctx, q, mysqlstore.UUID(scope), mysqlstore.UUID(id.OperationID))
			mysqlError(t, err, 1644)
		}
	})
	t.Run("restricted_receipt_writer", func(t *testing.T) {
		user := "cw_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:24]
		account := "`" + user + "`@`%`"
		if _, err := db.ExecContext(ctx, "CREATE USER "+account+" IDENTIFIED BY 'isolated-receipts-only'"); err != nil {
			t.Fatal(err)
		}
		defer func() {
			if _, err := db.Exec("DROP USER " + account); err != nil {
				t.Error(err)
			}
		}()
		if _, err := db.ExecContext(ctx, "GRANT SELECT, INSERT ON `"+database+"`.data_model_table_creation_operations TO "+account); err != nil {
			t.Fatal(err)
		}
		login, err := mysqlstore.Open(ctx, mysqlstore.Config{Address: os.Getenv("DATA_MODEL_MYSQL_TEST_ADDRESS"), Database: database, User: user, Password: "isolated-receipts-only", AllowInsecureTransport: true, MaxOpen: 1, MaxIdle: 1, ConnectTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute})
		if err != nil {
			t.Fatal(err)
		}
		defer login.Close()
		other := id
		other.OperationID = uuid.New()
		snapshot := candidate()
		for i := 0; i < 2; i++ {
			if err := mysqlstore.Mutation(ctx, login, func(tx *sql.Tx) error {
				r, e := store.NewCreationReceipts(tx, database, scope)
				if e != nil {
					return e
				}
				got, e := r.Reserve(ctx, other, snapshot, pubHash)
				if e == nil && (got.Snapshot != snapshot || got.Replayed != (i == 1)) {
					t.Fatal(got)
				}
				return e
			}); err != nil {
				t.Fatal("restricted reserve/replay", err)
			}
		}
		for _, q := range []string{"UPDATE `" + database + "`.data_model_table_creation_operations SET request_hash=RANDOM_BYTES(32) WHERE tenant_id=?", "DELETE FROM `" + database + "`.data_model_table_creation_operations WHERE tenant_id=?"} {
			_, err := login.ExecContext(ctx, q, mysqlstore.UUID(scope))
			mysqlError(t, err, 1142)
		}
	})
	t.Run("complete_upgraded_dependency_lineage", func(t *testing.T) {
		if _, err := installation.NewExactKeyReadiness(db, plan, resolver); err == nil {
			t.Fatal("old constructor accepted unconfigured receipt definer")
		}
		inspect, err := installation.NewExactKeyReadinessWithCreationReceipts(db, plan, resolver, guard)
		if err != nil {
			t.Fatal(err)
		}
		conn, err := db.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		if err := inspect.VerifyOnConn(ctx, conn, database, installation.Definer{User: "record_guard", Host: "%"}); err != nil {
			t.Fatal(err)
		}
		account, _ := guard.SQLAccount()
		if _, err := conn.ExecContext(ctx, "ALTER USER "+account+" ACCOUNT UNLOCK"); err != nil {
			t.Fatal(err)
		}
		if err := inspect.VerifyOnConn(ctx, conn, database, installation.Definer{User: "record_guard", Host: "%"}); err == nil {
			t.Fatal("unlocked receipt definer accepted")
		}
		if _, err := conn.ExecContext(ctx, "ALTER USER "+account+" ACCOUNT LOCK"); err != nil {
			t.Fatal(err)
		}
		if _, err := conn.ExecContext(ctx, "CREATE TRIGGER `"+database+"`.extra_receipt AFTER INSERT ON `"+database+"`.data_model_table_creation_operations FOR EACH ROW SET @extra_receipt=1"); err != nil {
			t.Fatal(err)
		}
		if err := inspect.VerifyOnConn(ctx, conn, database, installation.Definer{User: "record_guard", Host: "%"}); !errors.Is(err, migrations.ErrDrift) {
			t.Fatal("extra receipt trigger accepted", err)
		}
		if _, err := conn.ExecContext(ctx, "DROP TRIGGER `"+database+"`.extra_receipt"); err != nil {
			t.Fatal(err)
		}
	})
}
