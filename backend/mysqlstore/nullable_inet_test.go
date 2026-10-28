package mysqlstore

import (
	"context"
	"fmt"
	"net/netip"
	"testing"
	"time"
)

func TestNullInet(t *testing.T) {
	want := NullInet{Prefix: netip.MustParsePrefix("::ffff:192.0.2.129/120"), Valid: true}
	values, err := want.Values()
	if err != nil {
		t.Fatal(err)
	}
	var got NullInet
	if err := got.ScanColumns(values[0], values[1], values[2]); err != nil || got != want {
		t.Fatalf("round trip: %v %v", got, err)
	}
	for mask := 1; mask < 8; mask++ {
		v := values
		for i := range v {
			if mask&(1<<i) != 0 {
				v[i] = nil
			}
		}
		got = want
		err := got.ScanColumns(v[0], v[1], v[2])
		if (mask == 7) != (err == nil) || got != (NullInet{}) {
			t.Fatalf("null mask %d: %v %v", mask, got, err)
		}
	}
	for _, v := range [][3]any{
		{4, []byte{1}, 32}, {4, make([]byte, 4), 33},
		{6, make([]byte, 16), -1}, {6, make([]byte, 16), uint64(1 << 63)},
		{"bad", make([]byte, 4), 32}, {4, "abcd", 32},
		{4, make([]byte, 4), "bad"}, {4, []byte(nil), 32},
	} {
		got = want
		if err := got.ScanColumns(v[0], v[1], v[2]); err == nil || got != (NullInet{}) {
			t.Fatalf("invalid value retained state: %v %v", got, err)
		}
	}
	if v, err := (NullInet{Prefix: want.Prefix}).Values(); err != nil || v != ([3]any{}) {
		t.Fatalf("invalid flag must bind NULL: %v %v", v, err)
	}
	if _, err := (NullInet{Valid: true}).Values(); err == nil {
		t.Fatal("valid flag with invalid prefix accepted")
	}
	var missing *NullInet
	if missing.ScanColumns(nil, nil, nil) == nil {
		t.Fatal("nil receiver accepted")
	}
}

func TestMySQLNullInet(t *testing.T) {
	db := integrationDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	name, _ := Identifier(fmt.Sprintf("null_inet_%d", time.Now().UnixNano()))
	_, err := db.ExecContext(ctx, "CREATE TABLE "+name+` (
		id INT PRIMARY KEY, family TINYINT NULL, address VARBINARY(16) NULL,
		prefix_bits SMALLINT NULL,
		CHECK ((family IS NULL AND address IS NULL AND prefix_bits IS NULL) OR
		(family IS NOT NULL AND address IS NOT NULL AND prefix_bits IS NOT NULL AND
		((family = 4 AND OCTET_LENGTH(address) = 4 AND prefix_bits BETWEEN 0 AND 32) OR
		(family = 6 AND OCTET_LENGTH(address) = 16 AND prefix_bits BETWEEN 0 AND 128))))
	) ENGINE=InnoDB`)
	if err != nil {
		t.Fatal(err)
	}
	cases := []NullInet{{}}
	for _, input := range inetCorpus {
		cases = append(cases, NullInet{Prefix: netip.MustParsePrefix(input), Valid: true})
	}
	cases = append(cases, NullInet{}) // reuse a destination after valid values
	var got NullInet
	for id, want := range cases {
		v, err := want.Values()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, "INSERT INTO "+name+" VALUES (?, ?, ?, ?)", id, v[0], v[1], v[2]); err != nil {
			t.Fatal(err)
		}
		var f, a, p any
		if err := db.QueryRowContext(ctx, "SELECT family, address, prefix_bits FROM "+name+" WHERE id = ?", id).Scan(&f, &a, &p); err != nil {
			t.Fatal(err)
		}
		if err := got.ScanColumns(f, a, p); err != nil || got != want {
			t.Fatalf("row %d: got %v want %v: %v", id, got, want, err)
		}
	}
	for mask := 1; mask < 7; mask++ {
		v := [3]any{4, []byte{192, 0, 2, 129}, 24}
		for i := range v {
			if mask&(1<<i) != 0 {
				v[i] = nil
			}
		}
		if _, err := db.ExecContext(ctx, "INSERT INTO "+name+" VALUES (100, ?, ?, ?)", v[0], v[1], v[2]); Classify(err) != CheckViolation {
			t.Fatalf("partial NULL mask %d escaped constraint: %v", mask, err)
		}
	}
	for _, v := range [][3]any{{5, make([]byte, 4), 24}, {4, make([]byte, 16), 24}, {6, make([]byte, 16), 129}} {
		if _, err := db.ExecContext(ctx, "INSERT INTO "+name+" VALUES (100, ?, ?, ?)", v[0], v[1], v[2]); Classify(err) != CheckViolation {
			t.Fatalf("invalid tuple escaped constraint: %v", err)
		}
	}
}
