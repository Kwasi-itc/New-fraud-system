package ingestion

import "testing"

func TestValidateAggregateQueryRejectsDeepFilter(t *testing.T) {
	t.Parallel()

	filter := AggregateFilter{
		Kind: "group",
		Children: []AggregateFilter{{
			Kind: "group",
			Children: []AggregateFilter{{
				Kind: "group",
				Children: []AggregateFilter{{
					Kind: "group",
					Children: []AggregateFilter{{
						Kind: "group",
						Children: []AggregateFilter{{
							Kind:  "predicate",
							Field: "amount",
							Op:    "eq",
							Value: 1,
						}},
					}},
				}},
			}},
		}},
	}

	err := ValidateAggregateQuery(AggregateQuery{
		ObjectType: "transactions",
		Aggregate:  "count",
		Field:      "amount",
		Filter:     &filter,
	})
	if err == nil {
		t.Fatal("expected deep filter validation error")
	}
}

func TestValidateAggregateQueryRejectsLargeInList(t *testing.T) {
	t.Parallel()

	items := make([]any, MaxAggregateInListItems+1)
	for i := range items {
		items[i] = i
	}

	filter := AggregateFilter{
		Kind:  "predicate",
		Field: "amount",
		Op:    "in",
		Value: items,
	}

	err := ValidateAggregateQuery(AggregateQuery{
		ObjectType: "transactions",
		Aggregate:  "count",
		Field:      "amount",
		Filter:     &filter,
	})
	if err == nil {
		t.Fatal("expected oversized in-list validation error")
	}
}

func TestValidateAggregateQueryAllowsOrGroupOperator(t *testing.T) {
	t.Parallel()

	filter := AggregateFilter{
		Kind:     "group",
		Operator: "or",
		Children: []AggregateFilter{
			{Kind: "predicate", Field: "amount", Op: "eq", Value: 10},
			{Kind: "predicate", Field: "amount", Op: "gt", Value: 100},
		},
	}

	err := ValidateAggregateQuery(AggregateQuery{
		ObjectType: "transactions",
		Aggregate:  "count",
		Field:      "amount",
		Filter:     &filter,
	})
	if err != nil {
		t.Fatalf("expected OR filter to be allowed, got %v", err)
	}
}

func TestValidateAggregateQueryAllowsNotGroupOperator(t *testing.T) {
	t.Parallel()

	filter := AggregateFilter{
		Kind:     "group",
		Operator: "not",
		Children: []AggregateFilter{
			{Kind: "predicate", Field: "amount", Op: "eq", Value: 10},
		},
	}

	err := ValidateAggregateQuery(AggregateQuery{
		ObjectType: "transactions",
		Aggregate:  "count",
		Field:      "amount",
		Filter:     &filter,
	})
	if err != nil {
		t.Fatalf("expected NOT filter to be allowed, got %v", err)
	}
}

func TestValidateAggregateQueryRejectsInvalidNotArity(t *testing.T) {
	t.Parallel()

	filter := AggregateFilter{
		Kind:     "group",
		Operator: "not",
		Children: []AggregateFilter{
			{Kind: "predicate", Field: "amount", Op: "eq", Value: 10},
			{Kind: "predicate", Field: "amount", Op: "gt", Value: 100},
		},
	}

	err := ValidateAggregateQuery(AggregateQuery{
		ObjectType: "transactions",
		Aggregate:  "count",
		Field:      "amount",
		Filter:     &filter,
	})
	if err == nil {
		t.Fatal("expected invalid NOT arity validation error")
	}
}

func TestValidateAggregateQueryAllowsStartsWithPredicateOperator(t *testing.T) {
	t.Parallel()

	filter := AggregateFilter{
		Kind:  "predicate",
		Field: "email",
		Op:    "starts_with",
		Value: "alice",
	}

	err := ValidateAggregateQuery(AggregateQuery{
		ObjectType: "transactions",
		Aggregate:  "count",
		Field:      "email",
		Filter:     &filter,
	})
	if err != nil {
		t.Fatalf("expected starts_with predicate to be allowed, got %v", err)
	}
}

func TestValidateAggregateQueryRejectsNonIndexFriendlyPredicateOperator(t *testing.T) {
	t.Parallel()

	filter := AggregateFilter{
		Kind:  "predicate",
		Field: "email",
		Op:    "ends_with",
		Value: "@example.com",
	}

	err := ValidateAggregateQuery(AggregateQuery{
		ObjectType: "transactions",
		Aggregate:  "count",
		Field:      "email",
		Filter:     &filter,
	})
	if err == nil {
		t.Fatal("expected non-index-friendly predicate operator validation error")
	}
}
