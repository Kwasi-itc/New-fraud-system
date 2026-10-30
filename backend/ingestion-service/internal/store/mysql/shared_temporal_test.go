package mysql

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
)

// The PostgreSQL source has no implicit as-of/valid_until predicate and no SQL
// GROUP BY/window API. Keep caller-supplied timestamp filters explicit.
func TestFullSharedCurrentAndTimeWindowSemantics(t *testing.T) {
	shared, f := fullRecordFixture(t)
	ctx := WithServiceCredential(context.Background(), "fixture-token")
	base := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	for i, id := range []string{"before", "boundary", "after"} {
		if _, e := f.write(ctx, []map[string]any{{"object_id": id}}); e != nil {
			t.Fatal(e)
		}
		fixtureExec(t, f.db, "UPDATE "+f.qualified+" SET `"+f.columns["updated_at"]["value"]+"`=?,valid_from=?,valid_until=? WHERE `"+f.columns["object_id"]["value"]+"`=?", mysqlstore.DateTime{Time: base.Add(time.Duration(i) * time.Microsecond)}, mysqlstore.DateTime{Time: base.Add(-time.Hour)}, mysqlstore.DateTime{Time: base.Add(-time.Minute)}, id)
	}
	store, e := NewIngestionContractStore(f.db, shared.Database, 1<<20)
	if e != nil {
		t.Fatal(e)
	}
	limits := RecordQueryLimits{100, 1 << 20, 1 << 20}
	e = store.WithRecordRead(ctx, f.model.TenantID, "records", "fixture-token", []string{"object_id", "updated_at"}, func(ctx context.Context, tx *sql.Tx, p *RecordProjection) error {
		count, e := p.CountRecords(ctx, tx, "object_id", nil, limits)
		if e != nil || count != 3 {
			t.Fatal("unexpected implicit validity filter", count, e)
		}
		for _, tc := range []struct {
			op    string
			count int64
		}{{"eq", 1}, {"neq", 2}, {"gt", 1}, {"gte", 2}, {"lt", 1}, {"lte", 2}} {
			filter := filterLeaf("updated_at", tc.op, base.Add(time.Microsecond))
			count, e := p.CountRecords(ctx, tx, "object_id", &filter, limits)
			if e != nil || count != tc.count {
				t.Fatal(tc, count, e)
			}
		}
		for _, end := range []string{"lt", "lte"} {
			filter := filterGroup("and", filterLeaf("updated_at", "gte", base.Add(time.Microsecond)), filterLeaf("updated_at", end, base.Add(2*time.Microsecond)))
			want := int64(1)
			if end == "lte" {
				want = 2
			}
			count, e := p.CountRecords(ctx, tx, "object_id", &filter, limits)
			if e != nil || count != want {
				t.Fatal(end, count, e)
			}
		}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
}
