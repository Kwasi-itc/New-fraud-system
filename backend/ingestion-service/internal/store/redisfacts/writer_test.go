package redisfacts

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/ports"
)

func TestAggregateDeltasMaintainsMinuteHourAndDayForStringCount(t *testing.T) {
	now := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	definition := ports.AggregateFactDefinition{
		ID: "fact-1", Version: 1, DimensionFields: []string{"merchant_id"},
		EventTimeField: "date", MeasureField: "transaction_id", NeedsCount: true, MinuteEnabled: true,
	}
	deltas, err := aggregateDeltas("tenant-1", []ports.AggregateFactDefinition{definition}, []map[string]any{{
		"merchant_id": "merchant-1", "transaction_id": "txn-1", "date": "2026-06-01T10:37:15Z",
	}}, now)
	if err != nil {
		t.Fatalf("aggregateDeltas() error = %v", err)
	}
	if len(deltas) != 3 {
		t.Fatalf("deltas = %d, want minute, hour, and day", len(deltas))
	}
	for _, delta := range deltas {
		if delta.count != 1 {
			t.Fatalf("delta count = %d, want 1", delta.count)
		}
		if delta.dimension == "" {
			t.Fatal("dimension hash must not be empty")
		}
		if delta.expiresAt.Before(now.Add(7 * 24 * time.Hour)) {
			t.Fatalf("historical expiry = %v, want at least one full short-resolution retention from %v", delta.expiresAt, now)
		}
	}
}

func TestFactWriteManifestIgnoresExpiryButNotArithmetic(t *testing.T) {
	deltas := []ports.AggregateFactDelta{{Key: "bucket", Dimension: "merchant", Sum: 10, Count: 2, ExpiresAt: 100}}
	manifest := factWriteManifest(deltas)
	deltas[0].ExpiresAt = 200
	if got := factWriteManifest(deltas); got != manifest {
		t.Fatalf("expiry-only manifest = %q, want %q", got, manifest)
	}
	deltas[0].Count = 3
	if got := factWriteManifest(deltas); got == manifest {
		t.Fatal("an arithmetic change must change the manifest")
	}
}

func TestDeltaChunksAreBounded(t *testing.T) {
	deltas := make([]ports.AggregateFactDelta, factWriteChunkSize*2+1)
	chunks := deltaChunks(deltas)
	if len(chunks) != 3 || len(chunks[0]) != factWriteChunkSize || len(chunks[1]) != factWriteChunkSize || len(chunks[2]) != 1 {
		t.Fatalf("chunk sizes = %d/%d/%d", len(chunks[0]), len(chunks[1]), len(chunks[2]))
	}
}

func TestChunkedCommitResumesWithoutDoubleCounting(t *testing.T) {
	redisURL := os.Getenv("TEST_AGGREGATE_FACT_REDIS_URL")
	if redisURL == "" {
		t.Skip("TEST_AGGREGATE_FACT_REDIS_URL is not configured")
	}
	options, err := redis.ParseURL(redisURL)
	if err != nil {
		t.Fatal(err)
	}
	client := redis.NewClient(options)
	t.Cleanup(func() { _ = client.Close() })
	ctx := context.Background()
	if err := client.FlushDB(ctx).Err(); err != nil {
		t.Fatal(err)
	}

	tenantID := uuid.New()
	expiresAt := time.Now().Add(time.Hour).Unix()
	deltas := make([]ports.AggregateFactDelta, factWriteChunkSize+1)
	for index := range deltas {
		deltas[index] = ports.AggregateFactDelta{
			Key:       fmt.Sprintf("facts:{%s}:test:%d", tenantID, index),
			Dimension: "merchant", Sum: 1, Count: 1, ExpiresAt: expiresAt,
		}
	}
	write := ports.PreparedAggregateFactWrite{
		TenantID: tenantID, Marker: fmt.Sprintf("facts:{%s}:request:test", tenantID),
		Owner: uuid.NewString(), Deltas: deltas, Prepared: true, OwnsMarker: true,
	}
	write.Manifest = factWriteManifest(write.Deltas)
	w := &Writer{client: client}
	if result, err := w.begin(ctx, write); err != nil || result != 1 {
		t.Fatalf("begin = %d, %v", result, err)
	}
	if err := w.prepareChunks(ctx, write); err != nil {
		t.Fatal(err)
	}
	chunks := deltaChunks(write.Deltas)
	if err := w.applyChunk(ctx, write, 0, chunks[0]); err != nil {
		t.Fatal(err)
	}
	// Simulate a process dying after the first committed chunk. The next
	// request takes over the expired lease and reruns every step safely.
	if err := client.HSet(ctx, write.Marker, "lease_until_ms", 0).Err(); err != nil {
		t.Fatal(err)
	}
	recovery := write
	recovery.Owner = uuid.NewString()
	recovery.OwnsMarker = false
	if err := w.Commit(ctx, recovery); err != nil {
		t.Fatal(err)
	}
	if err := w.Commit(ctx, recovery); err != nil {
		t.Fatal(err)
	}
	for _, delta := range deltas {
		values, err := client.HMGet(ctx, delta.Key, "s:"+delta.Dimension, "c:"+delta.Dimension, "p:"+delta.Dimension).Result()
		if err != nil {
			t.Fatal(err)
		}
		if values[0] != "1" || values[1] != "1" || values[2] != nil {
			t.Fatalf("bucket %s values = %#v, want one sum, one count, no pending marker", delta.Key, values)
		}
	}
	if state := client.HGet(ctx, write.Marker, "state").Val(); state != "applied" {
		t.Fatalf("marker state = %q, want applied", state)
	}
}

