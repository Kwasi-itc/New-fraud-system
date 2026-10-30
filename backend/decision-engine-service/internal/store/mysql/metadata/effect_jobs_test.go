package metadata

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestEffectJobsPlanAppendOnlyAndSourceBound(t *testing.T) {
	prefix, err := BrowserAuthoringStatements("fraud", "de_keys")
	if err != nil {
		t.Fatal(err)
	}
	steps, err := EffectJobsStatements("fraud", "de_keys")
	if err != nil || len(steps) != 115 {
		t.Fatal(len(steps), err)
	}
	for i, step := range prefix {
		if steps[i] != step {
			t.Fatal("rewrote published lineage", i)
		}
	}
	for i := range steps {
		raw, _ := json.Marshal(map[string]string{"Object": steps[i].Name, "Definition": "unit fixture only"})
		steps[i].After = string(raw)
	}
	if _, err := EffectJobsPlan("fraud", "de_keys", steps); err != nil {
		t.Fatal(err)
	}
	steps[len(steps)-1].SQL += " "
	if _, err := EffectJobsPlan("fraud", "de_keys", steps); err == nil {
		t.Fatal("accepted changed source")
	}
	grants, err := EffectJobsDefinerGrants("fraud", "de_keys")
	if err != nil {
		t.Fatal(err)
	}
	for _, grant := range grants {
		if strings.Contains(grant, "*.*") || strings.Contains(grant, "GRANT OPTION") || strings.Contains(grant, "data_model") {
			t.Fatal("broad or foreign owner grant", grant)
		}
	}
}
