package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/netip"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/store/postgres"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Both databases are disposable fixtures. The source endpoint is deliberately
// fixed; a production or arbitrary connection string is never accepted here.
func paritySource(t *testing.T, model ingestion.PublishedDataModel) (*pgx.Conn, ingestion.PublishedDataModel) {
	t.Helper()
	if os.Getenv("INGESTION_REQUIRE_PARITY") != "1" {
		t.Skip("explicit isolated dual-database parity suite required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, err := pgx.Connect(ctx, "postgres://migration_test:isolated-test-only@ingestion-parity-postgres-1:5432/ingestion_test_baseline?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	schema := "parity_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err = c.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		c.Close(context.Background())
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		if _, err := c.Exec(cleanup, "DROP SCHEMA "+quoted+" CASCADE"); err != nil {
			t.Error(err)
		}
		if err := c.Close(cleanup); err != nil {
			t.Error(err)
		}
	})
	ddl := "CREATE TABLE " + quoted + `.records (id uuid PRIMARY KEY, object_id text COLLATE "C" UNIQUE NOT NULL, updated_at timestamptz NOT NULL, number integer UNIQUE, amount double precision, flag boolean, ip inet)`
	if _, err = c.Exec(ctx, ddl); err != nil {
		t.Fatal(err)
	}
	model.SchemaName = schema
	model.RecordLookupField = "object_id"
	return c, model
}

type parityFixture struct {
	f     recordFixtureState
	pg    *pgx.Conn
	model ingestion.PublishedDataModel
	tx    *sql.Tx
	p     *RecordProjection
}

