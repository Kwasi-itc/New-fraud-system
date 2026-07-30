package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

type aggregateTestDB struct {
	rows []aggregateTestRow
	sql  []string
}

type aggregateTestRow struct {
	values []any
	err    error
}

func (db *aggregateTestDB) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	db.sql = append(db.sql, sql)
	if len(db.rows) == 0 {
		return aggregateTestRow{err: errors.New("unexpected QueryRow")}
	}
	row := db.rows[0]
	db.rows = db.rows[1:]
	return row
}

func (db *aggregateTestDB) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("unexpected Query")
}

func (db *aggregateTestDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, errors.New("unexpected Exec")
}

func (row aggregateTestRow) Scan(destinations ...any) error {
	if row.err != nil {
		return row.err
	}
	if len(destinations) != len(row.values) {
		return errors.New("unexpected scan destination count")
	}
	for i, value := range row.values {
		switch destination := destinations[i].(type) {
		case *any:
			*destination = value
		case *int64:
			*destination = value.(int64)
		case *float64:
			*destination = value.(float64)
		default:
			return errors.New("unexpected scan destination type")
		}
	}
	return nil
}

type aggregateTestModelReader struct {
	model ports.TenantModel
}

func (r aggregateTestModelReader) GetTenantModel(context.Context, string) (ports.TenantModel, error) {
	return r.model, nil
}

func (aggregateTestModelReader) CreateIndexJob(context.Context, string, string, string, []string, string) (ports.ManagedIndexJob, error) {
	return ports.ManagedIndexJob{}, errors.New("not implemented")
}

func (aggregateTestModelReader) ListIndexJobs(context.Context, string) ([]ports.ManagedIndexJob, error) {
	return nil, errors.New("not implemented")
}

func (aggregateTestModelReader) RetryIndexJob(context.Context, string) error {
	return errors.New("not implemented")
}

type aggregateTestRecords struct {
	listCalls int
}

func (r *aggregateTestRecords) GetRecord(context.Context, string, string, string) (ports.TenantRecord, error) {
	return ports.TenantRecord{}, errors.New("not implemented")
}

func (r *aggregateTestRecords) ListRecords(context.Context, string, string, int) ([]ports.TenantRecord, error) {
	r.listCalls++
	return nil, errors.New("local list fallback must not be called")
}

func (*aggregateTestRecords) QueryRecords(context.Context, string, string, string, string, int) ([]ports.TenantRecord, error) {
	return nil, errors.New("not implemented")
}

func (*aggregateTestRecords) AggregateRecords(context.Context, string, ports.AggregateQuery) (any, error) {
	return nil, errors.New("legacy aggregate must not be called")
}

type aggregateTestCache struct {
	value    []byte
	found    bool
	getCalls int
	setCalls int
	lastKey  string
}

func (c *aggregateTestCache) Get(_ context.Context, key string) ([]byte, bool, error) {
	c.getCalls++
	c.lastKey = key
	return c.value, c.found, nil
}

func (c *aggregateTestCache) Set(_ context.Context, key string, value []byte) error {
	c.setCalls++
	c.lastKey = key
	c.value = value
	c.found = true
	return nil
}

func TestDirectAggregateReaderQueriesPublishedPhysicalTable(t *testing.T) {
	t.Parallel()

	db := &aggregateTestDB{rows: []aggregateTestRow{{values: []any{int64(7)}}}}
	records := &aggregateTestRecords{}
	reader := NewDirectAggregateReader(
		records,
		db,
		aggregateTestModelReader{model: aggregateTestModel(nil)},
		nil,
		time.Second,
		1,
	)

	value, err := reader.AggregateRecords(t.Context(), "tenant-1", ports.AggregateQuery{
		ObjectType: "transactions",
		Aggregate:  "count",
		Field:      "amount",
	})
	if err != nil {
		t.Fatalf("AggregateRecords() error = %v", err)
	}
	if value != int64(7) {
		t.Fatalf("AggregateRecords() = %#v, want 7", value)
	}
	if len(db.sql) != 1 || !strings.Contains(db.sql[0], `"tenant_acme"."transactions"`) {
		t.Fatalf("aggregate SQL = %#v, want published physical schema and table", db.sql)
	}
	if records.listCalls != 0 {
		t.Fatalf("ListRecords() calls = %d, want 0", records.listCalls)
	}
}

func TestDirectAggregateReaderUsesCachedSealedBucket(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	cachePayload, err := json.Marshal(aggregateComponent{Aggregate: "count", Count: 6})
	if err != nil {
		t.Fatal(err)
	}
	db := &aggregateTestDB{rows: []aggregateTestRow{
		{values: []any{int64(4)}},
		{values: []any{int64(4)}},
	}}
	cache := &aggregateTestCache{value: cachePayload, found: true}
	reader := NewDirectAggregateReader(
		&aggregateTestRecords{},
		db,
		aggregateTestModelReader{model: aggregateTestModel(ptrTime(now.Add(-24 * time.Hour)))},
		cache,
		time.Second,
		1,
	)
	reader.now = func() time.Time { return now }

	value, err := reader.AggregateRecords(t.Context(), "tenant-1", aggregateTestQuery(
		time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 7, 26, 0, 0, 0, 0, time.UTC),
	))
	if err != nil {
		t.Fatalf("AggregateRecords() error = %v", err)
	}
	if value != int64(6) {
		t.Fatalf("AggregateRecords() = %#v, want 6", value)
	}
	if len(db.sql) != 2 ||
		!strings.Contains(db.sql[0], "logical_bucket_generations") ||
		!strings.Contains(db.sql[1], "logical_bucket_generations") {
		t.Fatalf("queries = %#v, want generation checks only", db.sql)
	}
	if cache.getCalls != 1 || cache.setCalls != 0 {
		t.Fatalf("cache get/set calls = %d/%d, want 1/0", cache.getCalls, cache.setCalls)
	}
}

