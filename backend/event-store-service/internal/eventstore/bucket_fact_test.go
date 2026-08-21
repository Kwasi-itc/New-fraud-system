package eventstore

import (
	"testing"
	"time"
)

func TestAggregateFactTemplateIgnoresDimensionValuesAndSlidingLowerBound(t *testing.T) {
	table := testTableContract()
	table.Fields["account_ref"] = FieldContract{DataType: "string"}
	first := AggregateRequest{
		Table: table, Aggregate: "avg", Field: "amount",
		Filter: andFilter([]AggregateFilter{
			{Kind: "predicate", Field: "account_ref", Op: "eq", Value: "acct-1"},
			{Kind: "predicate", Field: "date", Op: "gte", Value: "2026-07-01T10:12:00Z"},
		}),
	}
	second := first
	second.Filter = andFilter([]AggregateFilter{
		{Kind: "predicate", Field: "account_ref", Op: "eq", Value: "acct-2"},
		{Kind: "predicate", Field: "date", Op: "gte", Value: "2026-07-01T10:13:00Z"},
	})
	firstPlan, ok := planAggregateFacts(first)
	if !ok {
		t.Fatal("first aggregate was not eligible for sealed facts")
	}
	secondPlan, ok := planAggregateFacts(second)
	if !ok {
		t.Fatal("second aggregate was not eligible for sealed facts")
	}
	if firstPlan.TemplateHash != secondPlan.TemplateHash {
		t.Fatalf("template hash includes concrete values: %s != %s", firstPlan.TemplateHash, secondPlan.TemplateHash)
	}
	if aggregateSeriesKey(firstPlan) == aggregateSeriesKey(secondPlan) {
		t.Fatal("concrete dimension series keys must remain distinct")
	}
	wantStart := time.Date(2026, 7, 1, 11, 0, 0, 0, time.UTC)
	if !firstPlan.FullBucketStart.Equal(wantStart) {
		t.Fatalf("full bucket start = %s, want %s", firstPlan.FullBucketStart, wantStart)
	}
}

func TestAggregationPolicySelectsConfiguredDimension(t *testing.T) {
	table := testTableContract()
	table.Fields["account_ref"] = FieldContract{
		DataType: "string", IsProjection: true,
		AggregationMode:         AggregationModeAdaptiveCache,
		AggregationColdBehavior: AggregationColdSkipRule,
	}
	request := AggregateRequest{
		Table: table, Aggregate: "count", Field: "amount",
		Filter: andFilter([]AggregateFilter{
			{Kind: "predicate", Field: "account_ref", Op: "eq", Value: "acct-1"},
			{Kind: "predicate", Field: "date", Op: "gte", Value: "2026-07-01T00:00:00Z"},
		}),
	}
	plan, ok := planAggregateFacts(request)
	if !ok {
		t.Fatal("aggregate was not eligible for policy selection")
	}
	policy := aggregatePolicyForPlan(plan)
	if policy.Mode != AggregationModeAdaptiveCache || policy.ColdBehavior != AggregationColdSkipRule || policy.Field != "account_ref" {
		t.Fatalf("policy = %#v", policy)
	}
}

func TestAggregationPolicyDoesNotWidenAcrossUnconfiguredDimension(t *testing.T) {
	table := testTableContract()
	table.Fields["merchant_id"] = FieldContract{
		DataType: "string", IsProjection: true,
		AggregationMode: AggregationModeTieredSummary,
	}
	table.Fields["account_ref"] = FieldContract{DataType: "string", IsProjection: true}
	request := AggregateRequest{
		Table: table, Aggregate: "count", Field: "amount",
		Filter: andFilter([]AggregateFilter{
			{Kind: "predicate", Field: "account_ref", Op: "eq", Value: "acct-1"},
			{Kind: "predicate", Field: "merchant_id", Op: "eq", Value: "merchant-1"},
			{Kind: "predicate", Field: "date", Op: "gte", Value: "2026-07-01T00:00:00Z"},
		}),
	}
	plan, ok := planAggregateFacts(request)
	if !ok {
		t.Fatal("aggregate was not eligible for policy selection")
	}
	policy := aggregatePolicyForPlan(plan)
	if policy.Mode != AggregationModeProjectionOnly || policy.Field != "account_ref" {
		t.Fatalf("policy = %#v; an unconfigured dimension must keep the combined aggregate projection-only", policy)
	}
}

