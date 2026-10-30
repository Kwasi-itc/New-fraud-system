package mysql

import (
	"errors"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
)

var ErrRecordFilter = errors.New("supported bounded mapped filter tree required")

// RecordFilter belongs to its originating projection and transaction. An empty
// SQL clause means no filter, including empty OR and NOT(empty), as in the source.
// Callers must handle this explicitly before adding WHERE; it does not mean false.
type RecordFilter struct {
	clause string
	args   []any
}

func (f RecordFilter) SQL() (string, []any) { return f.clause, cloneRecordArguments(f.args) }

func cloneRecordArguments(args []any) []any {
	out := append([]any(nil), args...)
	for i, value := range out {
		if raw, ok := value.([]byte); ok {
			out[i] = append([]byte(nil), raw...)
		}
	}
	return out
}

// CompileFilter performs bounded validation before generating SQL. It preserves
// the source validator's weighted node count (sum of node depths), without its
// repeated/unbounded descendant traversal. Predicate children are rejected rather
// than silently ignored; this stricter staged policy needs API compatibility review.
// Scalar leaves include bounded literal string predicates. Membership retains
// its separately scoped supported-type contract.
func (p *RecordProjection) CompileFilter(filter *ingestion.AggregateFilter) (RecordFilter, error) {
	if p == nil || p.table == "" || len(p.fields) == 0 {
		return RecordFilter{}, ErrRecordFilter
	}
	if filter == nil {
		return RecordFilter{}, nil
	}
	weighted := 0
	if err := validateRecordFilter(filter, 1, &weighted); err != nil {
		return RecordFilter{}, err
	}
	return p.compileRecordFilter(filter)
}
func validateRecordFilter(f *ingestion.AggregateFilter, depth int, weighted *int) error {
	if depth > ingestion.MaxAggregateFilterDepth {
		return ErrRecordFilter
	}
	*weighted += depth
	if *weighted > ingestion.MaxAggregateFilterNodes {
		return ErrRecordFilter
	}
	kind := strings.ToLower(strings.TrimSpace(f.Kind))
	switch kind {
	case "", ingestion.AggregateFilterKindGroup:
		op := strings.ToLower(strings.TrimSpace(f.Operator))
		if op != "" && op != "and" && op != "or" && op != "not" {
			return ErrRecordFilter
		}
		if len(f.Children) > ingestion.MaxAggregateGroupChildren || (op == "not" && len(f.Children) != 1) {
			return ErrRecordFilter
		}
		for i := range f.Children {
			if err := validateRecordFilter(&f.Children[i], depth+1, weighted); err != nil {
				return err
			}
		}
	case ingestion.AggregateFilterKindPredicate:
		if len(f.Children) != 0 {
			return ErrRecordFilter
		}
	default:
		return ErrRecordFilter
	}
	return nil
}
func (p *RecordProjection) compileRecordFilter(f *ingestion.AggregateFilter) (RecordFilter, error) {
	if strings.ToLower(strings.TrimSpace(f.Kind)) == ingestion.AggregateFilterKindPredicate {
		var clause string
		var args []any
		if strings.ToLower(strings.TrimSpace(f.Op)) == "in" {
			leaf, err := p.MembershipPredicate(f.Field, f.Value)
			if err != nil {
				return RecordFilter{}, errors.Join(ErrRecordFilter, err)
			}
			clause, args = leaf.SQL()
		} else {
			leaf, err := p.ScalarPredicate(f.Field, f.Op, f.Value)
			if err != nil {
				return RecordFilter{}, errors.Join(ErrRecordFilter, err)
			}
			clause, args = leaf.SQL()
		}
		return RecordFilter{clause: clause, args: args}, nil
	}
	op := strings.ToLower(strings.TrimSpace(f.Operator))
	if op == "" {
		op = "and"
	}
	var parts []string
	var args []any
	for i := range f.Children {
		child, err := p.compileRecordFilter(&f.Children[i])
		if err != nil {
			return RecordFilter{}, err
		}
		if child.clause == "" {
			continue
		}
		parts = append(parts, "("+child.clause+")")
		args = append(args, child.args...)
	}
	if len(parts) == 0 {
		return RecordFilter{}, nil
	}
	if op == "not" {
		return RecordFilter{clause: "NOT " + parts[0], args: args}, nil
	}
	return RecordFilter{clause: strings.Join(parts, " "+strings.ToUpper(op)+" "), args: args}, nil
}
