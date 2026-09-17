package ast_eval

import (
	"context"
	"fmt"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"golang.org/x/sync/singleflight"
)

type Runtime struct {
	TenantID                    string
	ObjectID                    string
	ObjectType                  string
	Fields                      map[string]any
	Now                         time.Time
	Model                       *ports.TenantModel
	TenantDataReader            ports.TenantDataReader
	CustomListRepo              ports.CustomListRepository
	RecordTagRepo               ports.RecordTagRepository
	RiskRepo                    ports.RiskSnapshotRepository
	IPFlagRepo                  ports.IPFlagRepository
	GeoIPLookup                 ports.GeoIPLookup
	DecisionRepo                ports.DecisionRepository
	AggregatePushdownMode       string
	AggregatePushdownAggregates []string
	AggregateRemoteConcurrency  int
	EvalCache                   *EvaluationCache
	AggregateResultCache        *AggregateResultCache
	RelatedPathCache            *RelatedPathCache
	GeoIPResultCache            *GeoIPResultCache
}

const (
	AggregatePushdownModeEnabled  = "enabled"
	AggregatePushdownModeDisabled = "disabled"
	AggregatePushdownModeStrict   = "strict"
)

func (r Runtime) aggregatePushdownMode() string {
	switch r.AggregatePushdownMode {
	case AggregatePushdownModeDisabled:
		return AggregatePushdownModeDisabled
	case AggregatePushdownModeStrict:
		return AggregatePushdownModeStrict
	default:
		return AggregatePushdownModeEnabled
	}
}

func (r Runtime) aggregatePushdownEnabled() bool {
	return r.aggregatePushdownMode() != AggregatePushdownModeDisabled
}

func (r Runtime) aggregatePushdownStrict() bool {
	return r.aggregatePushdownMode() == AggregatePushdownModeStrict
}

func (r Runtime) aggregatePushdownSupportsAggregate(name string) bool {
	if len(r.AggregatePushdownAggregates) == 0 {
		return true
	}
	canonical := strings.ToLower(strings.TrimSpace(name))
	for _, item := range r.AggregatePushdownAggregates {
		if strings.ToLower(strings.TrimSpace(item)) == canonical {
			return true
		}
	}
	return false
}

type ScoreComputationResult struct {
	Triggered bool `json:"triggered"`
	Modifier  int  `json:"modifier"`
	Floor     int  `json:"floor"`

	Branch   *int `json:"branch,omitempty"`
	Fallback bool `json:"fallback"`
	Default  bool `json:"default"`
}

type RelatedPathCache struct {
	mu      sync.Mutex
	entries map[string]relatedPathCacheEntry
}

type relatedPathCacheEntry struct {
	fields     map[string]any
	objectType string
	found      bool
}

func NewRelatedPathCache() *RelatedPathCache {
	return &RelatedPathCache{entries: map[string]relatedPathCacheEntry{}}
}

func (c *RelatedPathCache) Get(key string) (map[string]any, string, bool, bool) {
	if c == nil {
		return nil, "", false, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok {
		return nil, "", false, false
	}
	return cloneFieldMap(entry.fields), entry.objectType, entry.found, true
}

func (c *RelatedPathCache) Set(key string, fields map[string]any, objectType string, found bool) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = relatedPathCacheEntry{
		fields:     cloneFieldMap(fields),
		objectType: objectType,
		found:      found,
	}
}

func cloneFieldMap(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

type geoIPResultCacheEntry struct {
	location ports.GeoIPLocation
	found    bool
}

// GeoIPResultCache is scoped to one decision evaluation. It prevents repeated
// IPCountry/IPRegion accessors (including concurrent rule evaluations) from
// decoding the same MMDB record more than once.
type GeoIPResultCache struct {
	mu      sync.RWMutex
	entries map[string]geoIPResultCacheEntry
	group   singleflight.Group
}

func NewGeoIPResultCache() *GeoIPResultCache {
	return &GeoIPResultCache{entries: map[string]geoIPResultCacheEntry{}}
}

func (c *GeoIPResultCache) Lookup(ctx context.Context, lookup ports.GeoIPLookup, address netip.Addr) (ports.GeoIPLocation, bool, error) {
	if lookup == nil {
		return ports.GeoIPLocation{}, false, fmt.Errorf("geoip lookup is not configured")
	}
	if c == nil {
		return lookup.Lookup(ctx, address)
	}
	key := address.Unmap().String()
	c.mu.RLock()
	entry, ok := c.entries[key]
	c.mu.RUnlock()
	if ok {
		return entry.location, entry.found, nil
	}

	value, err, _ := c.group.Do(key, func() (any, error) {
		c.mu.RLock()
		cached, exists := c.entries[key]
		c.mu.RUnlock()
		if exists {
			return cached, nil
		}
		location, found, err := lookup.Lookup(ctx, address)
		if err != nil {
			return nil, err
		}
		loaded := geoIPResultCacheEntry{location: location, found: found}
		c.mu.Lock()
		c.entries[key] = loaded
		c.mu.Unlock()
		return loaded, nil
	})
	if err != nil {
		return ports.GeoIPLocation{}, false, err
	}
	loaded := value.(geoIPResultCacheEntry)
	return loaded.location, loaded.found, nil
}

type contextKey string

const ruleNameContextKey contextKey = "rule_name"

func WithRuleName(ctx context.Context, ruleName string) context.Context {
	if ctx == nil || strings.TrimSpace(ruleName) == "" {
		return ctx
	}
	return context.WithValue(ctx, ruleNameContextKey, ruleName)
}

func RuleNameFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	value, _ := ctx.Value(ruleNameContextKey).(string)
	return strings.TrimSpace(value)
}
