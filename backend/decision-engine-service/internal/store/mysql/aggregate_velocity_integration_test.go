package mysql

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	domainast "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/ast"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	asteval "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/runtime/ast_eval"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

// A native consumer-contract workload, not an ingestion/owner-publication load
// test. Never touches application data. Both plans use the same table and index.
func TestMySQLVelocityHistoryPlansAndExactResults(t *testing.T) {
	f := tenantRegistryFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	schema, _ := mysqlstore.Identifier(f.f.database)
	qualified := schema + "." + f.physical
	accountColumn := f.columns["wallet_number_hash"]
	if _, err := f.f.db.ExecContext(ctx, "ALTER TABLE "+qualified+" DROP INDEX wallet_lookup, MODIFY "+accountColumn+" LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin NOT NULL, ADD INDEX velocity_account_time("+accountColumn+"(191),"+f.columns["updated_at"]+")"); err != nil {
		t.Fatal(err)
	}
	anchor := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	type record struct {
		account, recipient string
		at                 time.Time
		amount             int
	}
	rows := make([]record, 0, 50010)
	for n := 0; n < 50000; n++ {
		account := fmt.Sprintf("account-%03d", n%100)
		if n%10 == 0 {
			account = "HOT"
		}
		rows = append(rows, record{account, fmt.Sprintf("recipient-%03d", (n/10)%137), anchor.Add(-time.Duration(n+1) * time.Second), n%1000 + 1})
	}
	// Half-open windows, exact Unicode/case/space and full-prefix collisions.
	long := strings.Repeat("x", 210)
	rows = append(rows, record{"HOT", "edge", anchor.Add(-5 * time.Minute), 9001}, record{"HOT", "upper", anchor, 9999}, record{"HOT", "future", anchor.Add(time.Microsecond), 9999}, record{"hot", "case", anchor.Add(-time.Second), 9999}, record{"HOT ", "space", anchor.Add(-time.Second), 9999}, record{long + "A", "one", anchor.Add(-time.Second), 11}, record{long + "B", "two", anchor.Add(-time.Second), 22}, record{"账户😀", "unicode", anchor.Add(-time.Second), 33})
	columns := []string{f.columns["object_id"], f.columns["updated_at"], accountColumn, f.columns["user_id"], f.columns["amount"], f.columns["verified"], "record_object_key_id"}
	for start := 0; start < len(rows); start += 250 {
		batch := rows[start:min(start+250, len(rows))]
		marks, args := make([]string, len(batch)), make([]any, 0, len(batch)*7)
		for i, row := range batch {
			marks[i] = "(?,?,?,?,?,?,?)"
			args = append(args, fmt.Sprintf("velocity-%06d", start+i), mysqlstore.DateTime{Time: row.at}, row.account, row.recipient, row.amount, 1, mysqlstore.UUID(uuid.New()))
		}
		if _, err := f.f.db.ExecContext(ctx, "INSERT INTO "+qualified+"("+strings.Join(columns, ",")+") VALUES"+strings.Join(marks, ","), args...); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.f.db.ExecContext(ctx, "ANALYZE TABLE "+qualified); err != nil {
		t.Fatal(err)
	}
	reader := f.reader.WithModel(f.tenant.String(), f.model).(TenantDataReader)
	predicate := func(field, op string, value any) ports.AggregateFilter {
		return ports.AggregateFilter{Kind: "predicate", Field: field, Op: op, Value: value}
	}
	query := func(account, aggregate, field string, window time.Duration) ports.AggregateQuery {
		return ports.AggregateQuery{ObjectType: "wallets", Aggregate: aggregate, Field: field, Filter: &ports.AggregateFilter{Kind: "group", Operator: "and", Children: []ports.AggregateFilter{
			predicate("wallet_number_hash", "eq", account), {Kind: "group", Operator: "and", Children: []ports.AggregateFilter{predicate("updated_at", "gte", anchor.Add(-window)), predicate("updated_at", "lt", anchor)}},
		}}}
	}
	type sample struct {
		Name                        string `json:"name"`
		BeforePlan, AfterPlan       json.RawMessage
		BeforeAnalyze, AfterAnalyze string
		BeforeMS, AfterMS, ReaderMS []float64
	}
	report := struct {
		Rows    int      `json:"rows"`
		Samples []sample `json:"samples"`
	}{Rows: len(rows)}
	for _, tc := range []struct {
		name, aggregate, field string
		window                 time.Duration
	}{
		{"count_5m", "count", "object_id", 5 * time.Minute}, {"sum_1h", "sum", "amount", time.Hour}, {"distinct_recipients_24h", "count_distinct", "user_id", 24 * time.Hour},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q := query("HOT", tc.aggregate, tc.field, tc.window)
			want, distinct := 0, map[string]bool{}
			for _, row := range rows {
				if row.account != "HOT" || row.at.Before(anchor.Add(-tc.window)) || !row.at.Before(anchor) {
					continue
				}
				switch tc.aggregate {
				case "count":
					want++
				case "sum":
					want += row.amount
				case "count_distinct":
					distinct[row.recipient] = true
				}
			}
			if tc.aggregate == "count_distinct" {
				want = len(distinct)
			}
			// Run a velocity fraud rule through the real evaluator as well as
			// checking scalar SQL. Resolve the account from each request payload.
			threshold := 25
			if tc.aggregate == "sum" {
				threshold = 100000
			}
			if tc.aggregate == "count_distinct" {
				threshold = 100
			}
			filter := func(field, op string, value domainast.Node) domainast.Node {
				return domainast.Node{Function: "Filter", NamedChildren: map[string]domainast.Node{"tableName": {Constant: "wallets"}, "fieldName": {Constant: field}, "operator": {Constant: op}, "value": value}}
			}
			aggregateNode := domainast.Node{Function: "Aggregator", NamedChildren: map[string]domainast.Node{
				"tableName": {Constant: "wallets"}, "fieldName": {Constant: tc.field}, "aggregator": {Constant: tc.aggregate},
				"filters": {Function: "And", Children: []domainast.Node{
					filter("wallet_number_hash", "=", domainast.Node{Function: "payload", Children: []domainast.Node{{Constant: "wallet_number_hash"}}}),
					filter("updated_at", ">=", domainast.Node{Constant: anchor.Add(-tc.window)}), filter("updated_at", "<", domainast.Node{Constant: anchor}),
				}},
			}}
			for _, account := range []string{"HOT", "absent"} {
				rule := domainast.Node{Function: "gt", Children: []domainast.Node{aggregateNode, {Constant: threshold}}}
				got, err := asteval.EvaluateNode(ctx, rule, asteval.Runtime{TenantID: f.tenant.String(), ObjectType: "wallets", Model: &f.model, Fields: map[string]any{"wallet_number_hash": account}, Now: anchor, TenantDataReader: reader, AggregateResultCache: asteval.NewAggregateResultCache()})
				if err != nil || got != (account == "HOT" && want > threshold) {
					t.Fatal("velocity fraud rule", account, got, err)
				}
			}
			var sql string
			var args []any
			if err := reader.read(ctx, f.tenant.String(), "wallets", func(repo authoringRepository, table registryTable) error {
				var err error
				sql, args, err = table.compileAggregate(q, repo.maxBytes)
				return err
			}); err != nil {
				t.Fatal(err)
			}
			// Exact predicate used before this optimization, with the same
			// published prefix index and the same complete-value authority.
			indexed := "(" + accountColumn + "=? COLLATE utf8mb4_0900_bin AND CAST(" + accountColumn + " AS BINARY)=CAST(? AS BINARY))"
			legacy := strings.Replace(sql, indexed, "CAST("+accountColumn+" AS BINARY)=CAST(? AS BINARY)", 1)
			if legacy == sql {
				t.Fatal("baseline predicate not replaced")
			}
			legacyArgs := args[1:]
			s := sample{Name: tc.name}
			for _, version := range []struct {
				sql     string
				args    []any
				plan    *json.RawMessage
				analyze *string
				timings *[]float64
			}{
				{legacy, legacyArgs, &s.BeforePlan, &s.BeforeAnalyze, &s.BeforeMS}, {sql, args, &s.AfterPlan, &s.AfterAnalyze, &s.AfterMS},
			} {
				var plan []byte
				if err := f.f.db.QueryRowContext(ctx, "EXPLAIN FORMAT=JSON "+version.sql, version.args...).Scan(&plan); err != nil {
					t.Fatal(err)
				}
				*version.plan = append(json.RawMessage{}, plan...)
				if err := f.f.db.QueryRowContext(ctx, "EXPLAIN ANALYZE "+version.sql, version.args...).Scan(version.analyze); err != nil {
					t.Fatal(err)
				}
				for i := 0; i < 13; i++ {
					start := time.Now()
					var got any
					if err := f.f.db.QueryRowContext(ctx, version.sql, version.args...).Scan(&got); err != nil {
						t.Fatal(err)
					}
					elapsed := float64(time.Since(start).Microseconds()) / 1000
					var actual string
					switch v := got.(type) {
					case []byte:
						actual = string(v)
					default:
						actual = fmt.Sprint(v)
					}
					if actual != fmt.Sprint(want) {
						t.Fatal(actual, "want", want)
					}
					if i >= 3 {
						*version.timings = append(*version.timings, elapsed)
					}
				}
			}
			if !strings.Contains(string(s.AfterPlan), `"key": "velocity_account_time"`) || strings.Contains(string(s.AfterPlan), `"access_type": "ALL"`) {
				t.Fatal("indexed plan required", string(s.AfterPlan))
			}
			for i := 0; i < 10; i++ {
				start := time.Now()
				got, err := reader.AggregateRecords(ctx, f.tenant.String(), q)
				if err != nil || fmt.Sprint(got) != fmt.Sprint(want) {
					t.Fatal(got, err, "want", want)
				}
				s.ReaderMS = append(s.ReaderMS, float64(time.Since(start).Microseconds())/1000)
			}
			report.Samples = append(report.Samples, s)
		})
	}
	for _, tc := range []struct {
		account string
		want    int
	}{{long + "A", 1}, {long + "B", 1}, {"hot", 1}, {"HOT ", 1}, {"账户😀", 1}, {"absent", 0}} {
		got, err := reader.AggregateRecords(ctx, f.tenant.String(), query(tc.account, "count", "object_id", time.Hour))
		if err != nil || fmt.Sprint(got) != fmt.Sprint(tc.want) {
			t.Fatal("exact account", tc.account, got, err)
		}
	}
	// Exercise the actual AST compiler/evaluator with dynamic payload values;
	// its request-local cache must not leak the HOT result to another payload.
	filterNode := func(field, op string, value domainast.Node) domainast.Node {
		return domainast.Node{Function: "Filter", NamedChildren: map[string]domainast.Node{"tableName": {Constant: "wallets"}, "fieldName": {Constant: field}, "operator": {Constant: op}, "value": value}}
	}
	node := domainast.Node{Function: "Aggregator", NamedChildren: map[string]domainast.Node{"tableName": {Constant: "wallets"}, "fieldName": {Constant: "object_id"}, "aggregator": {Constant: "COUNT"}, "filters": {Function: "And", Children: []domainast.Node{
		filterNode("wallet_number_hash", "=", domainast.Node{Function: "payload", Children: []domainast.Node{{Constant: "wallet_number_hash"}}}),
		filterNode("updated_at", ">=", domainast.Node{Constant: anchor.Add(-5 * time.Minute)}), filterNode("updated_at", "<", domainast.Node{Constant: anchor}),
	}}}}
	cache := asteval.NewAggregateResultCache()
	for _, account := range []string{"HOT", "absent", "HOT"} {
		runtime := asteval.Runtime{TenantID: f.tenant.String(), ObjectType: "wallets", Model: &f.model, Fields: map[string]any{"wallet_number_hash": account}, Now: anchor, TenantDataReader: reader, AggregateResultCache: cache}
		got, err := asteval.EvaluateNode(ctx, node, runtime)
		want, lookupErr := reader.AggregateRecords(ctx, f.tenant.String(), query(account, "count", "object_id", 5*time.Minute))
		if err != nil || lookupErr != nil || fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatal("AST evaluation", got, err, want, lookupErr)
		}
	}
	if _, err := reader.AggregateRecords(ctx, uuid.NewString(), query("HOT", "count", "object_id", time.Hour)); !errors.Is(err, ErrTenantScope) {
		t.Fatal("tenant isolation", err)
	}
	cancelled, stop := context.WithCancel(ctx)
	stop()
	if _, err := reader.AggregateRecords(cancelled, f.tenant.String(), query("HOT", "count", "object_id", time.Hour)); !errors.Is(err, context.Canceled) {
		t.Fatal("cancel", err)
	}
	if _, err := f.f.db.ExecContext(ctx, "UPDATE "+schema+".data_model_physical_tables SET write_state='closed'"); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.AggregateRecords(ctx, f.tenant.String(), query("HOT", "count", "object_id", time.Hour)); !errors.Is(err, ErrPublishedModelUnavailable) {
		t.Fatal("publication barrier", err)
	}
	// Timings are observations, never pass/fail thresholds on shared hardware.
	for i := range report.Samples {
		sort.Float64s(report.Samples[i].BeforeMS)
		sort.Float64s(report.Samples[i].AfterMS)
		sort.Float64s(report.Samples[i].ReaderMS)
	}
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("VELOCITY_EVIDENCE %s", data)
}
