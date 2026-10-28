package mysqlstore

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"
)

func TestJSONCodec(t *testing.T) {
	for _, text := range []string{`null`, `true`, `9007199254740993`, `"雪"`, `[]`, `{"x":1,"x":2}`} {
		input := JSON(text)
		v, err := input.Value()
		if err != nil || v != text {
			t.Fatalf("binding changed document: %v %v", v, err)
		}
		var got JSON
		if err := got.Scan([]byte(input)); err != nil || string(got) != text {
			t.Fatalf("scan changed document: %s %v", got, err)
		}
		input[0] ^= 1
		if string(got) != text || v != text {
			t.Fatal("codec aliases caller storage")
		}
	}
	for _, bad := range []any{nil, []byte(nil), "", "{", "true false", []byte{'"', 255, '"'}, 1} {
		got := JSON(`true`)
		if err := got.Scan(bad); err == nil || got != nil {
			t.Fatalf("invalid input retained state: %v %v", got, err)
		}
	}
	for _, bad := range []JSON{nil, JSON(""), JSON("{"), {'"', 255, '"'}} {
		if _, err := bad.Value(); err == nil {
			t.Fatal("invalid document bound successfully")
		}
	}
	n := NullJSON{}
	if err := n.Scan("null"); err != nil || !n.Valid || string(n.JSON) != "null" {
		t.Fatalf("JSON null lost: %+v %v", n, err)
	}
	if err := n.Scan(nil); err != nil || n.Valid || n.JSON != nil {
		t.Fatal("SQL NULL retained state")
	}
	n = NullJSON{JSON: JSON(`true`), Valid: true}
	if err := n.Scan("bad"); err == nil || n.Valid || n.JSON != nil {
		t.Fatal("invalid nullable scan retained state")
	}
	if v, err := (NullJSON{JSON: JSON(`true`)}).Value(); err != nil || v != nil {
		t.Fatal("invalid flag did not bind SQL NULL")
	}
	if _, err := (NullJSON{Valid: true}).Value(); err == nil {
		t.Fatal("present empty document accepted")
	}
	var j *JSON
	var nullable *NullJSON
	if j.Scan(nil) == nil || nullable.Scan(nil) == nil {
		t.Fatal("nil destination accepted")
	}
}

func TestMySQLJSONCodec(t *testing.T) {
	db := integrationDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	name, _ := Identifier(fmt.Sprintf("json_codec_%d", time.Now().UnixNano()))
	if _, err := db.ExecContext(ctx, "CREATE TABLE "+name+" (id INT PRIMARY KEY, doc JSON NULL) ENGINE=InnoDB"); err != nil {
		t.Fatal(err)
	}
	cases := []struct{ text, kind, canonical string }{
		{"", "", ""}, {"null", "NULL", "null"}, {"true", "BOOLEAN", "true"},
		{"9007199254740993", "UNSIGNED INTEGER", "9007199254740993"},
		{"-9007199254740993", "INTEGER", "-9007199254740993"},
		{`"雪"`, "STRING", `"雪"`}, {`[1,null,"x"]`, "ARRAY", `[1, null, "x"]`},
		{`{"x":1,"x":2}`, "OBJECT", `{"x": 2}`}, {"", "", ""},
	}
	var got NullJSON
	for id, c := range cases {
		input := NullJSON{JSON: JSON(c.text), Valid: c.text != ""}
		if _, err := db.ExecContext(ctx, "INSERT INTO "+name+" VALUES (?, ?)", id, input); err != nil {
			t.Fatal(err)
		}
		var kind sql.NullString
		if err := db.QueryRowContext(ctx, "SELECT doc, JSON_TYPE(doc) FROM "+name+" WHERE id = ?", id).Scan(&got, &kind); err != nil {
			t.Fatal(err)
		}
		if got.Valid != input.Valid || string(got.JSON) != c.canonical || kind.Valid != input.Valid || kind.String != c.kind {
			t.Fatalf("row %d: got %+v kind %+v", id, got, kind)
		}
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO "+name+" VALUES (100, ?)", JSON(`{`)); err == nil {
		t.Fatal("invalid codec value inserted")
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO "+name+" VALUES (101, ?)", `{`); err == nil {
		t.Fatal("server accepted malformed JSON bypassing codec")
	}
}
