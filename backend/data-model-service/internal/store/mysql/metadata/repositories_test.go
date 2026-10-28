package metadata_test

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	store "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func TestMetadataRepositories(t *testing.T) {
	db, database := metadataDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	at := time.Now().UTC().Truncate(time.Microsecond)
	a, b := uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{a, b} {
		_, err := db.ExecContext(ctx, "INSERT INTO data_model_tenants(id,name,schema_name,status,created_at,updated_at) VALUES (?,? ,?,'active',?,?)", mysqlstore.UUID(id), "tenant", id.String(), mysqlstore.DateTime{Time: at}, mysqlstore.DateTime{Time: at})
		if err != nil {
			t.Fatal(err)
		}
	}
	begin := func(t *testing.T, tenant uuid.UUID, rows, bytes int) (*sql.Tx, store.MetadataRepositories) {
		t.Helper()
		tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
				t.Error(err)
			}
		})
		r, err := store.NewMetadataRepositories(tx, database, tenant, rows, bytes)
		if err != nil {
			t.Fatal(err)
		}
		return tx, r
	}
	ta := datamodel.Table{ID: uuid.New(), TenantID: a, Name: "records", Description: "description", Alias: "alias", SemanticType: "record", CaptionField: "name", CreatedAt: at, UpdatedAt: at}
	tb := ta
	tb.ID = uuid.New()
	tb.TenantID = b
	fa := datamodel.Field{ID: uuid.New(), TenantID: a, TableID: ta.ID, Name: "name", Description: "description", Alias: "alias", DataType: datamodel.DataTypeString, Nullable: true, IsEnum: true, CreatedAt: at, UpdatedAt: at}
	fb := fa
	fb.ID = uuid.New()
	fb.TenantID = b
	fb.TableID = tb.ID
	ea := datamodel.FieldEnumValue{ID: uuid.New(), TenantID: a, FieldID: fa.ID, Value: "one", Label: "One", SortOrder: -1, CreatedAt: at, UpdatedAt: at}
	eb := ea
	eb.ID = uuid.New()
	eb.TenantID = b
	eb.FieldID = fb.ID
	for i, id := range []uuid.UUID{a, b} {
		tx, r := begin(t, id, 100, 1<<20)
		if err := r.Tables.Create(ctx, []datamodel.Table{ta, tb}[i]); err != nil {
			t.Fatal(err)
		}
		if err := r.Fields.Create(ctx, []datamodel.Field{fa, fb}[i]); err != nil {
			t.Fatal(err)
		}
		if err := r.Enums.Create(ctx, []datamodel.FieldEnumValue{ea, eb}[i]); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	link := datamodel.Link{ID: uuid.New(), TenantID: a, Name: "relationship", ParentTable: ta.ID, ParentField: fa.ID, ChildTable: ta.ID, ChildField: fa.ID, CreatedAt: at}
	nav := datamodel.NavigationOption{ID: uuid.New(), TenantID: a, SourceTableID: ta.ID, SourceFieldID: fa.ID, TargetTableID: ta.ID, FilterFieldID: fa.ID, OrderingFieldID: fa.ID, CreatedAt: at, SourceTableName: ta.Name, SourceFieldName: fa.Name, TargetTableName: ta.Name, FilterFieldName: fa.Name, OrderingFieldName: fa.Name}
	t.Run("pivot_ordered_paths_batching_and_limits", func(t *testing.T) {
		tx, r := begin(t, a, 2000, 1<<20)
		if err := r.Links.Create(ctx, link); err != nil {
			t.Fatal(err)
		}
		path := make([]uuid.UUID, 1001)
		for i := range path {
			path[i] = link.ID
		}
		v := datamodel.Pivot{ID: uuid.New(), TenantID: a, BaseTableID: ta.ID, PathLinkIDs: path, CreatedAt: at}
		if err := r.Pivots.Create(ctx, v); err != nil {
			t.Fatal(err)
		}
		got, err := r.Pivots.GetByID(ctx, v.ID)
		if err != nil || !reflect.DeepEqual(got, v) {
			t.Fatalf("path round trip length=%d %v", len(got.PathLinkIDs), err)
		}
		direct := datamodel.Pivot{ID: uuid.New(), TenantID: a, BaseTableID: ta.ID, FieldID: &fa.ID, PathLinkIDs: []uuid.UUID{}, CreatedAt: at.Add(-time.Second)}
		if err := r.Pivots.Create(ctx, direct); err != nil {
			t.Fatal(err)
		}
		pivots, err := r.Pivots.ListByTenant(ctx, a)
		if err != nil || len(pivots) != 2 || !reflect.DeepEqual(pivots[0], v) || !reflect.DeepEqual(pivots[1], direct) {
			t.Fatalf("pivot list length=%d %v", len(pivots), err)
		}
		small, _ := store.NewMetadataRepositories(tx, database, a, 1000, 1<<20)
		if got, err := small.Pivots.GetByID(ctx, v.ID); !errors.Is(err, store.ErrSnapshotLimit) || got.ID != uuid.Nil {
			t.Fatalf("partial path returned %+v %v", got, err)
		}
		if err := small.Pivots.Create(ctx, v); !errors.Is(err, store.ErrSnapshotLimit) {
			t.Fatalf("write path limit %v", err)
		}
		exact, _ := store.NewMetadataRepositories(tx, database, a, 1001, 1<<20)
		if got, err := exact.Pivots.GetByID(ctx, v.ID); err != nil || len(got.PathLinkIDs) != 1001 {
			t.Fatalf("exact path limit %v", err)
		}
		if got, err := exact.Pivots.ListByTenant(ctx, a); !errors.Is(err, store.ErrSnapshotLimit) || got != nil {
			t.Fatalf("aggregate path limit %v", err)
		}
		if err := r.Pivots.Delete(ctx, v.ID); err != nil {
			t.Fatal(err)
		}
		if err := r.Links.Delete(ctx, link.ID); err != nil {
			t.Fatal(err)
		}
		if err := r.Pivots.Delete(ctx, v.ID); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("missing pivot deletion %v", err)
		}
	})
	t.Run("pivot_scope_and_invalid_shapes", func(t *testing.T) {
		tx, r := begin(t, a, 100, 1<<20)
		v := datamodel.Pivot{ID: uuid.New(), TenantID: a, BaseTableID: ta.ID, FieldID: &fa.ID, CreatedAt: at}
		if err := r.Pivots.Create(ctx, v); err != nil {
			t.Fatal(err)
		}
		foreign, _ := store.NewMetadataRepositories(tx, database, b, 100, 1<<20)
		if _, err := foreign.Pivots.GetByID(ctx, v.ID); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("foreign pivot %v", err)
		}
		if err := foreign.Pivots.Delete(ctx, v.ID); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("foreign delete %v", err)
		}
		if err := foreign.Pivots.Create(ctx, v); err == nil {
			t.Fatal("foreign create accepted")
		}
		if _, err := foreign.Pivots.ListByTenant(ctx, a); err == nil {
			t.Fatal("foreign list accepted")
		}
		wrong := v
		wrong.ID = uuid.New()
		wrong.FieldID = &fb.ID
		mysqlError(t, r.Pivots.Create(ctx, wrong), 1452)
		for _, modify := range []func(*datamodel.Pivot){func(p *datamodel.Pivot) { p.FieldID = nil }, func(p *datamodel.Pivot) { p.PathLinkIDs = []uuid.UUID{link.ID} }, func(p *datamodel.Pivot) { p.BaseTableID = uuid.Nil }, func(p *datamodel.Pivot) { p.FieldID = nil; p.PathLinkIDs = []uuid.UUID{uuid.Nil} }} {
			bad := v
			bad.ID = uuid.New()
			modify(&bad)
			if err := r.Pivots.Create(ctx, bad); err == nil {
				t.Fatal("invalid pivot accepted")
			}
		}
	})
	t.Run("pivot_child_failure_rolls_back_owner", func(t *testing.T) {
		id := uuid.New()
		err := mysqlstore.Mutation(ctx, db, func(tx *sql.Tx) error {
			r, err := store.NewMetadataRepositories(tx, database, a, 100, 1<<20)
			if err != nil {
				return err
			}
			changed := ta
			changed.Description = "must roll back"
			if err := r.Tables.Update(ctx, changed); err != nil {
				return err
			}
			return r.Pivots.Create(ctx, datamodel.Pivot{ID: id, TenantID: a, BaseTableID: ta.ID, PathLinkIDs: []uuid.UUID{uuid.New()}, CreatedAt: at})
		})
		mysqlError(t, err, 1452)
		_, read := begin(t, a, 100, 1<<20)
		if _, err := read.Pivots.GetByID(ctx, id); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("parent survived %v", err)
		}
		if got, err := read.Tables.GetByID(ctx, ta.ID); err != nil || got.Description != ta.Description {
			t.Fatalf("owner write survived %+v %v", got, err)
		}
	})
	t.Run("pivot_corrupt_path_is_rejected", func(t *testing.T) {
		tx, r := begin(t, a, 100, 1<<20)
		if err := r.Links.Create(ctx, link); err != nil {
			t.Fatal(err)
		}
		v := datamodel.Pivot{ID: uuid.New(), TenantID: a, BaseTableID: ta.ID, PathLinkIDs: []uuid.UUID{link.ID, link.ID, link.ID}, CreatedAt: at}
		if err := r.Pivots.Create(ctx, v); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM data_model_pivot_path_links WHERE tenant_id=? AND pivot_id=? AND ordinal=1", mysqlstore.UUID(a), mysqlstore.UUID(v.ID)); err != nil {
			t.Fatal(err)
		}
		if got, err := r.Pivots.GetByID(ctx, v.ID); err == nil || got.ID != uuid.Nil {
			t.Fatalf("corrupt path returned %+v %v", got, err)
		}
		if got, err := r.Pivots.ListByTenant(ctx, a); err == nil || got != nil {
			t.Fatalf("corrupt list returned %+v %v", got, err)
		}
	})
	t.Run("relationship_round_trip_order_and_deletion", func(t *testing.T) {
		_, r := begin(t, a, 100, 1<<20)
		if err := r.Links.Create(ctx, link); err != nil {
			t.Fatal(err)
		}
		if err := r.Navigation.Create(ctx, nav); err != nil {
			t.Fatal(err)
		}
		got, err := r.Links.GetByID(ctx, link.ID)
		if err != nil || !reflect.DeepEqual(got, link) {
			t.Fatalf("link %+v %v", got, err)
		}
		gotNav, err := r.Navigation.GetByID(ctx, nav.ID)
		if err != nil || !reflect.DeepEqual(gotNav, nav) {
			t.Fatalf("navigation %+v %v", gotNav, err)
		}
		older := link
		older.ID = uuid.New()
		older.Name = "literal'; DROP TABLE data_model_links; --"
		older.CreatedAt = at.Add(-time.Second)
		if err := r.Links.Create(ctx, older); err != nil {
			t.Fatal(err)
		}
		olderNav := nav
		olderNav.ID = uuid.New()
		olderNav.CreatedAt = older.CreatedAt
		if err := r.Navigation.Create(ctx, olderNav); err != nil {
			t.Fatal(err)
		}
		links, err := r.Links.ListByTenant(ctx, a)
		if err != nil || len(links) != 2 || links[0].ID != link.ID || links[1].Name != older.Name {
			t.Fatalf("links %+v %v", links, err)
		}
		navigation, err := r.Navigation.ListByTenant(ctx, a)
		if err != nil || len(navigation) != 2 || navigation[0].ID != olderNav.ID {
			t.Fatalf("navigation %+v %v", navigation, err)
		}
		byTable, err := r.Navigation.ListBySourceTable(ctx, ta.ID)
		if err != nil || !reflect.DeepEqual(navigation, byTable) {
			t.Fatalf("source list %+v %v", byTable, err)
		}
		archived := fa
		archived.Archived = true
		if err := r.Fields.Update(ctx, archived); err != nil {
			t.Fatal(err)
		}
		if got, err := r.Navigation.GetByID(ctx, nav.ID); err != nil || got.SourceFieldName != fa.Name {
			t.Fatalf("archived name lookup %+v %v", got, err)
		}
		if err := r.Navigation.Delete(ctx, nav.ID); err != nil {
			t.Fatal(err)
		}
		if err := r.Links.Delete(ctx, link.ID); err != nil {
			t.Fatal(err)
		}
		_, e1 := r.Navigation.GetByID(ctx, nav.ID)
		_, e2 := r.Links.GetByID(ctx, link.ID)
		for _, err := range []error{e1, e2, r.Links.Delete(ctx, link.ID), r.Navigation.Delete(ctx, nav.ID)} {
			if !errors.Is(err, sql.ErrNoRows) {
				t.Fatalf("missing relationship: %v", err)
			}
		}
	})
	t.Run("relationship_tenant_and_endpoint_isolation", func(t *testing.T) {
		tx, r := begin(t, a, 100, 1<<20)
		if err := r.Links.Create(ctx, link); err != nil {
			t.Fatal(err)
		}
		if err := r.Navigation.Create(ctx, nav); err != nil {
			t.Fatal(err)
		}
		foreign, err := store.NewMetadataRepositories(tx, database, b, 100, 1<<20)
		if err != nil {
			t.Fatal(err)
		}
		_, e1 := foreign.Links.GetByID(ctx, link.ID)
		_, e2 := foreign.Navigation.GetByID(ctx, nav.ID)
		for _, err := range []error{e1, e2, foreign.Links.Delete(ctx, link.ID), foreign.Navigation.Delete(ctx, nav.ID)} {
			if !errors.Is(err, sql.ErrNoRows) {
				t.Fatalf("foreign relationship: %v", err)
			}
		}
		if err := foreign.Links.Create(ctx, link); err == nil {
			t.Fatal("foreign link accepted")
		}
		if err := foreign.Navigation.Create(ctx, nav); err == nil {
			t.Fatal("foreign navigation accepted")
		}
		if _, err := foreign.Links.ListByTenant(ctx, a); err == nil {
			t.Fatal("foreign link list accepted")
		}
		if _, err := foreign.Navigation.ListByTenant(ctx, a); err == nil {
			t.Fatal("foreign navigation list accepted")
		}
		if got, err := foreign.Navigation.ListBySourceTable(ctx, ta.ID); err != nil || len(got) != 0 {
			t.Fatalf("foreign source list %+v %v", got, err)
		}
		otherTable := ta
		otherTable.ID = uuid.New()
		otherTable.Name = "other"
		if err := r.Tables.Create(ctx, otherTable); err != nil {
			t.Fatal(err)
		}
		for _, modify := range []func(*datamodel.Link){func(v *datamodel.Link) { v.ParentTable = tb.ID; v.ParentField = fb.ID }, func(v *datamodel.Link) { v.ChildTable = tb.ID; v.ChildField = fb.ID }, func(v *datamodel.Link) { v.ParentTable = otherTable.ID }, func(v *datamodel.Link) { v.ChildTable = otherTable.ID }} {
			v := link
			v.ID = uuid.New()
			v.Name = v.ID.String()
			modify(&v)
			mysqlError(t, r.Links.Create(ctx, v), 1452)
		}
		for _, modify := range []func(*datamodel.NavigationOption){func(v *datamodel.NavigationOption) { v.SourceTableID = tb.ID; v.SourceFieldID = fb.ID }, func(v *datamodel.NavigationOption) {
			v.TargetTableID = tb.ID
			v.FilterFieldID = fb.ID
			v.OrderingFieldID = fb.ID
		}, func(v *datamodel.NavigationOption) { v.SourceTableID = otherTable.ID }, func(v *datamodel.NavigationOption) { v.FilterFieldID = fb.ID }, func(v *datamodel.NavigationOption) { v.OrderingFieldID = fb.ID }} {
			v := nav
			v.ID = uuid.New()
			modify(&v)
			mysqlError(t, r.Navigation.Create(ctx, v), 1452)
		}
	})
	t.Run("relationship_limits_and_invalid_endpoints", func(t *testing.T) {
		tx, r := begin(t, a, 100, 1<<20)
		v := link
		v.Name = strings.Repeat("\u754c", 20)
		if err := r.Links.Create(ctx, v); err != nil {
			t.Fatal(err)
		}
		if err := r.Navigation.Create(ctx, nav); err != nil {
			t.Fatal(err)
		}
		small, _ := store.NewMetadataRepositories(tx, database, a, 1, 1)
		if _, err := small.Links.GetByID(ctx, v.ID); !errors.Is(err, store.ErrSnapshotLimit) {
			t.Fatalf("link bytes %v", err)
		}
		if _, err := small.Navigation.GetByID(ctx, nav.ID); !errors.Is(err, store.ErrSnapshotLimit) {
			t.Fatalf("joined bytes %v", err)
		}
		if got, err := small.Links.ListByTenant(ctx, a); !errors.Is(err, store.ErrSnapshotLimit) || got != nil {
			t.Fatalf("list bytes %+v %v", got, err)
		}
		if got, err := small.Navigation.ListBySourceTable(ctx, ta.ID); !errors.Is(err, store.ErrSnapshotLimit) || got != nil {
			t.Fatalf("source bytes %+v %v", got, err)
		}
		if err := small.Links.Create(ctx, v); !errors.Is(err, store.ErrSnapshotLimit) {
			t.Fatalf("write bytes %v", err)
		}
		exact, _ := store.NewMetadataRepositories(tx, database, a, 1, 60)
		if got, err := exact.Links.GetByID(ctx, v.ID); err != nil || got.Name != v.Name {
			t.Fatalf("exact bytes %+v %v", got, err)
		}
		nav2 := nav
		nav2.ID = uuid.New()
		if err := r.Navigation.Create(ctx, nav2); err != nil {
			t.Fatal(err)
		}
		if got, err := exact.Navigation.ListByTenant(ctx, a); !errors.Is(err, store.ErrSnapshotLimit) || got != nil {
			t.Fatalf("row limit %+v %v", got, err)
		}
		bad := link
		bad.ParentField = uuid.Nil
		if err := r.Links.Create(ctx, bad); err == nil {
			t.Fatal("nil link endpoint accepted")
		}
		badNav := nav
		badNav.OrderingFieldID = uuid.Nil
		if err := r.Navigation.Create(ctx, badNav); err == nil {
			t.Fatal("nil navigation endpoint accepted")
		}
	})
	t.Run("relationship_rollback_and_pivot_delete_barrier", func(t *testing.T) {
		tx, r := begin(t, a, 100, 1<<20)
		if err := r.Links.Create(ctx, link); err != nil {
			t.Fatal(err)
		}
		if err := r.Navigation.Create(ctx, nav); err != nil {
			t.Fatal(err)
		}
		pivot := uuid.New()
		if _, err := tx.ExecContext(ctx, "INSERT INTO data_model_pivots(id,tenant_id,base_table_id,created_at) VALUES (?,?,?,?)", mysqlstore.UUID(pivot), mysqlstore.UUID(a), mysqlstore.UUID(ta.ID), mysqlstore.DateTime{Time: at}); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO data_model_pivot_path_links(tenant_id,pivot_id,ordinal,link_id) VALUES (?,?,0,?)", mysqlstore.UUID(a), mysqlstore.UUID(pivot), mysqlstore.UUID(link.ID)); err != nil {
			t.Fatal(err)
		}
		mysqlError(t, r.Links.Delete(ctx, link.ID), 1451)
		if _, err := r.Links.GetByID(ctx, link.ID); err != nil {
			t.Fatal(err)
		}
		if err := tx.Rollback(); err != nil {
			t.Fatal(err)
		}
		_, read := begin(t, a, 100, 1<<20)
		_, e1 := read.Links.GetByID(ctx, link.ID)
		_, e2 := read.Navigation.GetByID(ctx, nav.ID)
		if !errors.Is(e1, sql.ErrNoRows) || !errors.Is(e2, sql.ErrNoRows) {
			t.Fatalf("rollback %v %v", e1, e2)
		}
	})
	t.Run("concurrent_link_duplicate", func(t *testing.T) {
		start := make(chan struct{})
		results := make(chan error, 2)
		for i := 0; i < 2; i++ {
			go func() {
				<-start
				results <- mysqlstore.Mutation(ctx, db, func(tx *sql.Tx) error {
					r, err := store.NewMetadataRepositories(tx, database, a, 100, 1<<20)
					if err != nil {
						return err
					}
					v := link
					v.ID = uuid.New()
					v.Name = "concurrent"
					return r.Links.Create(ctx, v)
				})
			}()
		}
		close(start)
		first, second := <-results, <-results
		if first == nil {
			mysqlError(t, second, 1062)
		} else {
			mysqlError(t, first, 1062)
			if second != nil {
				t.Fatal(second)
			}
		}
		_, read := begin(t, a, 100, 1<<20)
		links, err := read.Links.ListByTenant(ctx, a)
		if err != nil || len(links) != 1 || links[0].Name != "concurrent" {
			t.Fatalf("duplicate winner %+v %v", links, err)
		}
	})
	t.Run("round_trip_and_supported_mutations", func(t *testing.T) {
		_, r := begin(t, a, 100, 1<<20)
		got, err := r.Tables.GetByID(ctx, ta.ID)
		if err != nil || !reflect.DeepEqual(got, ta) {
			t.Fatalf("table: %+v %v", got, err)
		}
		field, err := r.Fields.GetByID(ctx, fa.ID)
		if err != nil || !reflect.DeepEqual(field, fa) {
			t.Fatalf("field: %+v %v", field, err)
		}
		enum, err := r.Enums.GetByID(ctx, ea.ID)
		if err != nil || !reflect.DeepEqual(enum, ea) {
			t.Fatalf("enum: %+v %v", enum, err)
		}
		got.Archived = true
		got.Description = "changed"
		got.Alias = "new"
		got.SemanticType = "payment"
		got.CaptionField = "title"
		got.UpdatedAt = at.Add(time.Second)
		if err := r.Tables.Update(ctx, got); err != nil {
			t.Fatal(err)
		}
		actual, err := r.Tables.GetByID(ctx, ta.ID)
		if err != nil || !reflect.DeepEqual(actual, got) {
			t.Fatalf("updated table %+v %v", actual, err)
		}
		field.Archived = true
		field.Nullable = false
		field.IsEnum = false
		field.IsUnique = true
		field.Alias = "new"
		field.Description = "changed"
		field.UpdatedAt = at.Add(time.Second)
		if err := r.Fields.Update(ctx, field); err != nil {
			t.Fatal(err)
		}
		actualField, err := r.Fields.GetByID(ctx, fa.ID)
		if err != nil || !reflect.DeepEqual(actualField, field) {
			t.Fatalf("updated field %+v %v", actualField, err)
		}
		enum.Value = "two"
		enum.Label = "Two"
		enum.SortOrder = 2
		enum.UpdatedAt = at.Add(time.Second)
		if err := r.Enums.Update(ctx, enum); err != nil {
			t.Fatal(err)
		}
		actualEnum, err := r.Enums.GetByID(ctx, ea.ID)
		if err != nil || !reflect.DeepEqual(actualEnum, enum) {
			t.Fatalf("updated enum %+v %v", actualEnum, err)
		}
		// Repeating an identical update succeeds even with changed-row reporting.
		for _, err := range []error{r.Tables.Update(ctx, got), r.Fields.Update(ctx, field), r.Enums.Update(ctx, enum)} {
			if err != nil {
				t.Fatal(err)
			}
		}
		for _, typ := range datamodel.SupportedDataTypes() {
			v := fa
			v.ID = uuid.New()
			v.Name = string(typ)
			v.DataType = typ
			v.IsEnum = false
			if err := r.Fields.Create(ctx, v); err != nil {
				t.Fatal(err)
			}
			read, err := r.Fields.GetByID(ctx, v.ID)
			if err != nil || !reflect.DeepEqual(read, v) {
				t.Fatalf("type %s: %+v %v", typ, read, err)
			}
		}
	})
	t.Run("tenant_and_parent_isolation", func(t *testing.T) {
		_, r := begin(t, a, 100, 1<<20)
		_, e1 := r.Tables.GetByID(ctx, tb.ID)
		_, e2 := r.Fields.GetByID(ctx, fb.ID)
		_, e3 := r.Enums.GetByID(ctx, eb.ID)
		for _, err := range []error{e1, e2, e3, r.Tables.Delete(ctx, tb.ID), r.Fields.Delete(ctx, fb.ID), r.Enums.Delete(ctx, eb.ID)} {
			if !errors.Is(err, sql.ErrNoRows) {
				t.Fatalf("cross-tenant access: %v", err)
			}
		}
		for _, err := range []error{r.Tables.Create(ctx, tb), r.Fields.Create(ctx, fb), r.Enums.Create(ctx, eb), r.Tables.Update(ctx, tb), r.Fields.Update(ctx, fb), r.Enums.Update(ctx, eb)} {
			if err == nil {
				t.Fatal("foreign tenant accepted")
			}
		}
		if _, err := r.Tables.ListByTenant(ctx, b); err == nil {
			t.Fatal("foreign tenant list accepted")
		}
		fields, err := r.Fields.ListByTable(ctx, tb.ID)
		if err != nil || len(fields) != 0 {
			t.Fatalf("foreign fields %v %v", fields, err)
		}
		enums, err := r.Enums.ListByField(ctx, fb.ID)
		if err != nil || len(enums) != 0 {
			t.Fatalf("foreign enums %v %v", enums, err)
		}
		wrong := fa
		wrong.TableID = tb.ID
		if err := r.Fields.Update(ctx, wrong); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("wrong parent update: %v", err)
		}
		wrong.ID = uuid.New()
		mysqlError(t, r.Fields.Create(ctx, wrong), 1452)
		wrongEnum := ea
		wrongEnum.FieldID = fb.ID
		if err := r.Enums.Update(ctx, wrongEnum); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("wrong parent enum: %v", err)
		}
		wrongEnum.ID = uuid.New()
		mysqlError(t, r.Enums.Create(ctx, wrongEnum), 1452)
	})
	t.Run("ordered_lists_include_archived", func(t *testing.T) {
		_, r := begin(t, a, 100, 1<<20)
		v := ta
		v.ID = uuid.New()
		v.Name = "older"
		v.CreatedAt = at.Add(-time.Second)
		v.Archived = true
		if err := r.Tables.Create(ctx, v); err != nil {
			t.Fatal(err)
		}
		tables, err := r.Tables.ListByTenant(ctx, a)
		if err != nil || len(tables) != 2 || tables[0].ID != ta.ID || tables[1].ID != v.ID || !tables[1].Archived {
			t.Fatalf("tables %+v %v", tables, err)
		}
		f := fa
		f.ID = uuid.New()
		f.Name = "older"
		f.CreatedAt = at.Add(-time.Second)
		f.Archived = true
		if err := r.Fields.Create(ctx, f); err != nil {
			t.Fatal(err)
		}
		fields, err := r.Fields.ListByTable(ctx, ta.ID)
		if err != nil || len(fields) != 2 || fields[0].ID != f.ID || !fields[0].Archived {
			t.Fatalf("fields %+v %v", fields, err)
		}
		e := ea
		e.ID = uuid.New()
		e.Value = "later"
		e.Label = "Later"
		e.SortOrder = 0
		if err := r.Enums.Create(ctx, e); err != nil {
			t.Fatal(err)
		}
		enums, err := r.Enums.ListByField(ctx, fa.ID)
		if err != nil || len(enums) != 2 || enums[0].ID != ea.ID {
			t.Fatalf("enums %+v %v", enums, err)
		}
	})
	t.Run("delete_and_missing_mutations", func(t *testing.T) {
		_, r := begin(t, a, 100, 1<<20)
		if err := r.Enums.Delete(ctx, ea.ID); err != nil {
			t.Fatal(err)
		}
		if err := r.Fields.Delete(ctx, fa.ID); err != nil {
			t.Fatal(err)
		}
		if err := r.Tables.Delete(ctx, ta.ID); err != nil {
			t.Fatal(err)
		}
		for _, err := range []error{r.Tables.Update(ctx, ta), r.Fields.Update(ctx, fa), r.Enums.Update(ctx, ea), r.Tables.Delete(ctx, ta.ID), r.Fields.Delete(ctx, fa.ID), r.Enums.Delete(ctx, ea.ID)} {
			if !errors.Is(err, sql.ErrNoRows) {
				t.Fatalf("missing mutation: %v", err)
			}
		}
	})
	t.Run("transaction_rollback_and_exact_conflicts", func(t *testing.T) {
		tx, r := begin(t, a, 100, 1<<20)
		v := ta
		v.Description = "must roll back"
		if err := r.Tables.Update(ctx, v); err != nil {
			t.Fatal(err)
		}
		e := ea
		e.ID = uuid.New()
		e.Label = "other"
		mysqlError(t, r.Enums.Create(ctx, e), 1062)
		if err := tx.Rollback(); err != nil {
			t.Fatal(err)
		}
		_, read := begin(t, a, 100, 1<<20)
		got, err := read.Tables.GetByID(ctx, ta.ID)
		if err != nil || got.Description != ta.Description {
			t.Fatalf("rollback %+v %v", got, err)
		}
		if _, err := r.Tables.GetByID(ctx, ta.ID); !errors.Is(err, sql.ErrTxDone) {
			t.Fatalf("closed transaction: %v", err)
		}
	})
	t.Run("bounded_reads_and_writes", func(t *testing.T) {
		tx, r := begin(t, a, 100, 1<<20)
		v := ea
		v.ID = uuid.New()
		v.Value = strings.Repeat("\u754c", 20)
		v.Label = "long"
		if err := r.Enums.Create(ctx, v); err != nil {
			t.Fatal(err)
		}
		small, err := store.NewMetadataRepositories(tx, database, a, 1, 63)
		if err != nil {
			t.Fatal(err)
		}
		if got, err := small.Enums.GetByID(ctx, v.ID); !errors.Is(err, store.ErrSnapshotLimit) || got.ID != uuid.Nil {
			t.Fatalf("oversized record: %+v %v", got, err)
		}
		if got, err := small.Enums.ListByField(ctx, fa.ID); !errors.Is(err, store.ErrSnapshotLimit) || got != nil {
			t.Fatalf("row limit: %+v %v", got, err)
		}
		if err := small.Enums.Create(ctx, v); !errors.Is(err, store.ErrSnapshotLimit) {
			t.Fatalf("write limit: %v", err)
		}
		exact, _ := store.NewMetadataRepositories(tx, database, a, 10, 64)
		if got, err := exact.Enums.GetByID(ctx, v.ID); err != nil || got.Value != v.Value {
			t.Fatalf("exact byte boundary %+v %v", got, err)
		}
		if got, err := exact.Enums.ListByField(ctx, fa.ID); !errors.Is(err, store.ErrSnapshotLimit) || got != nil {
			t.Fatalf("cumulative byte limit %+v %v", got, err)
		}
		v.Value = "bad\x00value"
		if err := r.Enums.Update(ctx, v); err == nil {
			t.Fatal("NUL accepted")
		}
		v.Value = string([]byte{0xff})
		if err := r.Enums.Update(ctx, v); err == nil {
			t.Fatal("invalid UTF-8 accepted")
		}
	})
	t.Run("record_locks_and_independent_tenant", func(t *testing.T) {
		tx, r := begin(t, a, 100, 1<<20)
		if err := r.Tables.Update(ctx, ta); err != nil {
			t.Fatal(err)
		}
		otherTx, other := begin(t, b, 100, 1<<20)
		if err := other.Tables.Update(ctx, tb); err != nil {
			t.Fatal(err)
		}
		if err := otherTx.Commit(); err != nil {
			t.Fatal(err)
		}
		blockedTx, blocked := begin(t, a, 100, 1<<20)
		short, stop := context.WithTimeout(ctx, 150*time.Millisecond)
		defer stop()
		if err := blocked.Tables.Update(short, ta); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected cancelled record lock: %v", err)
		}
		// The driver can discard the connection on query cancellation. Its
		// rollback then reports the cancellation rather than confirmation;
		// mysqlstore.Mutation preserves this as RollbackError for owners.
		if err := blockedTx.Rollback(); err != nil && !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		_, next := begin(t, a, 100, 1<<20)
		if err := next.Tables.Update(ctx, ta); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("invalid_configuration_and_read_only", func(t *testing.T) {
		tx, _ := begin(t, a, 100, 1<<20)
		for _, c := range []struct {
			tx          *sql.Tx
			db          string
			tenant      uuid.UUID
			rows, bytes int
		}{{nil, database, a, 1, 1}, {tx, "bad`name", a, 1, 1}, {tx, database, uuid.Nil, 1, 1}, {tx, database, a, 0, 1}, {tx, database, a, 1, 0}, {tx, database, a, 100001, 1}, {tx, database, a, 1, (64 << 20) + 1}} {
			if _, err := store.NewMetadataRepositories(c.tx, c.db, c.tenant, c.rows, c.bytes); err == nil {
				t.Fatal("invalid constructor accepted")
			}
		}
		ro, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
		if err != nil {
			t.Fatal(err)
		}
		defer ro.Rollback()
		r, err := store.NewMetadataRepositories(ro, database, a, 100, 1<<20)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := r.Tables.GetByID(ctx, ta.ID); err != nil {
			t.Fatal(err)
		}
		v := ta
		v.ID = uuid.New()
		v.Name = "read_only"
		mysqlError(t, r.Tables.Create(ctx, v), 1792)
	})
}
