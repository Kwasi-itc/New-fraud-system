package mysqlstore

import (
	"fmt"
	"testing"
	"time"
)

func TestTextCodec(t *testing.T) {
	for _, want := range []string{"", "  Mixed Case  ", "🔒 café", "e\u0301", "é"} {
		for _, input := range []any{want, []byte(want)} {
			var got Text
			if err := got.Scan(input); err != nil || string(got) != want {
				t.Fatalf("text changed: %v", err)
			}
		}
	}
	for _, input := range []any{nil, true, 123, []byte{255}, "a\x00b"} {
		got := Text("stale")
		if err := got.Scan(input); err == nil || got != "" {
			t.Fatal("invalid text accepted or stale state retained")
		}
	}
	var nullable NullText
	if err := nullable.Scan(""); err != nil || !nullable.Valid {
		t.Fatal("empty text confused with SQL NULL")
	}
	if err := nullable.Scan(nil); err != nil || nullable.Valid || nullable.Text != "" {
		t.Fatal("SQL NULL retained stale text")
	}
	if err := nullable.Scan("valid"); err != nil {
		t.Fatal(err)
	}
	if err := nullable.Scan("\x00"); err == nil || nullable.Valid || nullable.Text != "" {
		t.Fatal("invalid nullable text retained")
	}
}

func TestMySQLTextCodec(t *testing.T) {
	db := integrationDB(t)
	name, _ := Identifier(fmt.Sprintf("text_%d", time.Now().UnixNano()))
	if _, err := db.Exec("CREATE TABLE " + name + " (id INT PRIMARY KEY,v LONGTEXT CHARACTER SET utf8mb4) ENGINE=InnoDB"); err != nil {
		t.Fatal(err)
	}
	for i, want := range []Text{"", "  Mixed Case  ", "🔒 café", "e\u0301", "é"} {
		if _, err := db.Exec("INSERT INTO "+name+" VALUES(?,?)", i, want); err != nil {
			t.Fatal(err)
		}
		var got Text
		if err := db.QueryRow("SELECT v FROM "+name+" WHERE id=?", i).Scan(&got); err != nil || got != want {
			t.Fatalf("text roundtrip: %v", err)
		}
	}
}
