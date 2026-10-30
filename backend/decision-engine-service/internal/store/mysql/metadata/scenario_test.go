package metadata

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/migrationaudit"
)

func TestScenarioFoundationPlanAppendOnlyAndSourceBound(t *testing.T) {
	steps, err := ScenarioFoundationStatements("fraud", "de_keys")
	if err != nil {
		t.Fatal(err)
	}
	platform, _ := PlatformStatements("fraud")
	if len(steps) != 30 {
		t.Fatalf("got %d steps", len(steps))
	}
	for i, s := range platform {
		if steps[i] != s {
			t.Fatal("rewrote shipped platform prefix")
		}
	}
	for i := range steps {
		definition, _ := json.Marshal(map[string]string{"Object": steps[i].Name, "Definition": "unit test only"})
		steps[i].After = string(definition)
	}
	if _, err := ScenarioFoundationPlan("fraud", "de_keys", steps); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(){func() { steps[3].SQL += " " }, func() { steps[3].Before = steps[3].After }, func() { steps[3].After = "" }} {
		saved := steps[3]
		change()
		if _, err := ScenarioFoundationPlan("fraud", "de_keys", steps); err == nil {
			t.Fatal("accepted altered reference")
		}
		steps[3] = saved
	}
	for _, name := range []string{"root", "ROOT", "", "x`;DROP USER root", "an_account_name_that_is_longer_than_thirty_two_characters"} {
		if _, err := ScenarioFoundationStatements("fraud", name); err == nil {
			t.Fatal("invalid definer accepted")
		}
	}
	grants, err := ScenarioDefinerGrants("fraud", "de_keys")
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range grants {
		if strings.Contains(g, "*.*") || strings.Contains(g, "GRANT OPTION") || strings.Contains(g, "data_model") {
			t.Fatal("broad/cross-owner grant")
		}
	}
}

// The catalog is separately compared to a fresh migrated PG fixture. This test
// prevents an initial-only schema translation from dropping later descriptions,
// nullable thresholds, rule identity fields or publication columns.
func TestScenarioFoundationSourceColumnCoverage(t *testing.T) {
	raw, err := os.ReadFile("../../../migrationaudit/testdata/decision-schema-mapping.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest migrationaudit.Manifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	steps, err := ScenarioFoundationStatements("fraud", "de_keys")
	if err != nil {
		t.Fatal(err)
	}
	targets := map[string]string{}
	for _, step := range steps {
		if step.Kind == "TABLE" {
			targets[step.Name] = step.SQL
		}
	}
	types := map[string]string{"uuid": "BINARY(16)", "text": "LONGTEXT", "integer": "INT", "jsonb": "JSON", "timestamp with time zone": "DATETIME(6)"}
	covered := 0
	for _, table := range manifest.Source.Tables {
		name := strings.TrimPrefix(table.Name, "core.")
		if name != "scenarios" && name != "scenario_iterations" && name != "rules" && name != "scenario_publications" {
			continue
		}
		target := targets["decision_engine_"+name]
		if target == "" {
			t.Fatal("missing target", name)
		}
		for _, column := range table.Columns {
			if _, ok := types[column.Type]; !ok {
				t.Fatal("unreviewed source type", column.Type)
			}
			mapped := target
			field := column.Name
			if name == "scenarios" && field == "live_iteration_id" {
				mapped = targets["decision_engine_scenario_live_iterations"]
				field = "iteration_id"
			}
			match := regexp.MustCompile(`(?m)^ ` + field + ` ([^\n]+)`).FindStringSubmatch(mapped)
			if len(match) != 2 || !strings.HasPrefix(match[1], types[column.Type]) {
				t.Fatal("missing/mistyped source column", name, column.Name)
			}
			if !column.Nullable && !strings.Contains(match[1], "NOT NULL") && !strings.Contains(match[1], "PRIMARY KEY") {
				t.Fatal("lost source non-nullability", name, column.Name)
			}
			covered++
		}
	}
	if covered != 39 {
		t.Fatal("source catalog changed; review all scenario mappings", covered)
	}
}
