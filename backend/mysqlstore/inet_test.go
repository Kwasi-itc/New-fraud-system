package mysqlstore

import (
	"context"
	"fmt"
	"net/netip"
	"testing"
	"time"
)

var inetCorpus = []string{
	"0.0.0.0/0", "192.0.2.129/24", "255.255.255.255/32",
	"::/0", "2001:db8::abcd/64", "ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff/128",
	"::ffff:192.0.2.129/120", "::192.0.2.129/128",
}

func TestInetColumns(t *testing.T) {
	for _, input := range inetCorpus {
		want := netip.MustParsePrefix(input)
		columns, err := EncodeInet(want)
		if err != nil {
			t.Fatal(err)
		}
		got, err := columns.Prefix()
		if err != nil || got != want {
			t.Fatalf("%s: got %v, %v", input, got, err)
		}
		// Neither direction may alias the mutable storage buffer.
		columns.Address[0] ^= 255
		if got != want || want != netip.MustParsePrefix(input) {
			t.Fatal("codec aliases address bytes")
		}
	}
	if _, err := EncodeInet(netip.Prefix{}); err == nil {
		t.Fatal("invalid prefix accepted")
	}
	for _, c := range []InetColumns{
		{}, {4, make([]byte, 16), 24}, {6, make([]byte, 4), 24},
		{4, make([]byte, 4), -1}, {4, make([]byte, 4), 33},
		{6, make([]byte, 16), 129}, {5, make([]byte, 4), 24},
	} {
		if got, err := c.Prefix(); err == nil || got.IsValid() {
			t.Fatalf("invalid columns accepted: %+v", c)
		}
	}
}

func TestMySQLInetColumns(t *testing.T) {
	db := integrationDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	name, _ := Identifier(fmt.Sprintf("inet_%d", time.Now().UnixNano()))
	_, err := db.ExecContext(ctx, "CREATE TABLE "+name+` (
		id INT PRIMARY KEY, family TINYINT NOT NULL, address VARBINARY(16) NOT NULL,
		prefix_bits SMALLINT NOT NULL,
		CHECK ((family = 4 AND OCTET_LENGTH(address) = 4 AND prefix_bits BETWEEN 0 AND 32)
		OR (family = 6 AND OCTET_LENGTH(address) = 16 AND prefix_bits BETWEEN 0 AND 128))
	) ENGINE=InnoDB`)
	if err != nil {
		t.Fatal(err)
	}
	for i, input := range inetCorpus {
		want := netip.MustParsePrefix(input)
		c, err := EncodeInet(want)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, "INSERT INTO "+name+" VALUES (?, ?, ?, ?)", i, c.Family, c.Address, c.PrefixBits); err != nil {
			t.Fatal(err)
		}
		var stored InetColumns
		if err := db.QueryRowContext(ctx, "SELECT family, address, prefix_bits FROM "+name+" WHERE id = ?", i).Scan(&stored.Family, &stored.Address, &stored.PrefixBits); err != nil {
			t.Fatal(err)
		}
		got, err := stored.Prefix()
		if err != nil || got != want {
			t.Fatalf("%s: persisted %v, %v", input, got, err)
		}
		var matches bool
		if err := db.QueryRowContext(ctx, "SELECT address = INET6_ATON(?) FROM "+name+" WHERE id = ?", want.Addr().String(), i).Scan(&matches); err != nil || !matches {
			t.Fatalf("MySQL address representation mismatch: %s: %v", input, err)
		}
	}
	for _, c := range []InetColumns{
		{4, make([]byte, 16), 24}, {6, make([]byte, 4), 24},
		{4, make([]byte, 4), -1}, {4, make([]byte, 4), 33},
		{6, make([]byte, 16), 129}, {5, make([]byte, 4), 24},
	} {
		_, err := db.ExecContext(ctx, "INSERT INTO "+name+" VALUES (100, ?, ?, ?)", c.Family, c.Address, c.PrefixBits)
		if Classify(err) != CheckViolation {
			t.Fatalf("database accepted invalid representation or wrong error: %v", err)
		}
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO "+name+" VALUES (101, 4, NULL, 32)"); err == nil {
		t.Fatal("NULL silently converted to address")
	}
}
