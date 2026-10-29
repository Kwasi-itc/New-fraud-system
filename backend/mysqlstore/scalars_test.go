package mysqlstore

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"math"
	"testing"
	"time"
)

func TestScalarCodecs(t *testing.T) {
	for _, input := range []any{true, int64(1), []byte("1"), "1"} {
		var b Bool
		if err := b.Scan(input); err != nil || !b {
			t.Fatalf("Boolean: %v %v", b, err)
		}
	}
	for _, input := range []any{nil, int64(-1), int64(2), "true", []byte("1.0"), float64(1)} {
		b := Bool(true)
		if err := b.Scan(input); err == nil || b {
			t.Fatal("Boolean accepted invalid value or retained stale state")
		}
	}
	for _, input := range []any{int64(math.MinInt32), []byte("2147483647"), "-12"} {
		var n Int32
		if err := n.Scan(input); err != nil {
			t.Fatal(err)
		}
	}
	for _, input := range []any{nil, int64(math.MaxInt32) + 1, int64(math.MinInt32) - 1, "2147483648", "1.2", float64(1), true} {
		n := Int32(7)
		if err := n.Scan(input); err == nil || n != 0 {
			t.Fatal("INTEGER accepted invalid value or retained stale state")
		}
	}
	for _, input := range []any{float64(-1.25), []byte("1.7976931348623157e308"), "5e-324"} {
		var n Float64
		if err := n.Scan(input); err != nil {
			t.Fatal(err)
		}
	}
	for _, input := range []any{nil, int64(1), "NaN", "Inf", math.Inf(-1), math.NaN(), "1e999", true} {
		n := Float64(7)
		if err := n.Scan(input); err == nil || n != 0 {
			t.Fatal("DOUBLE accepted invalid value or retained stale state")
		}
	}
	for _, n := range []Float64{Float64(math.NaN()), Float64(math.Inf(1))} {
		if _, err := n.Value(); err == nil {
			t.Fatal("bound non-finite DOUBLE")
		}
	}
	for _, c := range []struct {
		scan    sql.Scanner
		value   driver.Valuer
		valid   any
		invalid any
	}{
		{&NullBool{}, NullBool{}, int64(1), int64(3)},
		{&NullInt32{}, NullInt32{}, int64(-4), "bad"},
		{&NullFloat64{}, NullFloat64{}, float64(4.5), "NaN"},
	} {
		if v, err := c.value.Value(); err != nil || v != nil {
			t.Fatalf("NULL binding: %v %v", v, err)
		}
		if err := c.scan.Scan(c.valid); err != nil {
			t.Fatal(err)
		}
		if err := c.scan.Scan(nil); err != nil {
			t.Fatal(err)
		}
		if v, err := c.scan.(driver.Valuer).Value(); err != nil || v != nil {
			t.Fatal("NULL retained stale value")
		}
		if err := c.scan.Scan(c.valid); err != nil {
			t.Fatal(err)
		}
		if err := c.scan.Scan(c.invalid); err == nil {
			t.Fatal("accepted invalid nullable scalar")
		}
		if v, err := c.scan.(driver.Valuer).Value(); err != nil || v != nil {
			t.Fatal("invalid scan retained stale value")
		}
	}
	for _, destination := range []sql.Scanner{(*Bool)(nil), (*Int32)(nil), (*Float64)(nil), (*NullBool)(nil), (*NullInt32)(nil), (*NullFloat64)(nil)} {
		if err := destination.Scan(nil); err == nil {
			t.Fatal("nil destination accepted")
		}
	}
}

func TestMySQLScalarCodecs(t *testing.T) {
	db := integrationDB(t)
	name, _ := Identifier(fmt.Sprintf("scalar_%d", time.Now().UnixNano()))
	if _, err := db.Exec("CREATE TABLE " + name + " (id INT PRIMARY KEY, b BOOLEAN CHECK(b IN (0,1)), i INT, f DOUBLE) ENGINE=InnoDB"); err != nil {
		t.Fatal(err)
	}
	for _, input := range []struct {
		b any
		i any
		f any
	}{
		{Bool(true), Int32(math.MinInt32), Float64(-1.25)},
		{Bool(false), Int32(math.MaxInt32), Float64(math.MaxFloat64)},
		{NullBool{}, NullInt32{}, NullFloat64{}},
	} {
		if _, err := db.Exec("INSERT INTO "+name+" VALUES(1,?,?,?)", input.b, input.i, input.f); err != nil {
			t.Fatal(err)
		}
		var b NullBool
		var i NullInt32
		var f NullFloat64
		if err := db.QueryRow("SELECT b,i,f FROM "+name+" WHERE id=1").Scan(&b, &i, &f); err != nil {
			t.Fatal(err)
		}
		for _, v := range []struct {
			got  driver.Valuer
			want driver.Valuer
		}{{b, input.b.(driver.Valuer)}, {i, input.i.(driver.Valuer)}, {f, input.f.(driver.Valuer)}} {
			got, ge := v.got.Value()
			want, we := v.want.Value()
			if ge != nil || we != nil || got != want {
				t.Fatalf("round trip: %v != %v", got, want)
			}
		}
		if _, err := db.Exec("DELETE FROM " + name + " WHERE id=1"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec("INSERT INTO " + name + " VALUES(2,2,0,0)"); Classify(err) != CheckViolation {
		t.Fatalf("Boolean constraint: %v", err)
	}
	if err := Mutation(context.Background(), db, func(tx *sql.Tx) error {
		_, err := tx.Exec("INSERT INTO "+name+" VALUES(3,?,0,?)", Bool(true), Float64(math.NaN()))
		return err
	}); err == nil {
		t.Fatal("non-finite write accepted")
	}
}
