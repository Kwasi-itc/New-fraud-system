package ingestion

type AggregateQuery struct {
	ObjectType string           `json:"object_type"`
	Aggregate  string           `json:"aggregate"`
	Field      string           `json:"field,omitempty"`
	Filter     *AggregateFilter `json:"filter,omitempty"`
}

type AggregateFilter struct {
	Kind     string            `json:"kind,omitempty"`
	Operator string            `json:"operator,omitempty"`
	Children []AggregateFilter `json:"children,omitempty"`
	Field    string            `json:"field,omitempty"`
	Op       string            `json:"op,omitempty"`
	Value    any               `json:"value,omitempty"`
}

const (
	AggregateFilterKindGroup     = "group"
	AggregateFilterKindPredicate = "predicate"
)
