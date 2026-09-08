package redisfacts

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/tenantdata"
)

func TestFactResolutionPolicy(t *testing.T) {
	tests := []struct {
		name       string
		size       time.Duration
		retention  time.Duration
		recognized bool
	}{
		{name: "minute", size: time.Minute, retention: minuteRetention, recognized: true},
		{name: "hour", size: time.Hour, retention: hourRetention, recognized: true},
		{name: "day", size: 24 * time.Hour, retention: dayRetention, recognized: true},
		{name: "week", recognized: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			size, retention, recognized := factResolutionPolicy(test.name)
			if size != test.size || retention != test.retention || recognized != test.recognized {
				t.Fatalf("policy = (%v, %v, %v), want (%v, %v, %v)", size, retention, recognized, test.size, test.retention, test.recognized)
			}
		})
	}
}

func TestReadSegmentsReadsCommittedValueWhileDimensionIsPending(t *testing.T) {
	redisURL := os.Getenv("TEST_AGGREGATE_FACT_REDIS_URL")
	if redisURL == "" {
		t.Skip("TEST_AGGREGATE_FACT_REDIS_URL is not configured")
	}
	reader, err := New(redisURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reader.Close() })

	ctx := context.Background()
	tenantID := uuid.NewString()
	definition := ports.AggregateFactDefinition{ID: uuid.NewString(), Version: 1, NeedsCount: true}
	dimension := "merchant-a"
	start := time.Now().UTC().Truncate(time.Hour)
	key := factBucketKey(tenantID, definition, "hour", start)
	t.Cleanup(func() { _ = reader.client.Del(context.Background(), key).Err() })
	if err := reader.client.HSet(ctx, key,
		"c:"+dimension, 7,
		"p:"+dimension, 1,
	).Err(); err != nil {
		t.Fatal(err)
	}

	totals, err := reader.ReadSegments(ctx, tenantID, definition, dimension, []tenantdata.AggregateFactBucketSegment{{
		Resolution: "hour",
		Starts:     []time.Time{start},
	}})
	if err != nil {
		t.Fatalf("ReadSegments() error = %v", err)
	}
	if totals.Count != 7 {
		t.Fatalf("ReadSegments() count = %d, want last committed value 7", totals.Count)
	}
}
