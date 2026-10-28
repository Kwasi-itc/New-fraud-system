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
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
	store "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func TestJobRepository(t *testing.T) {
	db, database := metadataDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	at := time.Now().UTC().Truncate(time.Microsecond)
	a, b, table := uuid.New(), uuid.New(), uuid.New()
	begin := func(t *testing.T, id uuid.UUID) (*sql.Tx, store.MetadataRepositories) {
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
		r, err := store.NewMetadataRepositories(tx, database, id, 100, 10000)
		if err != nil {
			t.Fatal(err)
		}
		return tx, r
	}
	for _, id := range []uuid.UUID{a, b} {
		tx, r := begin(t, id)
		if err := r.Tenants.Create(ctx, tenant.New(id, at, "tenant", nil)); err != nil {
			t.Fatal(err)
		}
		if id == a {
			if err := r.Tables.Create(ctx, datamodel.Table{ID: table, TenantID: a, Name: "records", CreatedAt: at, UpdatedAt: at}); err != nil {
				t.Fatal(err)
			}
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	makeJob := func(key string) datamodel.IndexJob {
		return datamodel.IndexJob{ID: uuid.New(), TenantID: a, TableID: &table, TableName: "records", IndexType: "search", Columns: []string{"one", "two", "one"}, Status: "pending", RequestedByOperation: "create", RequestedAt: at, DedupeKey: key}
	}
	seed := makeJob("seed")
	tx, r := begin(t, a)
	if err := r.Jobs.Create(ctx, seed); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	t.Run("round_trip_and_state_transitions", func(t *testing.T) {
		_, r := begin(t, a)
		got, err := r.Jobs.GetByID(ctx, seed.ID)
		if err != nil || !reflect.DeepEqual(got, seed) {
			t.Fatalf("round trip %+v %v", got, err)
		}
		got, err = r.Jobs.StartAttempt(ctx, seed.ID, at)
		if err != nil || got.AttemptCount != 1 || got.Status != "running" || !got.StartedAt.Equal(at) {
			t.Fatalf("claim %+v %v", got, err)
		}
		if err := r.Jobs.MarkFailed(ctx, seed.ID, "failure", at); err != nil {
			t.Fatal(err)
		}
		got, err = r.Jobs.GetByID(ctx, seed.ID)
		if err != nil || got.Status != "failed" || got.ErrorMessage == nil || *got.ErrorMessage != "failure" || got.CompletedAt == nil {
			t.Fatalf("failed %+v %v", got, err)
		}
		if err := r.Jobs.MarkPendingRetry(ctx, seed.ID, ""); err != nil {
			t.Fatal(err)
		}
		got, err = r.Jobs.GetByID(ctx, seed.ID)
		if err != nil || got.CompletedAt != nil || got.ErrorMessage == nil || *got.ErrorMessage != "" || got.StartedAt == nil {
			t.Fatalf("pending %+v %v", got, err)
		}
		if err := r.Jobs.Retry(ctx, seed.ID, at.Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
		got, err = r.Jobs.GetByID(ctx, seed.ID)
		if err != nil || got.AttemptCount != 1 || got.ErrorMessage != nil || got.StartedAt != nil || got.ScheduledAt == nil {
			t.Fatalf("retry %+v %v", got, err)
		}
		if err := r.Jobs.MarkApplied(ctx, seed.ID, at); err != nil {
			t.Fatal(err)
		}
		if err := r.Jobs.MarkApplied(ctx, seed.ID, at); err != nil {
			t.Fatal(err)
		}
		nullable := makeJob("")
		nullable.TableID = nil
		nullable.Columns = []string{}
		if err := r.Jobs.Create(ctx, nullable); err != nil {
			t.Fatal(err)
		}
		got, err = r.Jobs.GetByID(ctx, nullable.ID)
		if err != nil || !reflect.DeepEqual(got, nullable) {
			t.Fatalf("nullable %+v %v", got, err)
		}
	})
	t.Run("scope_and_missing_rows", func(t *testing.T) {
		_, r := begin(t, b)
		if _, err := r.Jobs.GetByID(ctx, seed.ID); !errors.Is(err, sql.ErrNoRows) {
			t.Fatal(err)
		}
		if got, err := r.Jobs.GetByDedupeKey(ctx, "seed"); err != nil || got != nil {
			t.Fatalf("foreign key %+v %v", got, err)
		}
		for _, fn := range []func() error{func() error { return r.Jobs.MarkApplied(ctx, seed.ID, at) }, func() error { return r.Jobs.MarkFailed(ctx, seed.ID, "bad", at) }, func() error { return r.Jobs.MarkPendingRetry(ctx, seed.ID, "bad") }, func() error { return r.Jobs.Retry(ctx, seed.ID, at) }} {
			if err := fn(); !errors.Is(err, sql.ErrNoRows) {
				t.Fatal(err)
			}
		}
		if _, err := r.Jobs.StartAttempt(ctx, seed.ID, at); !errors.Is(err, sql.ErrNoRows) {
			t.Fatal(err)
		}
		if err := r.Jobs.Create(ctx, seed); err == nil {
			t.Fatal("foreign tenant accepted")
		}
		v := makeJob("foreign-parent")
		v.TenantID = b
		if err := r.Jobs.Create(ctx, v); err == nil {
			t.Fatal("foreign table accepted")
		}
		if _, err := r.Jobs.ListByTenant(ctx, a); err == nil {
			t.Fatal("foreign list accepted")
		}
	})
	t.Run("exact_keys_empty_and_rollback", func(t *testing.T) {
		tx, r := begin(t, a)
		for _, key := range []string{"quote' ", "quote'", "大小", "", ""} {
			v := makeJob(key)
			if err := r.Jobs.Create(ctx, v); err != nil {
				t.Fatal(err)
			}
			got, err := r.Jobs.GetByDedupeKeyForUpdate(ctx, key)
			if err != nil || (key != "" && (got == nil || got.ID != v.ID)) || (key == "" && got != nil) {
				t.Fatalf("key %+v %v", got, err)
			}
		}
		if err := r.Jobs.MarkFailed(ctx, seed.ID, "rollback", at); err != nil {
			t.Fatal(err)
		}
		if err := r.Jobs.Create(ctx, makeJob("seed")); err == nil {
			t.Fatal("duplicate accepted")
		}
		if err := tx.Rollback(); err != nil {
			t.Fatal(err)
		}
		_, check := begin(t, a)
		got, err := check.Jobs.GetByID(ctx, seed.ID)
		if err != nil || !reflect.DeepEqual(got, seed) {
			t.Fatalf("rollback %+v %v", got, err)
		}
		if got, err := check.Jobs.GetByDedupeKey(ctx, "quote'"); err != nil || got != nil {
			t.Fatalf("rollback insert %+v %v", got, err)
		}
	})
	t.Run("bounds_order_corrupt_json_and_exhaustion", func(t *testing.T) {
		tx, r := begin(t, a)
		for i := 0; i < 2; i++ {
			if err := r.Jobs.Create(ctx, makeJob("")); err != nil {
				t.Fatal(err)
			}
		}
		jobs, err := r.Jobs.ListByTenant(ctx, a)
		if err != nil || len(jobs) != 3 {
			t.Fatalf("list %v %v", jobs, err)
		}
		for i := 1; i < len(jobs); i++ {
			if strings.Compare(jobs[i-1].ID.String(), jobs[i].ID.String()) <= 0 {
				t.Fatal("unstable equal-time ordering")
			}
		}
		small, _ := store.NewMetadataRepositories(tx, database, a, 1, 10000)
		if got, err := small.Jobs.ListByTenant(ctx, a); !errors.Is(err, store.ErrSnapshotLimit) || got != nil {
			t.Fatalf("rows %v %v", got, err)
		}
		tiny, _ := store.NewMetadataRepositories(tx, database, a, 100, 1)
		if got, err := tiny.Jobs.GetByID(ctx, seed.ID); !errors.Is(err, store.ErrSnapshotLimit) || got.ID != uuid.Nil {
			t.Fatalf("bytes %v %v", got, err)
		}
		if err := tiny.Jobs.Create(ctx, makeJob("")); !errors.Is(err, store.ErrSnapshotLimit) {
			t.Fatal(err)
		}
		if err := small.Jobs.Create(ctx, makeJob("")); !errors.Is(err, store.ErrSnapshotLimit) {
			t.Fatal(err)
		}
		invalid := makeJob("")
		invalid.Columns = nil
		if err := r.Jobs.Create(ctx, invalid); err == nil {
			t.Fatal("nil columns accepted")
		}
		invalid.Columns = []string{"bad\x00"}
		if err := r.Jobs.Create(ctx, invalid); err == nil {
			t.Fatal("NUL accepted")
		}
		invalid.Columns = []string{}
		invalid.AttemptCount = 2147483647
		if err := r.Jobs.Create(ctx, invalid); err != nil {
			t.Fatal(err)
		}
		if _, err := r.Jobs.StartAttempt(ctx, invalid.ID, at); err == nil {
			t.Fatal("overflow accepted")
		}
		corrupt := uuid.New()
		if _, err := tx.ExecContext(ctx, "INSERT INTO data_model_index_jobs(id,tenant_id,table_name,index_type,columns_json,status,requested_at) VALUES (?,?,'records','search','[null]','pending',?)", mysqlstore.UUID(corrupt), mysqlstore.UUID(a), mysqlstore.DateTime{Time: at}); err != nil {
			t.Fatal(err)
		}
		if _, err := r.Jobs.GetByID(ctx, corrupt); err == nil {
			t.Fatal("null column accepted")
		}
	})
	t.Run("concurrent_duplicate_creation", func(t *testing.T) {
		start := make(chan struct{})
		results := make(chan error, 2)
		for i := 0; i < 2; i++ {
			go func() {
				<-start
				results <- mysqlstore.Mutation(ctx, db, func(tx *sql.Tx) error {
					r, err := store.NewMetadataRepositories(tx, database, a, 100, 10000)
					if err != nil {
						return err
					}
					return r.Jobs.Create(ctx, makeJob("concurrent"))
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
	})
	t.Run("exact_row_lock_and_cancelled_wait", func(t *testing.T) {
		tx, r := begin(t, a)
		if _, err := r.Jobs.GetByDedupeKeyForUpdate(ctx, "seed"); err != nil {
			t.Fatal(err)
		}
		other, rr := begin(t, a)
		// An unrelated row remains writable while the seed job is locked.
		if err := rr.Jobs.Create(ctx, makeJob("independent")); err != nil {
			t.Fatal(err)
		}
		wait, cancel := context.WithTimeout(ctx, 150*time.Millisecond)
		defer cancel()
		if _, err := rr.Jobs.GetByDedupeKeyForUpdate(wait, "seed"); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected cancelled lock wait: %v", err)
		}
		// Cancellation can discard the connection; rollback can report that same
		// deadline instead of confirmation. Verify durable state independently.
		if err := other.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) && !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal(err)
		}
		if err := tx.Rollback(); err != nil {
			t.Fatal(err)
		}
		_, check := begin(t, a)
		if got, err := check.Jobs.GetByDedupeKey(ctx, "independent"); err != nil || got != nil {
			t.Fatalf("cancelled transaction leaked write: %+v %v", got, err)
		}
	})
	t.Run("locked_lookup_sees_new_key_after_old_snapshot", func(t *testing.T) {
		tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		r, err := store.NewMetadataRepositories(tx, database, a, 100, 10000)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := r.Jobs.ListByTenant(ctx, a); err != nil {
			t.Fatal(err)
		}
		writer, wr := begin(t, a)
		v := makeJob("after-snapshot")
		if err := wr.Jobs.Create(ctx, v); err != nil {
			t.Fatal(err)
		}
		if err := writer.Commit(); err != nil {
			t.Fatal(err)
		}
		got, err := r.Jobs.GetByDedupeKeyForUpdate(ctx, v.DedupeKey)
		if err != nil || got == nil || got.ID != v.ID {
			t.Fatalf("stale locked lookup: %+v %v", got, err)
		}
	})
	t.Run("byte_boundaries_and_audit_rollback", func(t *testing.T) {
		tx, r := begin(t, a)
		var size int
		if err := tx.QueryRowContext(ctx, "SELECT OCTET_LENGTH(table_name)+OCTET_LENGTH(index_type)+OCTET_LENGTH(CAST(columns_json AS CHAR CHARACTER SET utf8mb4))+OCTET_LENGTH(status)+OCTET_LENGTH(requested_by_operation)+COALESCE(OCTET_LENGTH(error_message),0)+OCTET_LENGTH(dedupe_key) FROM data_model_index_jobs WHERE id=?", mysqlstore.UUID(seed.ID)).Scan(&size); err != nil {
			t.Fatal(err)
		}
		exact, _ := store.NewMetadataRepositories(tx, database, a, 100, size)
		if _, err := exact.Jobs.GetByID(ctx, seed.ID); err != nil {
			t.Fatal(err)
		}
		small, _ := store.NewMetadataRepositories(tx, database, a, 100, size-1)
		if _, err := small.Jobs.GetByID(ctx, seed.ID); !errors.Is(err, store.ErrSnapshotLimit) {
			t.Fatal(err)
		}
		if got, err := exact.Jobs.ListByTenant(ctx, a); !errors.Is(err, store.ErrSnapshotLimit) || got != nil {
			t.Fatalf("aggregate bytes: %+v %v", got, err)
		}
		if err := r.Jobs.Retry(ctx, seed.ID, at); err != nil {
			t.Fatal(err)
		}
		change := datamodel.SchemaChange{ID: uuid.New(), TenantID: a, ResourceID: seed.ID, Operation: "retry", ResourceType: "index_job", Status: "pending", Details: []byte(`{}`), CreatedAt: at}
		if err := r.History.Create(ctx, change); err != nil {
			t.Fatal(err)
		}
		if err := r.History.Create(ctx, change); err == nil {
			t.Fatal("duplicate audit accepted")
		}
		if err := tx.Rollback(); err != nil {
			t.Fatal(err)
		}
		_, check := begin(t, a)
		got, err := check.Jobs.GetByID(ctx, seed.ID)
		if err != nil || !reflect.DeepEqual(got, seed) {
			t.Fatalf("audit rollback: %+v %v", got, err)
		}
	})
}

func TestJobLookupDigestCollision(t *testing.T) {
	db, database := metadataDB(t)
	ctx := context.Background()
	at := time.Now().UTC().Truncate(time.Microsecond)
	id := uuid.New()
	// Test-only generated-column fault in a fresh disposable schema. Force every
	// nonempty key into the requested absent key's bucket to prove full equality.
	if _, err := db.ExecContext(ctx, "ALTER TABLE data_model_index_jobs MODIFY dedupe_lookup_hash BINARY(32) GENERATED ALWAYS AS (UNHEX(SHA2('absent',256))) STORED"); err != nil {
		t.Fatal(err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	r, err := store.NewMetadataRepositories(tx, database, id, 100, 10000)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Tenants.Create(ctx, tenant.New(id, at, "tenant", nil)); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"collision-a", "collision-b"} {
		if err := r.Jobs.Create(ctx, datamodel.IndexJob{ID: uuid.New(), TenantID: id, TableName: "records", IndexType: "search", Columns: []string{}, Status: "pending", RequestedAt: at, DedupeKey: key}); err != nil {
			t.Fatal(err)
		}
	}
	for _, locked := range []bool{false, true} {
		var got *datamodel.IndexJob
		if locked {
			got, err = r.Jobs.GetByDedupeKeyForUpdate(ctx, "absent")
		} else {
			got, err = r.Jobs.GetByDedupeKey(ctx, "absent")
		}
		if err != nil || got != nil {
			t.Fatalf("digest treated as equality: %+v %v", got, err)
		}
	}
}
