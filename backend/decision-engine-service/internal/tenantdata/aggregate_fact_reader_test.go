package tenantdata

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

type factRegistryStub struct {
	definitions []ports.AggregateFactDefinition
}

func (s factRegistryStub) SyncScenario(context.Context, string, string, string, []ports.AggregateFactDefinition) error {
	return nil
}
func (s factRegistryStub) RemoveScenario(context.Context, string, string) error { return nil }
func (s factRegistryStub) ListActive(context.Context, string, string) ([]ports.AggregateFactDefinition, error) {
	return s.definitions, nil
}

type factBucketsStub struct {
	covered      bool
	reads        int
	readErr      error
	segmentsSeen int
}

func (s *factBucketsStub) Covers(context.Context, string, ports.AggregateFactDefinition, time.Time) (bool, error) {
	return s.covered, nil
}
func (s *factBucketsStub) ReadSegments(_ context.Context, _ string, _ ports.AggregateFactDefinition, _ string, segments []AggregateFactBucketSegment) (AggregateFactTotals, error) {
	s.reads++
	if s.readErr != nil {
		return AggregateFactTotals{}, s.readErr
	}
	buckets := 0
	for _, segment := range segments {
		buckets += len(segment.Starts)
	}
	s.segmentsSeen = len(segments)
	return AggregateFactTotals{Sum: float64(buckets) * 10, Count: int64(buckets)}, nil
}

type factBaseReaderStub struct {
	aggregateCalls int
	aggregateValue any
}

func (s *factBaseReaderStub) GetRecord(context.Context, string, string, string) (ports.TenantRecord, error) {
	return ports.TenantRecord{}, nil
}
func (s *factBaseReaderStub) ListRecords(context.Context, string, string, int) ([]ports.TenantRecord, error) {
	return nil, nil
}
func (s *factBaseReaderStub) QueryRecords(context.Context, string, string, string, string, int) ([]ports.TenantRecord, error) {
	return nil, nil
}
func (s *factBaseReaderStub) AggregateRecords(context.Context, string, ports.AggregateQuery) (any, error) {
	s.aggregateCalls++
	return s.aggregateValue, nil
}

func TestAggregateFactReaderUsesCompleteAndActiveMinuteBucketsWithoutPostgres(t *testing.T) {
	before := AggregateFactMetricsSnapshot()
	lower := time.Date(2026, 8, 26, 10, 0, 30, 0, time.UTC)
	upper := lower.Add(time.Hour)
	definition := testFactDefinition()
	buckets := &factBucketsStub{covered: true}
	base := &factBaseReaderStub{aggregateValue: float64(2)}
	reader := NewAggregateFactReader(base, factRegistryStub{definitions: []ports.AggregateFactDefinition{definition}}, buckets)

	value, err := reader.AggregateRecords(context.Background(), "tenant-1", factQuery(lower, upper))
	if err != nil {
		t.Fatalf("AggregateRecords() error = %v", err)
	}
	// The lower partial minute is discarded and the active upper minute is
	// included, producing sixty cache-only buckets.
	if value != float64(600) {
		t.Fatalf("value = %#v, want 600", value)
	}
	if buckets.reads != 1 || base.aggregateCalls != 0 {
		t.Fatalf("bucket reads = %d, raw calls = %d, want 1 and 0", buckets.reads, base.aggregateCalls)
	}
	after := AggregateFactMetricsSnapshot()
	if after.FactHits-before.FactHits != 1 ||
		after.MinuteBucketsRead-before.MinuteBucketsRead != 60 {
		t.Fatalf("aggregate fact metrics delta = before:%#v after:%#v", before, after)
	}
}

func TestAggregateFactReaderDoesNotFallBackWhenValkeyReadFails(t *testing.T) {
	before := AggregateFactMetricsSnapshot()
	lower := time.Date(2026, 8, 26, 10, 0, 30, 0, time.UTC)
	buckets := &factBucketsStub{covered: true, readErr: errors.New("valkey unavailable")}
	base := &factBaseReaderStub{aggregateValue: float64(81)}
	reader := NewAggregateFactReader(base, factRegistryStub{definitions: []ports.AggregateFactDefinition{testFactDefinition()}}, buckets)

	_, err := reader.AggregateRecords(context.Background(), "tenant-1", factQuery(lower, lower.Add(time.Hour)))
	if !errors.Is(err, ErrAggregateFactUnavailable) {
		t.Fatalf("AggregateRecords() error = %v, want aggregate fact unavailable", err)
	}
	if base.aggregateCalls != 0 {
		t.Fatalf("raw calls = %d, want 0", base.aggregateCalls)
	}
	after := AggregateFactMetricsSnapshot()
	if after.BucketReadFailures-before.BucketReadFailures != 1 ||
		after.PendingBucketFailures != before.PendingBucketFailures ||
		after.FactHits != before.FactHits {
		t.Fatalf("aggregate fact metrics delta = before:%#v after:%#v", before, after)
	}
}

