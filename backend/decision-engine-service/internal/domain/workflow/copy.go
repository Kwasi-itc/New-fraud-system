package workflow

import (
	"encoding/json"
	"fmt"
)

// RemapRuleHit detaches the encoded condition and rejects references outside
// the copied scenario. Both legacy array keys retain their original ordering.
func RemapRuleHit(params json.RawMessage, mapping map[string]string) (json.RawMessage, error) {
	var values map[string]json.RawMessage
	if err := json.Unmarshal(params, &values); err != nil {
		return nil, err
	}
	found := 0
	for _, key := range []string{"rule_id", "rule_ids"} {
		raw, ok := values[key]
		if !ok {
			continue
		}
		var ids []string
		if err := json.Unmarshal(raw, &ids); err != nil {
			return nil, err
		}
		for i, id := range ids {
			next, ok := mapping[id]
			if !ok || next == "" {
				return nil, fmt.Errorf("rule-hit reference outside copied scenario")
			}
			ids[i] = next
			found++
		}
		encoded, err := json.Marshal(ids)
		if err != nil {
			return nil, err
		}
		values[key] = encoded
	}
	if found == 0 {
		return nil, fmt.Errorf("rule-hit condition requires references")
	}
	return json.Marshal(values)
}