func TestAggregateDeltasGroupsBatchByBucketAndDimension(t *testing.T) {
	definition := ports.AggregateFactDefinition{
		ID: "fact-1", Version: 1, DimensionFields: []string{"merchant_id"},
		EventTimeField: "date", MeasureField: "amount", NeedsCount: true, NeedsSum: true,
	}
	records := []map[string]any{
		{"merchant_id": "merchant-1", "amount": 10.5, "date": "2026-08-26T10:10:00Z"},
		{"merchant_id": "merchant-1", "amount": 4.5, "date": "2026-08-26T10:20:00Z"},
	}
	deltas, err := aggregateDeltas("tenant-1", []ports.AggregateFactDefinition{definition}, records, time.Date(2026, 8, 26, 11, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("aggregateDeltas() error = %v", err)
	}
	if len(deltas) != 2 {
		t.Fatalf("deltas = %d, want one hour and one day", len(deltas))
	}
	for _, delta := range deltas {
		if delta.sum != 15 || delta.count != 2 {
			t.Fatalf("delta = %#v, want sum 15 and count 2", delta)
		}
	}
}

func TestPendingBucketsAreScopedByDimensionAndTimeBucket(t *testing.T) {
	definition := ports.AggregateFactDefinition{
		ID: "fact-1", Version: 2, DimensionFields: []string{"merchant_id"},
		EventTimeField: "date", MeasureField: "amount", NeedsSum: true,
	}
	records := []map[string]any{
		{"merchant_id": "merchant-a", "amount": 10.0, "date": "2026-08-26T10:10:00Z"},
		{"merchant_id": "merchant-b", "amount": 20.0, "date": "2026-08-26T10:20:00Z"},
		{"merchant_id": "merchant-a", "amount": 30.0, "date": "2026-08-26T11:10:00Z"},
	}
	deltas, err := aggregateDeltas("tenant-1", []ports.AggregateFactDefinition{definition}, records, time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("aggregateDeltas() error = %v", err)
	}
	buckets := pendingBucketsFromDeltas(deltas)
	if len(buckets) != 5 {
		t.Fatalf("pending buckets = %d, want two dimensions in the day bucket and three dimension/hour combinations", len(buckets))
	}

	dimensionA, ok := dimensionValue(map[string]any{"merchant_id": "merchant-a"}, definition.DimensionFields)
	if !ok {
		t.Fatal("merchant-a dimension should be supported")
	}
	dimensionB, ok := dimensionValue(map[string]any{"merchant_id": "merchant-b"}, definition.DimensionFields)
	if !ok {
		t.Fatal("merchant-b dimension should be supported")
	}
	if dimensionA == dimensionB || factPendingField(dimensionA) == factPendingField(dimensionB) {
		t.Fatal("unrelated merchants must not share a pending field")
	}
	for _, bucket := range buckets {
		if bucket.Dimension != dimensionA && bucket.Dimension != dimensionB {
			t.Fatalf("unexpected pending dimension %q", bucket.Dimension)
		}
		if bucket.Key == "" || bucket.ExpiresAt == 0 {
			t.Fatalf("incomplete pending bucket: %#v", bucket)
		}
	}
}

func TestDimensionValueCanonicalizesEquivalentTimestampAndNumberForms(t *testing.T) {
	fields := []string{"occurred_at", "code"}
	fromNormalized, ok := dimensionValue(map[string]any{
		"occurred_at": time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC), "code": int64(1),
	}, fields)
	if !ok {
		t.Fatal("normalized dimension should be supported")
	}
	fromPayload, ok := dimensionValue(map[string]any{
		"occurred_at": "2026-08-26T14:00:00+02:00", "code": float64(1),
	}, fields)
	if !ok || fromPayload != fromNormalized {
		t.Fatalf("payload hash = %q, normalized hash = %q", fromPayload, fromNormalized)
	}
	if fromNormalized != "bb068b7e5cf60cd8cbce67a4a2c0a3f9" {
		t.Fatalf("cross-service canonical hash = %q", fromNormalized)
	}
}
