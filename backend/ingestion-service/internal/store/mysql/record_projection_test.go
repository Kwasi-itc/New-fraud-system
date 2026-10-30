package mysql

import (
	"context"
	"database/sql"
	"errors"
	"net/netip"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func TestRecordProjectionRequestValidation(t *testing.T) {
	model := ingestion.PublishedDataModel{Tables: map[string]ingestion.ObjectSchema{"r": {Fields: map[string]ingestion.FieldSchema{"ok": {Name: "ok"}, "old": {Name: "old", Archived: true}, "mismatch": {Name: "different"}}}}}
	for _, names := range [][]string{nil, {}, make([]string, 101), {"ok", "ok"}, {"old"}, {"mismatch"}, {"missing"}, {"id"}, {"ok` FROM secrets;--"}} {
		p, err := NewRecordProjection(t.Context(), nil, "db", uuid.New(), model, "r", uuid.New(), names)
		if !errors.Is(err, ErrRecordProjection) || p != nil {
			t.Fatal(names, p, err)
		}
	}
}
func TestRecordProjectionMappingFailures(t *testing.T) {
	for _, scenario := range []string{"tenant", "version", "closed", "archived_field", "wrong_type", "missing_component"} {
		t.Run(scenario, func(t *testing.T) {
			f := recordFixture(t)
			model := f.model
			tenant := model.TenantID
			version := f.version
			switch scenario {
			case "tenant":
				tenant = uuid.New()
			case "version":
				version = uuid.New()
			case "closed":
				if _, err := f.db.Exec("UPDATE data_model_physical_tables SET write_state='closed'"); err != nil {
					t.Fatal(err)
				}
			case "archived_field":
				field := model.Tables["records"].Fields["number"]
				field.Archived = true
				model.Tables["records"].Fields["number"] = field
			case "wrong_type":
				field := model.Tables["records"].Fields["number"]
				field.DataType = "string"
				model.Tables["records"].Fields["number"] = field
			case "missing_component":
				if _, err := f.db.Exec("DELETE FROM data_model_physical_fields WHERE component='prefix'"); err != nil {
					t.Fatal(err)
				}
			}
			err := mysqlstore.Mutation(t.Context(), f.db, func(tx *sql.Tx) error {
				p, err := NewRecordProjection(t.Context(), tx, f.database, tenant, model, "records", version, []string{"number"})
				if p != nil || err == nil {
					t.Fatal(p, err)
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestRecordProjectionRoundTripAndCopies(t *testing.T) {
	f := recordFixture(t)
	ctx := context.Background()
	at := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	err := mysqlstore.Mutation(ctx, f.db, func(tx *sql.Tx) error {
		writer, err := NewRecordWriter(ctx, tx, f.database, f.model.TenantID, f.model, "records", f.version)
		if err != nil {
			return err
		}
		if _, err = writer.WriteBatch(ctx, []map[string]any{{"object_id": "one", "number": int64(7), "ip": "::ffff:192.0.2.9", "flag": true, "amount": 1.5}}, ingestion.ModePatch, at, 4096); err != nil {
			return err
		}
		names := []string{"ip", "number", "object_id", "updated_at", "flag", "amount"}
		p, err := NewRecordProjection(ctx, tx, f.database, f.model.TenantID, f.model, "records", f.version, names)
		if err != nil {
			return err
		}
		names[0] = "mutated"
		field := f.model.Tables["records"].Fields["number"]
		field.Name = "changed"
		f.model.Tables["records"].Fields["number"] = field
		columns, table := p.SQL()
		if strings.Contains(columns, "*") || strings.Contains(columns, "object_id") || p.ColumnCount() != 8 || table != f.qualified {
			t.Fatal(columns, table, p.ColumnCount())
		}
		raw := make([]any, p.ColumnCount())
		dest := make([]any, len(raw))
		for i := range raw {
			dest[i] = &raw[i]
		}
		if err := tx.QueryRowContext(ctx, "SELECT "+columns+" FROM "+table+" WHERE `"+f.columns["object_id"]["value"]+"`=?", "one").Scan(dest...); err != nil {
			return err
		}
		got, err := p.Decode(raw, 64)
		if err != nil {
			return err
		}
		want := map[string]any{"ip": netip.MustParsePrefix("::ffff:192.0.2.9/128"), "number": int64(7), "object_id": "one", "updated_at": at, "flag": true, "amount": 1.5}
		if !reflect.DeepEqual(got, want) {
			t.Fatal(got, want)
		}
		if result, err := p.Decode(raw[:1], 64); err == nil || result != nil {
			t.Fatal(result, err)
		}
		raw[3] = int64(1) << 40
		if result, err := p.Decode(raw, 64); err == nil || result != nil {
			t.Fatal("partial record", result, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
func TestRecordProjectionBarrierLifetime(t *testing.T) {
	f := recordFixture(t)
	ctx := context.Background()
	tx, err := f.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := NewRecordProjection(ctx, tx, f.database, f.model.TenantID, f.model, "records", f.version, []string{"number"}); err != nil {
		t.Fatal(err)
	}
	blocked, cancel := context.WithTimeout(ctx, 150*time.Millisecond)
	defer cancel()
	_, err = f.db.ExecContext(blocked, "UPDATE data_model_physical_tables SET write_state='closed'")
	if err == nil {
		t.Fatal("publication crossed projection lock")
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.ExecContext(ctx, "UPDATE data_model_physical_tables SET write_state='closed'"); err != nil {
		t.Fatal(err)
	}
}

func TestRecordProjectionLogicalNamesNeverBecomeSQL(t *testing.T) {
	f := recordFixture(t)
	logical := "number` FROM secrets;--"
	table := f.model.Tables["records"]
	field := table.Fields["number"]
	delete(table.Fields, "number")
	field.Name = logical
	table.Fields[logical] = field
	err := mysqlstore.Mutation(t.Context(), f.db, func(tx *sql.Tx) error {
		p, err := NewRecordProjection(t.Context(), tx, f.database, f.model.TenantID, f.model, "records", f.version, []string{logical})
		if err != nil {
			return err
		}
		columns, _ := p.SQL()
		if columns != "`"+f.columns["number"]["value"]+"`" || strings.Contains(columns, "secrets") {
			t.Fatal(columns)
		}
		got, err := p.Decode([]any{int64(7)}, 64)
		if err != nil || got[logical] != int64(7) {
			t.Fatal(got, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var zero RecordProjection
	if got, err := zero.Decode(nil, 64); err == nil || got != nil {
		t.Fatal("uninitialized projection", got, err)
	}
}
