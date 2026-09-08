package redisfacts

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/tenantdata"
)

type Reader struct {
	client *redis.Client
}

var readBucketsScript = redis.NewScript(`
local sum_field = ARGV[1]
local count_field = ARGV[2]
local needs_sum = ARGV[3] == '1'
local needs_count = ARGV[4] == '1'
local out = {}
for i = 1, #KEYS do
  local sum_value = ''
  local count_value = ''
  if needs_sum then sum_value = redis.call('HGET', KEYS[i], sum_field) or '' end
  if needs_count then count_value = redis.call('HGET', KEYS[i], count_field) or '' end
  table.insert(out, sum_value)
  table.insert(out, count_value)
end
return out
`)

const (
	minuteRetention = 7 * 24 * time.Hour
	hourRetention   = 7 * 24 * time.Hour
	dayRetention    = 365 * 24 * time.Hour
)

func New(redisURL string) (*Reader, error) {
	options, err := redis.ParseURL(strings.TrimSpace(redisURL))
	if err != nil {
		return nil, fmt.Errorf("parse aggregate fact redis url: %w", err)
	}
	return &Reader{client: redis.NewClient(options)}, nil
}

func (r *Reader) Close() error {
	if r == nil || r.client == nil {
		return nil
	}
	return r.client.Close()
}

func (r *Reader) Ping(ctx context.Context) error {
	if r == nil || r.client == nil {
		return fmt.Errorf("aggregate fact reader is not configured")
	}
	return r.client.Ping(ctx).Err()
}

func (r *Reader) Covers(ctx context.Context, tenantID string, definition ports.AggregateFactDefinition, lower time.Time) (bool, error) {
	// Continue honoring the former definition-wide marker during upgrades. New
	// writers use bucket/dimension pending fields instead.
	values, err := r.client.MGet(ctx, factCoverageKey(tenantID, definition), factDirtyKey(tenantID, definition)).Result()
	if err != nil {
		return false, err
	}
	if values[0] == nil || values[1] != nil {
		return false, nil
	}
	raw := fmt.Sprint(values[0])
	prefix := definition.CoverageToken + "|"
	if !strings.HasPrefix(raw, prefix) {
		return false, nil
	}
	coverage := strings.TrimPrefix(raw, prefix)
	if coverage == "all" {
		return true, nil
	}
	start, err := time.Parse(time.RFC3339Nano, coverage)
	if err != nil {
		return false, err
	}
	return !lower.Before(start), nil
}

func (r *Reader) ReadSegments(ctx context.Context, tenantID string, definition ports.AggregateFactDefinition, dimension string, segments []tenantdata.AggregateFactBucketSegment) (tenantdata.AggregateFactTotals, error) {
	bucketCount := 0
	for _, segment := range segments {
		bucketCount += len(segment.Starts)
	}
	keys := make([]string, 0, bucketCount)
	args := make([]any, 0, 4)
	args = append(args, "s:"+dimension, "c:"+dimension, boolFlag(definition.NeedsSum), boolFlag(definition.NeedsCount))
	for _, segment := range segments {
		for _, start := range segment.Starts {
			keys = append(keys, factBucketKey(tenantID, definition, segment.Resolution, start))
		}
	}
	values, err := readBucketsScript.Run(ctx, r.client, keys, args...).Slice()
	if err != nil {
		return tenantdata.AggregateFactTotals{}, fmt.Errorf("read aggregate facts: %w", err)
	}
	if len(values) != 2*len(keys) {
		return tenantdata.AggregateFactTotals{}, fmt.Errorf("read aggregate facts: unexpected response length %d", len(values))
	}
	totals := tenantdata.AggregateFactTotals{}
	for i := range keys {
		sumRaw := redisString(values[2*i])
		countRaw := redisString(values[2*i+1])
		if definition.NeedsSum && sumRaw != "" {
			value, err := strconv.ParseFloat(sumRaw, 64)
			if err != nil {
				return totals, err
			}
			totals.Sum += value
		}
		if definition.NeedsCount && countRaw != "" {
			value, err := strconv.ParseInt(countRaw, 10, 64)
			if err != nil {
				return totals, err
			}
			totals.Count += value
		}
	}
	return totals, nil
}

func factResolutionPolicy(resolution string) (time.Duration, time.Duration, bool) {
	switch resolution {
	case "minute":
		return time.Minute, minuteRetention, true
	case "hour":
		return time.Hour, hourRetention, true
	case "day":
		return 24 * time.Hour, dayRetention, true
	default:
		return 0, 0, false
	}
}

func factDirtyKey(tenantID string, definition ports.AggregateFactDefinition) string {
	return fmt.Sprintf("facts:{%s}:definition:%s:v%d:dirty", tenantID, definition.ID, definition.Version)
}

func factCoverageKey(tenantID string, definition ports.AggregateFactDefinition) string {
	return fmt.Sprintf("facts:{%s}:definition:%s:v%d:coverage", tenantID, definition.ID, definition.Version)
}

func factBucketKey(tenantID string, definition ports.AggregateFactDefinition, resolution string, bucket time.Time) string {
	return fmt.Sprintf("facts:{%s}:definition:%s:v%d:%s:%d", tenantID, definition.ID, definition.Version, resolution, bucket.Unix())
}

func boolFlag(value bool) string {
	if value {
		return "1"
	}
	return "0"
}

func redisString(value any) string {
	if value == nil {
		return ""
	}
	if raw, ok := value.([]byte); ok {
		return string(raw)
	}
	return fmt.Sprint(value)
}

var _ tenantdata.AggregateFactBucketReader = (*Reader)(nil)
