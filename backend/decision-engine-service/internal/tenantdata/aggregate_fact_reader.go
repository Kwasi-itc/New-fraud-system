package tenantdata

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"golang.org/x/sync/singleflight"
)

type AggregateFactTotals struct {
	Sum   float64
	Count int64
}

var ErrAggregateFactUnavailable = errors.New("aggregate fact is temporarily unavailable")

type AggregateFactBucketSegment struct {
	Resolution string
	Starts     []time.Time
}

type AggregateFactBucketReader interface {
	Covers(ctx context.Context, tenantID string, definition ports.AggregateFactDefinition, lower time.Time) (bool, error)
	ReadSegments(ctx context.Context, tenantID string, definition ports.AggregateFactDefinition, dimension string, segments []AggregateFactBucketSegment) (AggregateFactTotals, error)
}

type aggregateFactReader struct {
	base     ports.TenantDataReader
	registry ports.AggregateFactRegistry
	buckets  AggregateFactBucketReader
	mu       sync.Mutex
	cache    map[string]factRegistryCacheEntry
	loads    singleflight.Group
}

type factRegistryCacheEntry struct {
	definitions []ports.AggregateFactDefinition
	expiresAt   time.Time
}

type factWindow struct {
	lower          time.Time
	upper          time.Time
	lowerInclusive bool
	upperInclusive bool
}

func NewAggregateFactReader(base ports.TenantDataReader, registry ports.AggregateFactRegistry, buckets AggregateFactBucketReader) ports.TenantDataReader {
	if base == nil || registry == nil || buckets == nil {
		return base
	}
	return &aggregateFactReader{base: base, registry: registry, buckets: buckets, cache: map[string]factRegistryCacheEntry{}}
}

func (r *aggregateFactReader) GetRecord(ctx context.Context, tenantID, objectType, objectID string) (ports.TenantRecord, error) {
	return r.base.GetRecord(ctx, tenantID, objectType, objectID)
}

func (r *aggregateFactReader) ListRecords(ctx context.Context, tenantID, objectType string, limit int) ([]ports.TenantRecord, error) {
	return r.base.ListRecords(ctx, tenantID, objectType, limit)
}

func (r *aggregateFactReader) QueryRecords(ctx context.Context, tenantID, objectType, fieldName, value string, limit int) ([]ports.TenantRecord, error) {
	return r.base.QueryRecords(ctx, tenantID, objectType, fieldName, value, limit)
}

func (r *aggregateFactReader) AggregateRecords(ctx context.Context, tenantID string, query ports.AggregateQuery) (any, error) {
	aggregateFactMetrics.requests.Add(1)
	definitions, err := r.activeDefinitions(ctx, tenantID, query.ObjectType)
	if err != nil {
		aggregateFactMetrics.fallbackRegistryFailure.Add(1)
		return r.base.AggregateRecords(ctx, tenantID, query)
	}
	definition, values, window, ok := matchAggregateFact(definitions, query)
	if !ok {
		aggregateFactMetrics.fallbackNoMatchingFact.Add(1)
		return r.base.AggregateRecords(ctx, tenantID, query)
	}
	aggregateFactMetrics.matchedRequests.Add(1)
	coverageStartedAt := time.Now()
	covered, err := r.buckets.Covers(ctx, tenantID, definition, window.lower)
	recordAggregateFactValkeyRead("coverage", time.Since(coverageStartedAt), err, 0)
	if err != nil {
		aggregateFactMetrics.coverageReadFailures.Add(1)
		return nil, fmt.Errorf("%w: read coverage: %v", ErrAggregateFactUnavailable, err)
	}
	if !covered {
		aggregateFactMetrics.incompleteCoverageFailures.Add(1)
		return nil, fmt.Errorf("%w: coverage does not include the requested window", ErrAggregateFactUnavailable)
	}
	dimension := hashDimensionValues(values)
	segments := planFactWindow(window, definition.MinuteEnabled)
	if len(segments) == 0 {
		aggregateFactMetrics.noUsableBucketsFailures.Add(1)
		return nil, fmt.Errorf("%w: no cache bucket can answer the requested window", ErrAggregateFactUnavailable)
	}
	bucketCount := 0
	for _, segment := range segments {
		bucketCount += len(segment.Starts)
		switch segment.Resolution {
		case "minute":
			aggregateFactMetrics.minuteBucketsRead.Add(uint64(len(segment.Starts)))
		case "hour":
			aggregateFactMetrics.hourBucketsRead.Add(uint64(len(segment.Starts)))
		case "day":
			aggregateFactMetrics.dayBucketsRead.Add(uint64(len(segment.Starts)))
		}
	}
	startedAt := time.Now()
	totals, readErr := r.buckets.ReadSegments(ctx, tenantID, definition, dimension, segments)
	recordAggregateFactValkeyRead("buckets", time.Since(startedAt), readErr, bucketCount)
	if readErr != nil {
		aggregateFactMetrics.bucketReadFailures.Add(1)
		return nil, fmt.Errorf("%w: read cache buckets: %v", ErrAggregateFactUnavailable, readErr)
	}
	aggregateFactMetrics.factHits.Add(1)
	switch strings.ToLower(query.Aggregate) {
	case "count":
		return totals.Count, nil
	case "sum":
		return totals.Sum, nil
	case "avg":
		if totals.Count == 0 {
			return nil, nil
		}
		return totals.Sum / float64(totals.Count), nil
	default:
		return r.base.AggregateRecords(ctx, tenantID, query)
	}
}