func liveParityFixture(t *testing.T) parityFixture {
	t.Helper()
	if os.Getenv("INGESTION_REQUIRE_PARITY") != "1" {
		t.Skip("explicit isolated dual-database parity suite required")
	}
	f := recordFixture(t)
	c, model := paritySource(t, f.model)
	at := time.Date(2026, 10, 30, 12, 0, 0, 123456000, time.UTC)
	records := []map[string]any{
		{"object_id": "a", "number": int64(-1), "amount": -0.5, "flag": false, "ip": "192.0.2.1"},
		{"object_id": "b", "number": int64(0), "amount": 0.0, "flag": true, "ip": "2001:db8::1"},
		{"object_id": "c", "number": int64(1), "amount": 0.5, "flag": true, "ip": "192.0.2.1"},
		{"object_id": "n"},
	}
	err := mysqlstore.Mutation(t.Context(), f.db, func(tx *sql.Tx) error {
		w, err := NewRecordWriter(t.Context(), tx, f.database, f.model.TenantID, f.model, "records", f.version)
		if err != nil {
			return err
		}
		for i, r := range records {
			instant := at.Add(time.Duration(i) * time.Microsecond)
			if _, err = w.WriteBatch(t.Context(), []map[string]any{r}, ingestion.ModeCreate, instant, 4096); err != nil {
				return err
			}
			if _, err = postgres.NewTenantDataWriter(c).UpsertRecord(t.Context(), model, "records", r, ingestion.ModeCreate, instant); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	tx, err := f.db.BeginTx(t.Context(), &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { tx.Rollback() })
	p, err := NewRecordProjection(t.Context(), tx, f.database, f.model.TenantID, f.model, "records", f.version, []string{"object_id", "updated_at", "number", "amount", "flag", "ip"})
	if err != nil {
		t.Fatal(err)
	}
	return parityFixture{f, c, model, tx, p}
}

func parityValue(t *testing.T, v any) string {
	t.Helper()
	if d, ok := v.(RecordDecimal); ok {
		v = json.Number(d)
	}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func (f parityFixture) compareCount(t *testing.T, filter *ingestion.AggregateFilter) {
	t.Helper()
	q := ingestion.AggregateQuery{ObjectType: "records", Aggregate: "count", Field: "object_id", Filter: filter}
	source, err := postgres.NewTenantDataReader(f.pg).AggregateRecords(t.Context(), f.model, q)
	if err != nil {
		t.Fatal("source", err)
	}
	normalized, err := f.p.NormalizeReadFilter(filter)
	if err != nil {
		t.Fatal("normalization", err)
	}
	target, err := f.p.CountRecords(t.Context(), f.tx, q.Field, normalized, queryLimits())
	if err != nil {
		t.Fatal("target", err)
	}
	if parityValue(t, source) != parityValue(t, target) {
		t.Fatalf("source=%v target=%v", source, target)
	}
}

func TestLiveParityScalarAndMembership(t *testing.T) {
	f := liveParityFixture(t)
	for field, value := range map[string]any{"number": int64(0), "amount": 0.0, "flag": true, "updated_at": time.Date(2026, 10, 30, 12, 0, 0, 123457000, time.UTC), "object_id": "b"} {
		for _, op := range []string{"eq", "neq", "gt", "gte", "lt", "lte", "is_null", "is_not_null"} {
			t.Run(field+"/"+op, func(t *testing.T) { q := filterLeaf(field, op, value); f.compareCount(t, &q) })
		}
		for _, values := range [][]any{{value}, {value, nil}, {nil}, {value, value}} {
			t.Run(field+"/in/"+fmt.Sprint(values), func(t *testing.T) { q := filterLeaf(field, "in", values); f.compareCount(t, &q) })
		}
	}
}

func TestLiveParityRawFilterInputs(t *testing.T) {
	f := liveParityFixture(t)
	for _, tc := range []struct {
		field string
		value any
	}{
		{"number", float64(0)}, {"number", "0"}, {"number", json.Number("0")}, {"number", nil},
		{"amount", "0.5"}, {"amount", json.Number("0.5")}, {"flag", "true"}, {"flag", false},
		{"updated_at", "2026-10-30T12:00:00.123457Z"}, {"object_id", "b"},
		{"number", "malformed"}, {"number", "2147483648"}, {"number", true}, {"flag", "malformed"}, {"updated_at", "not-a-time"},
	} {
		t.Run(fmt.Sprintf("%s/%T/%v", tc.field, tc.value, tc.value), func(t *testing.T) {
			filter := filterLeaf(tc.field, "eq", tc.value)
			source, se := postgres.NewTenantDataReader(f.pg).AggregateRecords(t.Context(), f.model, ingestion.AggregateQuery{ObjectType: "records", Aggregate: "count", Field: "object_id", Filter: &filter})
			normalized, te := f.p.NormalizeReadFilter(&filter)
			var target int64
			if te == nil {
				target, te = f.p.CountRecords(t.Context(), f.tx, "object_id", normalized, queryLimits())
			}
			if (se != nil) != (te != nil) {
				t.Fatalf("source=%v/%v target=%v/%v", source, se, target, te)
			}
			if se == nil && parityValue(t, source) != parityValue(t, target) {
				t.Fatal(source, target)
			}
		})
	}
}

func TestLiveParityLogicalFilters(t *testing.T) {
	f := liveParityFixture(t)
	a, b := filterLeaf("number", "gt", int64(0)), filterLeaf("flag", "eq", false)
	for i, q := range []ingestion.AggregateFilter{filterGroup("and", a, b), filterGroup("or", a, b), filterGroup("not", a), filterGroup("or"), filterGroup("and"), filterGroup("not", filterGroup("or")), filterGroup("and", filterGroup("or", a, b), filterGroup("not", filterLeaf("number", "is_null", nil)))} {
		t.Run(fmt.Sprint(i), func(t *testing.T) { f.compareCount(t, &q) })
	}
}

func TestLiveParitySupportedAggregates(t *testing.T) {
	f := liveParityFixture(t)
	// Independent oracle for the explicit four-row input, not another SQL query.
	oracle := map[string]map[string]any{
		"number":     {"count": 3.0, "sum": 0.0, "avg": 0.0, "min": -1.0, "max": 1.0, "count_distinct": 3.0},
		"amount":     {"count": 3.0, "sum": 0.0, "avg": 0.0, "min": -0.5, "max": 0.5, "count_distinct": 3.0},
		"flag":       {"count": 3.0, "count_distinct": 2.0},
		"updated_at": {"count": 4.0, "min": "2026-10-30T12:00:00.123456Z", "max": "2026-10-30T12:00:00.123459Z", "count_distinct": 4.0},
		"object_id":  {"count": 4.0, "min": "a", "max": "n", "count_distinct": 4.0},
	}
	for _, field := range []string{"number", "amount", "flag", "updated_at", "object_id"} {
		for _, op := range []string{"count", "sum", "avg", "min", "max", "count_distinct"} {
			if (op == "sum" || op == "avg") && field != "number" && field != "amount" {
				continue
			}
			if (op == "min" || op == "max") && field == "flag" {
				continue
			}
			for _, empty := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/empty=%v", field, op, empty), func(t *testing.T) {
					q := ingestion.AggregateQuery{ObjectType: "records", Aggregate: op, Field: field}
					if empty {
						v := filterLeaf("number", "gt", int64(99))
						q.Filter = &v
					}
					source, err := postgres.NewTenantDataReader(f.pg).AggregateRecords(t.Context(), f.model, q)
					if err != nil {
						t.Fatal(err)
					}
					var target any
					switch op {
					case "count":
						target, err = f.p.CountRecords(t.Context(), f.tx, field, q.Filter, queryLimits())
					case "sum":
						target, err = f.p.SumRecords(t.Context(), f.tx, field, q.Filter, queryLimits())
					case "avg":
						target, err = f.p.AverageRecords(t.Context(), f.tx, field, q.Filter, queryLimits())
					case "min":
						target, err = f.p.MinRecords(t.Context(), f.tx, field, q.Filter, queryLimits())
					case "max":
						target, err = f.p.MaxRecords(t.Context(), f.tx, field, q.Filter, queryLimits())
					case "count_distinct":
						target, err = f.p.CountDistinctRecords(t.Context(), f.tx, field, q.Filter, queryLimits())
					}
					if err != nil {
						t.Fatal(err)
					}
					// Numeric JSON scale is immaterial here; precision is tested separately.
					var a, b any
					if err = json.Unmarshal([]byte(parityValue(t, source)), &a); err != nil {
						t.Fatal(err)
					}
					if err = json.Unmarshal([]byte(parityValue(t, target)), &b); err != nil {
						t.Fatal(err)
					}
					if !reflect.DeepEqual(a, b) {
						t.Fatalf("source=%#v target=%#v", source, target)
					}
					want, ok := oracle[field][op]
					if !ok {
						t.Fatal("missing independent oracle", field, op)
					}
					if empty {
						want = nil
						if op == "sum" || op == "count" || op == "count_distinct" {
							want = 0.0
						}
					}
					if !reflect.DeepEqual(a, want) {
						t.Fatalf("source/target agree but violate independent oracle: actual=%#v want=%#v", a, want)
					}
				})
			}
		}
	}
}

func TestLiveParityCurrentReads(t *testing.T) {
	f := liveParityFixture(t)
	r := postgres.NewTenantDataReader(f.pg)
	limits := RecordResultLimits{FieldBytes: 1024, TransferBytes: 8192, RowBytes: 8192, PageBytes: 32768, Rows: 4}
	for _, id := range []string{"a", "n"} {
		source, err := r.GetRecord(t.Context(), f.model, "records", id)
		if err != nil {
			t.Fatal(err)
		}
		target, err := f.p.ReadCurrentExact(t.Context(), f.tx, id, limits, queryLimits())
		if err != nil {
			t.Fatal(err)
		}
		delete(source, "id") // independently generated storage identity, not a logical field
		if parityValue(t, source) != parityValue(t, target) {
			t.Fatalf("source=%s target=%s", parityValue(t, source), parityValue(t, target))
		}
	}
	for _, limit := range []int{1, 2, 4} {
		source, err := r.ListRecords(t.Context(), f.model, "records", limit)
		if err != nil {
			t.Fatal(err)
		}
		limits.Rows = limit
		target, err := f.p.ReadCurrentPage(t.Context(), f.tx, nil, limits, queryLimits())
		if err != nil {
			t.Fatal(err)
		}
		keys := func(rows []map[string]any) []string {
			out := []string{}
			for _, v := range rows {
				out = append(out, v["object_id"].(string))
			}
			return out
		}
		if !reflect.DeepEqual(keys(source), keys(target)) {
			t.Fatal(keys(source), keys(target))
		}
	}
}

func TestLiveParityStringMembershipBytes(t *testing.T) {
	f := liveParityFixture(t)
	// Existing rows test case and trailing-space distinctions, NULL and duplicates.
	for _, values := range [][]any{{"A"}, {"a "}, {"a", nil}, {"a", "a"}, {"a", "b", "c", "n"}, {"a_%"}} {
		q := filterLeaf("object_id", "in", values)
		f.compareCount(t, &q)
	}
	pred, err := f.p.MembershipPredicate("object_id", []any{"b", "a", nil})
	if err != nil {
		t.Fatal(err)
	}
	got := predicateMatches(t, f.tx, f.p, pred)
	sort.Strings(got)
	if strings.Join(got, ",") != "a,b" {
		t.Fatal(got)
	}
}

func TestLiveParityIPMembershipAndCount(t *testing.T) {
	f := liveParityFixture(t)
	predicate, err := f.p.MembershipPredicate("ip", []any{netip.MustParsePrefix("192.0.2.1/32")})
	if err != nil {
		t.Fatal(err)
	}
	_, first := predicate.SQL()
	first[0].([]byte)[0] = 0
	_, second := predicate.SQL()
	if second[0].([]byte)[0] != 192 {
		t.Fatal("membership arguments alias")
	}
	tree := filterLeaf("ip", "in", []any{netip.MustParsePrefix("192.0.2.1/32")})
	compiled, err := f.p.CompileFilter(&tree)
	if err != nil {
		t.Fatal(err)
	}
	_, first = compiled.SQL()
	first[0].([]byte)[0] = 0
	_, second = compiled.SQL()
	if second[0].([]byte)[0] != 192 {
		t.Fatal("filter arguments alias")
	}
	for _, values := range [][]any{{"192.0.2.1"}, {"192.0.2.1/32"}, {"192.0.2.1/24"}, {"2001:0db8:0:0:0:0:0:1"}, {"::ffff:192.0.2.1"}, {"192.0.2.1", nil}, {nil}, {"192.0.2.1", "192.0.2.1"}} {
		t.Run(fmt.Sprint(values), func(t *testing.T) {
			q := filterLeaf("ip", "in", values)
			f.compareCount(t, &q)
			negated := filterGroup("not", q)
			f.compareCount(t, &negated)
		})
	}
	source, err := postgres.NewTenantDataReader(f.pg).AggregateRecords(t.Context(), f.model, ingestion.AggregateQuery{ObjectType: "records", Aggregate: "count", Field: "ip"})
	if err != nil {
		t.Fatal(err)
	}
	target, err := f.p.CountRecords(t.Context(), f.tx, "ip", nil, queryLimits())
	if err != nil || target != 3 || parityValue(t, source) != parityValue(t, target) {
		t.Fatal(source, target, err)
	}
	source, err = postgres.NewTenantDataReader(f.pg).AggregateRecords(t.Context(), f.model, ingestion.AggregateQuery{ObjectType: "records", Aggregate: "count_distinct", Field: "ip"})
	if err != nil {
		t.Fatal(err)
	}
	target, err = f.p.CountDistinctRecords(t.Context(), f.tx, "ip", nil, queryLimits())
	if err != nil || target != 2 || parityValue(t, source) != parityValue(t, target) {
		t.Fatal(source, target, err)
	}
	for _, value := range []any{"invalid", "192.0.2.1/33", "fe80::1%eth0", int64(1)} {
		q := filterLeaf("ip", "in", []any{value})
		if _, err := f.p.NormalizeReadFilter(&q); err == nil {
			t.Fatal("invalid IP accepted", value)
		}
	}
}

func TestLiveParityTextAggregates(t *testing.T) {
	f := liveParityFixture(t)
	w, err := NewRecordWriter(t.Context(), f.tx, f.f.database, f.f.model.TenantID, f.f.model, "records", f.f.version)
	if err != nil {
		t.Fatal(err)
	}
	keys := []string{"A", "a ", "é", "e\u0301", strings.Repeat("x", 2048) + "a", strings.Repeat("x", 2048) + "b"}
	for _, key := range keys {
		r := map[string]any{"object_id": key}
		if _, err = w.WriteBatch(t.Context(), []map[string]any{r}, ingestion.ModeCreate, time.Now().UTC().Truncate(time.Microsecond), 1<<20); err != nil {
			t.Fatal(err)
		}
		if _, err = postgres.NewTenantDataWriter(f.pg).UpsertRecord(t.Context(), f.model, "records", r, ingestion.ModeCreate, time.Now().UTC().Truncate(time.Microsecond)); err != nil {
			t.Fatal(err)
		}
	}
	for _, op := range []string{"min", "max", "count_distinct"} {
		source, err := postgres.NewTenantDataReader(f.pg).AggregateRecords(t.Context(), f.model, ingestion.AggregateQuery{ObjectType: "records", Aggregate: op, Field: "object_id"})
		if err != nil {
			t.Fatal(err)
		}
		var target any
		switch op {
		case "min":
			target, err = f.p.MinRecords(t.Context(), f.tx, "object_id", nil, queryLimits())
		case "max":
			target, err = f.p.MaxRecords(t.Context(), f.tx, "object_id", nil, queryLimits())
		case "count_distinct":
			target, err = f.p.CountDistinctRecords(t.Context(), f.tx, "object_id", nil, queryLimits())
		}
		if err != nil || parityValue(t, source) != parityValue(t, target) {
			t.Fatalf("%s source=%v target=%v error=%v", op, source, target, err)
		}
	}
	// A large selected value is rejected by the SQL transfer guard, not truncated.
	r := map[string]any{"object_id": strings.Repeat("z", 16385)}
	if _, err = w.WriteBatch(t.Context(), []map[string]any{r}, ingestion.ModeCreate, time.Now().UTC().Truncate(time.Microsecond), 1<<20); err != nil {
		t.Fatal(err)
	}
	q := filterLeaf("object_id", "starts_with", "z")
	if _, err = f.p.MaxRecords(t.Context(), f.tx, "object_id", &q, queryLimits()); !errors.Is(err, ErrRecordResultLimit) {
		t.Fatal(err)
	}
}

func TestLiveParityIntegerAveragePrecision(t *testing.T) {
	f := liveParityFixture(t)
	column := f.f.columns["number"]["value"]
	if _, err := f.tx.ExecContext(t.Context(), "UPDATE "+f.p.table+" SET `"+column+"`=2 WHERE `"+column+"`=1"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pg.Exec(t.Context(), "UPDATE "+pgx.Identifier{f.model.SchemaName, "records"}.Sanitize()+" SET number=2 WHERE number=1"); err != nil {
		t.Fatal(err)
	}
	source, err := postgres.NewTenantDataReader(f.pg).AggregateRecords(t.Context(), f.model, ingestion.AggregateQuery{ObjectType: "records", Aggregate: "avg", Field: "number"})
	if err != nil {
		t.Fatal(err)
	}
	for _, increment := range []int{0, 4, 12} {
		if _, err = f.tx.ExecContext(t.Context(), fmt.Sprintf("SET SESSION div_precision_increment=%d", increment)); err != nil {
			t.Fatal(err)
		}
		target, err := f.p.AverageRecords(t.Context(), f.tx, "number", nil, queryLimits())
		if err != nil || parityValue(t, source) != parityValue(t, target) {
			t.Fatal(source, target, err)
		}
	}
	for _, numerator := range []int64{-2147483648, -1, 0, 1, 9999, 10000, 10001, 2147483647} {
		for _, denominator := range []int64{1, 3, 7, 9999, 10000, 10001, 100000001, math.MaxInt64} {
			var expected any
			if err = f.pg.QueryRow(t.Context(), "SELECT $1::bigint::numeric / $2::bigint::numeric", numerator, denominator).Scan(&expected); err != nil {
				t.Fatal(err)
			}
			actual, err := integerRecordAverage(numerator, denominator)
			if err != nil || parityValue(t, expected) != parityValue(t, actual) {
				t.Fatalf("%d/%d source=%s target=%s err=%v", numerator, denominator, parityValue(t, expected), parityValue(t, actual), err)
			}
		}
	}
}

func TestLiveParityIPOrder(t *testing.T) {
	f := liveParityFixture(t)
	columns := f.f.columns["ip"]
	a, family, prefix := "`"+columns["address"]+"`", "`"+columns["family"]+"`", "`"+columns["prefix"]+"`"
	pgtable := pgx.Identifier{f.model.SchemaName, "records"}.Sanitize()
	if _, err := f.tx.ExecContext(t.Context(), "UPDATE "+f.p.table+" SET "+a+"=NULL,"+family+"=NULL,"+prefix+"=NULL"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pg.Exec(t.Context(), "UPDATE "+pgtable+" SET ip=NULL"); err != nil {
		t.Fatal(err)
	}
	values := []string{"10.1.2.3/8", "10.0.0.1/16", "10.1.0.1/16", "10.1.2.3/32", "192.0.2.1/24", "::/0", "::ffff:192.0.2.1/128", "2001:db8::1/64", "2001:db8:1::/48"}
	for _, left := range values {
		for _, right := range values {
			for id, value := range map[string]string{"a": left, "b": right} {
				v, err := mysqlstore.EncodeInet(netip.MustParsePrefix(value))
				if err != nil {
					t.Fatal(err)
				}
				if _, err = f.tx.ExecContext(t.Context(), "UPDATE "+f.p.table+" SET "+a+"=?,"+family+"=?,"+prefix+"=? WHERE "+f.p.objectColumn+"=?", v.Address, v.Family, v.PrefixBits, id); err != nil {
					t.Fatal(err)
				}
				if _, err = f.pg.Exec(t.Context(), "UPDATE "+pgtable+" SET ip=$1 WHERE object_id=$2", value, id); err != nil {
					t.Fatal(err)
				}
			}
			for _, op := range []string{"min", "max"} {
				source, err := postgres.NewTenantDataReader(f.pg).AggregateRecords(t.Context(), f.model, ingestion.AggregateQuery{ObjectType: "records", Aggregate: op, Field: "ip"})
				if err != nil {
					t.Fatal(err)
				}
				call := f.p.MinRecords
				if op == "max" {
					call = f.p.MaxRecords
				}
				target, err := call(t.Context(), f.tx, "ip", nil, queryLimits())
				if err != nil || parityValue(t, source) != parityValue(t, target) {
					t.Fatalf("%s (%s,%s) source=%s target=%s err=%v", op, left, right, parityValue(t, source), parityValue(t, target), err)
				}
			}
			for _, op := range []string{"eq", "neq", "gt", "gte", "lt", "lte"} {
				q := filterLeaf("ip", op, left)
				f.compareCount(t, &q)
			}
		}
	}
	for _, op := range []string{"is_null", "is_not_null", "eq", "neq"} {
		q := filterLeaf("ip", op, nil)
		f.compareCount(t, &q)
	}
}

func (f parityFixture) writeBoth(t *testing.T, records []map[string]any, mode ingestion.Mode, want []string, conflict bool) {
	t.Helper()
	at := time.Date(2026, 10, 30, 13, 0, 0, 0, time.UTC)
	ptx, err := f.pg.BeginTx(t.Context(), pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer ptx.Rollback(context.Background())
	var source []string
	for _, record := range records {
		var action string
		action, err = postgres.NewTenantDataWriter(ptx).UpsertRecord(t.Context(), f.model, "records", record, mode, at)
		if err != nil {
			break
		}
		source = append(source, action)
	}
	sourceErr := err
	if sourceErr == nil {
		if err = ptx.Commit(t.Context()); err != nil {
			t.Fatal(err)
		}
	} else {
		if err = ptx.Rollback(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	var target []string
	targetErr := mysqlstore.Mutation(t.Context(), f.f.db, func(tx *sql.Tx) error {
		writer, err := NewRecordWriter(t.Context(), tx, f.f.database, f.f.model.TenantID, f.f.model, "records", f.f.version)
		if err != nil {
			return err
		}
		results, err := writer.WriteBatch(t.Context(), records, mode, at, 1<<20)
		if err != nil {
			return err
		}
		for _, r := range results {
			target = append(target, r.Action)
		}
		return nil
	})
	if conflict {
		var pgerr *pgconn.PgError
		if !errors.As(sourceErr, &pgerr) || pgerr.Code != "23505" || !errors.Is(targetErr, ErrRecordConflict) {
			t.Fatalf("expected targeted unique errors, source=%v target=%v", sourceErr, targetErr)
		}
		return
	}
	if sourceErr != nil || targetErr != nil || !reflect.DeepEqual(source, want) || !reflect.DeepEqual(target, want) {
		t.Fatalf("actions source=%v target=%v want=%v errors=%v/%v", source, target, want, sourceErr, targetErr)
	}
}

func (f parityFixture) compareRecordState(t *testing.T) {
	t.Helper()
	source, err := postgres.NewTenantDataReader(f.pg).ListRecords(t.Context(), f.model, "records", 100)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := f.f.db.BeginTx(t.Context(), &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	p, err := NewRecordProjection(t.Context(), tx, f.f.database, f.f.model.TenantID, f.f.model, "records", f.f.version, []string{"object_id", "number", "amount", "flag", "ip", "updated_at"})
	if err != nil {
		t.Fatal(err)
	}
	target, err := p.ReadCurrentPage(t.Context(), tx, nil, RecordResultLimits{FieldBytes: 1024, TransferBytes: 8192, RowBytes: 8192, PageBytes: 1 << 20, Rows: 100}, queryLimits())
	if err != nil {
		t.Fatal(err)
	}
	canonical := func(rows []map[string]any) string {
		for _, r := range rows {
			delete(r, "id")
		}
		sort.Slice(rows, func(i, j int) bool { return rows[i]["object_id"].(string) < rows[j]["object_id"].(string) })
		return parityValue(t, rows)
	}
	if a, b := canonical(source), canonical(target); a != b {
		t.Fatalf("source=%s target=%s", a, b)
	}
}

func TestLiveParitySingleWrites(t *testing.T) {
	f := liveParityFixture(t)
	if err := f.tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name     string
		record   map[string]any
		mode     ingestion.Mode
		action   string
		conflict bool
	}{
		{"create", map[string]any{"object_id": "new", "number": int64(10), "amount": 3.5, "flag": true}, ingestion.ModeCreate, "created", false},
		{"patch_omits_fields", map[string]any{"object_id": "new", "amount": 4.5}, ingestion.ModePatch, "updated", false},
		{"create_existing", map[string]any{"object_id": "new", "flag": false}, ingestion.ModeCreate, "updated", false},
		{"explicit_null", map[string]any{"object_id": "new", "amount": nil}, ingestion.ModePatch, "updated", false},
		{"patch_missing", map[string]any{"object_id": "patch-new", "number": int64(11)}, ingestion.ModePatch, "created", false},
		{"alternate_unique", map[string]any{"object_id": "conflict", "number": int64(10)}, ingestion.ModeCreate, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f.writeBoth(t, []map[string]any{tc.record}, tc.mode, []string{tc.action}, tc.conflict)
			f.compareRecordState(t)
		})
	}
}

func TestLiveParityBatchWrites(t *testing.T) {
	f := liveParityFixture(t)
	if err := f.tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	t.Run("ordered_and_partial", func(t *testing.T) {
		f.writeBoth(t, []map[string]any{{"object_id": "first", "number": int64(10), "amount": 1.5}, {"object_id": "second", "number": int64(11)}, {"object_id": "a", "flag": true}}, ingestion.ModeCreate, []string{"created", "created", "updated"}, false)
		f.compareRecordState(t)
	})
	t.Run("late_unique_rolls_back_all", func(t *testing.T) {
		f.writeBoth(t, []map[string]any{{"object_id": "rolled-back", "number": int64(12)}, {"object_id": "second", "number": int64(10)}}, ingestion.ModePatch, nil, true)
		f.compareRecordState(t)
		var count int
		if err := f.pg.QueryRow(t.Context(), "SELECT COUNT(*) FROM "+pgx.Identifier{f.model.SchemaName, "records"}.Sanitize()+" WHERE object_id='rolled-back'").Scan(&count); err != nil || count != 0 {
			t.Fatal(count, err)
		}
	})
}
