package metadata

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestExecutionPlanAppendOnlyAndSourceBound(t *testing.T) {
	prefix, err := EffectJobsStatements("fraud", "de_keys")
	if err != nil {
		t.Fatal(err)
	}
	steps, err := ExecutionStatements("fraud", "de_keys")
	if err != nil || len(steps) != 140 {
		t.Fatal(len(steps), err)
	}
	for i := range prefix {
		if steps[i] != prefix[i] {
			t.Fatal("changed existing lineage", i)
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
	if _, err := ExecutionPlan("fraud", "de_keys", steps); err != nil {
		t.Fatal(err)
	}
	before := steps[len(steps)-1].Before
	steps[len(steps)-1].Before = ""
	if _, err := ExecutionPlan("fraud", "de_keys", steps); err == nil {
		t.Fatal("cleanup ALTER adopted without reviewed before snapshot")
	}
	steps[len(steps)-1].Before = before
	steps[len(steps)-1].SQL += " "
	if _, err := ExecutionPlan("fraud", "de_keys", steps); err == nil {
		t.Fatal("accepted altered execution DDL")
	}
	grants, err := ExecutionDefinerGrants("fraud", "de_keys")
	if err != nil {
		t.Fatal(err)
	}
	for _, grant := range grants {
		if strings.Contains(grant, "*.*") || strings.Contains(grant, "GRANT OPTION") || strings.Contains(grant, "data_model") {
			t.Fatal("broad or foreign-owner grant", grant)
		}
	}
}
