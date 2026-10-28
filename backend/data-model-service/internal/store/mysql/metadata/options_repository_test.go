package metadata_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
	store "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func TestOptionsRepository(t *testing.T) {
	db, database := metadataDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	at := time.Now().UTC().Truncate(time.Microsecond)
	a, b, ta, tb, other := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	fields := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	foreignField := uuid.New()
	begin := func(t *testing.T, id uuid.UUID, maxRows int) (*sql.Tx, store.MetadataRepositories) {
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
		r, err := store.NewMetadataRepositories(tx, database, id, maxRows, 1<<20)
		if err != nil {
			t.Fatal(err)
		}
		return tx, r
	}
	for i, id := range []uuid.UUID{a, b} {
		tx, r := begin(t, id, 2000)
		if err := r.Tenants.Create(ctx, tenant.New(id, at, "Tenant", nil)); err != nil {
			t.Fatal(err)
		}
		table := []uuid.UUID{ta, tb}[i]
		if err := r.Tables.Create(ctx, datamodel.Table{ID: table, TenantID: id, Name: "records", CreatedAt: at, UpdatedAt: at}); err != nil {
			t.Fatal(err)
		}
		ids := fields
		if i == 1 {
			ids = []uuid.UUID{foreignField}
		}
		for j, field := range ids {
			if err := r.Fields.Create(ctx, datamodel.Field{ID: field, TenantID: id, TableID: table, Name: fmt.Sprintf("field_%d", j), DataType: datamodel.DataTypeString, CreatedAt: at, UpdatedAt: at}); err != nil {
				t.Fatal(err)
			}
		}
		if i == 0 {
			if err := r.Tables.Create(ctx, datamodel.Table{ID: other, TenantID: a, Name: "other", CreatedAt: at, UpdatedAt: at}); err != nil {
				t.Fatal(err)
			}
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	input := datamodel.TableOptions{ID: uuid.New(), TenantID: a, TableID: ta, DisplayedFields: []uuid.UUID{fields[1], fields[0]}, FieldOrder: []uuid.UUID{fields[2], fields[0], fields[1]}, UpdatedAt: at}
	version := func(t *testing.T, tx *sql.Tx, table uuid.UUID) int64 {
		t.Helper()
		var n int64
		if err := tx.QueryRowContext(ctx, "SELECT display_version FROM data_model_tables WHERE id=?", mysqlstore.UUID(table)).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	t.Run("round_trip_replace_identity_json_and_version", func(t *testing.T) {
		tx, r := begin(t, a, 2000)
		if got, err := r.Options.GetByTableID(ctx, ta); err != nil || got != nil {
			t.Fatalf("missing %+v %v", got, err)
		}
		if err := r.Options.Upsert(ctx, input); err != nil {
			t.Fatal(err)
		}
		got, err := r.Options.GetByTableID(ctx, ta)
		if err != nil || !reflect.DeepEqual(got, &input) {
			t.Fatalf("round trip %+v %v", got, err)
		}
		if version(t, tx, ta) != 1 {
			t.Fatal("first version")
		}
		columns := fmt.Sprintf(`[{"type":"direct","field_id":"%s"}]`, fields[0])
		if _, err := tx.ExecContext(ctx, "UPDATE data_model_table_options SET field_display=JSON_OBJECT('marker','kept'),display_columns=? WHERE table_id=?", columns, mysqlstore.UUID(ta)); err != nil {
			t.Fatal(err)
		}
		updated := input
		updated.ID = uuid.New()
		updated.DisplayedFields = []uuid.UUID{fields[2]}
		updated.FieldOrder = []uuid.UUID{fields[1], fields[0]}
		updated.UpdatedAt = at.Add(time.Second)
		if err := r.Options.Upsert(ctx, updated); err != nil {
			t.Fatal(err)
		}
		updated.ID = input.ID
		got, err = r.Options.GetByTableID(ctx, ta)
		if err != nil || !reflect.DeepEqual(got, &updated) {
			t.Fatalf("replace %+v %v", got, err)
		}
		var marker, field string
		if err := tx.QueryRowContext(ctx, "SELECT JSON_UNQUOTE(JSON_EXTRACT(field_display,'$.marker')),JSON_UNQUOTE(JSON_EXTRACT(display_columns,'$[0].field_id')) FROM data_model_table_options WHERE table_id=?", mysqlstore.UUID(ta)).Scan(&marker, &field); err != nil || marker != "kept" || field != fields[0].String() {
			t.Fatalf("JSON changed %s %s %v", marker, field, err)
		}
		if err := r.Options.Upsert(ctx, updated); err != nil {
			t.Fatal(err)
		}
		if version(t, tx, ta) != 3 {
			t.Fatal("unchanged upsert did not bump version")
		}
		updated.DisplayedFields = nil
		updated.FieldOrder = nil
		if err := r.Options.Upsert(ctx, updated); err != nil {
			t.Fatal(err)
		}
		got, err = r.Options.GetByTableID(ctx, ta)
		if err != nil || got == nil || len(got.DisplayedFields) != 0 || len(got.FieldOrder) != 0 {
			t.Fatalf("empty lists %+v %v", got, err)
		}
	})
	t.Run("tenant_parent_active_and_distinct_validation", func(t *testing.T) {
		tx, r := begin(t, a, 2000)
		foreign, _ := store.NewMetadataRepositories(tx, database, b, 2000, 1<<20)
		if err := r.Options.Upsert(ctx, input); err != nil {
			t.Fatal(err)
		}
		if got, err := foreign.Options.GetByTableID(ctx, ta); err != nil || got != nil {
			t.Fatalf("foreign read %+v %v", got, err)
		}
		if err := foreign.Options.Upsert(ctx, input); err == nil {
			t.Fatal("foreign write accepted")
		}
		for _, modify := range []func(*datamodel.TableOptions){func(v *datamodel.TableOptions) { v.TableID = tb }, func(v *datamodel.TableOptions) { v.DisplayedFields = []uuid.UUID{foreignField} }, func(v *datamodel.TableOptions) { v.TableID = other }, func(v *datamodel.TableOptions) { v.DisplayedFields = []uuid.UUID{fields[0], fields[0]} }, func(v *datamodel.TableOptions) { v.FieldOrder = []uuid.UUID{fields[0], fields[0]} }, func(v *datamodel.TableOptions) { v.DisplayedFields = make([]uuid.UUID, 13) }, func(v *datamodel.TableOptions) { v.FieldOrder = []uuid.UUID{uuid.Nil} }} {
			v := input
			modify(&v)
			if err := r.Options.Upsert(ctx, v); err == nil {
				t.Fatal("invalid options accepted")
			}
		}
		if _, err := tx.ExecContext(ctx, "UPDATE data_model_fields SET archived=TRUE WHERE id=?", mysqlstore.UUID(fields[0])); err != nil {
			t.Fatal(err)
		}
		if err := r.Options.Upsert(ctx, input); err == nil {
			t.Fatal("archived field accepted")
		}
		if version(t, tx, ta) != 1 {
			t.Fatal("failed validation changed version")
		}
	})
	t.Run("rollback_covers_children_and_version", func(t *testing.T) {
		err := mysqlstore.Mutation(ctx, db, func(tx *sql.Tx) error {
			r, err := store.NewMetadataRepositories(tx, database, a, 2000, 1<<20)
			if err != nil {
				return err
			}
			if err := r.Options.Upsert(ctx, input); err != nil {
				return err
			}
			event := datamodel.SchemaChange{ID: uuid.New(), TenantID: a, Operation: "options", ResourceType: "table", ResourceID: ta, Status: "applied", Details: []byte(`{}`), CreatedAt: at}
			if err := r.History.Create(ctx, event); err != nil {
				return err
			}
			return r.History.Create(ctx, event)
		})
		mysqlError(t, err, 1062)
		tx, read := begin(t, a, 2000)
		if got, err := read.Options.GetByTableID(ctx, ta); err != nil || got != nil {
			t.Fatalf("options survived %+v %v", got, err)
		}
		if version(t, tx, ta) != 0 {
			t.Fatal("version survived rollback")
		}
	})
	t.Run("limits_gaps_and_overflow", func(t *testing.T) {
		tx, r := begin(t, a, 2000)
		small, _ := store.NewMetadataRepositories(tx, database, a, 4, 1<<20)
		if err := small.Options.Upsert(ctx, input); !errors.Is(err, store.ErrSnapshotLimit) {
			t.Fatalf("write row budget %v", err)
		}
		if err := r.Options.Upsert(ctx, input); err != nil {
			t.Fatal(err)
		}
		if got, err := small.Options.GetByTableID(ctx, ta); !errors.Is(err, store.ErrSnapshotLimit) || got != nil {
			t.Fatalf("read row budget %+v %v", got, err)
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM data_model_field_order WHERE tenant_id=? AND table_id=? AND ordinal=1", mysqlstore.UUID(a), mysqlstore.UUID(ta)); err != nil {
			t.Fatal(err)
		}
		if got, err := r.Options.GetByTableID(ctx, ta); err == nil || got != nil {
			t.Fatalf("gap not rejected %+v %v", got, err)
		}
		if _, err := tx.ExecContext(ctx, "UPDATE data_model_tables SET display_version=9223372036854775807 WHERE id=?", mysqlstore.UUID(ta)); err != nil {
			t.Fatal(err)
		}
		if err := r.Options.Upsert(ctx, input); err == nil {
			t.Fatal("version overflow accepted")
		}
	})
	t.Run("large_order_uses_bounded_batches", func(t *testing.T) {
		tx, r := begin(t, a, 2000)
		order := make([]uuid.UUID, 1001)
		args := make([]any, 0, len(order)*7)
		for i := range order {
			order[i] = uuid.New()
			args = append(args, mysqlstore.UUID(order[i]), mysqlstore.UUID(a), mysqlstore.UUID(other), fmt.Sprintf("batch_%d", i), "string", mysqlstore.DateTime{Time: at}, mysqlstore.DateTime{Time: at})
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO data_model_fields(id,tenant_id,table_id,name,data_type,created_at,updated_at) VALUES "+strings.TrimSuffix(strings.Repeat("(?,?,?,?,?,?,?),", len(order)), ","), args...); err != nil {
			t.Fatal(err)
		}
		v := datamodel.TableOptions{ID: uuid.New(), TenantID: a, TableID: other, FieldOrder: order, UpdatedAt: at}
		if err := r.Options.Upsert(ctx, v); err != nil {
			t.Fatal(err)
		}
		exact, err := store.NewMetadataRepositories(tx, database, a, len(order), 1<<20)
		if err != nil {
			t.Fatal(err)
		}
		got, err := exact.Options.GetByTableID(ctx, other)
		if err != nil || got == nil || !reflect.DeepEqual(got.FieldOrder, order) || len(got.DisplayedFields) != 0 {
			t.Fatalf("batch round trip %v", err)
		}
		small, err := store.NewMetadataRepositories(tx, database, a, len(order)-1, 1<<20)
		if err != nil {
			t.Fatal(err)
		}
		if got, err := small.Options.GetByTableID(ctx, other); !errors.Is(err, store.ErrSnapshotLimit) || got != nil {
			t.Fatalf("batch budget %+v %v", got, err)
		}
	})
	t.Run("concurrent_first_upserts_do_not_mix_lists", func(t *testing.T) {
		start := make(chan struct{})
		results := make(chan error, 2)
		first := input
		second := input
		second.ID = uuid.New()
		second.DisplayedFields = []uuid.UUID{fields[2]}
		second.FieldOrder = []uuid.UUID{fields[1], fields[2]}
		for _, v := range []datamodel.TableOptions{first, second} {
			go func(v datamodel.TableOptions) {
				<-start
				results <- mysqlstore.Mutation(ctx, db, func(tx *sql.Tx) error {
					r, err := store.NewMetadataRepositories(tx, database, a, 2000, 1<<20)
					if err != nil {
						return err
					}
					return r.Options.Upsert(ctx, v)
				})
			}(v)
		}
		close(start)
		for i := 0; i < 2; i++ {
			if err := <-results; err != nil {
				t.Fatal(err)
			}
		}
		tx, read := begin(t, a, 2000)
		got, err := read.Options.GetByTableID(ctx, ta)
		if err != nil || got == nil {
			t.Fatalf("final options %+v %v", got, err)
		}
		match := func(v datamodel.TableOptions) bool {
			return reflect.DeepEqual(got.DisplayedFields, v.DisplayedFields) && reflect.DeepEqual(got.FieldOrder, v.FieldOrder)
		}
		if !match(first) && !match(second) {
			t.Fatalf("mixed lists %+v", got)
		}
		if version(t, tx, ta) != 2 {
			t.Fatal("lost concurrent version")
		}
	})
}
