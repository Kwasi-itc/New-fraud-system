package mysql

import (
	"errors"
	"math"
	"net/netip"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
)

func TestRecordDecodeScalars(t *testing.T) {
	for _, tc := range []struct {
		typ       string
		raw, want any
		bad       bool
	}{
		{"bool", int64(0), false, false}, {"bool", uint64(1), true, false}, {"bool", int64(2), nil, true}, {"bool", []byte("true"), nil, true},
		{"int", int64(math.MinInt32), int64(math.MinInt32), false}, {"int", []byte("2147483647"), int64(math.MaxInt32), false},
		{"int", int64(math.MaxInt32) + 1, nil, true}, {"int", uint64(math.MaxUint64), nil, true}, {"int", float64(1), nil, true}, {"int", []byte("1.5"), nil, true},
		{"float", math.MaxFloat64, math.MaxFloat64, false}, {"float", math.SmallestNonzeroFloat64, math.SmallestNonzeroFloat64, false}, {"float", []byte("1.25"), 1.25, false},
		{"float", math.Inf(1), nil, true}, {"float", math.NaN(), nil, true}, {"float", []byte("1e999"), nil, true}, {"float", int64(1), nil, true},
	} {
		got, err := DecodeRecordValue(ingestion.FieldSchema{DataType: tc.typ}, []any{tc.raw}, 64)
		if (err != nil) != tc.bad || !reflect.DeepEqual(got, tc.want) {
			t.Fatal(tc, got, err)
		}
	}
}
func TestRecordDecodeStringsAndNulls(t *testing.T) {
	field := ingestion.FieldSchema{DataType: "string", Nullable: true}
	raw := []byte("a\x00?")
	got, err := DecodeRecordValue(field, []any{raw}, len(raw))
	if err != nil || got != string(raw) {
		t.Fatal(got, err)
	}
	raw[0] = 'z'
	if got.(string)[0] != 'a' {
		t.Fatal("driver buffer retained")
	}
	for _, parts := range [][]any{{[]byte{0xff}}, {strings.Repeat("x", 5)}, {42}, {}, {"x", "y"}} {
		if got, err := DecodeRecordValue(field, parts, 4); !errors.Is(err, ErrRecordDecode) || got != nil {
			t.Fatal(got, err)
		}
	}
	for _, typ := range []string{"bool", "int", "float", "string", "timestamp", "ip_address"} {
		field := ingestion.FieldSchema{DataType: typ, Nullable: true}
		parts := []any{nil}
		if typ == "ip_address" {
			parts = []any{nil, nil, nil}
		}
		if got, err := DecodeRecordValue(field, parts, 4); err != nil || got != nil {
			t.Fatal(typ, got, err)
		}
		field.Nullable = false
		if got, err := DecodeRecordValue(field, parts, 4); err == nil || got != nil {
			t.Fatal("required null", typ)
		}
	}
	for _, field := range []ingestion.FieldSchema{{DataType: "unsupported", Nullable: true}, {DataType: "string", Archived: true}} {
		if _, err := DecodeRecordValue(field, []any{nil}, 4); err == nil {
			t.Fatal(field)
		}
	}
	for _, limit := range []int{0, 16<<20 + 1} {
		if _, err := DecodeRecordValue(field, []any{"x"}, limit); err == nil {
			t.Fatal(limit)
		}
	}
}
func TestRecordDecodeTimestamp(t *testing.T) {
	field := ingestion.FieldSchema{DataType: "timestamp"}
	for _, at := range []time.Time{time.Date(1000, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(9999, 12, 31, 23, 59, 59, 999999000, time.UTC), time.Date(2026, 1, 1, 12, 0, 0, 123456000, time.FixedZone("offset", 3600))} {
		got, err := DecodeRecordValue(field, []any{at}, 32)
		if err != nil || got.(time.Time).Location() != time.UTC || !got.(time.Time).Equal(at) {
			t.Fatal(got, err)
		}
	}
	for _, raw := range []any{time.Time{}, time.Date(2026, 1, 1, 0, 0, 0, 1, time.UTC), []byte("2026-01-01 00:00:00"), "infinity"} {
		if got, err := DecodeRecordValue(field, []any{raw}, 32); err == nil || got != nil {
			t.Fatal(got, err)
		}
	}
}
func TestRecordDecodeIP(t *testing.T) {
	field := ingestion.FieldSchema{DataType: "ip_address", Nullable: true}
	for _, value := range []string{"192.0.2.9/24", "2001:db8::9/64", "::ffff:192.0.2.9/128", "0.0.0.0/0", "::/0"} {
		p := netip.MustParsePrefix(value)
		family := int64(6)
		if p.Addr().Is4() {
			family = 4
		}
		address := p.Addr().AsSlice()
		got, err := DecodeRecordValue(field, []any{address, family, int64(p.Bits())}, 32)
		if err != nil || got != p {
			t.Fatal(got, err)
		}
		address[0] ^= 255
		if got != p {
			t.Fatal("driver address retained")
		}
	}
	for _, parts := range [][]any{{nil, int64(4), int64(32)}, {[]byte{1, 2, 3, 4}, nil, nil}, {[]byte{1, 2, 3, 4}, int64(6), int64(32)}, {[]byte{1, 2, 3, 4}, int64(4), int64(33)}, {[]byte{1, 2, 3, 4}, int64(4), int64(-1)}} {
		if got, err := DecodeRecordValue(field, parts, 32); err == nil || got != nil {
			t.Fatal(got, err)
		}
	}
}

func TestRecordDecodeDatabaseRoundTrips(t *testing.T) {
	f := recordFixture(t)
	// Use actual normalized writer encodings and MySQL physical types. The generated
	// fixture connection has parseTime enabled; no application database is touched.
	name := f.qualified[:strings.LastIndex(f.qualified, ".")] + ".decoder_roundtrip"
	if _, err := f.db.Exec("CREATE TABLE " + name + "(b TINYINT NULL,i INT NULL,f DOUBLE NULL,s LONGTEXT CHARACTER SET utf8mb4 NULL,t DATETIME(6) NULL,a VARBINARY(16) NULL,family TINYINT UNSIGNED NULL,prefix TINYINT UNSIGNED NULL)"); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 29, 12, 13, 14, 123456000, time.UTC)
	for _, tc := range []struct {
		integer int64
		number  float64
		ip      string
		at      time.Time
	}{
		{math.MinInt32, math.SmallestNonzeroFloat64, "192.0.2.9", at},
		{math.MaxInt32, math.MaxFloat64, "::ffff:192.0.2.9", time.Date(9999, 12, 31, 23, 59, 59, 999999000, time.UTC)},
		{0, -1.25, "2001:db8::9", time.Date(1000, 1, 1, 0, 0, 0, 0, time.UTC)},
	} {
		types := []string{"bool", "int", "float", "string", "timestamp", "ip_address"}
		values := []any{true, tc.integer, tc.number, "?\x00Case_ ", tc.at, tc.ip}
		var args []any
		for i, typ := range types {
			_, parts, err := encodeWriteValue(ingestion.FieldSchema{DataType: typ}, values[i])
			if err != nil {
				t.Fatal(err)
			}
			args = append(args, parts...)
		}
		if _, err := f.db.Exec("INSERT INTO "+name+" VALUES(?,?,?,?,?,?,?,?)", args...); err != nil {
			t.Fatal(err)
		}
		row := f.db.QueryRow("SELECT b,i,f,s,t,a,family,prefix FROM " + name + " LIMIT 1")
		var raw [8]any
		dest := make([]any, 8)
		for i := range raw {
			dest[i] = &raw[i]
		}
		if err := row.Scan(dest...); err != nil {
			t.Fatal(err)
		}
		for i, typ := range types {
			parts := raw[i : i+1]
			want := values[i]
			if typ == "ip_address" {
				parts = raw[5:8]
				addr := netip.MustParseAddr(tc.ip)
				want = netip.PrefixFrom(addr, addr.BitLen())
			}
			got, err := DecodeRecordValue(ingestion.FieldSchema{DataType: typ}, parts, 64)
			if err != nil {
				t.Fatal(typ, raw, err)
			}
			if stamp, ok := want.(time.Time); ok {
				if !got.(time.Time).Equal(stamp) {
					t.Fatal(got, want)
				}
			} else if !reflect.DeepEqual(got, want) {
				t.Fatal(typ, got, want)
			}
		}
		if _, err := f.db.Exec("DELETE FROM " + name); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.db.Exec("INSERT INTO " + name + " VALUES(NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL)"); err != nil {
		t.Fatal(err)
	}
	var raw [8]any
	dest := make([]any, 8)
	for i := range raw {
		dest[i] = &raw[i]
	}
	if err := f.db.QueryRow("SELECT * FROM " + name).Scan(dest...); err != nil {
		t.Fatal(err)
	}
	for i, typ := range []string{"bool", "int", "float", "string", "timestamp", "ip_address"} {
		parts := raw[i : i+1]
		if typ == "ip_address" {
			parts = raw[5:8]
		}
		if got, err := DecodeRecordValue(ingestion.FieldSchema{DataType: typ, Nullable: true}, parts, 64); err != nil || got != nil {
			t.Fatal(typ, got, err)
		}
	}
}
