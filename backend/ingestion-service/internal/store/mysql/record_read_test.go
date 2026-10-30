package mysql

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func exactLimits() RecordResultLimits {
	return RecordResultLimits{FieldBytes: 128, TransferBytes: 1024, RowBytes: 2048, PageBytes: 4096, Rows: 1}
}
func exactRead(t *testing.T, f recordFixtureState, id string, limits RecordResultLimits) (map[string]any, error) {
	t.Helper()
	tx, err := f.db.BeginTx(t.Context(), &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	return ReadExactRecord(t.Context(), tx, f.database, f.model.TenantID, f.model, "records", f.version, []string{"object_id"}, id, limits)
}
func TestExactReadInvalidIdentity(t *testing.T) {
	for _, id := range []string{"", strings.Repeat("x", 1<<20+1), string([]byte{255})} {
		got, err := ReadExactRecord(t.Context(), &sql.Tx{}, "db", uuid.New(), ingestion.PublishedDataModel{}, "records", uuid.New(), nil, id, exactLimits())
		if got != nil || !errors.Is(err, ErrRecordLookup) {
			t.Fatal(got, err)
		}
	}
}
func TestExactReadByteIdentity(t *testing.T) {
	f := recordFixture(t)
	keys := []string{"a", "A", "a ", "a\x00", "\u00e9", "e\u0301", "x' OR 1=1 --"}
	err := mysqlstore.Mutation(t.Context(), f.db, func(tx *sql.Tx) error {
		w, err := NewRecordWriter(t.Context(), tx, f.database, f.model.TenantID, f.model, "records", f.version)
		if err != nil {
			return err
		}
		var records []map[string]any
		for _, key := range keys {
			records = append(records, map[string]any{"object_id": key})
		}
		_, err = w.WriteBatch(t.Context(), records, ingestion.ModeCreate, time.Now().UTC().Truncate(time.Microsecond), 8192)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range keys {
		got, err := exactRead(t, f, key, exactLimits())
		if err != nil || got["object_id"] != key || len(got) != 1 {
			t.Fatal(key, got, err)
		}
	}
	got, err := exactRead(t, f, "absent", exactLimits())
	if got != nil || !errors.Is(err, sql.ErrNoRows) {
		t.Fatal(got, err)
	}
}
func TestExactReadScopeAndVersion(t *testing.T) {
	f, tx, _ := boundedFixture(t)
	for _, tc := range []struct{ tenant, version uuid.UUID }{{uuid.New(), f.version}, {f.model.TenantID, uuid.New()}} {
		got, err := ReadExactRecord(t.Context(), tx, f.database, tc.tenant, f.model, "records", tc.version, []string{"object_id"}, "a", exactLimits())
		if got != nil || err == nil || errors.Is(err, sql.ErrNoRows) {
			t.Fatal(got, err)
		}
	}
	model := f.model
	model.Tables = map[string]ingestion.ObjectSchema{"records": f.model.Tables["records"]}
	table := model.Tables["records"]
	table.ID = uuid.New()
	model.Tables["records"] = table
	got, err := ReadExactRecord(t.Context(), tx, f.database, model.TenantID, model, "records", f.version, []string{"object_id"}, "a", exactLimits())
	if got != nil || err == nil || errors.Is(err, sql.ErrNoRows) {
		t.Fatal(got, err)
	}
}
func TestExactReadBounds(t *testing.T) {
	f, _, _ := boundedFixture(t)
	limits := exactLimits()
	limits.FieldBytes = 1
	got, err := exactRead(t, f, "a", limits)
	if err != nil || got["object_id"] != "a" {
		t.Fatal(got, err)
	}
	for _, key := range []string{"bb", strings.Repeat("x", 4096)} {
		got, err = exactRead(t, f, key, limits)
		if got != nil || !errors.Is(err, ErrRecordResultLimit) {
			t.Fatal(got, err)
		}
	}
	limits = exactLimits()
	limits.RowBytes = 67
	got, err = exactRead(t, f, "a", limits)
	if got != nil || !errors.Is(err, ErrRecordResultLimit) {
		t.Fatal(got, err)
	}
}
func TestExactReadFailuresAreNotMissing(t *testing.T) {
	f, tx, _ := boundedFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	got, err := ReadExactRecord(ctx, tx, f.database, f.model.TenantID, f.model, "records", f.version, []string{"object_id"}, "a", exactLimits())
	if got != nil || !errors.Is(err, context.Canceled) {
		t.Fatal(got, err)
	}
	schema, _ := mysqlstore.Identifier(f.database)
	if _, err = f.db.Exec("DROP FUNCTION " + schema + ".data_model_find_record_object_key_v1"); err != nil {
		t.Fatal(err)
	}
	got, err = exactRead(t, f, "a", exactLimits())
	if got != nil || err == nil || errors.Is(err, sql.ErrNoRows) {
		t.Fatal(got, err)
	}
}
