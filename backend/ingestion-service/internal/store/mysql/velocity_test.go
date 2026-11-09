package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/velocity"
	"github.com/google/uuid"
)

func velocityOwnerFixture(t *testing.T) (recordFixtureState, *KeyedRecordOwner, velocity.Definition) {
	t.Helper()
	f, owner, _ := ownerFixture(t)
	schema, _ := mysqlstore.Identifier(f.database)
	table := f.model.Tables["records"]
	for _, spec := range []struct{ name, kind, sql string }{{"account", "string", "LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin NULL"}, {"event_at", "timestamp", "DATETIME(6) NULL"}, {"fraud_amount", "int", "INT NULL"}} {
		id := uuid.New()
		if _, err := f.db.Exec("INSERT INTO "+schema+".data_model_fields(tenant_id,table_id,id,data_type) VALUES(?,?,?,?)", mysqlstore.UUID(f.model.TenantID), mysqlstore.UUID(table.ID), mysqlstore.UUID(id), spec.kind); err != nil {
			t.Fatal(err)
		}
		if _, err := f.db.Exec("INSERT INTO "+schema+".data_model_physical_fields(tenant_id,table_id,field_id,component,field_label) VALUES(?,?,?,'value','velocity')", mysqlstore.UUID(f.model.TenantID), mysqlstore.UUID(table.ID), mysqlstore.UUID(id)); err != nil {
			t.Fatal(err)
		}
		var column string
		if err := f.db.QueryRow("SELECT physical_name FROM "+schema+".data_model_physical_fields WHERE tenant_id=? AND table_id=? AND field_id=?", mysqlstore.UUID(f.model.TenantID), mysqlstore.UUID(table.ID), mysqlstore.UUID(id)).Scan(&column); err != nil {
			t.Fatal(err)
		}
		quoted, err := mysqlstore.Identifier(column)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.db.Exec("ALTER TABLE " + f.qualified + " ADD COLUMN " + quoted + " " + spec.sql); err != nil {
			t.Fatal(err)
		}
		table.Fields[spec.name] = ingestion.FieldSchema{ID: id, Name: spec.name, DataType: spec.kind, Nullable: true}
		f.columns[spec.name] = map[string]string{"value": column}
	}
	f.model.Tables["records"] = table
	statements, err := velocity.Schema(f.database)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range statements {
		if _, err := f.db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	d := velocity.Definition{Tenant: f.model.TenantID, Table: table.ID, Version: f.version, GroupField: "account", TimeField: "event_at", AmountField: "fraud_amount"}
	return f, owner.WithVelocitySummaries(), d
}

func TestVelocityOwnerReplayCorrectionsLateEventsRollbackAndConcurrentReaders(t *testing.T) {
	f, owner, definition := velocityOwnerFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	anchor := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	command := func(id, account string, at time.Time, amount any) ingestion.ReceiptCommand {
		return ingestion.ReceiptCommand{TenantID: definition.Tenant, TableID: definition.Table, Mode: ingestion.ModePatch, Kind: "single", Records: []map[string]any{{"object_id": id, "account": account, "event_at": at.Format(time.RFC3339Nano), "fraud_amount": amount}}}
	}
	if err := owner.RebuildVelocity(ctx, definition, 1000); err != nil {
		t.Fatal(err)
	}
	read := func(group string, start, end time.Time, aggregate, field string, projected bool) (int64, error) {
		column := func(name string) string { return "`" + f.columns[name]["value"] + "`" }
		expression := "COUNT(" + column(field) + ")"
		if aggregate == "sum" {
			expression = "COALESCE(SUM(" + column(field) + "),0)"
		}
		where := " WHERE CAST(" + column("account") + " AS BINARY)=CAST(? AS BINARY) AND " + column("event_at") + ">=? AND " + column("event_at") + "<?"
		args := []any{mysqlstore.Text(group), mysqlstore.DateTime{Time: start}, mysqlstore.DateTime{Time: end}}
		statement := "SELECT " + expression + " FROM " + f.qualified + where
		if projected {
			expr, bound, ok, err := velocity.AggregateExpression(f.database, f.qualified, expression, where, column("event_at"), definition, []byte(group), start, end, args, aggregate, field)
			if err != nil {
				return 0, err
			}
			if !ok {
				return 0, errors.New("eligible projection required")
			}
			statement, args = "SELECT "+expr, bound
		}
		var raw any
		if err := f.db.QueryRowContext(ctx, statement, args...).Scan(&raw); err != nil {
			return 0, err
		}
		switch v := raw.(type) {
		case []byte:
			return strconv.ParseInt(string(v), 10, 64)
		case int64:
			return v, nil
		}
		return 0, fmt.Errorf("unexpected %T", raw)
	}
	write := func(c ingestion.ReceiptCommand, key string) {
		t.Helper()
		if _, err := owner.Execute(ctx, c, key); err != nil {
			t.Fatal(err)
		}
	}
	first := command("first", "HOT", anchor.Add(-2*time.Minute), json.Number("10"))
	write(first, "first")
	write(first, "first")
	write(command("edge-lower", "HOT", anchor.Add(-5*time.Minute), json.Number("20")), "lower")
	write(command("edge-upper", "HOT", anchor, json.Number("999")), "upper")
	write(command("null", "HOT", anchor.Add(-time.Minute), nil), "null")
	write(command("late", "HOT", anchor.Add(-time.Hour), json.Number("40")), "late")
	write(command("case", "hot", anchor.Add(-time.Minute), json.Number("500")), "case")
	assertWindow := func(group string, start, end time.Time, aggregate, field string, want int64) {
		t.Helper()
		raw, err := read(group, start, end, aggregate, field, false)
		if err != nil {
			t.Fatal(err)
		}
		projected, err := read(group, start, end, aggregate, field, true)
		if err != nil || raw != want || projected != raw {
			t.Fatal(group, aggregate, field, raw, projected, want, err)
		}
	}
	assertWindow("HOT", anchor.Add(-5*time.Minute), anchor, "count", "object_id", 3)
	assertWindow("HOT", anchor.Add(-5*time.Minute), anchor, "count", "fraud_amount", 2)
	assertWindow("HOT", anchor.Add(-5*time.Minute), anchor, "sum", "fraud_amount", 30)
	assertWindow("HOT", anchor.Add(-5*time.Minute+time.Microsecond), anchor.Add(-time.Microsecond), "sum", "fraud_amount", 10)
	assertWindow("HOT", anchor.Add(-2*time.Hour), anchor, "sum", "fraud_amount", 70)
	// Correction changes group, amount and event-time; null transitions and
	// unchanged replays must subtract/add exact contributions only once.
	write(command("first", "OTHER", anchor.Add(-3*time.Minute), json.Number("15")), "correct-first")
	write(command("null", "HOT", anchor.Add(-time.Minute), json.Number("7")), "fill-null")
	assertWindow("HOT", anchor.Add(-5*time.Minute), anchor, "sum", "fraud_amount", 27)
	assertWindow("OTHER", anchor.Add(-5*time.Minute), anchor, "sum", "fraud_amount", 15)
	write(command("null", "HOT", anchor.Add(-time.Minute), nil), "clear-null")
	assertWindow("HOT", anchor.Add(-5*time.Minute), anchor, "sum", "fraud_amount", 20)
	write(command("null", "HOT", anchor.Add(-time.Minute), json.Number("7")), "restore-null")
	// A failure after physical data/summary changes must roll back both and
	// leave the idempotency key available for a subsequent successful attempt.
	failure := errors.New("forced owner rollback")
	rejected := command("rollback", "HOT", anchor.Add(-time.Minute), json.Number("500"))
	if result, err := owner.execute(ctx, rejected, "rollback", recordHooks{after: func(context.Context, *sql.Tx, ReceiptResult) error { return failure }}); !errors.Is(err, failure) || result.Payload != nil {
		t.Fatal(result, err)
	}
	assertWindow("HOT", anchor.Add(-5*time.Minute), anchor, "sum", "fraud_amount", 27)
	write(rejected, "rollback")
	assertWindow("HOT", anchor.Add(-5*time.Minute), anchor, "sum", "fraud_amount", 527)
	// One SQL statement combines base boundaries and summary interior. Under
	// concurrent atomic corrections every observed sum must be a whole state.
	var group sync.WaitGroup
	failures := make(chan error, 2)
	group.Add(1)
	go func() {
		defer group.Done()
		for i := 0; i < 12; i++ {
			at := anchor.Add(-3 * time.Minute)
			if i%2 != 0 {
				at = anchor.Add(-time.Microsecond)
			}
			c := command("first", "OTHER", at, json.Number(fmt.Sprint(15+i%2)))
			if _, err := owner.Execute(ctx, c, fmt.Sprintf("concurrent-%d", i)); err != nil {
				failures <- err
				return
			}
		}
	}()
	for i := 0; i < 24; i++ {
		value, err := read("OTHER", anchor.Add(-5*time.Minute+time.Microsecond), anchor.Add(-time.Microsecond), "sum", "fraud_amount", true)
		if err != nil || value != 15 && value != 16 {
			failures <- fmt.Errorf("incoherent state %d: %v", value, err)
			break
		}
	}
	group.Wait()
	close(failures)
	for err := range failures {
		t.Fatal(err)
	}
	if err := owner.RebuildVelocity(ctx, definition, 2); err == nil {
		t.Fatal("partial backfill accepted")
	}
	assertWindow("HOT", anchor.Add(-5*time.Minute), anchor, "sum", "fraud_amount", 527)
	unsupported := definition
	unsupported.AmountField = "amount" // Existing float field.
	if err := owner.RebuildVelocity(ctx, unsupported, 1000); !errors.Is(err, ErrWriteMapping) {
		t.Fatal("float projection accepted", err)
	}
	assertWindow("HOT", anchor.Add(-5*time.Minute), anchor, "sum", "fraud_amount", 527)
	foreign := definition
	foreign.Tenant = uuid.New()
	if err := owner.RebuildVelocity(ctx, foreign, 1000); !errors.Is(err, ErrOwnerAuthorization) {
		t.Fatal(err)
	}
	// Keys beyond the bounded exact group are intentionally absent from buckets;
	// the decision adapter must use ordinary SQL for these inputs.
	long := strings.Repeat("x", 257)
	write(command("long", long, anchor.Add(-time.Minute), json.Number("77")), "long")
	if _, _, ok, err := velocity.AggregateExpression(f.database, f.qualified, "COUNT(*)", " WHERE TRUE", "event_at", definition, []byte(long), anchor.Add(-time.Hour), anchor, nil, "count", "object_id"); err != nil || ok {
		t.Fatal("long group used projection", ok, err)
	}
}
