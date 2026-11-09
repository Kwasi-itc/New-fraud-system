package mysql

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	asteval "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/runtime/ast_eval"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/velocity"
)

// Bounded native observations, not a latency assertion or HTTP capacity claim.
// The actual ingestion owner separately verifies atomic summary maintenance.
func TestMySQLAggregateOptimizationMeasurements(t *testing.T) {
	f := tenantRegistryFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	f.f.db.SetMaxOpenConns(8)
	f.f.db.SetMaxIdleConns(8)
	schema, _ := mysqlstore.Identifier(f.f.database)
	qualified := schema + "." + f.physical
	anchor := time.Date(2026, 11, 6, 12, 30, 30, 0, time.UTC)
	conn, err := f.f.db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, "SET SESSION cte_max_recursion_depth=50001"); err != nil {
		t.Fatal(err)
	}
	columns := f.columns
	statement := "INSERT INTO " + qualified + "(" + columns["object_id"] + "," + columns["updated_at"] + "," + columns["wallet_number_hash"] + "," + columns["user_id"] + "," + columns["amount"] + "," + columns["verified"] + ",record_object_key_id) WITH RECURSIVE sequence(n) AS(SELECT 0 UNION ALL SELECT n+1 FROM sequence WHERE n<49999) SELECT CONCAT('tx-',n),DATE_SUB(?,INTERVAL MOD(n,3600) SECOND),IF(MOD(n,10)=0,'HOT',CONCAT('account-',MOD(n,100))),CONCAT('recipient-',MOD(n,137)),MOD(n,1000),1,UUID_TO_BIN(UUID()) FROM sequence"
	if _, err := conn.ExecContext(ctx, statement, mysqlstore.DateTime{Time: anchor}); err != nil {
		t.Fatal(err)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := f.f.db.ExecContext(ctx, "ALTER TABLE "+qualified+" ADD INDEX aggregate_account_time("+columns["wallet_number_hash"]+","+columns["updated_at"]+")"); err != nil {
		t.Fatal(err)
	}
	statements, err := velocity.Schema(f.f.database)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range statements {
		if _, err := f.f.db.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.f.db.ExecContext(ctx, "INSERT INTO "+schema+".ingestion_velocity_definitions VALUES(?,?,?,?,?,?)", mysqlstore.UUID(f.tenant), mysqlstore.UUID(f.table), mysqlstore.UUID(f.version), "wallet_number_hash", "updated_at", "amount"); err != nil {
		t.Fatal(err)
	}
	bucket := "TIMESTAMP(DATE_FORMAT(" + columns["updated_at"] + ",'%Y-%m-%d %H:%i:00'))"
	if _, err := f.f.db.ExecContext(ctx, "INSERT INTO "+schema+".ingestion_velocity_buckets SELECT ?,?,?,CAST("+columns["wallet_number_hash"]+" AS BINARY),"+bucket+",COUNT(*),COUNT("+columns["amount"]+"),SUM("+columns["amount"]+") FROM "+qualified+" GROUP BY CAST("+columns["wallet_number_hash"]+" AS BINARY),"+bucket, mysqlstore.UUID(f.tenant), mysqlstore.UUID(f.table), mysqlstore.UUID(f.version)); err != nil {
		t.Fatal(err)
	}
	raw := f.reader.WithModel(f.tenant.String(), f.model).(TenantDataReader)
	summary := raw.WithVelocitySummaries(true)
	filter := &ports.AggregateFilter{Kind: "group", Operator: "and", Children: []ports.AggregateFilter{{Kind: "predicate", Field: "wallet_number_hash", Op: "eq", Value: "HOT"}, {Kind: "predicate", Field: "updated_at", Op: "gte", Value: anchor.Add(-time.Hour)}, {Kind: "predicate", Field: "updated_at", Op: "lt", Value: anchor}}}
	queries := []ports.AggregateQuery{{ObjectType: "wallets", Aggregate: "count", Field: "object_id", Filter: filter}, {ObjectType: "wallets", Aggregate: "sum", Field: "amount", Filter: filter}, {ObjectType: "wallets", Aggregate: "count", Field: "amount", Filter: filter}}
	count, sum := 0, 0
	for n := 0; n < 50000; n++ {
		if n%10 == 0 && n%3600 != 0 {
			count++
			sum += n % 1000
		}
	}
	want := fmt.Sprintf("[%d %d %d]", count, sum, count)
	report := struct {
		Rows     int                  `json:"rows"`
		Expected string               `json:"expected"`
		Paths    map[string][]float64 `json:"paths_ms"`
		Budgets  []map[string]any     `json:"budgets"`
	}{Rows: 50000, Expected: want, Paths: make(map[string][]float64)}
	for _, path := range []string{"separate_reads", "batch_read", "batch_summary"} {
		for sample := 0; sample < 13; sample++ {
			started := time.Now()
			var values []any
			var err error
			switch path {
			case "separate_reads":
				for _, query := range queries {
					v, e := raw.AggregateRecords(ctx, f.tenant.String(), query)
					if e != nil {
						err = e
						break
					}
					values = append(values, v)
				}
			case "batch_read":
				values, err = raw.AggregateRecordsBatch(ctx, f.tenant.String(), queries)
			case "batch_summary":
				values, err = summary.AggregateRecordsBatch(ctx, f.tenant.String(), queries)
			}
			if err != nil || fmt.Sprint(values) != want {
				t.Fatal(path, values, want, err)
			}
			if sample >= 3 {
				report.Paths[path] = append(report.Paths[path], float64(time.Since(started).Microseconds())/1000)
			}
		}
	}
	for _, limit := range []int{2, 4, 8} {
		executor, err := asteval.NewAggregateExecutor(limit, 2*time.Second, 150*time.Microsecond)
		if err != nil {
			t.Fatal(err)
		}
		started := time.Now()
		var group sync.WaitGroup
		failures := make(chan error, 96)
		controlSamples := []float64{}
		controlErrors := 0
		group.Add(1)
		go func() {
			defer group.Done()
			for range 32 {
				started := time.Now()
				var value int
				control, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
				err := f.f.db.QueryRowContext(control, "SELECT 1").Scan(&value)
				cancel()
				if err != nil || value != 1 {
					controlErrors++
				}
				controlSamples = append(controlSamples, float64(time.Since(started).Microseconds())/1000)
				time.Sleep(time.Millisecond)
			}
		}()
		for range 32 {
			group.Add(1)
			go func() {
				defer group.Done()
				batcher := asteval.NewAggregateBatcher(ctx, f.tenant.String(), summary, executor)
				var calls sync.WaitGroup
				for _, query := range queries {
					calls.Add(1)
					go func(q ports.AggregateQuery) {
						defer calls.Done()
						value, err := batcher.Evaluate(ctx, q)
						expected := count
						if q.Aggregate == "sum" {
							expected = sum
						}
						if err == nil && fmt.Sprint(value) != fmt.Sprint(expected) {
							t.Errorf("wrong concurrent aggregate value %v", value)
						}
						if err != nil {
							failures <- err
						}
					}(query)
				}
				calls.Wait()
			}()
		}
		group.Wait()
		close(failures)
		errors := 0
		for range failures {
			errors++
		}
		elapsed := time.Since(started)
		batches, demands := executor.Stats()
		report.Budgets = append(report.Budgets, map[string]any{"slots": limit, "offered_evaluations": 32, "aggregate_demands": demands, "data_query_batches": batches, "errors": errors, "elapsed_ms": float64(elapsed.Microseconds()) / 1000, "control_read_ms": controlSamples, "control_errors": controlErrors})
	}
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("AGGREGATE_OPTIMIZATION_EVIDENCE %s", data)
}
