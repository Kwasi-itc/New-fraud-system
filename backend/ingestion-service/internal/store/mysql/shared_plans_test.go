package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
)

func TestFullSharedReadPlans(t *testing.T) {
	shared, f := fullRecordFixture(t)
	for offset := 0; offset < 1000; offset += 250 {
		records := make([]map[string]any, 250)
		for i := range records {
			n := int64(offset + i)
			records[i] = map[string]any{"object_id": fmt.Sprintf("record-%04d", n), "number": n, "amount": float64(n % 10), "ip": fmt.Sprintf("192.0.2.%d", n%256)}
		}
		if _, e := f.write(context.Background(), records); e != nil {
			t.Fatal(e)
		}
	}
	fixtureExec(t, f.db, "ANALYZE TABLE "+f.qualified)
	store, e := NewIngestionContractStore(f.db, shared.Database, 1<<20)
	if e != nil {
		t.Fatal(e)
	}
	ctx := WithServiceCredential(context.Background(), "fixture-token")
	e = store.WithRecordRead(ctx, f.model.TenantID, "records", "fixture-token", []string{"object_id", "number", "amount", "ip"}, func(ctx context.Context, tx *sql.Tx, p *RecordProjection) error {
		limits := RecordQueryLimits{100, 1 << 20, 1 << 20}
		bounds := RecordResultLimits{1024, 4096, 8192, 81920, 10}
		explain := func(name, query string, args []any) {
			t.Helper()
			var raw string
			if e := tx.QueryRowContext(ctx, "EXPLAIN FORMAT=JSON "+query, args...).Scan(&raw); e != nil {
				t.Fatal(name, e)
			}
			if !json.Valid([]byte(raw)) {
				t.Fatal(name, raw)
			}
			t.Logf("PLAN name=%s dataset=1000 query=%s args=%v plan=%s", name, query, args, raw)
		}
		var contractKey mysqlstore.UUID
		if e := tx.QueryRowContext(ctx, "SELECT table_name_key_id FROM "+shared.Database+".data_model_ingestion_contracts WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(f.model.TenantID), mysqlstore.UUID(f.model.Tables["records"].ID)).Scan(&contractKey); e != nil {
			return e
		}
		explain("contract_bound_key", store.contractReadSQL(), []any{1 << 20, mysqlstore.UUID{}, mysqlstore.UUID{}, 1 << 20, contractKey, mysqlstore.UUID(f.model.TenantID)})
		b, e := p.Bounded(bounds)
		if e != nil {
			return e
		}
		columns, table, args := b.SQL()
		args = append(args, mysqlstore.UUID(f.model.TenantID), mysqlstore.UUID(f.model.Tables["records"].ID), []byte("record-0500"), []byte("record-0500"))
		explain("exact_previous_scan", "SELECT "+columns+" FROM "+table+" WHERE record_object_key_id="+shared.Database+".data_model_find_record_object_key_v1(?,?,?) AND CAST("+p.objectColumn+" AS BINARY)=? LIMIT 1", args)
		key, found, e := FindRecordKey(ctx, tx, shared.Database, f.model.TenantID, f.model.Tables["records"].ID, "record-0500")
		if e != nil || !found {
			t.Fatal(key, found, e)
		}
		query, boundArgs, _, e := p.compileExactRead(key, "record-0500", bounds)
		if e != nil {
			return e
		}
		explain("exact_bound_key", query, boundArgs)
		var newPlan string
		if e := tx.QueryRowContext(ctx, "EXPLAIN FORMAT=JSON "+query, boundArgs...).Scan(&newPlan); e != nil {
			return e
		}
		var decoded struct {
			QueryBlock struct {
				Table struct {
					AccessType string `json:"access_type"`
					Rows       int    `json:"rows_examined_per_scan"`
				} `json:"table"`
			} `json:"query_block"`
		}
		if e := json.Unmarshal([]byte(newPlan), &decoded); e != nil {
			return e
		}
		if decoded.QueryBlock.Table.AccessType != "const" || decoded.QueryBlock.Table.Rows != 1 {
			t.Fatal("exact read lost unique-key plan", newPlan)
		}
		for _, tc := range []struct {
			name   string
			filter *ingestion.AggregateFilter
		}{{"unfiltered", nil}, {"unique_range", ptrFilter(filterLeaf("number", "gte", int64(990)))}, {"unindexed_amount", ptrFilter(filterLeaf("amount", "gte", float64(9)))}} {
			query, args, _, e := p.compileSelection(tc.filter, bounds, limits)
			if e != nil {
				return e
			}
			explain("selection_"+tc.name, query, args)
			query, args, e = p.compileCount("number", tc.filter, limits)
			if e != nil {
				return e
			}
			explain("count_"+tc.name, query, args)
			query, args, _, e = p.compileSum("amount", tc.filter, limits)
			if e != nil {
				return e
			}
			explain("sum_"+tc.name, query, args)
			for _, operation := range []string{"AVG", "MIN", "MAX", "COUNT DISTINCT"} {
				query, args, _, e := p.compileAggregate("number", operation, tc.filter, limits)
				if e != nil {
					return e
				}
				explain(operation+"_"+tc.name, query, args)
			}
			for _, operation := range []string{"MIN", "MAX", "COUNT DISTINCT"} {
				query, args, _, e = p.compileAggregate("object_id", operation, tc.filter, limits)
				if e != nil {
					return e
				}
				explain(operation+"_text_"+tc.name, query, args)
			}
			query, args, e = p.compileCount("ip", tc.filter, limits)
			if e != nil {
				return e
			}
			explain("count_ip_"+tc.name, query, args)
			query, args, _, e = p.compileAggregate("ip", "COUNT DISTINCT", tc.filter, limits)
			if e != nil {
				return e
			}
			explain("distinct_ip_"+tc.name, query, args)
			for _, field := range p.fields {
				if field.definition.DataType != "ip_address" {
					continue
				}
				for _, operation := range []string{"MIN", "MAX"} {
					query, args, e = p.compileIPExtreme(field, operation, tc.filter, limits)
					if e != nil {
						return e
					}
					explain(operation+"_ip_"+tc.name, query, args)
				}
			}
		}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
}
