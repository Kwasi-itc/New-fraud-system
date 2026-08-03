package ast_eval

import (
	"context"
	"strings"
	"testing"
	"time"

	domainast "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/ast"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

func aggregateTestModel() *ports.TenantModel {
	return &ports.TenantModel{
		RecordLookupField: "object_id",
		Tables: map[string]ports.TenantModelTable{
			"transactions": {
				Name: "transactions",
				Fields: map[string]ports.TenantModelField{
					"object_id": {Name: "object_id", Type: "string"},
					"amount":    {Name: "amount", Type: "number"},
				},
			},
		},
	}
}

func TestCompareValuesHandlesNilOperands(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		op    string
		left  any
		right any
		want  bool
	}{
		{name: "eq both nil", op: "eq", left: nil, right: nil, want: true},
		{name: "eq left nil", op: "eq", left: nil, right: 10, want: false},
		{name: "neq left nil", op: "neq", left: nil, right: 10, want: true},
		{name: "gt left nil", op: "gt", left: nil, right: 10, want: false},
		{name: "gte right nil", op: "gte", left: 10, right: nil, want: false},
		{name: "lt right nil", op: "lt", left: 10, right: nil, want: false},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := compareValues(tc.op, tc.left, tc.right)
			if err != nil {
				t.Fatalf("compareValues(%q, %v, %v) error = %v", tc.op, tc.left, tc.right, err)
			}
			if got != tc.want {
				t.Fatalf("compareValues(%q, %v, %v) = %v, want %v", tc.op, tc.left, tc.right, got, tc.want)
			}
		})
	}
}

type evaluatorTenantDataReaderStub struct {
	listLimit      int
	listCalls      int
	listDelay      time.Duration
	aggregateDelay time.Duration
	aggregateValue any
}

func (s *evaluatorTenantDataReaderStub) GetRecord(context.Context, string, string, string) (ports.TenantRecord, error) {
	return ports.TenantRecord{}, nil
}

func (s *evaluatorTenantDataReaderStub) ListRecords(_ context.Context, _ string, _ string, limit int) ([]ports.TenantRecord, error) {
	s.listCalls++
	s.listLimit = limit
	if s.listDelay > 0 {
		time.Sleep(s.listDelay)
	}
	return []ports.TenantRecord{
		{ObjectID: "txn-1", ObjectType: "transactions", Fields: map[string]any{"amount": 100}},
		{ObjectID: "txn-2", ObjectType: "transactions", Fields: map[string]any{"amount": 200}},
	}, nil
}

func (s *evaluatorTenantDataReaderStub) QueryRecords(context.Context, string, string, string, string, int) ([]ports.TenantRecord, error) {
	return nil, nil
}

func (s *evaluatorTenantDataReaderStub) AggregateRecords(context.Context, string, ports.AggregateQuery) (any, error) {
	if s.aggregateDelay > 0 {
		time.Sleep(s.aggregateDelay)
	}
	return s.aggregateValue, nil
}

