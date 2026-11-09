package mysql

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/velocity"
	"github.com/google/uuid"
)

func TestMySQLVelocitySummaryReaderExactWindowsAndFallbacks(t *testing.T) {
	f := tenantRegistryFixture(t)
	ctx := context.Background()
	schema, _ := mysqlstore.Identifier(f.f.database)
	qualified := schema + "." + f.physical
	statements, err := velocity.Schema(f.f.database)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range statements {
		if _, err := f.f.db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	anchor := time.Date(2026, 11, 6, 12, 3, 30, 0, time.UTC)
	start := anchor.Add(-5 * time.Minute)
	for _, row := range []struct {
		id, account string
		at          time.Time
		amount      int
	}{{"interior", "HOT", anchor.Add(-3 * time.Minute), 10}, {"lower", "HOT", start, 20}, {"upper", "HOT", anchor, 999}, {"end-fragment", "HOT", anchor.Add(-time.Microsecond), 30}, {"case", "hot", anchor.Add(-time.Minute), 1000}} {
		f.insert(t, row.id, row.account, "recipient", row.amount)
		if _, err := f.f.db.Exec("UPDATE "+qualified+" SET "+f.columns["updated_at"]+"=? WHERE "+f.columns["object_id"]+"=?", mysqlstore.DateTime{Time: row.at}, row.id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.f.db.Exec("INSERT INTO "+schema+".ingestion_velocity_definitions VALUES(?,?,?,?,?,?)", mysqlstore.UUID(f.tenant), mysqlstore.UUID(f.table), mysqlstore.UUID(f.version), "wallet_number_hash", "updated_at", "amount"); err != nil {
		t.Fatal(err)
	}
	bucket := "TIMESTAMP(DATE_FORMAT(" + f.columns["updated_at"] + ",'%Y-%m-%d %H:%i:00'))"
	if _, err := f.f.db.Exec("INSERT INTO "+schema+".ingestion_velocity_buckets SELECT ?,?,?,CAST("+f.columns["wallet_number_hash"]+" AS BINARY),"+bucket+",COUNT(*),COUNT("+f.columns["amount"]+"),SUM("+f.columns["amount"]+") FROM "+qualified+" GROUP BY CAST("+f.columns["wallet_number_hash"]+" AS BINARY),"+bucket, mysqlstore.UUID(f.tenant), mysqlstore.UUID(f.table), mysqlstore.UUID(f.version)); err != nil {
		t.Fatal(err)
	}
	raw := f.reader.WithModel(f.tenant.String(), f.model).(TenantDataReader)
	summary := raw.WithVelocitySummaries(true)
	filter := &ports.AggregateFilter{Kind: "group", Operator: "and", Children: []ports.AggregateFilter{
		{Kind: "predicate", Field: "wallet_number_hash", Op: "eq", Value: "HOT"},
		{Kind: "predicate", Field: "updated_at", Op: "gte", Value: start},
		{Kind: "predicate", Field: "updated_at", Op: "lt", Value: anchor},
	}}
	queries := []ports.AggregateQuery{{ObjectType: "wallets", Aggregate: "count", Field: "object_id", Filter: filter}, {ObjectType: "wallets", Aggregate: "sum", Field: "amount", Filter: filter}, {ObjectType: "wallets", Aggregate: "count", Field: "amount", Filter: filter}}
	traceCtx := ports.WithEvaluationTrace(ctx)
	values, err := summary.AggregateRecordsBatch(traceCtx, f.tenant.String(), queries)
	if err != nil || fmt.Sprint(values) != "[3 60 3]" {
		t.Fatal(values, err)
	}
	if ports.EvaluationTraceFromContext(traceCtx).Snapshot()["aggregate_sql_summary"] <= 0 {
		t.Fatal("summary path was not executed")
	}
	// The same physical query is the independent consumer-contract oracle;
	// ingestion owner atomic maintenance is tested in its owning module.
	for i, q := range queries {
		v, err := raw.AggregateRecords(ctx, f.tenant.String(), q)
		if err != nil || fmt.Sprint(v) != fmt.Sprint(values[i]) {
			t.Fatal(v, values[i], err)
		}
	}
	for _, kind := range []string{"avg", "count_distinct"} {
		q := queries[1]
		q.Aggregate = kind
		c := ports.WithEvaluationTrace(ctx)
		value, err := summary.AggregateRecords(c, f.tenant.String(), q)
		want, e := raw.AggregateRecords(ctx, f.tenant.String(), q)
		if err != nil || e != nil || fmt.Sprint(value) != fmt.Sprint(want) || ports.EvaluationTraceFromContext(c).Snapshot()["aggregate_sql_summary"] != 0 {
			t.Fatal("unsupported summary used", kind, value, err)
		}
	}
	filter.Children = append(filter.Children, ports.AggregateFilter{Kind: "predicate", Field: "amount", Op: "gt", Value: 0})
	c := ports.WithEvaluationTrace(ctx)
	if value, err := summary.AggregateRecords(c, f.tenant.String(), queries[1]); err != nil || fmt.Sprint(value) != "60" || ports.EvaluationTraceFromContext(c).Snapshot()["aggregate_sql_summary"] != 0 {
		t.Fatal("extra predicate ignored", value, err)
	}
	filter.Children = filter.Children[:3]
	if _, err := f.f.db.Exec("DELETE FROM " + schema + ".ingestion_velocity_buckets"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.f.db.Exec("UPDATE "+schema+".ingestion_velocity_definitions SET published_version=?", mysqlstore.UUID(uuid.New())); err != nil {
		t.Fatal(err)
	}
	c = ports.WithEvaluationTrace(ctx)
	if value, err := summary.AggregateRecords(c, f.tenant.String(), queries[1]); err != nil || fmt.Sprint(value) != "60" || ports.EvaluationTraceFromContext(c).Snapshot()["aggregate_sql_summary"] != 0 {
		t.Fatal("stale version used", value, err)
	}
}
