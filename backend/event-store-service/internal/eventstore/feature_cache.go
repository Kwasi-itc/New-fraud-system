package eventstore

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"sync"
	"time"
)

// featureCache stores only promoted bucket series. A series is keyed by the
// value-independent fact template plus its concrete dimension values. Each
// bucket carries its durable ClickHouse generation, so an event write makes
// only the affected bucket stale instead of invalidating the whole table.
type featureCache struct {
	client           valkeyClient
	maxKeys          int
	maxKeysPerTenant int
	admissionHits    int
	slowQuery        time.Duration
	ttl              time.Duration
	namespace        string
	localMu          sync.Mutex
	localHits        sync.Map
}

type localFeatureHit struct {
	Count     int
	ExpiresAt time.Time
}

func newFeatureCache(cfg Config) *featureCache {
	namespace := strings.TrimSuffix(strings.TrimSpace(cfg.FeatureNamespace), ":")
	if namespace == "" {
		namespace = "fraud:event-feature:v1"
	}
	return &featureCache{
		client:           newValkeyClient(cfg.ValkeyAddress, 100*time.Millisecond),
		maxKeys:          maxInt(1, cfg.FeatureMaxKeys),
		maxKeysPerTenant: maxInt(1, cfg.FeatureMaxKeysPerTenant),
		admissionHits:    maxInt(1, cfg.FeatureAdmissionHits),
		slowQuery:        time.Duration(maxInt(1, cfg.FeatureSlowQueryMS)) * time.Millisecond,
		ttl:              cfg.FeatureTTL,
		namespace:        namespace,
	}
}

func (c *featureCache) shouldPromoteTemplate(ctx context.Context, tenantID, templateHash string, elapsed time.Duration) bool {
	threshold := c.admissionHits
	// A slow template may be promoted on its second observation, never its
	// first. This prevents a single expensive, one-off dimension from filling
	// the cache while still reacting quickly to repeatedly expensive rules.
	if elapsed >= c.slowQuery && threshold > 2 {
		threshold = 2
	}
	key := "template:" + tenantID + ":" + templateHash
	return c.incrementFrequency(ctx, key) >= threshold
}

func (c *featureCache) getSeries(ctx context.Context, tenantID, seriesKey string) (aggregateBucketSeries, bool) {
	empty := aggregateBucketSeries{Buckets: map[string]aggregateBucketFact{}}
	if c == nil {
		return empty, false
	}
	member := tenantID + ":" + seriesKey
	if promotedAt, err := c.client.command(ctx, "ZSCORE", c.key("series:promoted"), member); err != nil || promotedAt == "" {
		return empty, false
	}
	value, err := c.client.command(ctx, "GET", c.key("series:result:"+member))
	if err != nil || value == "" {
		return empty, false
	}
	var series aggregateBucketSeries
	if err := json.Unmarshal([]byte(value), &series); err != nil {
		return empty, false
	}
	if series.Buckets == nil {
		series.Buckets = map[string]aggregateBucketFact{}
	}
	return series, true
}

func (c *featureCache) observeSeries(ctx context.Context, tenantID, seriesKey string, series aggregateBucketSeries, cacheHit, forceAdmission bool) {
	if c == nil {
		return
	}
	member := tenantID + ":" + seriesKey
	if !cacheHit && !forceAdmission {
		if c.incrementFrequency(ctx, "series:"+member) < c.admissionHits {
			return
		}
	}
	if !cacheHit {
		if !c.admitSeries(ctx, tenantID, member) {
			return
		}
	} else {
		now := strconv.FormatInt(time.Now().Unix(), 10)
		_, _ = c.client.command(ctx, "ZADD", c.key("series:promoted"), now, member)
		_, _ = c.client.command(ctx, "ZADD", c.key("series:promoted:tenant:"+tenantID), now, member)
	}
	seconds := maxInt(1, int(c.ttl.Seconds()))
	_, _ = c.client.command(ctx, "SETEX", c.key("series:result:"+member), strconv.Itoa(seconds), encodeBucketSeries(series))
}

func (c *featureCache) admitSeries(ctx context.Context, tenantID, member string) bool {
	now := time.Now()
	expiredBefore := strconv.FormatInt(now.Add(-c.ttl).Unix(), 10)
	globalKey := c.key("series:promoted")
	tenantKey := c.key("series:promoted:tenant:" + tenantID)
	if _, err := c.client.command(ctx, "ZREMRANGEBYSCORE", globalKey, "-inf", expiredBefore); err != nil {
		return false
	}
	if _, err := c.client.command(ctx, "ZREMRANGEBYSCORE", tenantKey, "-inf", expiredBefore); err != nil {
		return false
	}
	if promotedAt, err := c.client.command(ctx, "ZSCORE", globalKey, member); err == nil && promotedAt != "" {
		return true
	}
	globalSize, err := c.client.command(ctx, "ZCARD", globalKey)
	if err != nil {
		return false
	}
	tenantSize, err := c.client.command(ctx, "ZCARD", tenantKey)
	if err != nil {
		return false
	}
	globalCount, _ := strconv.Atoi(globalSize)
	tenantCount, _ := strconv.Atoi(tenantSize)
	if globalCount >= c.maxKeys || tenantCount >= c.maxKeysPerTenant {
		return false
	}
	score := strconv.FormatInt(now.Unix(), 10)
	if _, err := c.client.command(ctx, "ZADD", globalKey, score, member); err != nil {
		return false
	}
	if _, err := c.client.command(ctx, "ZADD", tenantKey, score, member); err != nil {
		return false
	}
	seconds := strconv.Itoa(maxInt(1, int(c.ttl.Seconds())))
	_, _ = c.client.command(ctx, "EXPIRE", globalKey, seconds)
	_, _ = c.client.command(ctx, "EXPIRE", tenantKey, seconds)
	return true
}

func (c *featureCache) incrementFrequency(ctx context.Context, suffix string) int {
	key := c.key("frequency:" + suffix)
	if raw, err := c.client.command(ctx, "INCR", key); err == nil {
		_, _ = c.client.command(ctx, "EXPIRE", key, "900")
		count, _ := strconv.Atoi(raw)
		return count
	}
	now := time.Now()
	c.localMu.Lock()
	defer c.localMu.Unlock()
	value, _ := c.localHits.LoadOrStore(suffix, localFeatureHit{Count: 0, ExpiresAt: now.Add(15 * time.Minute)})
	hit := value.(localFeatureHit)
	if now.After(hit.ExpiresAt) {
		hit = localFeatureHit{ExpiresAt: now.Add(15 * time.Minute)}
	}
	hit.Count++
	c.localHits.Store(suffix, hit)
	return hit.Count
}

func (c *featureCache) key(suffix string) string { return c.namespace + ":" + suffix }

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
