package workflow

import (
	"encoding/json"
	"testing"
)

func TestRemapRuleHitRejectsForeignReferencesAndPreservesSource(t *testing.T) {
	raw := json.RawMessage(`{"rule_id":["a"],"rule_ids":["b","a"],"mode":"all"}`)
	copied, e := RemapRuleHit(raw, map[string]string{"a": "new-a", "b": "new-b"})
	if e != nil {
		t.Fatal(e)
	}
	var out map[string]any
	if e = json.Unmarshal(copied, &out); e != nil {
		t.Fatal(e)
	}
	if out["rule_id"].([]any)[0] != "new-a" || out["rule_ids"].([]any)[0] != "new-b" || out["mode"] != "all" {
		t.Fatal(out)
	}
	if string(raw) != `{"rule_id":["a"],"rule_ids":["b","a"],"mode":"all"}` {
		t.Fatal("source mutated")
	}
	for _, invalid := range []string{`{}`, `{"rule_ids":[]}`, `{"rule_ids":["foreign"]}`, `{"rule_ids":"a"}`, `null`} {
		if _, e = RemapRuleHit(json.RawMessage(invalid), map[string]string{"a": "new-a"}); e == nil {
			t.Fatal("accepted", invalid)
		}
	}
}
