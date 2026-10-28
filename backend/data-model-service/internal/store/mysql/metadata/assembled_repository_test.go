package metadata_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/service"
	store "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func TestAssembledRepository(t *testing.T) {
	db, database := metadataDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	at := time.Now().UTC().Truncate(time.Microsecond)
	a, b, empty := uuid.New(), uuid.New(), uuid.New()
	parent := datamodel.Table{ID: uuid.New(), TenantID: a, Name: "parent", CreatedAt: at, UpdatedAt: at}
	child := datamodel.Table{ID: uuid.New(), TenantID: a, Name: "child", Alias: "v0", CreatedAt: at, UpdatedAt: at}
	pf := datamodel.Field{ID: uuid.New(), TenantID: a, TableID: parent.ID, Name: "name", DataType: datamodel.DataTypeString, IsEnum: true, CreatedAt: at, UpdatedAt: at}
	cf := datamodel.Field{ID: uuid.New(), TenantID: a, TableID: child.ID, Name: "name", Alias: "v0", DataType: datamodel.DataTypeString, CreatedAt: at, UpdatedAt: at}
	link := datamodel.Link{ID: uuid.New(), TenantID: a, Name: "parent_link", ParentTable: parent.ID, ParentField: pf.ID, ChildTable: child.ID, ChildField: cf.ID, CreatedAt: at}
	direct := datamodel.Pivot{ID: uuid.New(), TenantID: a, BaseTableID: parent.ID, FieldID: &pf.ID, CreatedAt: at}
	path := datamodel.Pivot{ID: uuid.New(), TenantID: a, BaseTableID: child.ID, PathLinkIDs: []uuid.UUID{link.ID, link.ID}, CreatedAt: at.Add(time.Second)}
	option := datamodel.TableOptions{ID: uuid.New(), TenantID: a, TableID: child.ID, DisplayedFields: []uuid.UUID{cf.ID}, FieldOrder: []uuid.UUID{cf.ID}, UpdatedAt: at}
	nav := datamodel.NavigationOption{ID: uuid.New(), TenantID: a, SourceTableID: child.ID, SourceFieldID: cf.ID, TargetTableID: parent.ID, FilterFieldID: pf.ID, OrderingFieldID: pf.ID, CreatedAt: at}
	values := []datamodel.FieldEnumValue{{ID: uuid.New(), TenantID: a, FieldID: pf.ID, Value: "b", Label: "B", SortOrder: 2, CreatedAt: at, UpdatedAt: at}, {ID: uuid.New(), TenantID: a, FieldID: pf.ID, Value: "a", Label: "A", SortOrder: 1, CreatedAt: at, UpdatedAt: at}}
	mutate := func(fn func(store.MetadataRepositories) error) error {
		return mysqlstore.Mutation(ctx, db, func(tx *sql.Tx) error {
			r, err := store.NewMetadataRepositories(tx, database, a, 10000, 1<<20)
			if err != nil {
				return err
			}
			return fn(r)
		})
	}
	if err := mutate(func(r store.MetadataRepositories) error {
		if err := r.Tenants.Create(ctx, tenant.New(a, at, "A", nil)); err != nil {
			return err
		}
		for _, v := range []datamodel.Table{parent, child} {
			if err := r.Tables.Create(ctx, v); err != nil {
				return err
			}
		}
		for _, v := range []datamodel.Field{pf, cf} {
			if err := r.Fields.Create(ctx, v); err != nil {
				return err
			}
		}
		for _, v := range values {
			if err := r.Enums.Create(ctx, v); err != nil {
				return err
			}
		}
		if err := r.Links.Create(ctx, link); err != nil {
			return err
		}
		for _, v := range []datamodel.Pivot{direct, path} {
			if err := r.Pivots.Create(ctx, v); err != nil {
				return err
			}
		}
		if err := r.Options.Upsert(ctx, option); err != nil {
			return err
		}
		return r.Navigation.Create(ctx, nav)
	}); err != nil {
		t.Fatal(err)
	}
	if err := mysqlstore.Mutation(ctx, db, func(tx *sql.Tx) error {
		r, err := store.NewMetadataRepositories(tx, database, b, 100, 10000)
		if err != nil {
			return err
		}
		if err := r.Tenants.Create(ctx, tenant.New(b, at, "B", nil)); err != nil {
			return err
		}
		return r.Tables.Create(ctx, datamodel.Table{ID: uuid.New(), TenantID: b, Name: "child", Description: "foreign", CreatedAt: at, UpdatedAt: at})
	}); err != nil {
		t.Fatal(err)
	}
	read := func(t *testing.T, rows, bytes int) *store.DataModelReadRepository {
		t.Helper()
		r, err := store.NewDataModelReadRepository(db, database, a, rows, bytes)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	t.Run("all_relations_and_empty_shapes", func(t *testing.T) {
		got, err := read(t, 100, 10000).GetAssembledDataModel(ctx, a)
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Tables) != 2 || len(got.Pivots) != 2 {
			t.Fatalf("model %+v", got)
		}
		childModel, parentModel := got.Tables["child"], got.Tables["parent"]
		if !reflect.DeepEqual(childModel.Options, &option) || parentModel.Options != nil {
			t.Fatalf("options %+v %+v", childModel.Options, parentModel.Options)
		}
		if childModel.Fields["name"].EnumValues == nil || len(childModel.Fields["name"].EnumValues) != 0 {
			t.Fatal("empty enum shape")
		}
		if !reflect.DeepEqual(parentModel.Fields["name"].EnumValues, []datamodel.FieldEnumValue{values[1], values[0]}) {
			t.Fatal("enum order")
		}
		l := childModel.LinksToSingle[link.Name]
		if l.ParentTableName != "parent" || l.ChildTableName != "child" || l.ParentFieldName != "name" || l.ChildFieldName != "name" {
			t.Fatalf("link %+v", l)
		}
		if len(childModel.NavigationOptions) != 1 || childModel.NavigationOptions[0].TargetTableName != "parent" || childModel.NavigationOptions[0].FilterFieldName != "name" {
			t.Fatal("navigation")
		}
		if got.Pivots[0].ID != path.ID || got.Pivots[0].Field != "name" || !reflect.DeepEqual(got.Pivots[0].PathLinks, []string{link.Name, link.Name}) || got.Pivots[1].FieldID == nil {
			t.Fatalf("pivots %+v", got.Pivots)
		}
		if parentModel.LinksToSingle == nil || parentModel.NavigationOptions == nil {
			t.Fatal("empty collections became null")
		}
	})
	t.Run("scope_and_invalid_construction", func(t *testing.T) {
		r := read(t, 100, 10000)
		if got, err := r.GetAssembledDataModel(ctx, b); err == nil || got.Tables != nil {
			t.Fatal("cross tenant accepted")
		}
		absent, err := store.NewDataModelReadRepository(db, database, empty, 100, 10000)
		if err != nil {
			t.Fatal(err)
		}
		got, err := absent.GetAssembledDataModel(ctx, empty)
		if err != nil || got.Tables == nil || len(got.Tables) != 0 || got.Pivots == nil {
			t.Fatalf("empty %+v %v", got, err)
		}
		for _, cfg := range []struct {
			db          *sql.DB
			name        string
			id          uuid.UUID
			rows, bytes int
		}{{nil, database, a, 1, 1}, {db, "bad`", a, 1, 1}, {db, database, uuid.Nil, 1, 1}, {db, database, a, 0, 1}, {db, database, a, 1, 0}, {db, database, a, 100001, 1}, {db, database, a, 1, 64<<20 + 1}} {
			if _, err := store.NewDataModelReadRepository(cfg.db, cfg.name, cfg.id, cfg.rows, cfg.bytes); err == nil {
				t.Fatal("invalid constructor accepted")
			}
		}
	})
	t.Run("aggregate_row_and_text_boundaries", func(t *testing.T) {
		// 2 tables, 2 fields, 2 enums, link, 3 pivot joined rows, navigation,
		// options parent, and 2 option child rows = 14. Text projections total 73.
		if _, err := read(t, 14, 73).GetAssembledDataModel(ctx, a); err != nil {
			t.Fatalf("exact budget: %v", err)
		}
		for _, limit := range [][2]int{{13, 10000}, {100, 72}, {1, 1}} {
			got, err := read(t, limit[0], limit[1]).GetAssembledDataModel(ctx, a)
			if !errors.Is(err, store.ErrSnapshotLimit) || got.Tables != nil || got.Pivots != nil {
				t.Fatalf("partial result %+v %v", got, err)
			}
		}
	})
	t.Run("archived_metadata_retained", func(t *testing.T) {
		if err := mutate(func(r store.MetadataRepositories) error {
			v := parent
			v.Archived = true
			if err := r.Tables.Update(ctx, v); err != nil {
				return err
			}
			f := pf
			f.Archived = true
			return r.Fields.Update(ctx, f)
		}); err != nil {
			t.Fatal(err)
		}
		got, err := read(t, 100, 10000).GetAssembledDataModel(ctx, a)
		if err != nil || !got.Tables["parent"].Archived || !got.Tables["parent"].Fields["name"].Archived || len(got.Tables["parent"].Fields["name"].EnumValues) != 2 {
			t.Fatalf("archive %+v %v", got, err)
		}
	})
	t.Run("concurrent_writer_never_tears_snapshot", func(t *testing.T) {
		start := make(chan struct{})
		finished := make(chan error, 1)
		go func() {
			<-start
			for i := 0; i < 60; i++ {
				err := mutate(func(r store.MetadataRepositories) error {
					v, f := child, cf
					v.Alias = fmt.Sprintf("v%d", i)
					f.Alias = v.Alias
					if err := r.Tables.Update(ctx, v); err != nil {
						return err
					}
					return r.Fields.Update(ctx, f)
				})
				if err != nil {
					finished <- err
					return
				}
			}
			finished <- nil
		}()
		close(start)
		r := read(t, 100, 10000)
		var readErr error
		for i := 0; i < 60; i++ {
			got, err := r.GetAssembledDataModel(ctx, a)
			if err != nil {
				readErr = err
				break
			}
			v := got.Tables["child"]
			if v.Alias != v.Fields["name"].Alias {
				readErr = fmt.Errorf("torn aliases %q/%q", v.Alias, v.Fields["name"].Alias)
				break
			}
		}
		if err := <-finished; err != nil {
			t.Fatal(err)
		}
		if readErr != nil {
			t.Fatal(readErr)
		}
	})
	t.Run("constant_query_count_with_many_tables", func(t *testing.T) {
		db.SetMaxOpenConns(1)
		defer db.SetMaxOpenConns(8)
		count := func() int {
			var name string
			var n int
			if err := db.QueryRowContext(ctx, "SHOW SESSION STATUS LIKE 'Com_select'").Scan(&name, &n); err != nil {
				t.Fatal(err)
			}
			return n
		}
		r := read(t, 10000, 1<<20)
		before := count()
		if _, err := r.GetAssembledDataModel(ctx, a); err != nil {
			t.Fatal(err)
		}
		small := count() - before
		if err := mutate(func(r store.MetadataRepositories) error {
			for i := 0; i < 100; i++ {
				v := datamodel.Table{ID: uuid.New(), TenantID: a, Name: fmt.Sprintf("table_%03d", i), CreatedAt: at, UpdatedAt: at}
				if err := r.Tables.Create(ctx, v); err != nil {
					return err
				}
				if err := r.Fields.Create(ctx, datamodel.Field{ID: uuid.New(), TenantID: a, TableID: v.ID, Name: "name", DataType: datamodel.DataTypeString, CreatedAt: at, UpdatedAt: at}); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		before = count()
		got, err := r.GetAssembledDataModel(ctx, a)
		if err != nil {
			t.Fatal(err)
		}
		large := count() - before
		if len(got.Tables) != 102 || small != 9 || large != 9 {
			t.Fatalf("queries small=%d large=%d tables=%d", small, large, len(got.Tables))
		}
		cancelled, stop := context.WithCancel(ctx)
		stop()
		if got, err := r.GetAssembledDataModel(cancelled, a); err == nil || got.Tables != nil {
			t.Fatal("cancellation ignored")
		}
		if _, err := r.GetAssembledDataModel(ctx, a); err != nil {
			t.Fatalf("single-slot pool reuse: %v", err)
		}
	})
	t.Run("published_snapshot_owner_and_revision_consistency", func(t *testing.T) {
		r := read(t, 10000, 1<<20)
		owner, err := service.NewSnapshotDataModelReadService(r)
		if err != nil {
			t.Fatal(err)
		}
		before, err := owner.Get(ctx, a)
		if err != nil || before.RevisionID == "" || before.Tenant.ID != a {
			t.Fatalf("owner read %+v %v", before, err)
		}
		if err := mutate(func(r store.MetadataRepositories) error {
			return r.Revisions.Create(ctx, datamodel.TenantSchemaMigration{ID: uuid.New(), TenantID: a, Version: before.Model.Tables["child"].Alias, AppliedAt: at.Add(60 * time.Second)})
		}); err != nil {
			t.Fatal(err)
		}
		after, err := owner.Get(ctx, a)
		if err != nil || after.RevisionID == before.RevisionID {
			t.Fatalf("revision did not change: %v", err)
		}
		finished := make(chan error, 1)
		go func() {
			for i := 100; i < 140; i++ {
				if err := mutate(func(r store.MetadataRepositories) error {
					v := child
					v.Alias = fmt.Sprintf("v%d", i)
					if err := r.Tables.Update(ctx, v); err != nil {
						return err
					}
					return r.Revisions.Create(ctx, datamodel.TenantSchemaMigration{ID: uuid.New(), TenantID: a, Version: v.Alias, AppliedAt: at.Add(time.Duration(i) * time.Second)})
				}); err != nil {
					finished <- err
					return
				}
			}
			finished <- nil
		}()
		var readErr error
		for i := 0; i < 40; i++ {
			snapshot, err := r.GetDataModelSnapshot(ctx, a)
			if err != nil {
				readErr = err
				break
			}
			if snapshot.Model.Tables["child"].Alias != snapshot.Migrations[0].Version {
				readErr = fmt.Errorf("revision/model snapshot mismatch")
				break
			}
		}
		if err := <-finished; err != nil {
			t.Fatal(err)
		}
		if readErr != nil {
			t.Fatal(readErr)
		}
		if got, err := r.GetDataModelSnapshot(ctx, b); err == nil || got.Model.Tables != nil {
			t.Fatal("foreign snapshot accepted")
		}
		absent, err := store.NewDataModelReadRepository(db, database, empty, 100, 10000)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := absent.GetDataModelSnapshot(ctx, empty); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("missing tenant: %v", err)
		}
		if got, err := read(t, 1, 10000).GetDataModelSnapshot(ctx, a); !errors.Is(err, store.ErrSnapshotLimit) || got.Tenant.ID != uuid.Nil || got.Model.Tables != nil {
			t.Fatalf("snapshot limits: %+v %v", got, err)
		}
	})
	t.Run("corrupt_order_returns_no_model", func(t *testing.T) {
		if _, err := db.ExecContext(ctx, "DELETE FROM data_model_pivot_path_links WHERE tenant_id=? AND pivot_id=? AND ordinal=0", mysqlstore.UUID(a), mysqlstore.UUID(path.ID)); err != nil {
			t.Fatal(err)
		}
		got, err := read(t, 10000, 1<<20).GetAssembledDataModel(ctx, a)
		if err == nil || got.Tables != nil || got.Pivots != nil {
			t.Fatalf("corrupt result %+v %v", got, err)
		}
	})
}
