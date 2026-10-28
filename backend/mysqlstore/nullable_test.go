package mysqlstore

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestNullableScalars(t *testing.T) {
	id := UUID{0: 255, 15: 1}
	instant := time.Date(2026, 10, 22, 12, 30, 0, 123456000, time.FixedZone("offset", 3600))
	var u NullUUID
	var d NullDateTime
	b, _ := id.Value()
	if err := u.Scan(b); err != nil || !u.Valid || u.UUID != id {
		t.Fatalf("UUID scan: %v %v", u, err)
	}
	if err := d.Scan(instant); err != nil || !d.Valid || !d.DateTime.Equal(instant) || d.DateTime.Location() != time.UTC {
		t.Fatalf("time scan: %v %v", d, err)
	}
	// NULL must clear previous data when a destination is reused for another row.
	if err := u.Scan(nil); err != nil || u != (NullUUID{}) {
		t.Fatalf("UUID NULL: %v %v", u, err)
	}
	if err := d.Scan(nil); err != nil || d != (NullDateTime{}) {
		t.Fatalf("time NULL: %v %v", d, err)
	}
	for _, bad := range []any{"text UUID", []byte{1}, 0} {
		u = NullUUID{UUID: id, Valid: true}
		if err := u.Scan(bad); err == nil || u != (NullUUID{}) {
			t.Fatalf("bad UUID retained state: %v %v", u, err)
		}
	}
	for _, bad := range []any{"2026-10-22", time.Time{}, instant.Add(time.Nanosecond)} {
		d = NullDateTime{DateTime: DateTime{instant}, Valid: true}
		if err := d.Scan(bad); err == nil || d != (NullDateTime{}) {
			t.Fatalf("bad time retained state: %v %v", d, err)
		}
	}
	if value, err := (NullUUID{UUID: id}).Value(); err != nil || value != nil {
		t.Fatalf("invalid UUID must bind NULL: %v %v", value, err)
	}
	if value, err := (NullDateTime{DateTime: DateTime{instant}}).Value(); err != nil || value != nil {
		t.Fatalf("invalid time must bind NULL: %v %v", value, err)
	}
	if _, err := (NullDateTime{Valid: true}).Value(); err == nil {
		t.Fatal("valid zero timestamp accepted")
	}
	var nilUUID *NullUUID
	var nilTime *NullDateTime
	if nilUUID.Scan(nil) == nil || nilTime.Scan(nil) == nil {
		t.Fatal("nil scan destination accepted")
	}
}

func TestMySQLNullableRoundTrip(t *testing.T) {
	db := integrationDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	name, _ := Identifier(fmt.Sprintf("nullable_%d", time.Now().UnixNano()))
	if _, err := db.ExecContext(ctx, "CREATE TABLE "+name+" (seq INT PRIMARY KEY, id BINARY(16) NULL, completed_at DATETIME(6) NULL) ENGINE=InnoDB"); err != nil {
		t.Fatal(err)
	}
	id := NullUUID{UUID: UUID{0: 255, 15: 1}, Valid: true}
	instant := NullDateTime{DateTime: DateTime{time.Date(2026, 10, 22, 12, 30, 0, 123456000, time.FixedZone("offset", 3600))}, Valid: true}
	if _, err := db.ExecContext(ctx, "INSERT INTO "+name+" VALUES (1,?,?),(2,?,?),(3,?,?)", id, instant, NullUUID{}, NullDateTime{}, NullUUID{Valid: true}, NullDateTime{}); err != nil {
		t.Fatal(err)
	}
	rows, err := db.QueryContext(ctx, "SELECT id,completed_at FROM "+name+" ORDER BY seq")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var actualID NullUUID
	var actualTime NullDateTime
	count := 0
	for rows.Next() {
		count++
		if err := rows.Scan(&actualID, &actualTime); err != nil {
			t.Fatal(err)
		}
		switch count {
		case 1:
			if actualID != id || !actualTime.Valid || !actualTime.DateTime.Equal(instant.DateTime.Time) || actualTime.DateTime.Location() != time.UTC {
				t.Fatalf("round trip mismatch: %v %v", actualID, actualTime)
			}
		case 2:
			if actualID != (NullUUID{}) || actualTime != (NullDateTime{}) {
				t.Fatal("NULL row retained prior values")
			}
		case 3:
			if !actualID.Valid || actualID.UUID != (UUID{}) || actualTime.Valid {
				t.Fatal("zero UUID confused with NULL")
			}
		}
	}
	if err := rows.Err(); err != nil || count != 3 {
		t.Fatalf("rows=%d error=%v", count, err)
	}
}