func TestAggregationPolicyUsesLeastExpansiveConfiguredMode(t *testing.T) {
	table := testTableContract()
	table.Fields["merchant_id"] = FieldContract{
		DataType: "string", IsProjection: true,
		AggregationMode: AggregationModeTieredSummary,
	}
	table.Fields["account_ref"] = FieldContract{
		DataType: "string", IsProjection: true,
		AggregationMode: AggregationModeAdaptiveCache,
	}
	request := AggregateRequest{
		Table: table, Aggregate: "count", Field: "amount",
		Filter: andFilter([]AggregateFilter{
			{Kind: "predicate", Field: "account_ref", Op: "eq", Value: "acct-1"},
			{Kind: "predicate", Field: "merchant_id", Op: "eq", Value: "merchant-1"},
			{Kind: "predicate", Field: "date", Op: "gte", Value: "2026-07-01T00:00:00Z"},
		}),
	}
	plan, ok := planAggregateFacts(request)
	if !ok {
		t.Fatal("aggregate was not eligible for policy selection")
	}
	policy := aggregatePolicyForPlan(plan)
	if policy.Mode != AggregationModeAdaptiveCache || policy.Field != "account_ref" {
		t.Fatalf("policy = %#v; adaptive must prevent a template-wide combined summary", policy)
	}
}

func TestTieredSummarySharesOneDurableShapeAcrossDimensionValues(t *testing.T) {
	table := testTableContract()
	table.Fields["account_ref"] = FieldContract{DataType: "string", IsProjection: true, AggregationMode: AggregationModeTieredSummary}
	request := AggregateRequest{
		Table: table, Aggregate: "count", Field: "amount",
		Filter: andFilter([]AggregateFilter{
			{Kind: "predicate", Field: "account_ref", Op: "eq", Value: "acct-1"},
			{Kind: "predicate", Field: "date", Op: "gte", Value: "2026-07-01T00:00:00Z"},
		}),
	}
	first, ok := planAggregateFacts(request)
	if !ok {
		t.Fatal("first aggregate was not eligible")
	}
	request.Filter.Children[0].Value = "acct-2"
	second, ok := planAggregateFacts(request)
	if !ok {
		t.Fatal("second aggregate was not eligible")
	}
	first.TemplateWide = true
	second.TemplateWide = true
	if aggregateFactShapeKey(first) != aggregateFactShapeKey(second) {
		t.Fatal("tiered summary should build one durable shape for all account values")
	}
	first.TemplateWide = false
	second.TemplateWide = false
	if aggregateFactShapeKey(first) == aggregateFactShapeKey(second) {
		t.Fatal("adaptive summaries should remain scoped to admitted account values")
	}
}

func TestFieldAggregationPolicyValidation(t *testing.T) {
	field := FieldContract{DataType: "string", AggregationMode: AggregationModeAdaptiveCache}
	if err := validateFieldAggregationPolicy("account_ref", field); err == nil {
		t.Fatal("accelerated field without projection was accepted")
	}
	field.IsProjection = true
	field.AggregationColdBehavior = AggregationColdUseDefault
	if err := validateFieldAggregationPolicy("account_ref", field); err == nil {
		t.Fatal("use_default field without a default was accepted")
	}
	zero := 0.0
	field.AggregationDefaultValue = &zero
	if err := validateFieldAggregationPolicy("account_ref", field); err != nil {
		t.Fatalf("valid accelerated field rejected: %v", err)
	}
}

func TestAggregateFactPlanPreservesPartialBucketBoundary(t *testing.T) {
	request := AggregateRequest{
		Table: testTableContract(), Aggregate: "sum", Field: "amount",
		Filter: &AggregateFilter{Kind: "predicate", Field: "date", Op: "gte", Value: "2026-07-01T10:12:00Z"},
	}
	plan, ok := planAggregateFacts(request)
	if !ok {
		t.Fatal("aggregate was not eligible for sealed facts")
	}
	partial := plan.partialFilter()
	predicates, ok := flattenAndPredicates(partial)
	if !ok || len(predicates) != 2 {
		t.Fatalf("partial filter = %#v, want lower and upper time predicates", partial)
	}
	if predicates[0].Op != "gte" || predicates[1].Op != "lt" {
		t.Fatalf("partial operators = %s,%s, want gte,lt", predicates[0].Op, predicates[1].Op)
	}

	request.Filter.Value = "2026-07-01T10:00:00Z"
	plan, ok = planAggregateFacts(request)
	if !ok || plan.partialFilter() != nil {
		t.Fatal("an inclusive hour-aligned lower bound must not create a partial bucket")
	}
	request.Filter.Op = "gt"
	plan, ok = planAggregateFacts(request)
	if !ok || plan.partialFilter() == nil {
		t.Fatal("an exclusive hour-aligned lower bound must preserve a partial bucket")
	}
}