func TestAggregateFactReaderUsesOneAtomicReadForMixedDayAndHourBuckets(t *testing.T) {
	before := AggregateFactMetricsSnapshot()
	lower := time.Date(2026, 8, 26, 10, 0, 30, 0, time.UTC)
	buckets := &factBucketsStub{covered: true}
	base := &factBaseReaderStub{aggregateValue: float64(93)}
	definition := testFactDefinition()
	definition.MaxWindowSeconds = int64((30 * 24 * time.Hour) / time.Second)
	definition.MinuteEnabled = false
	reader := NewAggregateFactReader(base, factRegistryStub{definitions: []ports.AggregateFactDefinition{definition}}, buckets)

	value, err := reader.AggregateRecords(context.Background(), "tenant-1", factQuery(lower, lower.Add(30*24*time.Hour)))
	if err != nil {
		t.Fatalf("AggregateRecords() error = %v", err)
	}
	if value != float64(530) {
		t.Fatalf("value = %#v, want 530", value)
	}
	if base.aggregateCalls != 0 || buckets.reads != 1 || buckets.segmentsSeen != 2 {
		t.Fatalf("raw calls = %d, bucket reads = %d, segments = %d; want 0, 1, 2", base.aggregateCalls, buckets.reads, buckets.segmentsSeen)
	}
	after := AggregateFactMetricsSnapshot()
	if after.FactHits-before.FactHits != 1 || after.IncompleteCoverageFailures != before.IncompleteCoverageFailures {
		t.Fatalf("aggregate fact metrics delta = before:%#v after:%#v", before, after)
	}
}

func TestAggregateFactReaderDoesNotFallBackWhenCoverageIsIncomplete(t *testing.T) {
	before := AggregateFactMetricsSnapshot()
	lower := time.Date(2026, 8, 26, 10, 0, 30, 0, time.UTC)
	buckets := &factBucketsStub{covered: false}
	base := &factBaseReaderStub{aggregateValue: float64(77)}
	reader := NewAggregateFactReader(base, factRegistryStub{definitions: []ports.AggregateFactDefinition{testFactDefinition()}}, buckets)

	_, err := reader.AggregateRecords(context.Background(), "tenant-1", factQuery(lower, lower.Add(time.Hour)))
	if !errors.Is(err, ErrAggregateFactUnavailable) {
		t.Fatalf("AggregateRecords() error = %v, want aggregate fact unavailable", err)
	}
	if base.aggregateCalls != 0 || buckets.reads != 0 {
		t.Fatalf("raw calls = %d, bucket reads = %d", base.aggregateCalls, buckets.reads)
	}
	after := AggregateFactMetricsSnapshot()
	if after.IncompleteCoverageFailures-before.IncompleteCoverageFailures != 1 || after.FactHits != before.FactHits {
		t.Fatalf("aggregate fact metrics delta = before:%#v after:%#v", before, after)
	}
}

func TestHashDimensionValuesCanonicalizesEquivalentTimestampAndNumberForms(t *testing.T) {
	normalized := hashDimensionValues([]any{time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC), int64(1)})
	payload := hashDimensionValues([]any{"2026-08-26T14:00:00+02:00", float64(1)})
	if payload != normalized {
		t.Fatalf("payload hash = %q, normalized hash = %q", payload, normalized)
	}
	if normalized != "bb068b7e5cf60cd8cbce67a4a2c0a3f9" {
		t.Fatalf("cross-service canonical hash = %q", normalized)
	}
}

func testFactDefinition() ports.AggregateFactDefinition {
	return ports.AggregateFactDefinition{
		ID: "fact-1", TableName: "transactions", DimensionFields: []string{"merchant_id"},
		EventTimeField: "date", MeasureField: "amount", NeedsSum: true, NeedsCount: true,
		MaxWindowSeconds: 3600, MinuteEnabled: true, Version: 1, CoverageToken: "coverage-1",
	}
}

func factQuery(lower, upper time.Time) ports.AggregateQuery {
	return ports.AggregateQuery{
		ObjectType: "transactions", Aggregate: "sum", Field: "amount",
		Filter: &ports.AggregateFilter{Kind: "group", Operator: "and", Children: []ports.AggregateFilter{
			{Kind: "predicate", Field: "merchant_id", Op: "eq", Value: "merchant-1"},
			{Kind: "predicate", Field: "date", Op: "gt", Value: lower},
			{Kind: "predicate", Field: "date", Op: "lte", Value: upper},
		}},
	}
}
