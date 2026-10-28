package metadata_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/display"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
	store "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func TestMySQLDisplayStore(t *testing.T) {
	db, database := metadataDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	tenantID, child, parent, cf, pf, target, link := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	at := time.Now().UTC().Truncate(time.Microsecond)
	err := mysqlstore.Mutation(ctx, db, func(tx *sql.Tx) error {
		r, err := store.NewMetadataRepositories(tx, database, tenantID, 1000, 1<<20)
		if err != nil {
			return err
		}
		if err := r.Tenants.Create(ctx, tenant.New(tenantID, at, "display", nil)); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE data_model_tenants SET status='active' WHERE id=?", mysqlstore.UUID(tenantID)); err != nil {
			return err
		}
		for i, id := range []uuid.UUID{child, parent} {
			if err := r.Tables.Create(ctx, datamodel.Table{ID: id, TenantID: tenantID, Name: []string{"child", "parent"}[i], CreatedAt: at, UpdatedAt: at}); err != nil {
				return err
			}
		}
		for i, id := range []uuid.UUID{cf, pf, target} {
			table := parent
			if i == 0 {
				table = child
			}
			if err := r.Fields.Create(ctx, datamodel.Field{ID: id, TenantID: tenantID, TableID: table, Name: []string{"child_key", "parent_key", "caption"}[i], DataType: datamodel.DataTypeString, IsUnique: i == 1, CreatedAt: at, UpdatedAt: at}); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, "UPDATE data_model_tables SET caption_field='caption' WHERE id=?", mysqlstore.UUID(parent)); err != nil {
			return err
		}
		return r.Links.Create(ctx, datamodel.Link{ID: link, TenantID: tenantID, Name: "relation", ParentTable: parent, ParentField: pf, ChildTable: child, ChildField: cf, CreatedAt: at})
	})
	if err != nil {
		t.Fatal(err)
	}
	s, err := store.NewDisplayStore(db, database, tenantID, 1000, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	// Test callback only: production must use authorized caption/index effects.
	effects := func(context.Context, *sql.Tx) error { return nil }
	input := func(t *testing.T) display.SaveRequest {
		t.Helper()
		v, err := s.Get(ctx, child)
		if err != nil {
			t.Fatal(err)
		}
		return display.SaveRequest{Version: &v.Version, Caption: "child_key", Display: map[string]display.FieldDisplay{cf.String(): {Label: " Key ", LinkID: &link}}, DisplayColumns: []display.DisplayColumn{{ID: "direct", Type: "direct", FieldID: &cf}, {ID: "relation", Type: "relation", LinkID: &link, TargetFieldID: &target}}}
	}
	t.Run("atomic_save_read_and_dependencies", func(t *testing.T) {
		in := input(t)
		v, err := s.Save(ctx, child, in, effects)
		if err != nil {
			t.Fatal(err)
		}
		if v.Version != *in.Version+2 || v.Caption != "child_key" || len(v.Links) != 1 || len(v.Links[0].TargetFields) != 2 || v.Display[cf.String()].Label != "Key" || !reflect.DeepEqual(v.Displayed, []uuid.UUID{cf}) {
			t.Fatalf("saved %+v", v)
		}
		for _, relation := range []string{"data_model_display_field_references", "data_model_display_link_references", "data_model_display_table_references"} {
			var count int
			if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+relation+" WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(child)).Scan(&count); err != nil || count == 0 {
				t.Fatalf("%s %d %v", relation, count, err)
			}
		}
		var history int
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM data_model_schema_change_log WHERE tenant_id=? AND operation='update_display_settings'", mysqlstore.UUID(tenantID)).Scan(&history); err != nil || history != 1 {
			t.Fatalf("audit %d %v", history, err)
		}
	})
	t.Run("reject_wrong_target_despite_parent_caption", func(t *testing.T) {
		in := input(t)
		wrong := uuid.New()
		in.DisplayColumns[1].TargetFieldID = &wrong
		if _, err := s.Save(ctx, child, in, effects); err == nil {
			t.Fatal("wrong target accepted")
		}
	})
	t.Run("validate_legacy_json_even_with_columns", func(t *testing.T) {
		in := input(t)
		in.Display[uuid.NewString()] = display.FieldDisplay{Label: "bad"}
		if _, err := s.Save(ctx, child, in, effects); err == nil {
			t.Fatal("invalid hidden legacy JSON accepted")
		}
		in = input(t)
		in.Order = []uuid.UUID{cf, cf}
		if _, err := s.Save(ctx, child, in, effects); err == nil {
			t.Fatal("duplicate order accepted")
		}
		in = input(t)
		in.DisplayColumns[0].Label = strings.Repeat("x", 101)
		if _, err := s.Save(ctx, child, in, effects); err == nil {
			t.Fatal("oversize label accepted")
		}
	})
	t.Run("owner_failure_rolls_back_everything", func(t *testing.T) {
		before, err := s.Get(ctx, child)
		if err != nil {
			t.Fatal(err)
		}
		in := input(t)
		in.Caption = ""
		failure := errors.New("owner scheduling failed")
		v, err := s.Save(ctx, child, in, func(context.Context, *sql.Tx) error { return failure })
		if !errors.Is(err, failure) || v.TableID != uuid.Nil {
			t.Fatalf("partial save %+v %v", v, err)
		}
		after, err := s.Get(ctx, child)
		if err != nil || !reflect.DeepEqual(before, after) {
			t.Fatalf("rollback mismatch %v", err)
		}
	})
	t.Run("concurrent_optimistic_write_one_winner", func(t *testing.T) {
		in := input(t)
		var wg sync.WaitGroup
		results := make(chan error, 2)
		for range 2 {
			wg.Add(1)
			go func() { defer wg.Done(); _, err := s.Save(ctx, child, in, effects); results <- err }()
		}
		wg.Wait()
		close(results)
		won, conflicts := 0, 0
		for err := range results {
			if err == nil {
				won++
			} else if errors.Is(err, display.ErrConflict) {
				conflicts++
			} else {
				t.Fatal(err)
			}
		}
		if won != 1 || conflicts != 1 {
			t.Fatalf("won=%d conflicts=%d", won, conflicts)
		}
	})
	t.Run("tenant_bounds_and_missing_effects", func(t *testing.T) {
		other, err := store.NewDisplayStore(db, database, uuid.New(), 1000, 1<<20)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := other.Get(ctx, child); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("cross tenant %v", err)
		}
		small, _ := store.NewDisplayStore(db, database, tenantID, 1, 1)
		if v, err := small.Get(ctx, child); !errors.Is(err, store.ErrSnapshotLimit) || v.TableID != uuid.Nil {
			t.Fatalf("limit %+v %v", v, err)
		}
		if _, err := s.Save(ctx, child, input(t), nil); err == nil {
			t.Fatal("missing owner effects")
		}
	})
	t.Run("parent_caption_invalidates_child_version", func(t *testing.T) {
		before, err := s.Get(ctx, child)
		if err != nil {
			t.Fatal(err)
		}
		p, err := s.Get(ctx, parent)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.Save(ctx, parent, display.SaveRequest{Version: &p.Version, Caption: "parent_key"}, effects); err != nil {
			t.Fatal(err)
		}
		after, err := s.Get(ctx, child)
		if err != nil || after.Version != before.Version+1 || after.Links[0].Caption != "parent_key" {
			t.Fatalf("caption invalidation %+v %v", after, err)
		}
	})
	t.Run("lost_commit_acknowledgement_returns_no_success", func(t *testing.T) {
		in := input(t)
		var observed atomic.Bool
		faultDB := deliveryFaultDB(t, database, &observed)
		fault, err := store.NewDisplayStore(faultDB, database, tenantID, 1000, 1<<20)
		if err != nil {
			t.Fatal(err)
		}
		var before int
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM data_model_schema_change_log WHERE tenant_id=?", mysqlstore.UUID(tenantID)).Scan(&before); err != nil {
			t.Fatal(err)
		}
		v, err := fault.Save(ctx, child, in, effects)
		var commit *mysqlstore.CommitError
		if !errors.As(err, &commit) || !observed.Load() || v.TableID != uuid.Nil {
			t.Fatalf("uncertain save %+v %v observed=%v", v, err, observed.Load())
		}
		current, err := s.Get(ctx, child)
		if err != nil || current.Version != *in.Version+1 {
			t.Fatalf("durable outcome %+v %v", current, err)
		}
		called := false
		if _, err := s.Save(ctx, child, in, func(context.Context, *sql.Tx) error { called = true; return nil }); !errors.Is(err, display.ErrConflict) || called {
			t.Fatalf("blind replay %v called=%v", err, called)
		}
		var after int
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM data_model_schema_change_log WHERE tenant_id=?", mysqlstore.UUID(tenantID)).Scan(&after); err != nil || after != before+1 {
			t.Fatalf("durable audit %d -> %d %v", before, after, err)
		}
	})
	t.Run("bounded_batched_reads_no_per_link_field_loading", func(t *testing.T) {
		db.SetMaxOpenConns(1)
		db.SetMaxIdleConns(1)
		defer func() { db.SetMaxOpenConns(8); db.SetMaxIdleConns(4) }()
		count := func() int {
			var name string
			var n int
			if err := db.QueryRowContext(ctx, "SHOW SESSION STATUS LIKE 'Com_select'").Scan(&name, &n); err != nil {
				t.Fatal(err)
			}
			return n
		}
		before := count()
		if _, err := s.Get(ctx, child); err != nil {
			t.Fatal(err)
		}
		small := count() - before
		temporary := []uuid.UUID{}
		if err := mysqlstore.Mutation(ctx, db, func(tx *sql.Tx) error {
			r, err := store.NewMetadataRepositories(tx, database, tenantID, 1000, 1<<20)
			if err != nil {
				return err
			}
			for i := 0; i < 100; i++ {
				id := uuid.New()
				temporary = append(temporary, id)
				if err := r.Links.Create(ctx, datamodel.Link{ID: id, TenantID: tenantID, Name: fmt.Sprintf("extra_%d", i), ParentTable: parent, ParentField: pf, ChildTable: child, ChildField: cf, CreatedAt: at}); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		defer func() {
			for _, id := range temporary {
				if _, err := db.ExecContext(ctx, "DELETE FROM data_model_links WHERE tenant_id=? AND id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(id)); err != nil {
					t.Error(err)
				}
			}
		}()
		before = count()
		v, err := s.Get(ctx, child)
		if err != nil {
			t.Fatal(err)
		}
		large := count() - before
		if small != 5 || large != 5 || len(v.Links) != 101 {
			t.Fatalf("queries small=%d large=%d links=%d", small, large, len(v.Links))
		}
		cancelled, stop := context.WithCancel(ctx)
		stop()
		if v, err := s.Get(cancelled, child); err == nil || v.TableID != uuid.Nil {
			t.Fatalf("cancelled %+v %v", v, err)
		}
		if _, err := s.Get(ctx, child); err != nil {
			t.Fatalf("pool reuse %v", err)
		}
		for _, limits := range [][2]int{{1, 1 << 20}, {1000, 1}} {
			bounded, err := store.NewDisplayStore(db, database, tenantID, limits[0], limits[1])
			if err != nil {
				t.Fatal(err)
			}
			if v, err := bounded.Get(ctx, child); !errors.Is(err, store.ErrSnapshotLimit) || v.TableID != uuid.Nil {
				t.Fatalf("limits=%v result=%+v err=%v", limits, v, err)
			}
		}
	})
	t.Run("clearing_parent_caption_removes_legacy_link_only", func(t *testing.T) {
		p, err := s.Get(ctx, parent)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.Save(ctx, parent, display.SaveRequest{Version: &p.Version}, effects); err != nil {
			t.Fatal(err)
		}
		c, err := s.Get(ctx, child)
		if err != nil || c.Display[cf.String()].LinkID != nil || len(c.DisplayColumns) != 2 {
			t.Fatalf("caption cleanup %+v %v", c, err)
		}
		p, err = s.Get(ctx, parent)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.Save(ctx, parent, display.SaveRequest{Version: &p.Version, Caption: "parent_key"}, effects); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Save(ctx, child, input(t), effects); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("audit_insert_failure_rolls_back_owner_effects", func(t *testing.T) {
		before, err := s.Get(ctx, child)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, "CREATE TRIGGER display_audit_failure BEFORE INSERT ON data_model_schema_change_log FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='injected display audit failure'"); err != nil {
			t.Fatal(err)
		}
		defer func() {
			if _, err := db.ExecContext(ctx, "DROP TRIGGER display_audit_failure"); err != nil {
				t.Error(err)
			}
		}()
		v, err := s.Save(ctx, child, input(t), func(ctx context.Context, tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, "UPDATE data_model_tenants SET name='should rollback' WHERE id=?", mysqlstore.UUID(tenantID))
			return err
		})
		if err == nil || v.TableID != uuid.Nil {
			t.Fatalf("audit failure %+v %v", v, err)
		}
		after, err := s.Get(ctx, child)
		if err != nil || !reflect.DeepEqual(before, after) {
			t.Fatalf("audit rollback %v", err)
		}
		var name string
		if err := db.QueryRowContext(ctx, "SELECT name FROM data_model_tenants WHERE id=?", mysqlstore.UUID(tenantID)).Scan(&name); err != nil || name != "display" {
			t.Fatalf("owner effect leaked %q %v", name, err)
		}
	})
	t.Run("cleanup_rejects_unfinished_owner_mutation", func(t *testing.T) {
		before, err := s.Get(ctx, child)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.RemoveReferences(ctx, "field", target, effects); err == nil {
			t.Fatal("unfinished mutation accepted")
		}
		after, err := s.Get(ctx, child)
		if err != nil || !reflect.DeepEqual(before, after) {
			t.Fatalf("unfinished cleanup leaked %v", err)
		}
	})
	t.Run("cleanup_owner_failure_rolls_back_all_tables", func(t *testing.T) {
		before, err := s.Get(ctx, child)
		if err != nil {
			t.Fatal(err)
		}
		failure := errors.New("archive audit failed")
		if err := s.RemoveReferences(ctx, "field", target, func(context.Context, *sql.Tx) error { return failure }); !errors.Is(err, failure) {
			t.Fatal(err)
		}
		after, err := s.Get(ctx, child)
		if err != nil || !reflect.DeepEqual(before, after) {
			t.Fatalf("cleanup rollback %v", err)
		}
	})
	t.Run("cleanup_lock_cancels_competing_save_without_leaks", func(t *testing.T) {
		in := input(t)
		before, err := s.Get(ctx, child)
		if err != nil {
			t.Fatal(err)
		}
		entered, release := make(chan struct{}), make(chan struct{})
		done := make(chan error, 1)
		failure := errors.New("abort controlled cleanup")
		go func() {
			done <- s.RemoveReferences(ctx, "field", target, func(context.Context, *sql.Tx) error { close(entered); <-release; return failure })
		}()
		select {
		case <-entered:
		case err := <-done:
			t.Fatalf("cleanup never reached callback: %v", err)
		}
		blocked, stop := context.WithTimeout(ctx, 100*time.Millisecond)
		called := false
		v, saveErr := s.Save(blocked, child, in, func(context.Context, *sql.Tx) error { called = true; return nil })
		stop()
		close(release)
		cleanupErr := <-done
		if saveErr == nil || called || v.TableID != uuid.Nil || !errors.Is(cleanupErr, failure) {
			t.Fatalf("save=%v called=%v cleanup=%v", saveErr, called, cleanupErr)
		}
		after, err := s.Get(ctx, child)
		if err != nil || !reflect.DeepEqual(before, after) {
			t.Fatalf("concurrent rollback %v", err)
		}
	})
	t.Run("dependent_version_exhaustion_rolls_back_parent", func(t *testing.T) {
		childBefore, err := s.Get(ctx, child)
		if err != nil {
			t.Fatal(err)
		}
		parentBefore, err := s.Get(ctx, parent)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, "UPDATE data_model_tables SET display_version=9223372036854775807 WHERE tenant_id=? AND id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(child)); err != nil {
			t.Fatal(err)
		}
		defer func() {
			if _, err := db.ExecContext(ctx, "UPDATE data_model_tables SET display_version=? WHERE tenant_id=? AND id=?", childBefore.Version, mysqlstore.UUID(tenantID), mysqlstore.UUID(child)); err != nil {
				t.Error(err)
			}
		}()
		called := false
		v, err := s.Save(ctx, parent, display.SaveRequest{Version: &parentBefore.Version, Caption: "caption"}, func(context.Context, *sql.Tx) error { called = true; return nil })
		if err == nil || called || v.TableID != uuid.Nil {
			t.Fatalf("exhaustion %+v %v called=%v", v, err, called)
		}
		after, err := s.Get(ctx, parent)
		if err != nil || !reflect.DeepEqual(parentBefore, after) {
			t.Fatalf("parent changed %v", err)
		}
	})
	t.Run("archive_target_removes_relation_preserves_direct", func(t *testing.T) {
		if err := s.RemoveReferences(ctx, "field", target, func(ctx context.Context, tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, "UPDATE data_model_fields SET archived=TRUE WHERE tenant_id=? AND id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(target))
			return err
		}); err != nil {
			t.Fatal(err)
		}
		v, err := s.Get(ctx, child)
		if err != nil || len(v.DisplayColumns) != 1 || v.DisplayColumns[0].Type != "direct" || v.Display[cf.String()].LinkID == nil {
			t.Fatalf("target cleanup %+v %v", v, err)
		}
		var refs int
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM data_model_display_field_references WHERE tenant_id=? AND field_id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(target)).Scan(&refs); err != nil || refs != 0 {
			t.Fatalf("stale refs %d %v", refs, err)
		}
	})
	t.Run("delete_link_removes_legacy_link_and_reverse_dependencies", func(t *testing.T) {
		if err := s.RemoveReferences(ctx, "link", link, func(ctx context.Context, tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, "DELETE FROM data_model_links WHERE tenant_id=? AND id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(link))
			return err
		}); err != nil {
			t.Fatal(err)
		}
		v, err := s.Get(ctx, child)
		if err != nil || len(v.DisplayColumns) != 1 || len(v.Links) != 0 || v.Display[cf.String()].LinkID != nil || v.Display[cf.String()].Label != "Key" {
			t.Fatalf("link cleanup %+v %v", v, err)
		}
		var refs int
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM data_model_display_table_references WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(child)).Scan(&refs); err != nil || refs != 0 {
			t.Fatalf("stale parent refs %d %v", refs, err)
		}
	})
	t.Run("archive_direct_caption_cleans_order_and_version", func(t *testing.T) {
		before, err := s.Get(ctx, child)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.RemoveReferences(ctx, "field", cf, func(ctx context.Context, tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, "UPDATE data_model_fields SET archived=TRUE WHERE tenant_id=? AND id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(cf))
			return err
		}); err != nil {
			t.Fatal(err)
		}
		v, err := s.Get(ctx, child)
		if err != nil || v.Caption != "" || len(v.DisplayColumns) != 0 || len(v.Displayed) != 0 || len(v.Order) != 0 || len(v.Display) != 0 || v.Version != before.Version+2 {
			t.Fatalf("field cleanup %+v %v", v, err)
		}
	})
}