func TestDirectAggregateReaderPopulatesCacheOnFirstStableMiss(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	db := &aggregateTestDB{rows: []aggregateTestRow{
		{values: []any{int64(4)}},
		{values: []any{int64(6)}},
		{values: []any{int64(4)}},
	}}
	cache := &aggregateTestCache{}
	reader := NewDirectAggregateReader(
		&aggregateTestRecords{},
		db,
		aggregateTestModelReader{model: aggregateTestModel(ptrTime(now.Add(-24 * time.Hour)))},
		cache,
		time.Second,
		1,
	)
	reader.now = func() time.Time { return now }

	value, err := reader.AggregateRecords(t.Context(), "tenant-1", aggregateTestQuery(
		time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 7, 26, 0, 0, 0, 0, time.UTC),
	))
	if err != nil {
		t.Fatalf("AggregateRecords() error = %v", err)
	}
	if value != int64(6) || cache.setCalls != 1 {
		t.Fatalf("value/set calls = %#v/%d, want 6/1", value, cache.setCalls)
	}
}

func TestDirectAggregateReaderRetriesChangedGeneration(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	db := &aggregateTestDB{rows: []aggregateTestRow{
		{values: []any{int64(4)}},
		{values: []any{int64(6)}},
		{values: []any{int64(5)}},
		{values: []any{int64(5)}},
		{values: []any{int64(7)}},
		{values: []any{int64(5)}},
	}}
	cache := &aggregateTestCache{}
	reader := NewDirectAggregateReader(
		&aggregateTestRecords{},
		db,
		aggregateTestModelReader{model: aggregateTestModel(ptrTime(now.Add(-24 * time.Hour)))},
		cache,
		time.Second,
		1,
	)
	reader.now = func() time.Time { return now }

	value, err := reader.AggregateRecords(t.Context(), "tenant-1", aggregateTestQuery(
		time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 7, 26, 0, 0, 0, 0, time.UTC),
	))
	if err != nil {
		t.Fatalf("AggregateRecords() error = %v", err)
	}
	if value != int64(7) || cache.setCalls != 1 {
		t.Fatalf("value/set calls = %#v/%d, want 7/1 after retry", value, cache.setCalls)
	}
}

func TestBuildBucketPlanUsesLocalDSTDayBoundaries(t *testing.T) {
	t.Parallel()

	location, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 3, 8, 0, 0, 0, 0, location).UTC()
	end := time.Date(2026, 3, 9, 0, 0, 0, 0, location).UTC()
	eligible := start.Add(-time.Hour)
	model := aggregateTestModel(&eligible)
	model.LogicalBuckets[0].Timezone = "America/New_York"
	model.LogicalBuckets[0].SealDelay = 0

	plan, ok := buildBucketPlan(
		model,
		model.Tables["transactions"],
		aggregateTestQuery(start, end),
		end.Add(time.Hour),
	)
	if !ok {
		t.Fatal("buildBucketPlan() supported = false, want true")
	}
	if len(plan.Parts) != 1 || plan.Parts[0].End.Sub(plan.Parts[0].Start) != 23*time.Hour {
		t.Fatalf("plan parts = %#v, want one 23-hour local day", plan.Parts)
	}
}

func aggregateTestModel(cacheEligibleAt *time.Time) ports.TenantModel {
	model := ports.TenantModel{
		RevisionID:         "revision-1",
		PhysicalSchemaName: "tenant_acme",
		RecordLookupField:  "object_id",
		Tables: map[string]ports.TenantModelTable{
			"transactions": {
				ID:   "table-1",
				Name: "transactions",
				Fields: map[string]ports.TenantModelField{
					"amount":      {Name: "amount", Type: "float"},
					"occurred_at": {Name: "occurred_at", Type: "timestamp"},
				},
			},
		},
	}
	if cacheEligibleAt != nil {
		model.LogicalBuckets = []ports.LogicalBucketDefinition{{
			ID:                 "bucket-1",
			TableID:            "table-1",
			TimestampFieldName: "occurred_at",
			Grain:              "daily",
			Timezone:           "UTC",
			SealDelay:          48 * time.Hour,
			DefinitionVersion:  1,
			Status:             "active",
			CacheEligibleAt:    cacheEligibleAt,
		}}
	}
	return model
}

func aggregateTestQuery(start, end time.Time) ports.AggregateQuery {
	return ports.AggregateQuery{
		ObjectType: "transactions",
		Aggregate:  "count",
		Field:      "amount",
		Filter: &ports.AggregateFilter{
			Kind:     "group",
			Operator: "and",
			Children: []ports.AggregateFilter{
				{Kind: "predicate", Field: "occurred_at", Op: "gte", Value: start},
				{Kind: "predicate", Field: "occurred_at", Op: "lt", Value: end},
			},
		},
	}
}

func ptrTime(value time.Time) *time.Time { return &value }
