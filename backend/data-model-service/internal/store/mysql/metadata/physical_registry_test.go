package metadata_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
	store "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func TestPhysicalRegistryAndIntents(t *testing.T) {
	db, database := metadataDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	a, b := uuid.New(), uuid.New()
	at := time.Now().UTC().Truncate(time.Microsecond)
	mutate := func(scope uuid.UUID, fn func(*sql.Tx, store.MetadataRepositories, store.PhysicalRegistry) error) error {
		return mysqlstore.Mutation(ctx, db, func(tx *sql.Tx) error {
			r, err := store.NewMetadataRepositories(tx, database, scope, 10000, 1<<20)
			if err != nil {
				return err
			}
			p, err := store.NewPhysicalRegistry(tx, database, scope, 10000)
			if err != nil {
				return err
			}
			return fn(tx, r, p)
		})
	}
	for _, scope := range []uuid.UUID{a, b} {
		if err := mutate(scope, func(_ *sql.Tx, r store.MetadataRepositories, _ store.PhysicalRegistry) error {
			return r.Tenants.Create(ctx, tenant.New(scope, at, "registry", nil))
		}); err != nil {
			t.Fatal(err)
		}
	}
	newTable := func(scope uuid.UUID, types ...datamodel.DataType) (uuid.UUID, []uuid.UUID) {
		t.Helper()
		table := uuid.New()
		var fields []uuid.UUID
		err := mutate(scope, func(_ *sql.Tx, r store.MetadataRepositories, p store.PhysicalRegistry) error {
			if err := r.Tables.Create(ctx, datamodel.Table{ID: table, TenantID: scope, Name: table.String(), CreatedAt: at, UpdatedAt: at}); err != nil {
				return err
			}
			if err := p.RegisterTable(ctx, table); err != nil {
				return err
			}
			for i, typ := range types {
				id := uuid.New()
				fields = append(fields, id)
				if err := r.Fields.Create(ctx, datamodel.Field{ID: id, TenantID: scope, TableID: table, Name: fmt.Sprintf("field_%d", i), DataType: typ, CreatedAt: at, UpdatedAt: at}); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return table, fields
	}
	intents := func(scope uuid.UUID, limit int) *store.DDLIntentStore {
		t.Helper()
		s, err := store.NewDDLIntentStore(db, database, scope, limit)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	request := func(table uuid.UUID) store.DDLIntent {
		return store.DDLIntent{TableID: table, OperationID: uuid.New(), RequestHash: sha256.Sum256([]byte("reviewed owner command")), ActorKind: "api", ActorID: "trusted-subject", Shape: []byte(`{"version":1,"action":"create"}`)}
	}
	assertEmpty := func(operation uuid.UUID) {
		t.Helper()
		var n int
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM data_model_schema_change_intents WHERE tenant_id=? AND operation_id=?", mysqlstore.UUID(a), mysqlstore.UUID(operation)).Scan(&n); err != nil || n != 0 {
			t.Fatalf("intent rows=%d err=%v", n, err)
		}
	}
	openFixture := func(table uuid.UUID) uuid.UUID {
		t.Helper()
		version := uuid.New()
		// Privileged test setup only: physical verification/publisher is not yet
		// implemented and no production API exposes this readiness shortcut.
		if _, err := db.ExecContext(ctx, "UPDATE data_model_physical_tables SET published_version=?,write_state='open' WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(version), mysqlstore.UUID(a), mysqlstore.UUID(table)); err != nil {
			t.Fatal(err)
		}
		return version
	}
	t.Run("stable_generated_mappings_all_field_types_and_bounds", func(t *testing.T) {
		table, fields := newTable(a, "bool", "int", "float", "string", "timestamp", "ip_address")
		if err := mutate(a, func(_ *sql.Tx, _ store.MetadataRepositories, p store.PhysicalRegistry) error {
			if err := p.RegisterFields(ctx, table, fields); err != nil {
				return err
			}
			return p.RegisterFields(ctx, table, fields)
		}); err != nil {
			t.Fatal(err)
		}
		if err := mutate(a, func(tx *sql.Tx, _ store.MetadataRepositories, p store.PhysicalRegistry) error {
			got, err := p.Fields(ctx, table)
			if err != nil {
				return err
			}
			if len(got) != 8 {
				t.Fatalf("components=%d", len(got))
			}
			for _, v := range got {
				label := ""
				for i, id := range fields {
					if id == v.FieldID {
						label = fmt.Sprintf("field_%d", i)
					}
				}
				if v.Name != "field_"+label+"_"+strings.ReplaceAll(v.FieldID.String(), "-", "")+"_"+v.Component {
					t.Fatal(v)
				}
			}
			small, err := store.NewPhysicalRegistry(tx, database, a, 7)
			if err != nil {
				return err
			}
			if partial, err := small.Fields(ctx, table); !errors.Is(err, store.ErrSnapshotLimit) || partial != nil {
				t.Fatalf("partial=%v err=%v", partial, err)
			}
			if _, err := p.LockPublished(ctx, table, uuid.New()); !errors.Is(err, store.ErrModelUnavailable) {
				t.Fatal(err)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, "UPDATE data_model_tables SET name='renamed logical table' WHERE tenant_id=? AND id=?", mysqlstore.UUID(a), mysqlstore.UUID(table)); err != nil {
			t.Fatal(err)
		}
		var name string
		if err := db.QueryRowContext(ctx, "SELECT physical_name FROM data_model_physical_tables WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(a), mysqlstore.UUID(table)).Scan(&name); err != nil || name != "tenant_registry_"+strings.ReplaceAll(table.String()[:12], "-", "_")+"_"+strings.ReplaceAll(table.String(), "-", "") {
			t.Fatalf("name=%s err=%v", name, err)
		}
	})
	t.Run("tenant_and_parent_isolation", func(t *testing.T) {
		table, _ := newTable(a)
		other, foreign := newTable(b, "string")
		if err := mutate(a, func(_ *sql.Tx, _ store.MetadataRepositories, p store.PhysicalRegistry) error {
			return p.RegisterFields(ctx, table, foreign)
		}); !errors.Is(err, sql.ErrNoRows) {
			t.Fatal(err)
		}
		if _, err := intents(a, 1000).Prepare(ctx, request(other)); !errors.Is(err, sql.ErrNoRows) {
			t.Fatal(err)
		}
		_, err := db.ExecContext(ctx, "INSERT INTO data_model_physical_fields(tenant_id,table_id,field_id,component,field_label) VALUES (?,?,?,'value','foreign')", mysqlstore.UUID(a), mysqlstore.UUID(table), mysqlstore.UUID(foreign[0]))
		mysqlError(t, err, 1452)
	})
	t.Run("descriptive_names_preserve_identity_across_renames", func(t *testing.T) {
		for range 2 { // Same friendly labels in different tenants cannot collide.
			scope, table, field := uuid.New(), uuid.New(), uuid.New()
			err := mutate(scope, func(_ *sql.Tx, r store.MetadataRepositories, p store.PhysicalRegistry) error {
				if err := r.Tenants.Create(ctx, tenant.New(scope, at, "Acme", nil)); err != nil {
					return err
				}
				if err := r.Tables.Create(ctx, datamodel.Table{ID: table, TenantID: scope, Name: "Customers", CreatedAt: at, UpdatedAt: at}); err != nil {
					return err
				}
				if err := r.Fields.Create(ctx, datamodel.Field{ID: field, TenantID: scope, TableID: table, Name: "Email", DataType: "string", CreatedAt: at, UpdatedAt: at}); err != nil {
					return err
				}
				if err := p.RegisterTable(ctx, table); err != nil {
					return err
				}
				return p.RegisterFields(ctx, table, []uuid.UUID{field})
			})
			if err != nil {
				t.Fatal(err)
			}
			var name string
			if err := db.QueryRowContext(ctx, "SELECT physical_name FROM data_model_physical_tables WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(scope), mysqlstore.UUID(table)).Scan(&name); err != nil || name != "tenant_acme_customers_"+strings.ReplaceAll(table.String(), "-", "") {
				t.Fatalf("table=%s err=%v", name, err)
			}
			if _, err := db.ExecContext(ctx, "UPDATE data_model_tenants SET name='Renamed tenant' WHERE id=?", mysqlstore.UUID(scope)); err != nil {
				t.Fatal(err)
			}
			if _, err := db.ExecContext(ctx, "UPDATE data_model_tables SET name='Renamed table' WHERE tenant_id=? AND id=?", mysqlstore.UUID(scope), mysqlstore.UUID(table)); err != nil {
				t.Fatal(err)
			}
			if _, err := db.ExecContext(ctx, "UPDATE data_model_fields SET name='Renamed field' WHERE tenant_id=? AND id=?", mysqlstore.UUID(scope), mysqlstore.UUID(field)); err != nil {
				t.Fatal(err)
			}
			if err := mutate(scope, func(_ *sql.Tx, _ store.MetadataRepositories, p store.PhysicalRegistry) error {
				if err := p.RegisterTable(ctx, table); err != nil {
					return err
				}
				if err := p.RegisterFields(ctx, table, []uuid.UUID{field}); err != nil {
					return err
				}
				fields, err := p.Fields(ctx, table)
				if err != nil {
					return err
				}
				if len(fields) != 1 || fields[0].Name != "field_email_"+strings.ReplaceAll(field.String(), "-", "")+"_value" {
					t.Fatalf("fields=%+v", fields)
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			var again string
			if err := db.QueryRowContext(ctx, "SELECT physical_name FROM data_model_physical_tables WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(scope), mysqlstore.UUID(table)).Scan(&again); err != nil || again != name {
				t.Fatalf("renamed=%s err=%v", again, err)
			}
			_, err = db.ExecContext(ctx, "UPDATE data_model_physical_tables SET tenant_label='changed' WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(scope), mysqlstore.UUID(table))
			mysqlError(t, err, 1644)
			_, err = db.ExecContext(ctx, "UPDATE data_model_physical_fields SET field_label='unsafe`name' WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(scope), mysqlstore.UUID(table))
			mysqlError(t, err, 1644)
		}
	})
	t.Run("mapping_batches_rollback_and_corrupt_component_rejection", func(t *testing.T) {
		types := make([]datamodel.DataType, 1000)
		for i := range types {
			types[i] = "ip_address"
		}
		table, fields := newTable(a, types...)
		injected := errors.New("owner failure after mapping batches")
		err := mutate(a, func(_ *sql.Tx, _ store.MetadataRepositories, p store.PhysicalRegistry) error {
			if err := p.RegisterFields(ctx, table, fields); err != nil {
				return err
			}
			return injected
		})
		if !errors.Is(err, injected) {
			t.Fatal(err)
		}
		if err := mutate(a, func(_ *sql.Tx, _ store.MetadataRepositories, p store.PhysicalRegistry) error {
			got, err := p.Fields(ctx, table)
			if err != nil {
				return err
			}
			if len(got) != 0 {
				t.Fatal("mapping batches survived rollback")
			}
			if err := p.RegisterFields(ctx, table, fields); err != nil {
				return err
			}
			got, err = p.Fields(ctx, table)
			if err != nil {
				return err
			}
			if len(got) != 3000 {
				t.Fatalf("components=%d", len(got))
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, "DELETE FROM data_model_physical_fields WHERE tenant_id=? AND table_id=? AND field_id=? AND component='prefix'", mysqlstore.UUID(a), mysqlstore.UUID(table), mysqlstore.UUID(fields[0])); err != nil {
			t.Fatal(err)
		}
		if err := mutate(a, func(_ *sql.Tx, _ store.MetadataRepositories, p store.PhysicalRegistry) error {
			got, err := p.Fields(ctx, table)
			if got != nil {
				t.Fatal("partial mapping returned")
			}
			return err
		}); !errors.Is(err, store.ErrPhysicalConflict) {
			t.Fatal(err)
		}
		if err := mutate(a, func(_ *sql.Tx, _ store.MetadataRepositories, p store.PhysicalRegistry) error {
			return p.RegisterFields(ctx, table, fields)
		}); !errors.Is(err, store.ErrPhysicalConflict) {
			t.Fatal(err)
		}
	})
	t.Run("bounded_labels_unicode_fallback_and_database_guard", func(t *testing.T) {
		scope, table, field, unregistered := uuid.New(), uuid.New(), uuid.New(), uuid.New()
		if err := mutate(scope, func(_ *sql.Tx, r store.MetadataRepositories, p store.PhysicalRegistry) error {
			if err := r.Tenants.Create(ctx, tenant.New(scope, at, strings.Repeat("A", 80), nil)); err != nil {
				return err
			}
			for _, v := range []datamodel.Table{{ID: table, TenantID: scope, Name: strings.Repeat("B", 80), CreatedAt: at, UpdatedAt: at}, {ID: unregistered, TenantID: scope, Name: "unregistered", CreatedAt: at, UpdatedAt: at}} {
				if err := r.Tables.Create(ctx, v); err != nil {
					return err
				}
			}
			if err := r.Fields.Create(ctx, datamodel.Field{ID: field, TenantID: scope, TableID: table, Name: "客户", DataType: "string", CreatedAt: at, UpdatedAt: at}); err != nil {
				return err
			}
			if err := p.RegisterTable(ctx, table); err != nil {
				return err
			}
			if err := p.RegisterFields(ctx, table, []uuid.UUID{field}); err != nil {
				return err
			}
			fields, err := p.Fields(ctx, table)
			if err != nil {
				return err
			}
			if len(fields) != 1 || fields[0].Name != "field_field_"+strings.ReplaceAll(field.String(), "-", "")+"_value" {
				t.Fatalf("fields=%+v", fields)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		var name string
		if err := db.QueryRowContext(ctx, "SELECT physical_name FROM data_model_physical_tables WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(scope), mysqlstore.UUID(table)).Scan(&name); err != nil {
			t.Fatal(err)
		}
		if len(name) != 63 || !strings.HasPrefix(name, "tenant_aaaaaaaaaa_bbbbbbbbbbbb_") {
			t.Fatal(name)
		}
		if _, err := mysqlstore.Identifier(name); err != nil {
			t.Fatal(err)
		}
		for _, bad := range []string{"Upper", "safe\n", "foo`bar", "two__parts", ""} {
			_, err := db.ExecContext(ctx, "INSERT INTO data_model_physical_tables(tenant_id,table_id,tenant_label,table_label) VALUES (?,?,?,'safe')", mysqlstore.UUID(scope), mysqlstore.UUID(unregistered), bad)
			mysqlError(t, err, 1644)
		}
	})
	t.Run("stale_version_and_retired_table_are_rejected", func(t *testing.T) {
		table, _ := newTable(a)
		version := openFixture(table)
		if err := mutate(a, func(_ *sql.Tx, _ store.MetadataRepositories, p store.PhysicalRegistry) error {
			_, err := p.LockPublished(ctx, table, uuid.New())
			return err
		}); !errors.Is(err, store.ErrPhysicalConflict) {
			t.Fatal(err)
		}
		v := request(table)
		if _, err := intents(a, 1000).Prepare(ctx, v); !errors.Is(err, store.ErrPhysicalConflict) {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, "UPDATE data_model_physical_tables SET write_state='retired' WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(a), mysqlstore.UUID(table)); err != nil {
			t.Fatal(err)
		}
		v.ExpectedVersion = &version
		if _, err := intents(a, 1000).Prepare(ctx, v); !errors.Is(err, store.ErrPhysicalConflict) {
			t.Fatal(err)
		}
	})
	t.Run("prepare_replay_and_immutable_request", func(t *testing.T) {
		table, _ := newTable(a)
		v := request(table)
		s := intents(a, 1000)
		first, err := s.Prepare(ctx, v)
		if err != nil || first.Replayed || first.State != "prepared" {
			t.Fatalf("first=%+v err=%v", first, err)
		}
		again, err := s.Prepare(ctx, v)
		if err != nil || !again.Replayed {
			t.Fatalf("replay=%+v err=%v", again, err)
		}
		for _, change := range []func(*store.DDLIntent){func(v *store.DDLIntent) { v.ActorID = "another" }, func(v *store.DDLIntent) { v.Shape = []byte(`{"action":"different"}`) }, func(v *store.DDLIntent) { v.RequestHash[0] ^= 1 }, func(v *store.DDLIntent) { v.ExpectedEpoch++ }} {
			changed := v
			change(&changed)
			if _, err := s.Prepare(ctx, changed); !errors.Is(err, store.ErrPhysicalConflict) {
				t.Fatal(err)
			}
		}
		got, state, err := s.Read(ctx, v.OperationID)
		if err != nil || state != "prepared" || got.ActorID != v.ActorID || got.RequestHash != v.RequestHash {
			t.Fatalf("read=%+v state=%s err=%v", got, state, err)
		}
		_, err = db.ExecContext(ctx, "UPDATE data_model_schema_change_intents SET actor_id='changed' WHERE tenant_id=? AND operation_id=?", mysqlstore.UUID(a), mysqlstore.UUID(v.OperationID))
		mysqlError(t, err, 1644)
		if _, _, err := intents(b, 1000).Read(ctx, v.OperationID); !errors.Is(err, sql.ErrNoRows) {
			t.Fatal(err)
		}
		if got, _, err := intents(a, 2).Read(ctx, v.OperationID); !errors.Is(err, store.ErrSnapshotLimit) || got.OperationID != uuid.Nil {
			t.Fatalf("limited=%+v err=%v", got, err)
		}
	})
	t.Run("writer_barrier_waits_and_does_not_serialize_other_tables", func(t *testing.T) {
		table, _ := newTable(a)
		other, _ := newTable(a)
		version := openFixture(table)
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		p, err := store.NewPhysicalRegistry(tx, database, a, 100)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := p.LockPublished(ctx, table, version); err != nil {
			t.Fatal(err)
		}
		v := request(table)
		v.ExpectedVersion = &version
		short, stop := context.WithTimeout(ctx, 150*time.Millisecond)
		_, err = intents(a, 1000).Prepare(short, v)
		stop()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("barrier did not wait: %v", err)
		}
		assertEmpty(v.OperationID)
		if _, err := intents(a, 1000).Prepare(ctx, request(other)); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		if _, err := intents(a, 1000).Prepare(ctx, v); err != nil {
			t.Fatal(err)
		}
		if err := mutate(a, func(_ *sql.Tx, _ store.MetadataRepositories, p store.PhysicalRegistry) error {
			_, err := p.LockPublished(ctx, table, version)
			return err
		}); !errors.Is(err, store.ErrModelUnavailable) {
			t.Fatal(err)
		}
	})
	t.Run("competing_operations_and_same_operation_replay", func(t *testing.T) {
		for _, same := range []bool{false, true} {
			table, _ := newTable(a)
			one := request(table)
			two := request(table)
			if same {
				two = one
			}
			type outcome struct {
				value store.DDLIntentResult
				err   error
			}
			done := make(chan outcome, 2)
			for _, v := range []store.DDLIntent{one, two} {
				go func(v store.DDLIntent) { result, err := intents(a, 1000).Prepare(ctx, v); done <- outcome{result, err} }(v)
			}
			success, replayed := 0, 0
			for range 2 {
				got := <-done
				if got.err == nil {
					success++
					if got.value.Replayed {
						replayed++
					}
				} else if !errors.Is(got.err, store.ErrPhysicalConflict) {
					t.Fatal(got.err)
				}
			}
			if (!same && success != 1) || (same && (success != 2 || replayed != 1)) {
				t.Fatalf("same=%v successes=%d replays=%d", same, success, replayed)
			}
		}
	})
	t.Run("intent_and_barrier_rollback_together", func(t *testing.T) {
		table, _ := newTable(a)
		v := request(table)
		if _, err := db.ExecContext(ctx, "CREATE TRIGGER data_model_fail_barrier BEFORE UPDATE ON data_model_physical_tables FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='injected barrier failure'"); err != nil {
			t.Fatal(err)
		}
		_, err := intents(a, 1000).Prepare(ctx, v)
		if _, dropErr := db.ExecContext(ctx, "DROP TRIGGER data_model_fail_barrier"); dropErr != nil {
			t.Fatal(dropErr)
		}
		if err == nil {
			t.Fatal("barrier failure swallowed")
		}
		assertEmpty(v.OperationID)
		if _, err := intents(a, 1000).Prepare(ctx, v); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("lost_commit_acknowledgement_replays_without_second_epoch", func(t *testing.T) {
		table, _ := newTable(a)
		v := request(table)
		var observed atomic.Bool
		faultDB := deliveryFaultDB(t, database, &observed)
		fault, err := store.NewDDLIntentStore(faultDB, database, a, 1000)
		if err != nil {
			t.Fatal(err)
		}
		got, err := fault.Prepare(ctx, v)
		var uncertain *mysqlstore.CommitError
		if !observed.Load() || !errors.As(err, &uncertain) || got.OperationID != uuid.Nil {
			t.Fatalf("got=%+v observed=%v err=%v", got, observed.Load(), err)
		}
		got, err = intents(a, 1000).Prepare(ctx, v)
		if err != nil || !got.Replayed {
			t.Fatalf("replay=%+v err=%v", got, err)
		}
		var epoch int
		if err := db.QueryRowContext(ctx, "SELECT epoch FROM data_model_physical_tables WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(a), mysqlstore.UUID(table)).Scan(&epoch); err != nil || epoch != 1 {
			t.Fatalf("epoch=%d err=%v", epoch, err)
		}
	})
	t.Run("normalized_plan_limit_rolls_back", func(t *testing.T) {
		table, _ := newTable(a)
		v := request(table)
		v.Shape = []byte(`{"x":1}`)
		if _, err := intents(a, len(v.Shape)).Prepare(ctx, v); !errors.Is(err, store.ErrSnapshotLimit) {
			t.Fatal(err)
		}
		assertEmpty(v.OperationID)
	})
	t.Run("keyset_pending_pages", func(t *testing.T) {
		var want []uuid.UUID
		for range 3 {
			table, _ := newTable(b)
			v := request(table)
			if _, err := intents(b, 1000).Prepare(ctx, v); err != nil {
				t.Fatal(err)
			}
			want = append(want, v.OperationID)
		}
		sort.Slice(want, func(i, j int) bool { return want[i].String() < want[j].String() })
		after := uuid.Nil
		for _, id := range want {
			got, err := intents(b, 1000).Pending(ctx, "prepared", after, 1)
			if err != nil || len(got) != 1 || got[0] != id {
				t.Fatalf("page=%v err=%v", got, err)
			}
			after = id
		}
		got, err := intents(b, 1000).Pending(ctx, "prepared", after, 1)
		if err != nil || len(got) != 0 {
			t.Fatalf("end=%v err=%v", got, err)
		}
	})
}