func TestAggregatePushdownStrictUnsupportedReturnsError(t *testing.T) {
	t.Parallel()

	node := domainast.Node{
		Function: "gt",
		Children: []domainast.Node{
			{
				Function: "Aggregator",
				NamedChildren: map[string]domainast.Node{
					"tableName":  {Constant: "transactions"},
					"fieldName":  {Constant: "amount"},
					"aggregator": {Constant: "SUM"},
				},
			},
			{Constant: 10},
		},
	}

	_, err := EvaluateNode(context.Background(), node, Runtime{
		TenantID:                    "tenant-1",
		ObjectType:                  "transactions",
		Model:                       aggregateTestModel(),
		TenantDataReader:            &evaluatorTenantDataReaderStub{},
		AggregatePushdownMode:       AggregatePushdownModeStrict,
		AggregatePushdownAggregates: []string{"count"},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "aggregate pushdown unsupported") {
		t.Fatalf("error = %v, want aggregate pushdown unsupported", err)
	}
}

func TestAggregatePushdownDisabledReturnsErrorWithoutListingRecords(t *testing.T) {
	t.Parallel()

	reader := &evaluatorTenantDataReaderStub{}
	node := domainast.Node{
		Function: "gt",
		Children: []domainast.Node{
			{
				Function: "Aggregator",
				NamedChildren: map[string]domainast.Node{
					"tableName":  {Constant: "transactions"},
					"fieldName":  {Constant: "amount"},
					"aggregator": {Constant: "COUNT"},
				},
			},
			{Constant: 1},
		},
	}

	_, err := EvaluateNode(context.Background(), node, Runtime{
		TenantID:                    "tenant-1",
		ObjectType:                  "transactions",
		Model:                       aggregateTestModel(),
		TenantDataReader:            reader,
		AggregatePushdownMode:       AggregatePushdownModeDisabled,
		AggregatePushdownAggregates: []string{"count"},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "aggregate pushdown unsupported") {
		t.Fatalf("error = %v, want aggregate pushdown unsupported", err)
	}
	if reader.listCalls != 0 {
		t.Fatalf("ListRecords calls = %d, want 0", reader.listCalls)
	}
}

func TestAggregatePushdownMetricsSnapshotTracksAggregateLatencyPercentiles(t *testing.T) {
	before := AggregatePushdownMetricsSnapshot()

	reader := &evaluatorTenantDataReaderStub{
		aggregateDelay: 3 * time.Millisecond,
		aggregateValue: float64(2),
	}
	node := domainast.Node{
		Function: "gt",
		Children: []domainast.Node{
			{
				Function: "Aggregator",
				NamedChildren: map[string]domainast.Node{
					"tableName":  {Constant: "transactions"},
					"fieldName":  {Constant: "amount"},
					"aggregator": {Constant: "COUNT"},
				},
			},
			{Constant: 1},
		},
	}

	for i := 0; i < 12; i++ {
		value, err := EvaluateNode(context.Background(), node, Runtime{
			TenantID:                    "tenant-1",
			ObjectType:                  "transactions",
			Model:                       aggregateTestModel(),
			TenantDataReader:            reader,
			AggregatePushdownMode:       AggregatePushdownModeEnabled,
			AggregatePushdownAggregates: []string{"count"},
		})
		if err != nil {
			t.Fatalf("EvaluateNode() error = %v", err)
		}
		if value != true {
			t.Fatalf("EvaluateNode() = %#v, want true", value)
		}
	}

	after := AggregatePushdownMetricsSnapshot()
	if after.AggregateEvaluationCount < before.AggregateEvaluationCount+12 {
		t.Fatalf("AggregateEvaluationCount = %d, want at least %d", after.AggregateEvaluationCount, before.AggregateEvaluationCount+12)
	}
	if after.AggregateLatencyP50 <= 0 {
		t.Fatalf("AggregateLatencyP50 = %s, want > 0", after.AggregateLatencyP50)
	}
	if after.AggregateLatencyP95 <= 0 {
		t.Fatalf("AggregateLatencyP95 = %s, want > 0", after.AggregateLatencyP95)
	}
	if after.AggregateLatencyP99 <= 0 {
		t.Fatalf("AggregateLatencyP99 = %s, want > 0", after.AggregateLatencyP99)
	}
}

func TestBroadReadHelperMetricsSnapshotTracksRejectedHelpers(t *testing.T) {
	before := BroadReadHelperMetricsSnapshot()

	relatedCountNode := domainast.Node{
		Function: "related_count",
		NamedChildren: map[string]domainast.Node{
			"object_type": {Constant: "transactions"},
			"field":       {Constant: "owner_id"},
		},
	}
	relatedRecordsNode := domainast.Node{
		Function: "related_records",
		NamedChildren: map[string]domainast.Node{
			"object_type": {Constant: "transactions"},
		},
	}
	runtime := Runtime{
		TenantID:         "tenant-1",
		ObjectType:       "transactions",
		TenantDataReader: &evaluatorTenantDataReaderStub{},
	}

	if _, err := EvaluateNode(context.Background(), relatedCountNode, runtime); err == nil {
		t.Fatal("expected related_count error, got nil")
	}
	if _, err := EvaluateNode(context.Background(), relatedRecordsNode, runtime); err == nil {
		t.Fatal("expected related_records error, got nil")
	}

	after := BroadReadHelperMetricsSnapshot()
	if after.RejectedCount < before.RejectedCount+2 {
		t.Fatalf("RejectedCount = %d, want at least %d", after.RejectedCount, before.RejectedCount+2)
	}
	if after.RejectedByFunctionName["related_count"] < before.RejectedByFunctionName["related_count"]+1 {
		t.Fatalf("related_count rejects = %d, want at least %d", after.RejectedByFunctionName["related_count"], before.RejectedByFunctionName["related_count"]+1)
	}
	if after.RejectedByFunctionName["related_records"] < before.RejectedByFunctionName["related_records"]+1 {
		t.Fatalf("related_records rejects = %d, want at least %d", after.RejectedByFunctionName["related_records"], before.RejectedByFunctionName["related_records"]+1)
	}
}
