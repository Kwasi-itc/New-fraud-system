package metadata

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestEffectCancellationLineageAndGrants(t *testing.T) {
	prefix, err := ExecutionStatements("fraud", "de_keys")
	if err != nil {
		t.Fatal(err)
	}
	steps, err := EffectCancellationStatements("fraud", "de_keys")
	if err != nil || len(steps) != 145 {
		t.Fatal(len(steps), err)
	}
	for i := range prefix {
		if steps[i] != prefix[i] {
			t.Fatal("rewrote existing 140-step lineage", i)
		}
	}
	latest := map[string]string{}
	for i := range steps {
		key := steps[i].Kind + ":" + steps[i].Name
		if strings.HasPrefix(steps[i].SQL, "ALTER ") {
			steps[i].Before = latest[key]
		}
		b, _ := json.Marshal(map[string]string{"Object": steps[i].Name, "Definition": fmt.Sprintf("unit fixture step %d only", i)})
		steps[i].After = string(b)
		latest[key] = steps[i].After
	}
	if _, err := EffectCancellationPlan("fraud", "de_keys", steps); err != nil {
		t.Fatal(err)
	}
	steps[len(steps)-1].SQL += " "
	if _, err := EffectCancellationPlan("fraud", "de_keys", steps); err == nil {
		t.Fatal("accepted changed cancellation definition")
	}
	grants, err := EffectCancellationDefinerGrants("fraud", "de_keys")
	if err != nil {
		t.Fatal(err)
	}
	for _, grant := range grants {
		if strings.Contains(grant, "*.*") || strings.Contains(grant, "GRANT OPTION") || strings.Contains(grant, "data_model") {
			t.Fatal("broad/foreign owner grant", grant)
		}
	}
}
