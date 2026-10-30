package ingestion

import "testing"

// Pin current validation differences so the MySQL port cannot silently change
// them. This does not endorse repeated subtree counting as the desired policy.
func TestSourceAggregateValidationCharacterization(t *testing.T) {
	leaves := make([]AggregateFilter, 20)
	for i := range leaves {
		leaves[i] = AggregateFilter{Kind: "predicate", Field: "amount", Op: "eq", Value: i}
	}
	flat := AggregateFilter{Kind: "group", Children: leaves}
	query := AggregateQuery{ObjectType: "transactions", Aggregate: "count", Field: "amount", Filter: &flat}
	if shape := MeasureAggregateQueryShape(query); shape.FilterNodes != 21 {
		t.Fatal(shape)
	}
	if err := ValidateAggregateQuery(query); err != nil {
		t.Fatal(err)
	}
	nested := AggregateFilter{Kind: "group", Children: []AggregateFilter{flat}}
	query.Filter = &nested
	if shape := MeasureAggregateQueryShape(query); shape.FilterNodes != 22 || shape.FilterDepth != 3 {
		t.Fatal(shape)
	}
	if err := ValidateAggregateQuery(query); err == nil {
		t.Fatal("current validator counts nested nodes repeatedly; expected size rejection")
	}
	emptyIn := AggregateFilter{Kind: "predicate", Field: "amount", Op: "in", Value: []any{}}
	query.Filter = &emptyIn
	if err := ValidateAggregateQuery(query); err != nil {
		t.Fatal("domain accepts empty IN; repository rejects it", err)
	}
	query.Filter = nil
	query.Aggregate = "unsupported"
	query.Field = ""
	if err := ValidateAggregateQuery(query); err != nil {
		t.Fatal("aggregate allowlist and field checks occur in repository", err)
	}
}
