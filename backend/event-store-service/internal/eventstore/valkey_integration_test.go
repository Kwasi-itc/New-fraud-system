package eventstore

import (
	"os"
	"testing"
	"time"
)

func TestValkeyBucketSeriesIntegration(t *testing.T) {
	address := os.Getenv("EVENT_STORE_TEST_VALKEY_ADDRESS")
	if address == "" {
		t.Skip("set EVENT_STORE_TEST_VALKEY_ADDRESS to run Valkey integration tests")
	}
	namespace := "fraud:event-feature:test:" + time.Now().Format("150405.000000000")
	cache := newFeatureCache(Config{
		ValkeyAddress: address, FeatureNamespace: namespace, FeatureAdmissionHits: 2,
		FeatureSlowQueryMS: 1000, FeatureMaxKeys: 10, FeatureMaxKeysPerTenant: 10, FeatureTTL: time.Minute,
	})
	if cache.shouldPromoteTemplate(t.Context(), "tenant-1", "series-1", 10*time.Millisecond) {
		t.Fatal("template promoted before reaching the admission threshold")
	}
	if !cache.shouldPromoteTemplate(t.Context(), "tenant-1", "series-1", 10*time.Millisecond) {
		t.Fatal("template was not promoted at the admission threshold")
	}
	series := aggregateBucketSeries{Buckets: map[string]aggregateBucketFact{
		"2026-08-20T10:00:00Z": {Generation: 7, Sum: 25, Count: 2},
	}}
	cache.observeSeries(t.Context(), "tenant-1", "series-1", series, false, false)
	cache.observeSeries(t.Context(), "tenant-1", "series-1", series, false, false)
	loaded, ok := cache.getSeries(t.Context(), "tenant-1", "series-1")
	if !ok {
		t.Fatal("promoted bucket series was not returned from Valkey")
	}
	fact := loaded.Buckets["2026-08-20T10:00:00Z"]
	if fact.Generation != 7 || fact.Sum != 25 || fact.Count != 2 {
		t.Fatalf("loaded bucket fact = %#v", fact)
	}
}
