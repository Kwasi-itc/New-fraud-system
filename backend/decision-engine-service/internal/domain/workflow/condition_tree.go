package workflow

import (
	"encoding/json"
	"fmt"
	"math/big"
	"slices"
	"strings"
)

// ConditionTree is the bounded, persisted workflow editor contract. Payload leaves
// read one field from the evaluated object; they never perform related-record reads.
type ConditionTree struct {
	Kind     string          `json:"kind"`
	Children []ConditionTree `json:"children,omitempty"`
	Outcomes []string        `json:"outcomes,omitempty"`
	Field    string          `json:"field,omitempty"`
	Operator string          `json:"operator,omitempty"`
	Value    json.RawMessage `json:"value,omitempty"`
}

func (n ConditionTree) Validate() error {
	count := 0
	var visit func(ConditionTree, int) error
	visit = func(n ConditionTree, depth int) error {
		count++
		if count > 64 || depth > 4 {
			return fmt.Errorf("conditions allow at most 64 nodes and 4 levels")
		}
		group := n.Kind == "all" || n.Kind == "any"
		if !group && len(n.Children) != 0 {
			return fmt.Errorf("only groups may have children")
		}
		if n.Kind != "outcome_in" && len(n.Outcomes) != 0 {
			return fmt.Errorf("unexpected outcomes")
		}
		if n.Kind != "payload" && (n.Field != "" || n.Operator != "" || len(n.Value) != 0) {
			return fmt.Errorf("unexpected payload parameters")
		}
		switch n.Kind {
		case "all", "any":
			if len(n.Children) == 0 {
				return fmt.Errorf("condition groups cannot be empty")
			}
			for _, child := range n.Children {
				if err := visit(child, depth+1); err != nil {
					return err
				}
			}
		case "always", "never":
		case "outcome_in":
			if len(n.Outcomes) == 0 || len(n.Outcomes) > 4 {
				return fmt.Errorf("select decision outcomes")
			}
			seen := map[string]bool{}
			for _, value := range n.Outcomes {
				if !slices.Contains([]string{"approve", "review", "block_and_review", "decline"}, value) || seen[value] {
					return fmt.Errorf("invalid or duplicate outcome")
				}
				seen[value] = true
			}
		case "payload":
			if strings.TrimSpace(n.Field) == "" || len(n.Field) > 200 {
				return fmt.Errorf("payload field is required (maximum 200 bytes)")
			}
			if n.Operator == "exists" || n.Operator == "not_exists" {
				if len(n.Value) != 0 {
					return fmt.Errorf("existence conditions do not accept a value")
				}
				return nil
			}
			if !slices.Contains([]string{"eq", "neq", "gt", "gte", "lt", "lte", "contains"}, n.Operator) {
				return fmt.Errorf("invalid payload operator")
			}
			var value any
			if len(n.Value) > 2000 || json.Unmarshal(n.Value, &value) != nil {
				return fmt.Errorf("invalid payload value")
			}
			switch value.(type) {
			case nil, bool, string, float64:
			default:
				return fmt.Errorf("payload value must be a scalar")
			}
			if slices.Contains([]string{"gt", "gte", "lt", "lte"}, n.Operator) {
				if _, ok := value.(float64); !ok {
					return fmt.Errorf("numeric comparison requires a number")
				}
			}
			if n.Operator == "contains" {
				if _, ok := value.(string); !ok {
					return fmt.Errorf("contains requires text")
				}
			}
		default:
			return fmt.Errorf("unknown condition kind %q", n.Kind)
		}
		return nil
	}
	return visit(n, 1)
}

// Matches requires a validated tree. Missing fields do not match comparisons,
// including 'not equal'; use not_exists to explicitly match missing fields.
func (n ConditionTree) Matches(outcome string, fields map[string]any) bool {
	switch n.Kind {
	case "always":
		return true
	case "never":
		return false
	case "all", "any":
		for _, child := range n.Children {
			matched := child.Matches(outcome, fields)
			if n.Kind == "all" && !matched {
				return false
			}
			if n.Kind == "any" && matched {
				return true
			}
		}
		return n.Kind == "all"
	case "outcome_in":
		return slices.Contains(n.Outcomes, outcome)
	case "payload":
		value, exists := fields[n.Field]
		if n.Operator == "exists" {
			return exists
		}
		if n.Operator == "not_exists" {
			return !exists
		}
		if !exists {
			return false
		}
		raw, err := json.Marshal(value)
		if err != nil {
			return false
		}
		var left, right any
		decode := func(raw []byte, target *any) error {
			d := json.NewDecoder(strings.NewReader(string(raw)))
			d.UseNumber()
			return d.Decode(target)
		}
		if decode(raw, &left) != nil || decode(n.Value, &right) != nil {
			return false
		}
		comparison, comparable := 0, false
		if a, ok := left.(json.Number); ok {
			if b, ok := right.(json.Number); ok {
				x, xok := new(big.Rat).SetString(string(a))
				y, yok := new(big.Rat).SetString(string(b))
				if xok && yok {
					comparison, comparable = x.Cmp(y), true
				}
			}
		}
		equal := false
		switch a := left.(type) {
		case nil:
			equal = right == nil
		case bool:
			b, ok := right.(bool)
			equal = ok && a == b
		case string:
			b, ok := right.(string)
			equal = ok && a == b
		case json.Number:
			equal = comparable && comparison == 0
		}
		switch n.Operator {
		case "eq":
			return equal
		case "neq":
			return !equal
		case "gt":
			return comparable && comparison > 0
		case "gte":
			return comparable && comparison >= 0
		case "lt":
			return comparable && comparison < 0
		case "lte":
			return comparable && comparison <= 0
		case "contains":
			a, aok := left.(string)
			b, bok := right.(string)
			return aok && bok && strings.Contains(a, b)
		}
	}
	return false
}
