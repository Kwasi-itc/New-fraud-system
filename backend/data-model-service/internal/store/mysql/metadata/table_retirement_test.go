package metadata_test

import (
	"context"
	"database/sql"
	"errors"
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

func TestTableRetirementRecovery(t *testing.T) {
	db, database := metadataDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	for _, scenario := range []string{"normal", "lost_start_commit", "after_drop_failure"} {
		t.Run(scenario, func(t *testing.T) {
			tid, table, field, op := uuid.New(), uuid.New(), uuid.New(), uuid.New()
			now := time.Now().UTC().Truncate(time.Microsecond)
			var physical string
			err := mysqlstore.Mutation(ctx, db, func(tx *sql.Tx) error {
				r, e := store.NewMetadataRepositories(tx, database, tid, 1000, 4<<20)
				if e != nil {
					return e
				}
				if e = r.Tenants.Create(ctx, tenant.New(tid, now, "Retirement", nil)); e != nil {
					return e
				}
				if e = r.Tenants.UpdateStatus(ctx, tid, tenant.StatusActive); e != nil {
					return e
				}
				if e = r.Tables.Create(ctx, datamodel.Table{ID: table, TenantID: tid, Name: "records", CreatedAt: now, UpdatedAt: now}); e != nil {
					return e
				}
				if e = r.Fields.Create(ctx, datamodel.Field{ID: field, TenantID: tid, TableID: table, Name: "value", DataType: datamodel.DataTypeString, CreatedAt: now, UpdatedAt: now}); e != nil {
					return e
				}
				registry, e := store.NewPhysicalRegistry(tx, database, tid, 1000)
				if e != nil {
					return e
				}
				if e = registry.RegisterTable(ctx, table); e != nil {
					return e
				}
				if _, e = tx.ExecContext(ctx, "UPDATE data_model_physical_tables SET write_state='open',published_version=? WHERE table_id=?", mysqlstore.UUID(uuid.New()), mysqlstore.UUID(table)); e != nil {
					return e
				}
				return tx.QueryRowContext(ctx, "SELECT physical_name FROM data_model_physical_tables WHERE table_id=?", mysqlstore.UUID(table)).Scan(&physical)
			})
			if err != nil {
				t.Fatal(err)
			}
			qualified := "`" + database + "`.`" + physical + "`"
			if _, err = db.ExecContext(ctx, "CREATE TABLE "+qualified+" (id INT PRIMARY KEY)"); err != nil {
				t.Fatal(err)
			}
			if _, err = db.ExecContext(ctx, "INSERT INTO "+qualified+" VALUES(1)"); err != nil {
				t.Fatal(err)
			}
			authorize := func(context.Context, *sql.Tx, uuid.UUID) error { return nil }
			manager, err := store.NewTableRetirement(db, database, tid, authorize)
			if err != nil {
				t.Fatal(err)
			}
			denied, _ := store.NewTableRetirement(db, database, tid, func(context.Context, *sql.Tx, uuid.UUID) error { return errors.New("denied") })
			if _, err = denied.Delete(ctx, table, op, false); err == nil {
				t.Fatal("authority bypass")
			}
			foreign, _ := store.NewTableRetirement(db, database, uuid.New(), authorize)
			if _, err = foreign.Delete(ctx, table, op, false); !errors.Is(err, sql.ErrNoRows) {
				t.Fatal("tenant bypass", err)
			}
			dry, err := manager.Delete(ctx, table, op, true)
			if err != nil || dry.Performed {
				t.Fatal(dry, err)
			}
			var count int
			if err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+qualified).Scan(&count); err != nil || count != 1 {
				t.Fatal("dry run changed data", err)
			}
			pivot := uuid.New()
			if _, err = db.ExecContext(ctx, "INSERT INTO data_model_pivots(id,tenant_id,base_table_id,field_id,created_at) VALUES(?,?,?,?,?)", mysqlstore.UUID(pivot), mysqlstore.UUID(tid), mysqlstore.UUID(table), mysqlstore.UUID(field), now); err != nil {
				t.Fatal(err)
			}
			blocked, err := manager.Delete(ctx, table, op, false)
			if !errors.Is(err, store.ErrTableDeletionDependency) || len(blocked.Conflicts.Pivots) != 1 {
				t.Fatal(blocked, err)
			}
			if _, err = db.ExecContext(ctx, "DELETE FROM data_model_pivots WHERE id=?", mysqlstore.UUID(pivot)); err != nil {
				t.Fatal(err)
			}
			selected := manager
			if scenario == "lost_start_commit" {
				var observed atomic.Bool
				pool := deliveryFaultDB(t, database, &observed)
				selected, err = store.NewTableRetirement(pool, database, tid, authorize)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = selected.Delete(ctx, table, op, false); err == nil || !observed.Load() {
					t.Fatal("commit fault missing", err)
				}
			}
			if scenario == "after_drop_failure" {
				calls := 0
				selected, err = store.NewTableRetirement(db, database, tid, func(context.Context, *sql.Tx, uuid.UUID) error {
					calls++
					if calls == 2 {
						return errors.New("injected final owner failure")
					}
					return nil
				})
				if err != nil {
					t.Fatal(err)
				}
				if _, err = selected.Delete(ctx, table, op, false); err == nil {
					t.Fatal("owner fault missing")
				}
				if err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=? AND table_name=?", database, physical).Scan(&count); err != nil || count != 0 {
					t.Fatal("drop did not occur", count, err)
				}
			}
			if scenario != "normal" {
				var state string
				if err = db.QueryRowContext(ctx, "SELECT write_state FROM data_model_physical_tables WHERE table_id=?", mysqlstore.UUID(table)).Scan(&state); err != nil || state != "closed" {
					t.Fatal("failure reopened gate", state, err)
				}
			}
			result, err := manager.Delete(ctx, table, op, false)
			if err != nil || !result.Performed {
				t.Fatal(result, err)
			}
			if _, err = manager.Delete(ctx, table, op, false); err != nil {
				t.Fatal("replay failed", err)
			}
			if _, err = manager.Delete(ctx, table, uuid.New(), false); !errors.Is(err, sql.ErrNoRows) {
				t.Fatal("new command must see missing table", err)
			}
			var name, state string
			if err = db.QueryRowContext(ctx, "SELECT t.name,p.write_state FROM data_model_tables t JOIN data_model_physical_tables p ON p.table_id=t.id WHERE t.id=?", mysqlstore.UUID(table)).Scan(&name, &state); err != nil || state != "retired" || !strings.HasPrefix(name, "!deleted:") {
				t.Fatal(name, state, err)
			}
			r, e := store.NewPublishedDataModelReadRepository(db, database, tid, 1000, 4<<20)
			if e != nil {
				t.Fatal(e)
			}
			model, e := r.GetAssembledDataModel(ctx, tid)
			if e != nil || len(model.Tables) != 0 {
				t.Fatal("retired table leaked", model, e)
			}
			if err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=? AND table_name=?", database, physical).Scan(&count); err != nil || count != 0 {
				t.Fatal("physical table remains", count, err)
			}
		})
	}
}
