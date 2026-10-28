package metadata_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	store "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func TestEventRepositories(t *testing.T) {
	db, database := metadataDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	a, b := uuid.New(), uuid.New()
	at := time.Now().UTC().Truncate(time.Microsecond)
	for _, id := range []uuid.UUID{a, b} {
		if _, err := db.ExecContext(ctx, "INSERT INTO data_model_tenants(id,name,schema_name,status,created_at,updated_at) VALUES (?,? ,?,'active',?,?)", mysqlstore.UUID(id), "tenant", id.String(), mysqlstore.DateTime{Time: at}, mysqlstore.DateTime{Time: at}); err != nil {
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
	event := datamodel.SchemaChange{ID: uuid.New(), TenantID: a, Operation: "update_table", ResourceType: "table", ResourceID: uuid.New(), Status: "applied", Details: []byte(`{"value":9007199254740993,"label":"界"}`), CreatedAt: at}
	revision := datamodel.TenantSchemaMigration{ID: uuid.New(), TenantID: a, Version: "v1", AppliedAt: at}
	t.Run("round_trip_json_and_immutable_events", func(t *testing.T) {
		tx, r := begin(t, a, 10, 10000)
		if err := r.History.Create(ctx, event); err != nil {
			t.Fatal(err)
		}
		if err := r.Revisions.Create(ctx, revision); err != nil {
			t.Fatal(err)
		}
		history, err := r.History.ListByTenant(ctx, a)
		if err != nil || len(history) != 1 {
			t.Fatalf("history %v %v", history, err)
		}
		want := event
		want.Details = history[0].Details
		if !reflect.DeepEqual(want, history[0]) {
			t.Fatalf("round trip %+v", history[0])
		}
		var details map[string]json.RawMessage
		if err := json.Unmarshal(history[0].Details, &details); err != nil {
			t.Fatal(err)
		}
		if string(details["value"]) != "9007199254740993" {
			t.Fatalf("number lost %s", details["value"])
		}
		versions, err := r.Revisions.ListByTenant(ctx, a)
		if err != nil || len(versions) != 1 || !reflect.DeepEqual(versions[0], revision) {
			t.Fatalf("revisions %+v %v", versions, err)
		}
		_, err = tx.ExecContext(ctx, "UPDATE data_model_schema_change_log SET status='changed' WHERE id=?", mysqlstore.UUID(event.ID))
		mysqlError(t, err, 1644)
		_, err = tx.ExecContext(ctx, "UPDATE data_model_tenant_schema_migrations SET version='changed' WHERE id=?", mysqlstore.UUID(revision.ID))
		mysqlError(t, err, 1644)
		for _, payload := range []string{`null`, `[]`, `"text"`} {
			v := event
			v.ID = uuid.New()
			v.Details = []byte(payload)
			if err := r.History.Create(ctx, v); err != nil {
				t.Fatal(err)
			}
		}
	})
	t.Run("keyset_pages_handle_equal_timestamps", func(t *testing.T) {
		_, r := begin(t, a, 10, 10000)
		for i := 1; i <= 5; i++ {
			id := uuid.UUID{}
			id[15] = byte(i)
			v := event
			v.ID = id
			v.CreatedAt = at
			if i == 5 {
				v.CreatedAt = at.Add(time.Second)
			}
			if err := r.History.Create(ctx, v); err != nil {
				t.Fatal(err)
			}
			rev := revision
			rev.ID = id
			rev.AppliedAt = v.CreatedAt
			if err := r.Revisions.Create(ctx, rev); err != nil {
				t.Fatal(err)
			}
		}
		var after *store.EventCursor
		var ids []byte
		for {
			page, next, err := r.History.ListPage(ctx, 2, after)
			if err != nil {
				t.Fatal(err)
			}
			for _, v := range page {
				ids = append(ids, v.ID[15])
			}
			if next == nil {
				break
			}
			after = next
			if len(ids) > 5 {
				t.Fatal("non-advancing history cursor")
			}
		}
		if !reflect.DeepEqual(ids, []byte{5, 4, 3, 2, 1}) {
			t.Fatalf("history order %v", ids)
		}
		after = nil
		ids = nil
		for {
			page, next, err := r.Revisions.ListPage(ctx, 2, after)
			if err != nil {
				t.Fatal(err)
			}
			for _, v := range page {
				ids = append(ids, v.ID[15])
			}
			if next == nil {
				break
			}
			after = next
			if len(ids) > 5 {
				t.Fatal("non-advancing revision cursor")
			}
		}
		if !reflect.DeepEqual(ids, []byte{5, 4, 3, 2, 1}) {
			t.Fatalf("revision order %v", ids)
		}
		_, next, err := r.History.ListPage(ctx, 5, nil)
		if err != nil || next != nil {
			t.Fatalf("exact final page %v %v", next, err)
		}
	})
	t.Run("tenant_and_cursor_scope", func(t *testing.T) {
		tx, r := begin(t, a, 10, 10000)
		if err := r.History.Create(ctx, event); err != nil {
			t.Fatal(err)
		}
		if err := r.Revisions.Create(ctx, revision); err != nil {
			t.Fatal(err)
		}
		foreign, _ := store.NewMetadataRepositories(tx, database, b, 10, 10000)
		if err := foreign.History.Create(ctx, event); err == nil {
			t.Fatal("foreign event accepted")
		}
		if err := foreign.Revisions.Create(ctx, revision); err == nil {
			t.Fatal("foreign revision accepted")
		}
		if _, err := foreign.History.ListByTenant(ctx, a); err == nil {
			t.Fatal("foreign history listed")
		}
		if _, err := foreign.Revisions.ListByTenant(ctx, a); err == nil {
			t.Fatal("foreign revisions listed")
		}
		if got, next, err := foreign.History.ListPage(ctx, 2, nil); err != nil || len(got) != 0 || next != nil {
			t.Fatalf("tenant leak %+v %v %v", got, next, err)
		}
		if got, next, err := foreign.Revisions.ListPage(ctx, 2, nil); err != nil || len(got) != 0 || next != nil {
			t.Fatalf("revision leak %+v %v %v", got, next, err)
		}
		for _, cursor := range []*store.EventCursor{{TenantID: b, Stream: "history", At: at, ID: event.ID}, {TenantID: a, Stream: "revisions", At: at, ID: event.ID}, {TenantID: a, Stream: "history", At: at}, {TenantID: a, Stream: "history", ID: event.ID}} {
			if _, _, err := r.History.ListPage(ctx, 2, cursor); err == nil {
				t.Fatal("invalid cursor accepted")
			}
		}
		if _, _, err := r.Revisions.ListPage(ctx, 2, &store.EventCursor{TenantID: a, Stream: "history", At: at, ID: revision.ID}); err == nil {
			t.Fatal("wrong stream accepted")
		}
		for _, limit := range []int{0, 11} {
			if _, _, err := r.History.ListPage(ctx, limit, nil); err == nil {
				t.Fatal("invalid page limit accepted")
			}
		}
	})
	t.Run("limits_and_invalid_json", func(t *testing.T) {
		tx, r := begin(t, a, 10, 10000)
		for i := 0; i < 2; i++ {
			v := event
			v.ID = uuid.New()
			if err := r.History.Create(ctx, v); err != nil {
				t.Fatal(err)
			}
			rev := revision
			rev.ID = uuid.New()
			if err := r.Revisions.Create(ctx, rev); err != nil {
				t.Fatal(err)
			}
		}
		small, _ := store.NewMetadataRepositories(tx, database, a, 1, 1)
		if got, next, err := small.History.ListPage(ctx, 1, nil); !errors.Is(err, store.ErrSnapshotLimit) || got != nil || next != nil {
			t.Fatalf("history partial page %v %v %v", got, next, err)
		}
		if got, next, err := small.Revisions.ListPage(ctx, 1, nil); !errors.Is(err, store.ErrSnapshotLimit) || got != nil || next != nil {
			t.Fatalf("revision partial page %v %v %v", got, next, err)
		}
		if err := small.History.Create(ctx, event); !errors.Is(err, store.ErrSnapshotLimit) {
			t.Fatalf("history write size %v", err)
		}
		if err := small.Revisions.Create(ctx, revision); !errors.Is(err, store.ErrSnapshotLimit) {
			t.Fatalf("revision write size %v", err)
		}
		rowsOnly, _ := store.NewMetadataRepositories(tx, database, a, 1, 10000)
		if got, err := rowsOnly.History.ListByTenant(ctx, a); !errors.Is(err, store.ErrSnapshotLimit) || got != nil {
			t.Fatalf("history row limit %v %v", got, err)
		}
		if got, err := rowsOnly.Revisions.ListByTenant(ctx, a); !errors.Is(err, store.ErrSnapshotLimit) || got != nil {
			t.Fatalf("revision row limit %v %v", got, err)
		}
		for _, payload := range [][]byte{nil, []byte(`{`), {0xff}} {
			v := event
			v.ID = uuid.New()
			v.Details = payload
			if err := r.History.Create(ctx, v); err == nil {
				t.Fatal("invalid JSON accepted")
			}
		}
	})
	t.Run("event_byte_boundaries", func(t *testing.T) {
		tx, r := begin(t, a, 10, 10000)
		inputBytes := len(event.Operation) + len(event.ResourceType) + len(event.Status) + len(event.Details)
		tooSmall, _ := store.NewMetadataRepositories(tx, database, a, 10, inputBytes-1)
		if err := tooSmall.History.Create(ctx, event); !errors.Is(err, store.ErrSnapshotLimit) {
			t.Fatalf("JSON input boundary %v", err)
		}
		exactInput, _ := store.NewMetadataRepositories(tx, database, a, 10, inputBytes)
		if err := exactInput.History.Create(ctx, event); err != nil {
			t.Fatal(err)
		}
		copyEvent := event
		copyEvent.ID = uuid.New()
		if err := r.History.Create(ctx, copyEvent); err != nil {
			t.Fatal(err)
		}
		var storedBytes int
		if err := tx.QueryRowContext(ctx, "SELECT OCTET_LENGTH(operation)+OCTET_LENGTH(resource_type)+OCTET_LENGTH(status)+OCTET_LENGTH(CAST(details AS CHAR CHARACTER SET utf8mb4)) FROM data_model_schema_change_log WHERE id=?", mysqlstore.UUID(event.ID)).Scan(&storedBytes); err != nil {
			t.Fatal(err)
		}
		exactRead, _ := store.NewMetadataRepositories(tx, database, a, 10, storedBytes)
		if got, next, err := exactRead.History.ListPage(ctx, 1, nil); err != nil || len(got) != 1 || next == nil {
			t.Fatalf("exact stored boundary %v %v %v", got, next, err)
		}
		tooSmall, _ = store.NewMetadataRepositories(tx, database, a, 10, storedBytes-1)
		if got, next, err := tooSmall.History.ListPage(ctx, 1, nil); !errors.Is(err, store.ErrSnapshotLimit) || got != nil || next != nil {
			t.Fatalf("stored boundary %v %v %v", got, next, err)
		}
		if got, next, err := exactRead.History.ListPage(ctx, 2, nil); !errors.Is(err, store.ErrSnapshotLimit) || got != nil || next != nil {
			t.Fatalf("cumulative page limit %v %v %v", got, next, err)
		}
	})
	t.Run("audit_failure_rolls_back_owner_and_revision", func(t *testing.T) {
		table := datamodel.Table{ID: uuid.New(), TenantID: a, Name: "rollback_history", CreatedAt: at, UpdatedAt: at}
		err := mysqlstore.Mutation(ctx, db, func(tx *sql.Tx) error {
			r, err := store.NewMetadataRepositories(tx, database, a, 10, 10000)
			if err != nil {
				return err
			}
			if err := r.Tables.Create(ctx, table); err != nil {
				return err
			}
			if err := r.Revisions.Create(ctx, revision); err != nil {
				return err
			}
			if err := r.History.Create(ctx, event); err != nil {
				return err
			}
			return r.History.Create(ctx, event)
		})
		mysqlError(t, err, 1062)
		_, read := begin(t, a, 10, 10000)
		if _, err := read.Tables.GetByID(ctx, table.ID); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("owner survived %v", err)
		}
		if got, err := read.History.ListByTenant(ctx, a); err != nil || len(got) != 0 {
			t.Fatalf("history survived %v %v", got, err)
		}
		if got, err := read.Revisions.ListByTenant(ctx, a); err != nil || len(got) != 0 {
			t.Fatalf("revision survived %v %v", got, err)
		}
	})
	t.Run("revision_failure_rolls_back_owner_and_audit", func(t *testing.T) {
		table := datamodel.Table{ID: uuid.New(), TenantID: a, Name: "rollback_revision", CreatedAt: at, UpdatedAt: at}
		err := mysqlstore.Mutation(ctx, db, func(tx *sql.Tx) error {
			r, err := store.NewMetadataRepositories(tx, database, a, 10, 10000)
			if err != nil {
				return err
			}
			if err := r.Tables.Create(ctx, table); err != nil {
				return err
			}
			if err := r.History.Create(ctx, event); err != nil {
				return err
			}
			if err := r.Revisions.Create(ctx, revision); err != nil {
				return err
			}
			return r.Revisions.Create(ctx, revision)
		})
		mysqlError(t, err, 1062)
		_, read := begin(t, a, 10, 10000)
		if _, err := read.Tables.GetByID(ctx, table.ID); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("owner survived %v", err)
		}
		if got, err := read.History.ListByTenant(ctx, a); err != nil || len(got) != 0 {
			t.Fatalf("history survived %v %v", got, err)
		}
		if got, err := read.Revisions.ListByTenant(ctx, a); err != nil || len(got) != 0 {
			t.Fatalf("revision survived %v %v", got, err)
		}
	})
}
