package eventstore

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"
)

func TestClickHouseTypedEventTableIntegration(t *testing.T) {
	clickHouseURL := os.Getenv("EVENT_STORE_TEST_CLICKHOUSE_URL")
	if clickHouseURL == "" {
		t.Skip("set EVENT_STORE_TEST_CLICKHOUSE_URL to run ClickHouse integration tests")
	}
	database := fmt.Sprintf("fraud_events_typed_test_%d", time.Now().UnixNano())
	client := newClickHouseClient(Config{ClickHouseURL: clickHouseURL, ClickHouseDatabase: database, HTTPTimeout: 10 * time.Second})
	ctx := context.Background()
	if err := client.initialize(ctx); err != nil {
		t.Fatalf("initialize ClickHouse: %v", err)
	}
	t.Cleanup(func() { _, _ = client.execute(context.Background(), "DROP DATABASE IF EXISTS "+database, nil) })

	table := testTableContract()
	table.Fields["updated_at"] = FieldContract{DataType: "timestamp"}
	table.Fields["account_ref"] = FieldContract{DataType: "string", IsProjection: true}
	table.Fields["merchant_id"] = FieldContract{DataType: "string"}
	table.Fields["source_id"] = FieldContract{DataType: "string"}
	eventTime := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	event := Event{
		EventID:     "cccccccc-cccc-4ccc-8ccc-cccccccccccc",
		ObjectID:    "txn-1",
		EventTime:   eventTime,
		IngestedAt:  eventTime.Add(time.Second),
		RequestHash: "request-hash",
		Payload: map[string]any{
			"object_id": "txn-1", "date": eventTime, "amount": 12500.5, "country": "GH",
			"account_ref": "acct-1", "merchant_id": "merchant-1", "source_id": "source-1",
		},
	}
	if err := client.insert(ctx, table, []Event{event}); err != nil {
		t.Fatalf("insert typed event: %v", err)
	}

	body, err := client.execute(ctx, "SELECT name FROM system.columns WHERE database = "+quote(database)+" AND table = "+quote(physicalTableName(table.TenantID, table.TableID))+" ORDER BY position FORMAT JSONEachRow", nil)
	if err != nil {
		t.Fatalf("inspect typed columns: %v", err)
	}
	for _, name := range []string{"account_ref", "merchant_id", "source_id", "amount"} {
		if !bytes.Contains(body, []byte(`"name":"`+name+`"`)) {
			t.Fatalf("ClickHouse schema missing %s: %s", name, body)
		}
	}
	if bytes.Contains(body, []byte(`"name":"payload"`)) {
		t.Fatalf("ClickHouse schema unexpectedly contains payload JSON column: %s", body)
	}
	projectionBody, err := client.execute(ctx, "SELECT name FROM system.projections WHERE database = "+quote(database)+" AND table = "+quote(physicalTableName(table.TenantID, table.TableID))+" FORMAT JSONEachRow", nil)
	if err != nil {
		t.Fatalf("inspect typed projections: %v", err)
	}
	if !bytes.Contains(projectionBody, []byte(`"name":"event_by_account_ref"`)) {
		t.Fatalf("ClickHouse schema missing account projection: %s", projectionBody)
	}

	value, err := client.aggregate(ctx, AggregateRequest{
		Table: table, Aggregate: "sum", Field: "amount",
		Filter: &AggregateFilter{Kind: "predicate", Field: "date", Op: "gte", Value: "2026-08-20T00:00:00Z"},
	})
	if err != nil {
		t.Fatalf("aggregate typed amount: %v", err)
	}
	encoded, _ := json.Marshal(value)
	if string(encoded) != "12500.5" {
		t.Fatalf("typed sum = %s, want 12500.5", encoded)
	}
	values, err := client.aggregateBatch(ctx, []AggregateRequest{
		{
			Table: table, Aggregate: "count", Field: "amount",
			Filter: &AggregateFilter{Kind: "group", Operator: "and", Children: []AggregateFilter{
				{Kind: "predicate", Field: "account_ref", Op: "eq", Value: "acct-1"},
				{Kind: "predicate", Field: "date", Op: "gte", Value: "2026-08-20T00:00:00Z"},
			}},
		},
		{
			Table: table, Aggregate: "sum", Field: "amount",
			Filter: &AggregateFilter{Kind: "group", Operator: "and", Children: []AggregateFilter{
				{Kind: "predicate", Field: "account_ref", Op: "eq", Value: "acct-1"},
				{Kind: "predicate", Field: "date", Op: "gte", Value: "2026-08-01T00:00:00Z"},
			}},
		},
	})
	if err != nil {
		t.Fatalf("batch aggregate projected account: %v", err)
	}
	encoded, _ = json.Marshal(values)
	if string(encoded) != "[1,12500.5]" {
		t.Fatalf("batch aggregate values = %s, want [1,12500.5]", encoded)
	}
}