func (r *aggregateFactReader) activeDefinitions(ctx context.Context, tenantID, tableName string) ([]ports.AggregateFactDefinition, error) {
	key := tenantID + "|" + tableName
	r.mu.Lock()
	entry, ok := r.cache[key]
	r.mu.Unlock()
	if ok && time.Now().Before(entry.expiresAt) {
		return entry.definitions, nil
	}
	loaded, err, _ := r.loads.Do(key, func() (any, error) {
		r.mu.Lock()
		cached, cachedOK := r.cache[key]
		r.mu.Unlock()
		if cachedOK && time.Now().Before(cached.expiresAt) {
			return cached.definitions, nil
		}
		definitions, loadErr := r.registry.ListActive(ctx, tenantID, tableName)
		if loadErr != nil {
			return nil, loadErr
		}
		r.mu.Lock()
		r.cache[key] = factRegistryCacheEntry{definitions: definitions, expiresAt: time.Now().Add(2 * time.Second)}
		r.mu.Unlock()
		return definitions, nil
	})
	if err != nil {
		return nil, err
	}
	return loaded.([]ports.AggregateFactDefinition), nil
}

func matchAggregateFact(definitions []ports.AggregateFactDefinition, query ports.AggregateQuery) (ports.AggregateFactDefinition, []any, factWindow, bool) {
	if query.Filter == nil {
		return ports.AggregateFactDefinition{}, nil, factWindow{}, false
	}
	predicates, ok := flatFactPredicates(*query.Filter)
	if !ok {
		return ports.AggregateFactDefinition{}, nil, factWindow{}, false
	}
	for _, definition := range definitions {
		aggregate := strings.ToLower(strings.TrimSpace(query.Aggregate))
		if query.Field != definition.MeasureField ||
			(aggregate == "sum" && !definition.NeedsSum) ||
			(aggregate == "count" && !definition.NeedsCount) ||
			(aggregate == "avg" && (!definition.NeedsSum || !definition.NeedsCount)) {
			continue
		}
		valuesByField := map[string]any{}
		var lower, upper time.Time
		lowerInclusive := false
		matched := true
		for _, predicate := range predicates {
			switch {
			case containsString(definition.DimensionFields, predicate.Field) && predicate.Op == "eq":
				valuesByField[predicate.Field] = predicate.Value
			case predicate.Field == definition.EventTimeField && (predicate.Op == "gt" || predicate.Op == "gte"):
				lower, ok = factTime(predicate.Value)
				matched = matched && ok
				lowerInclusive = predicate.Op == "gte"
			case predicate.Field == definition.EventTimeField && predicate.Op == "lte":
				upper, ok = factTime(predicate.Value)
				matched = matched && ok
			default:
				matched = false
			}
		}
		if !matched || len(predicates) != len(definition.DimensionFields)+2 || !upper.After(lower) || upper.Sub(lower) > time.Duration(definition.MaxWindowSeconds)*time.Second {
			continue
		}
		values := make([]any, len(definition.DimensionFields))
		for index, field := range definition.DimensionFields {
			value, exists := valuesByField[field]
			if !exists || value == nil {
				matched = false
				break
			}
			values[index] = value
		}
		if matched {
			return definition, values, factWindow{
				lower: lower.UTC(), upper: upper.UTC(), lowerInclusive: lowerInclusive, upperInclusive: true,
			}, true
		}
	}
	return ports.AggregateFactDefinition{}, nil, factWindow{}, false
}

func flatFactPredicates(filter ports.AggregateFilter) ([]ports.AggregateFilter, bool) {
	if strings.EqualFold(filter.Kind, "predicate") {
		return []ports.AggregateFilter{filter}, true
	}
	if filter.Kind != "" && !strings.EqualFold(filter.Kind, "group") {
		return nil, false
	}
	if filter.Operator != "" && !strings.EqualFold(filter.Operator, "and") {
		return nil, false
	}
	out := make([]ports.AggregateFilter, 0, len(filter.Children))
	for _, child := range filter.Children {
		children, ok := flatFactPredicates(child)
		if !ok {
			return nil, false
		}
		out = append(out, children...)
	}
	return out, true
}

func planFactWindow(window factWindow, minuteEnabled bool) []AggregateFactBucketSegment {
	duration := window.upper.Sub(window.lower)
	if minuteEnabled && duration <= 6*time.Hour {
		return factSegmentsForResolution(window, "minute", time.Minute)
	}
	if duration <= 7*24*time.Hour {
		return factSegmentsForResolution(window, "hour", time.Hour)
	}
	return compactHourSegments(window)
}