func TestAggregateFactPlanSupportsAsOfUpperBound(t *testing.T) {
	request := AggregateRequest{
		Table: testTableContract(), Aggregate: "sum", Field: "amount",
		Filter: andFilter([]AggregateFilter{
			{Kind: "predicate", Field: "account_ref", Op: "eq", Value: "acct-1"},
			{Kind: "predicate", Field: "date", Op: "gte", Value: "2026-07-01T10:12:00Z"},
			{Kind: "predicate", Field: "date", Op: "lte", Value: "2026-07-02T12:34:00Z"},
		}),
	}
	plan, ok := planAggregateFacts(request)
	if !ok {
		t.Fatal("as-of aggregate was not eligible for sealed facts")
	}
	if want := time.Date(2026, 7, 2, 12, 34, 0, 0, time.UTC); !plan.UpperBound.Equal(want) || plan.UpperBoundOp != "lte" {
		t.Fatalf("upper bound = %s %s, want lte %s", plan.UpperBoundOp, plan.UpperBound, want)
	}
	sealedBefore := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	fullEnd := plan.fullBucketEnd(sealedBefore)
	if want := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC); !fullEnd.Equal(want) {
		t.Fatalf("full bucket end = %s, want %s", fullEnd, want)
	}
	upper := plan.upperBoundaryFilter(fullEnd)
	predicates, ok := flattenAndPredicates(upper)
	if !ok || len(predicates) != 4 {
		t.Fatalf("upper boundary filter = %#v, want original predicates plus a lower boundary", upper)
	}
	if got := predicates[len(predicates)-1]; got.Op != "gte" || got.Value != fullEnd.Format(time.RFC3339Nano) {
		t.Fatalf("upper boundary predicate = %#v", got)
	}
}

func TestAggregateFactPlanOmitsEmptyExclusiveUpperBoundary(t *testing.T) {
	request := AggregateRequest{
		Table: testTableContract(), Aggregate: "count", Field: "amount",
		Filter: andFilter([]AggregateFilter{
			{Kind: "predicate", Field: "date", Op: "gte", Value: "2026-07-01T10:00:00Z"},
			{Kind: "predicate", Field: "date", Op: "lt", Value: "2026-07-02T12:00:00Z"},
		}),
	}
	plan, ok := planAggregateFacts(request)
	if !ok {
		t.Fatal("bounded aggregate was not eligible")
	}
	end := plan.fullBucketEnd(time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC))
	if plan.upperBoundaryFilter(end) != nil {
		t.Fatal("exclusive hour-aligned upper bound should not create an empty raw boundary query")
	}
}

func TestAggregateFactPlanRejectsUnsafeShapes(t *testing.T) {
	table := testTableContract()
	for name, request := range map[string]AggregateRequest{
		"or": {
			Table: table, Aggregate: "sum", Field: "amount",
			Filter: &AggregateFilter{Kind: "group", Operator: "or", Children: []AggregateFilter{
				{Kind: "predicate", Field: "date", Op: "gte", Value: "2026-07-01T00:00:00Z"},
				{Kind: "predicate", Field: "country", Op: "eq", Value: "GH"},
			}},
		},
		"duplicate-upper-time-bound": {
			Table: table, Aggregate: "sum", Field: "amount",
			Filter: andFilter([]AggregateFilter{
				{Kind: "predicate", Field: "date", Op: "gte", Value: "2026-07-01T00:00:00Z"},
				{Kind: "predicate", Field: "date", Op: "lte", Value: "2026-07-02T00:00:00Z"},
				{Kind: "predicate", Field: "date", Op: "lt", Value: "2026-07-03T00:00:00Z"},
			}),
		},
		"unsupported-aggregate": {
			Table: table, Aggregate: "max", Field: "amount",
			Filter: &AggregateFilter{Kind: "predicate", Field: "date", Op: "gte", Value: "2026-07-01T00:00:00Z"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			if _, ok := planAggregateFacts(request); ok {
				t.Fatal("unsafe aggregate shape was accepted")
			}
		})
	}
}

func TestComposeAggregateFactResult(t *testing.T) {
	components := aggregateFactComponents{Sum: 75, Count: 3}
	if got := composeAggregateFactResult("sum", components); got != float64(75) {
		t.Fatalf("sum = %#v", got)
	}
	if got := composeAggregateFactResult("count", components); got != uint64(3) {
		t.Fatalf("count = %#v", got)
	}
	if got := composeAggregateFactResult("avg", components); got != float64(25) {
		t.Fatalf("avg = %#v", got)
	}
	if got := composeAggregateFactResult("avg", aggregateFactComponents{}); got != nil {
		t.Fatalf("empty avg = %#v, want nil", got)
	}
}

func TestFeatureTemplateAdmissionNeverPromotesOneOffSlowQuery(t *testing.T) {
	cache := newFeatureCache(Config{
		FeatureNamespace: "test", FeatureAdmissionHits: 3, FeatureSlowQueryMS: 100,
		FeatureMaxKeys: 10, FeatureMaxKeysPerTenant: 10, FeatureTTL: time.Hour,
	})
	if cache.shouldPromoteTemplate(t.Context(), "tenant", "shape", time.Second) {
		t.Fatal("a one-off slow query must not be promoted")
	}
	if !cache.shouldPromoteTemplate(t.Context(), "tenant", "shape", time.Second) {
		t.Fatal("a repeatedly slow query should be promoted on its second observation")
	}
}
