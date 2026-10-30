package mysql

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
)

func boundTestProjection() *RecordProjection {
	return &RecordProjection{table: "`db`.`records`", list: "`safe`", fields: []projectionField{{definition: ingestion.FieldSchema{Name: "s", DataType: "string", Nullable: true}, width: 1}}, columns: 1}
}
func boundTestLimits() RecordResultLimits {
	return RecordResultLimits{FieldBytes: 8, TransferBytes: 128, RowBytes: 64, PageBytes: 128, Rows: 2}
}
func TestRecordBoundsConfiguration(t *testing.T) {
	p := boundTestProjection()
	good := boundTestLimits()
	for _, change := range []func(*RecordResultLimits){func(l *RecordResultLimits) { l.FieldBytes = 0 }, func(l *RecordResultLimits) { l.FieldBytes = 1<<20 + 1 }, func(l *RecordResultLimits) { l.TransferBytes = 71 }, func(l *RecordResultLimits) { l.RowBytes = 2 }, func(l *RecordResultLimits) { l.PageBytes = 63 }, func(l *RecordResultLimits) { l.Rows = 1001 }} {
		limits := good
		change(&limits)
		if b, err := p.Bounded(limits); err == nil || b != nil {
			t.Fatal(limits, b, err)
		}
	}
	b, err := p.Bounded(good)
	if err != nil {
		t.Fatal(err)
	}
	sql, _, args := b.SQL()
	if strings.Contains(sql, "*") || !strings.Contains(sql, "CASE WHEN OCTET_LENGTH(`safe`)<=?") || !reflect.DeepEqual(args, []any{8}) {
		t.Fatal(sql, args)
	}
	args[0] = 999
	_, _, again := b.SQL()
	if again[0] != 8 {
		t.Fatal("mutable arguments")
	}
}
func TestRecordBoundsDecode(t *testing.T) {
	b, err := boundTestProjection().Bounded(boundTestLimits())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		raw  []any
		want any
		bad  error
	}{
		{[]any{int64(8), []byte("12345678")}, "12345678", nil},
		{[]any{int64(0), []byte{}}, "", nil},
		{[]any{nil, nil}, nil, nil},
		{[]any{int64(9), nil}, nil, ErrRecordResultLimit},
		{[]any{int64(2), []byte("x")}, nil, ErrRecordDecode},
		{[]any{nil, "x"}, nil, ErrRecordDecode},
		{[]any{int64(-1), nil}, nil, ErrRecordDecode},
	} {
		got, _, err := b.decode(tc.raw)
		if !errors.Is(err, tc.bad) {
			t.Fatal(got, err)
		}
		if tc.bad != nil {
			if got != nil {
				t.Fatal("partial row", got)
			}
		} else if got["s"] != tc.want {
			t.Fatal(got, tc.want)
		}
	}
	limits := boundTestLimits()
	limits.RowBytes = 59
	tight, err := boundTestProjection().Bounded(limits)
	if err != nil {
		t.Fatal(err)
	}
	if row, _, err := tight.decode([]any{int64(8), []byte("12345678")}); !errors.Is(err, ErrRecordResultLimit) || row != nil {
		t.Fatal(row, err)
	}
}
func boundedFixture(t *testing.T) (recordFixtureState, *sql.Tx, *RecordProjection) {
	t.Helper()
	f := recordFixture(t)
	ctx := context.Background()
	err := mysqlstore.Mutation(ctx, f.db, func(tx *sql.Tx) error {
		writer, err := NewRecordWriter(ctx, tx, f.database, f.model.TenantID, f.model, "records", f.version)
		if err != nil {
			return err
		}
		_, err = writer.WriteBatch(ctx, []map[string]any{{"object_id": "a"}, {"object_id": "bb"}, {"object_id": strings.Repeat("x", 4096)}}, ingestion.ModePatch, time.Now().UTC().Truncate(time.Microsecond), 8192)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	tx, err := f.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { tx.Rollback() })
	p, err := NewRecordProjection(ctx, tx, f.database, f.model.TenantID, f.model, "records", f.version, []string{"object_id"})
	if err != nil {
		t.Fatal(err)
	}
	return f, tx, p
}
func TestRecordBoundsSQLTransferGuard(t *testing.T) {
	f, tx, p := boundedFixture(t)
	limits := boundTestLimits()
	limits.RowBytes = 256
	limits.PageBytes = 512
	b, err := p.Bounded(limits)
	if err != nil {
		t.Fatal(err)
	}
	cols, table, args := b.SQL()
	column := "`" + f.columns["object_id"]["value"] + "`"
	var size int64
	var value any
	if err := tx.QueryRow("SELECT "+cols+" FROM "+table+" WHERE OCTET_LENGTH("+column+")>100 LIMIT 1", args...).Scan(&size, &value); err != nil {
		t.Fatal(err)
	}
	if size != 4096 || value != nil {
		t.Fatal("oversized data transferred", size, value)
	}
	rows, err := tx.Query("SELECT "+cols+" FROM "+table+" ORDER BY OCTET_LENGTH("+column+") LIMIT 3", args...)
	if err != nil {
		t.Fatal(err)
	}
	if items, err := b.Collect(rows); !errors.Is(err, ErrRecordResultLimit) || items != nil {
		t.Fatal(items, err)
	}
}
func TestRecordBoundsPageCollection(t *testing.T) {
	f, tx, p := boundedFixture(t)
	column := "`" + f.columns["object_id"]["value"] + "`"
	// Accounting: first object is 68 bytes, second 74; array brackets/comma = 3.
	for _, tc := range []struct {
		name             string
		row, page, count int
		bad              bool
	}{{"exact", 74, 145, 2, false}, {"page_over", 74, 144, 2, true}, {"row_over", 73, 200, 2, true}, {"row_count", 74, 200, 1, true}} {
		t.Run(tc.name, func(t *testing.T) {
			limits := boundTestLimits()
			limits.RowBytes = tc.row
			limits.PageBytes = tc.page
			limits.Rows = tc.count
			b, err := p.Bounded(limits)
			if err != nil {
				t.Fatal(err)
			}
			cols, table, args := b.SQL()
			rows, err := tx.Query("SELECT "+cols+" FROM "+table+" WHERE OCTET_LENGTH("+column+")<3 ORDER BY OCTET_LENGTH("+column+") LIMIT 3", args...)
			if err != nil {
				t.Fatal(err)
			}
			items, err := b.Collect(rows)
			if (err != nil) != tc.bad {
				t.Fatal(items, err)
			}
			if tc.bad && items != nil {
				t.Fatal("partial page", items)
			}
			if !tc.bad && len(items) != 2 {
				t.Fatal(items)
			}
		})
	}
}
func TestRecordBoundsRestrictedAndCancellation(t *testing.T) {
	f, tx, p := boundedFixture(t)
	defer tx.Rollback()
	pool := restrictedReceiptPool(t, f.db, f.database)
	var login string
	if err := pool.QueryRow("SELECT CURRENT_USER()").Scan(&login); err != nil {
		t.Fatal(err)
	}
	account := "'" + strings.Split(login, "@")[0] + "'@'%'"
	if _, err := f.db.Exec("REVOKE ALL PRIVILEGES, GRANT OPTION FROM " + account); err != nil {
		t.Fatal(err)
	}
	column := "`" + f.columns["object_id"]["value"] + "`"
	if _, err := f.db.Exec("GRANT SELECT(" + column + ") ON " + f.qualified + " TO " + account); err != nil {
		t.Fatal(err)
	}
	limits := boundTestLimits()
	limits.RowBytes = 256
	limits.PageBytes = 512
	b, err := p.Bounded(limits)
	if err != nil {
		t.Fatal(err)
	}
	cols, table, args := b.SQL()
	rows, err := pool.Query("SELECT "+cols+" FROM "+table+" WHERE OCTET_LENGTH("+column+")<3 LIMIT 2", args...)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := b.Collect(rows); err != nil || len(got) != 2 {
		t.Fatal(got, err)
	}
	forbidden, err := pool.Query("SELECT id FROM " + table)
	if err == nil {
		forbidden.Close()
		t.Fatal("unprojected access granted")
	}
	badRows, err := pool.Query("SELECT 1")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := b.Collect(badRows); err == nil || got != nil {
		t.Fatal("scan mismatch", got, err)
	}
	if pool.Stats().InUse != 0 {
		t.Fatal("failed scan retained connection")
	}
	ctx, cancel := context.WithCancel(context.Background())
	rows, err = pool.QueryContext(ctx, "SELECT "+cols+" FROM "+table+" LIMIT 3", args...)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	// database/sql closes canceled rows asynchronously; wait boundedly for its Err.
	deadline := time.Now().Add(time.Second)
	for rows.Err() == nil && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got, err := b.Collect(rows); err == nil || got != nil {
		t.Fatal("partial canceled page", got, err)
	}
	if pool.Stats().InUse != 0 {
		t.Fatal("rows retained connection", pool.Stats())
	}
}
