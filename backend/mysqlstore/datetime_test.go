package mysqlstore

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"
)

func TestDateTimeBoundaries(t *testing.T) {
	for _, instant := range []time.Time{
		time.Time{}, time.Date(999, 12, 31, 23, 59, 59, 0, time.UTC),
		time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 1, 1, 0, 0, 0, 1, time.UTC),
	} {
		if _, err := (DateTime{instant}).Value(); err == nil {
			t.Fatalf("invalid time accepted: %v", instant)
		}
	}
	for _, instant := range []time.Time{
		time.Date(1000, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(9999, 12, 31, 23, 59, 59, 999999000, time.UTC),
		time.Date(2026, 10, 22, 12, 30, 0, 123456000, time.FixedZone("offset", 3600)),
	} {
		value, err := (DateTime{instant}).Value()
		if err != nil {
			t.Fatal(err)
		}
		var got DateTime
		if err := got.Scan(value); err != nil || !got.Equal(instant) || got.Location() != time.UTC {
			t.Fatalf("time mismatch: %v %v", got, err)
		}
	}
	var got DateTime
	if err := got.Scan(nil); err == nil {
		t.Fatal("SQL NULL became a timestamp")
	}
}

func TestMySQLScalarRoundTrip(t *testing.T) {
	db := integrationDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	name, _ := Identifier(fmt.Sprintf("scalar_%d", time.Now().UnixNano()))
	if _, err := db.ExecContext(ctx, "CREATE TABLE "+name+" (id BINARY(16) PRIMARY KEY, at_time DATETIME(6), doc JSON) ENGINE=InnoDB"); err != nil {
		t.Fatal(err)
	}
	id := UUID{0: 255, 15: 1}
	instant := DateTime{time.Date(2026, 10, 22, 12, 30, 0, 123456000, time.FixedZone("offset", 3600))}
	if _, err := db.ExecContext(ctx, "INSERT INTO "+name+" VALUES(?,?,?)", id, instant, `{"integer":9007199254740993,"value":null}`); err != nil {
		t.Fatal(err)
	}
	var actualID UUID
	var actualTime DateTime
	var integer string
	if err := db.QueryRowContext(ctx, "SELECT id,at_time,JSON_UNQUOTE(JSON_EXTRACT(doc,'$.integer')) FROM "+name+" WHERE id=?", id).Scan(&actualID, &actualTime, &integer); err != nil {
		t.Fatal(err)
	}
	if actualID != id || !actualTime.Equal(instant.Time) || integer != "9007199254740993" {
		t.Fatalf("scalar changed: %v %v %s", actualID, actualTime, integer)
	}
	var jsonNull, sqlNull sql.NullString
	if err := db.QueryRowContext(ctx, "SELECT JSON_TYPE(JSON_EXTRACT(doc,'$.value')), JSON_TYPE(NULL) FROM "+name).Scan(&jsonNull, &sqlNull); err != nil {
		t.Fatal(err)
	}
	if !jsonNull.Valid || jsonNull.String != "NULL" || sqlNull.Valid {
		t.Fatalf("JSON/SQL null confused: %v %v", jsonNull, sqlNull)
	}
}
