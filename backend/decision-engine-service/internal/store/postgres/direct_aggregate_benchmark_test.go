package postgres

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// This opt-in benchmark gate compares buffer work rather than machine-specific
// latency. Set DECISION_ENGINE_BENCHMARK_DATABASE_URL to run it.
func TestAggregateSmallRangeUsesFewerBuffersThanFullRangeBenchmark(t *testing.T) {
	databaseURL := os.Getenv("DECISION_ENGINE_BENCHMARK_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("set DECISION_ENGINE_BENCHMARK_DATABASE_URL to run aggregate benchmark")
	}

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()

	statements := []string{`
		CREATE TEMP TABLE aggregate_range_benchmark (
			id bigint PRIMARY KEY,
			occurred_at timestamptz NOT NULL,
			amount double precision NOT NULL
		)`, `
		INSERT INTO aggregate_range_benchmark (id, occurred_at, amount)
		SELECT g,
		       timestamptz '2025-01-01 00:00:00+00' + (((g * 157) % 31536000) * interval '1 second'),
		       (g % 10000)::double precision / 100
		FROM generate_series(1, 200000) AS g
		ORDER BY md5(g::text)`, `
		CREATE INDEX aggregate_range_benchmark_occurred_idx
			ON aggregate_range_benchmark (occurred_at)`, `
		ANALYZE aggregate_range_benchmark`,
	}
	for _, statement := range statements {
		if _, err := conn.Exec(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}

	small := explainAggregatePlan(t, ctx, conn, `
		SELECT count(amount)
		FROM aggregate_range_benchmark
		WHERE occurred_at >= timestamptz '2025-02-01 00:00:00+00'
		  AND occurred_at <  timestamptz '2025-02-02 00:00:00+00'
	`)
	full := explainAggregatePlan(t, ctx, conn, `
		SELECT count(amount)
		FROM aggregate_range_benchmark
		WHERE occurred_at >= timestamptz '2025-01-01 00:00:00+00'
		  AND occurred_at <  timestamptz '2026-01-01 00:00:00+00'
	`)

	if !small.usesIndex {
		t.Fatalf("small-range plan did not use an index or bitmap index plan: %s", small.plan)
	}
	if full.buffers <= 0 || small.buffers*5 >= full.buffers {
		t.Fatalf(
			"small/full buffer work = %d/%d, want small range below 20%% of full range",
			small.buffers,
			full.buffers,
		)
	}
}

type aggregateExplainResult struct {
	buffers   int64
	usesIndex bool
	plan      string
}

func explainAggregatePlan(t *testing.T, ctx context.Context, conn *pgxpool.Conn, query string) aggregateExplainResult {
	t.Helper()
	var payload []byte
	if err := conn.QueryRow(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+query).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	var document []map[string]any
	if err := json.Unmarshal(payload, &document); err != nil {
		t.Fatal(err)
	}
	plan, _ := document[0]["Plan"].(map[string]any)
	buffers, usesIndex := inspectAggregatePlan(plan)
	return aggregateExplainResult{buffers: buffers, usesIndex: usesIndex, plan: string(payload)}
}

func inspectAggregatePlan(plan map[string]any) (int64, bool) {
	nodeType, _ := plan["Node Type"].(string)
	usesIndex := strings.Contains(nodeType, "Index") || strings.Contains(nodeType, "Bitmap")
	buffers := jsonInt64(plan["Shared Hit Blocks"]) + jsonInt64(plan["Shared Read Blocks"])
	children, _ := plan["Plans"].([]any)
	for _, child := range children {
		childPlan, _ := child.(map[string]any)
		childBuffers, childUsesIndex := inspectAggregatePlan(childPlan)
		buffers += childBuffers
		usesIndex = usesIndex || childUsesIndex
	}
	return buffers, usesIndex
}

func jsonInt64(value any) int64 {
	number, _ := value.(float64)
	return int64(number)
}