func TestClickHouseSealedAggregateFactsIntegration(t *testing.T) {
	clickHouseURL := os.Getenv("EVENT_STORE_TEST_CLICKHOUSE_URL")
	if clickHouseURL == "" {
		t.Skip("set EVENT_STORE_TEST_CLICKHOUSE_URL to run ClickHouse integration tests")
	}
	database := fmt.Sprintf("fraud_events_fact_test_%d", time.Now().UnixNano())
	client := newClickHouseClient(Config{
		ClickHouseURL: clickHouseURL, ClickHouseDatabase: database, HTTPTimeout: 30 * time.Second,
		FeatureMaxKeys: 10, FeatureMaxKeysPerTenant: 10,
	})
	ctx := context.Background()
	if err := client.initialize(ctx); err != nil {
		t.Fatalf("initialize ClickHouse: %v", err)
	}
	t.Cleanup(func() { _, _ = client.execute(context.Background(), "DROP DATABASE IF EXISTS "+database, nil) })

	table := testTableContract()
	table.Fields["account_ref"] = FieldContract{DataType: "string"}
	base := time.Now().UTC().Truncate(time.Hour).Add(-3 * time.Hour)
	events := []Event{
		factTestEvent("11111111-1111-4111-8111-111111111111", "txn-1", "acct-1", 10, base.Add(10*time.Minute)),
		factTestEvent("22222222-2222-4222-8222-222222222222", "txn-2", "acct-1", 20, base.Add(40*time.Minute)),
		factTestEvent("33333333-3333-4333-8333-333333333333", "txn-3", "acct-1", 30, base.Add(70*time.Minute)),
		factTestEvent("44444444-4444-4444-8444-444444444444", "txn-4", "acct-2", 50, base.Add(75*time.Minute)),
	}
	if err := client.insert(ctx, table, events); err != nil {
		t.Fatalf("insert fact test events: %v", err)
	}
	request := AggregateRequest{
		Table: table, Aggregate: "avg", Field: "amount",
		Filter: andFilter([]AggregateFilter{
			{Kind: "predicate", Field: "account_ref", Op: "eq", Value: "acct-1"},
			{Kind: "predicate", Field: "date", Op: "gte", Value: base.Add(30 * time.Minute).Format(time.RFC3339)},
		}),
	}
	plan, ok := planAggregateFacts(request)
	if !ok {
		t.Fatal("fact integration request was not eligible")
	}
	if err := client.promoteFactShape(ctx, plan); err != nil {
		t.Fatalf("promote fact shape: %v", err)
	}
	value, err := client.aggregateFromFacts(ctx, plan, nil, false)
	if err != nil {
		t.Fatalf("aggregate from facts: %v", err)
	}
	if value != float64(25) {
		t.Fatalf("sealed average = %#v, want 25", value)
	}

	newEvent := factTestEvent("55555555-5555-4555-8555-555555555555", "txn-5", "acct-1", 40, base.Add(90*time.Minute))
	if err := client.insert(ctx, table, []Event{newEvent}); err != nil {
		t.Fatalf("insert event into previously built bucket: %v", err)
	}
	value, err = client.aggregateFromFacts(ctx, plan, nil, false)
	if err != nil {
		t.Fatalf("aggregate after bucket invalidation: %v", err)
	}
	if value != float64(30) {
		t.Fatalf("rebuilt average = %#v, want 30", value)
	}

	openBucketStart := time.Now().UTC().Truncate(time.Hour)
	openEvent := factTestEvent("66666666-6666-4666-8666-666666666666", "txn-6", "acct-1", 100, openBucketStart.Add(time.Minute))
	if err := client.insert(ctx, table, []Event{openEvent}); err != nil {
		t.Fatalf("insert event into open bucket: %v", err)
	}
	value, err = client.aggregateFromFacts(ctx, plan, nil, false)
	if err != nil {
		t.Fatalf("aggregate with open bucket: %v", err)
	}
	if value != float64(47.5) {
		t.Fatalf("average with raw open bucket = %#v, want 47.5", value)
	}
	boundedRequest := request
	boundedRequest.Filter = andFilter([]AggregateFilter{
		{Kind: "predicate", Field: "account_ref", Op: "eq", Value: "acct-1"},
		{Kind: "predicate", Field: "date", Op: "gte", Value: base.Add(30 * time.Minute).Format(time.RFC3339)},
		{Kind: "predicate", Field: "date", Op: "lte", Value: base.Add(140 * time.Minute).Format(time.RFC3339)},
	})
	boundedPlan, ok := planAggregateFacts(boundedRequest)
	if !ok {
		t.Fatal("bounded fact integration request was not eligible")
	}
	value, err = client.aggregateFromFacts(ctx, boundedPlan, nil, false)
	if err != nil {
		t.Fatalf("bounded aggregate from facts: %v", err)
	}
	if value != float64(30) {
		t.Fatalf("bounded sealed average = %#v, want 30", value)
	}
	body, err := client.execute(ctx, "SELECT count() AS value FROM "+database+"."+factValueTable+
		" WHERE bucket_start >= toDateTime("+quote(openBucketStart.Format("2006-01-02 15:04:05"))+
		", 'UTC') FORMAT JSONEachRow", nil)
	if err != nil {
		t.Fatalf("inspect open-bucket facts: %v", err)
	}
	var openFactCount struct {
		Value uint64 `json:"value"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(body), &openFactCount); err != nil {
		t.Fatalf("decode open-bucket fact count: %v", err)
	}
	if openFactCount.Value != 0 {
		t.Fatalf("open bucket produced %d durable facts; want 0", openFactCount.Value)
	}

	countRequest := AggregateRequest{
		Table: table, Aggregate: "count", Field: "object_id",
		Filter: &AggregateFilter{Kind: "predicate", Field: "date", Op: "gte", Value: base.Format(time.RFC3339)},
	}
	countPlan, ok := planAggregateFacts(countRequest)
	if !ok {
		t.Fatal("dimensionless count request was not eligible")
	}
	if err := client.promoteFactShape(ctx, countPlan); err != nil {
		t.Fatalf("promote dimensionless count facts: %v", err)
	}
	value, err = client.aggregateFromFacts(ctx, countPlan, nil, false)
	if err != nil {
		t.Fatalf("dimensionless count from facts: %v", err)
	}
	if value != uint64(6) {
		t.Fatalf("dimensionless count = %#v, want 6", value)
	}
}

func factTestEvent(eventID, objectID, accountRef string, amount float64, eventTime time.Time) Event {
	return Event{
		EventID: eventID, ObjectID: objectID, EventTime: eventTime, IngestedAt: time.Now().UTC(), RequestHash: eventID,
		Payload: map[string]any{
			"object_id": objectID, "date": eventTime, "amount": amount, "country": "GH", "account_ref": accountRef,
		},
	}
}