// factSegmentsForResolution deliberately rounds the lower boundary forward,
// discarding that partial bucket, and includes the active bucket containing the
// payload upper timestamp. Facts therefore answer the whole supported query
// without a PostgreSQL edge lookup.
func factSegmentsForResolution(window factWindow, resolution string, size time.Duration) []AggregateFactBucketSegment {
	if !window.upper.After(window.lower) || size <= 0 {
		return nil
	}
	first := window.lower.Truncate(size)
	if first.Before(window.lower) {
		first = first.Add(size)
	}
	last := window.upper.Truncate(size)
	if first.After(last) {
		first = last
	}
	starts := make([]time.Time, 0, int(last.Sub(first)/size)+1)
	for start := first; !start.After(last); start = start.Add(size) {
		starts = append(starts, start)
	}
	if len(starts) == 0 {
		return nil
	}
	return []AggregateFactBucketSegment{{Resolution: resolution, Starts: starts}}
}

func compactHourSegments(window factWindow) []AggregateFactBucketSegment {
	hourSegments := factSegmentsForResolution(window, "hour", time.Hour)
	if len(hourSegments) == 0 {
		return nil
	}
	hours := hourSegments[0].Starts
	days := make([]time.Time, 0, len(hours)/24)
	edges := make([]time.Time, 0, 48)
	for index := 0; index < len(hours); {
		start := hours[index]
		if start.Hour() == 0 && start.Minute() == 0 && start.Second() == 0 && index+24 <= len(hours) {
			completeDay := true
			for offset := 1; offset < 24; offset++ {
				if !hours[index+offset].Equal(start.Add(time.Duration(offset) * time.Hour)) {
					completeDay = false
					break
				}
			}
			if completeDay {
				days = append(days, start)
				index += 24
				continue
			}
		}
		edges = append(edges, start)
		index++
	}
	segments := make([]AggregateFactBucketSegment, 0, 2)
	if len(days) > 0 {
		segments = append(segments, AggregateFactBucketSegment{Resolution: "day", Starts: days})
	}
	if len(edges) > 0 {
		segments = append(segments, AggregateFactBucketSegment{Resolution: "hour", Starts: edges})
	}
	return segments
}

func hashDimensionValues(values []any) string {
	canonical := make([]string, len(values))
	for index, value := range values {
		canonical[index] = canonicalFactDimensionToken(value)
	}
	payload, _ := json.Marshal(canonical)
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:16])
}

func canonicalFactDimensionToken(value any) string {
	switch typed := value.(type) {
	case time.Time:
		return "t:" + typed.UTC().Format(time.RFC3339Nano)
	case string:
		if parsed, err := time.Parse(time.RFC3339Nano, typed); err == nil {
			return "t:" + parsed.UTC().Format(time.RFC3339Nano)
		}
		return "s:" + typed
	case bool:
		return "b:" + strconv.FormatBool(typed)
	case int:
		return "n:" + strconv.FormatInt(int64(typed), 10)
	case int8:
		return "n:" + strconv.FormatInt(int64(typed), 10)
	case int16:
		return "n:" + strconv.FormatInt(int64(typed), 10)
	case int32:
		return "n:" + strconv.FormatInt(int64(typed), 10)
	case int64:
		return "n:" + strconv.FormatInt(typed, 10)
	case uint:
		return "n:" + strconv.FormatUint(uint64(typed), 10)
	case uint8:
		return "n:" + strconv.FormatUint(uint64(typed), 10)
	case uint16:
		return "n:" + strconv.FormatUint(uint64(typed), 10)
	case uint32:
		return "n:" + strconv.FormatUint(uint64(typed), 10)
	case uint64:
		return "n:" + strconv.FormatUint(typed, 10)
	case float32:
		return canonicalFactDimensionFloat(float64(typed))
	case float64:
		return canonicalFactDimensionFloat(typed)
	case json.Number:
		parsed, err := typed.Float64()
		if err == nil {
			return canonicalFactDimensionFloat(parsed)
		}
	}
	payload, _ := json.Marshal(value)
	return "j:" + string(payload)
}

func canonicalFactDimensionFloat(value float64) string {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return "n:invalid"
	}
	return "n:" + strconv.FormatFloat(value, 'g', -1, 64)
}

func factTime(value any) (time.Time, bool) {
	switch typed := value.(type) {
	case time.Time:
		return typed, true
	case string:
		parsed, err := time.Parse(time.RFC3339Nano, typed)
		return parsed, err == nil
	default:
		return time.Time{}, false
	}
}

func containsString(values []string, candidate string) bool {
	index := sort.SearchStrings(values, candidate)
	return index < len(values) && values[index] == candidate
}

var _ ports.TenantDataReader = (*aggregateFactReader)(nil)
