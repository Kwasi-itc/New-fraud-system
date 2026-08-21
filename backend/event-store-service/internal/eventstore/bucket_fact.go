package eventstore

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

const aggregateFactBucketSize = time.Hour

type aggregateFactDimension struct {
	Field string
	Value any
}

type aggregateFactPlan struct {
	Request         AggregateRequest
	TemplateHash    string
	Dimensions      []aggregateFactDimension
	StaticFilter    *AggregateFilter
	LowerBound      time.Time
	LowerBoundOp    string
	UpperBound      time.Time
	UpperBoundOp    string
	FullBucketStart time.Time
	NumericField    bool
	TemplateWide    bool
}

type aggregateFactComponents struct {
	Sum   float64 `json:"sum"`
	Count uint64  `json:"count"`
}

type aggregateBucketFact struct {
	Generation uint64  `json:"generation"`
	Sum        float64 `json:"sum"`
	Count      uint64  `json:"count"`
}

type aggregateBucketSeries struct {
	Buckets map[string]aggregateBucketFact `json:"buckets"`
}

func planAggregateFacts(request AggregateRequest) (aggregateFactPlan, bool) {
	aggregate := strings.ToLower(strings.TrimSpace(request.Aggregate))
	if aggregate != "count" && aggregate != "sum" && aggregate != "avg" {
		return aggregateFactPlan{}, false
	}
	field, ok := request.Table.Fields[request.Field]
	if !ok {
		return aggregateFactPlan{}, false
	}
	numeric := field.DataType == "int" || field.DataType == "float"
	if (aggregate == "sum" || aggregate == "avg") && !numeric {
		return aggregateFactPlan{}, false
	}

	predicates, ok := flattenAndPredicates(request.Filter)
	if !ok {
		return aggregateFactPlan{}, false
	}
	dimensions := make([]aggregateFactDimension, 0, len(predicates))
	staticPredicates := make([]AggregateFilter, 0, len(predicates))
	seenDimensions := map[string]struct{}{}
	var lowerBound time.Time
	lowerOp := ""
	var upperBound time.Time
	upperOp := ""
	for _, predicate := range predicates {
		if predicate.Field == request.Table.EventTimeField {
			switch predicate.Op {
			case "gte", "gt":
				if lowerOp != "" {
					return aggregateFactPlan{}, false
				}
				parsed, err := parseAggregateFactTime(predicate.Value)
				if err != nil {
					return aggregateFactPlan{}, false
				}
				lowerBound = parsed
				lowerOp = predicate.Op
			case "lte", "lt":
				if upperOp != "" {
					return aggregateFactPlan{}, false
				}
				parsed, err := parseAggregateFactTime(predicate.Value)
				if err != nil {
					return aggregateFactPlan{}, false
				}
				upperBound = parsed
				upperOp = predicate.Op
			default:
				return aggregateFactPlan{}, false
			}
			continue
		}
		if predicate.Op == "eq" && predicate.Value != nil {
			if _, exists := seenDimensions[predicate.Field]; exists {
				return aggregateFactPlan{}, false
			}
			seenDimensions[predicate.Field] = struct{}{}
			dimensions = append(dimensions, aggregateFactDimension{Field: predicate.Field, Value: predicate.Value})
			continue
		}
		staticPredicates = append(staticPredicates, predicate)
	}
	if lowerOp == "" {
		return aggregateFactPlan{}, false
	}
	if upperOp != "" && upperBound.Before(lowerBound) {
		return aggregateFactPlan{}, false
	}
	sort.Slice(dimensions, func(i, j int) bool { return dimensions[i].Field < dimensions[j].Field })
	sort.Slice(staticPredicates, func(i, j int) bool {
		return string(canonicalJSON(staticPredicates[i])) < string(canonicalJSON(staticPredicates[j]))
	})

	var staticFilter *AggregateFilter
	if len(staticPredicates) == 1 {
		copy := staticPredicates[0]
		staticFilter = &copy
	} else if len(staticPredicates) > 1 {
		staticFilter = &AggregateFilter{Kind: "group", Operator: "and", Children: staticPredicates}
	}

	fullStart := lowerBound.Truncate(aggregateFactBucketSize)
	if lowerOp == "gt" || !lowerBound.Equal(fullStart) {
		fullStart = fullStart.Add(aggregateFactBucketSize)
	}
	dimensionFields := make([]string, len(dimensions))
	for i, dimension := range dimensions {
		dimensionFields[i] = dimension.Field
	}
	template := struct {
		TenantID       string           `json:"tenant_id"`
		TableID        string           `json:"table_id"`
		SchemaRevision string           `json:"schema_revision"`
		EventTimeField string           `json:"event_time_field"`
		MeasureField   string           `json:"measure_field"`
		Dimensions     []string         `json:"dimensions"`
		StaticFilter   *AggregateFilter `json:"static_filter,omitempty"`
		BucketSeconds  int64            `json:"bucket_seconds"`
	}{
		TenantID: request.Table.TenantID, TableID: request.Table.TableID,
		SchemaRevision: request.Table.SchemaRevision, EventTimeField: request.Table.EventTimeField,
		MeasureField: request.Field, Dimensions: dimensionFields, StaticFilter: staticFilter,
		BucketSeconds: int64(aggregateFactBucketSize / time.Second),
	}
	hash := sha256.Sum256(canonicalJSON(template))
	return aggregateFactPlan{
		Request: request, TemplateHash: hex.EncodeToString(hash[:]), Dimensions: dimensions,
		StaticFilter: staticFilter, LowerBound: lowerBound, LowerBoundOp: lowerOp,
		UpperBound: upperBound, UpperBoundOp: upperOp,
		FullBucketStart: fullStart, NumericField: numeric,
	}, true
}

func flattenAndPredicates(filter *AggregateFilter) ([]AggregateFilter, bool) {
	if filter == nil {
		return nil, false
	}
	kind := strings.ToLower(strings.TrimSpace(filter.Kind))
	if kind == "predicate" {
		return []AggregateFilter{*filter}, true
	}
	if kind != "" && kind != "group" {
		return nil, false
	}
	operator := strings.ToLower(strings.TrimSpace(filter.Operator))
	if operator == "" {
		operator = "and"
	}
	if operator != "and" {
		return nil, false
	}
	out := make([]AggregateFilter, 0, len(filter.Children))
	for i := range filter.Children {
		children, ok := flattenAndPredicates(&filter.Children[i])
		if !ok {
			return nil, false
		}
		out = append(out, children...)
	}
	return out, len(out) > 0
}

func parseAggregateFactTime(value any) (time.Time, error) {
	switch typed := value.(type) {
	case time.Time:
		return typed.UTC(), nil
	case string:
		parsed, err := time.Parse(time.RFC3339Nano, typed)
		if err != nil {
			return time.Time{}, err
		}
		return parsed.UTC(), nil
	default:
		return time.Time{}, fmt.Errorf("aggregate time bound must be RFC3339 text")
	}
}

func (p aggregateFactPlan) partialFilter() *AggregateFilter {
	if p.LowerBound.Equal(p.FullBucketStart) && p.LowerBoundOp == "gte" {
		return nil
	}
	children := []AggregateFilter{
		*p.Request.Filter,
		AggregateFilter{Kind: "predicate", Field: p.Request.Table.EventTimeField, Op: "lt", Value: p.FullBucketStart.Format(time.RFC3339Nano)},
	}
	return andFilter(children)
}

func (p aggregateFactPlan) fullBucketEnd(sealedBefore time.Time) time.Time {
	end := sealedBefore.UTC().Truncate(aggregateFactBucketSize)
	if !p.UpperBound.IsZero() {
		upperEnd := p.UpperBound.UTC().Truncate(aggregateFactBucketSize)
		if upperEnd.Before(end) {
			end = upperEnd
		}
	}
	return end
}

func (p aggregateFactPlan) upperBoundaryFilter(fullBucketEnd time.Time) *AggregateFilter {
	if !p.UpperBound.IsZero() && p.UpperBound.Equal(fullBucketEnd) && p.UpperBoundOp == "lt" {
		return nil
	}
	children := []AggregateFilter{*p.Request.Filter, AggregateFilter{
		Kind: "predicate", Field: p.Request.Table.EventTimeField, Op: "gte",
		Value: fullBucketEnd.UTC().Format(time.RFC3339Nano),
	}}
	return andFilter(children)
}

func (p aggregateFactPlan) nonTimePredicates() []AggregateFilter {
	children := make([]AggregateFilter, 0, len(p.Dimensions)+1)
	for _, dimension := range p.Dimensions {
		children = append(children, AggregateFilter{Kind: "predicate", Field: dimension.Field, Op: "eq", Value: dimension.Value})
	}
	if p.StaticFilter != nil {
		if flattened, ok := flattenAndPredicates(p.StaticFilter); ok {
			children = append(children, flattened...)
		} else {
			children = append(children, *p.StaticFilter)
		}
	}
	return children
}

func andFilter(children []AggregateFilter) *AggregateFilter {
	if len(children) == 0 {
		return nil
	}
	if len(children) == 1 {
		copy := children[0]
		return &copy
	}
	return &AggregateFilter{Kind: "group", Operator: "and", Children: children}
}

func composeAggregateFactResult(aggregate string, components aggregateFactComponents) any {
	switch strings.ToLower(strings.TrimSpace(aggregate)) {
	case "count":
		return components.Count
	case "sum":
		return components.Sum
	case "avg":
		if components.Count == 0 {
			return nil
		}
		return components.Sum / float64(components.Count)
	default:
		return nil
	}
}

func aggregateSeriesKey(plan aggregateFactPlan) string {
	values := make([]any, len(plan.Dimensions))
	for i, dimension := range plan.Dimensions {
		values[i] = dimension.Value
	}
	payload := struct {
		TemplateHash string `json:"template_hash"`
		Dimensions   []any  `json:"dimensions"`
	}{TemplateHash: plan.TemplateHash, Dimensions: values}
	hash := sha256.Sum256(canonicalJSON(payload))
	return hex.EncodeToString(hash[:])
}

func aggregateFactShapeKey(plan aggregateFactPlan) string {
	if plan.TemplateWide {
		return plan.TemplateHash
	}
	return aggregateSeriesKey(plan)
}

func encodeBucketSeries(series aggregateBucketSeries) string {
	body, _ := json.Marshal(series)
	return string(body)
}
