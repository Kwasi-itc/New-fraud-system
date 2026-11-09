package installation

import (
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/migrations"
)

func TestWorkerIndexUpgradePreservesLineage(t *testing.T) {
	base := migrations.Plan{Database: "fraud", Owner: "data_model", Steps: []migrations.Step{
		{ID: "old_jobs", Kind: "TABLE", Name: "data_model_index_jobs", SQL: "CREATE TABLE `fraud`.data_model_index_jobs (id BINARY(16) PRIMARY KEY)", After: `{"definition":"jobs","type":"fixture"}`},
		{ID: "old_intents", Kind: "TABLE", Name: "data_model_schema_change_intents", SQL: "CREATE TABLE `fraud`.data_model_schema_change_intents (id BINARY(16) PRIMARY KEY)", After: `{"definition":"intents","type":"fixture"}`},
		{ID: "data_model_v006_contract_lookup", Kind: "FUNCTION", Name: "data_model_find_ingestion_contract_key_v1", SQL: "CREATE FUNCTION `fraud`.data_model_find_ingestion_contract_key_v1() RETURNS INT DETERMINISTIC RETURN 1", After: `{"definition":"lookup","type":"fixture"}`},
	}}
	refs, err := WorkerIndexUpgradeStatements("fraud")
	if err != nil {
		t.Fatal(err)
	}
	for i := range refs {
		refs[i].Before = base.Steps[i].After
		refs[i].After = `{"definition":"upgraded","type":"fixture"}`
	}
	bound, err := ExtendWorkerIndexPlan(base, refs)
	if err != nil || bound.Owner != "data_model" || len(bound.Steps) != 5 {
		t.Fatal(bound, err)
	}
	for i, old := range base.Steps {
		if bound.Steps[i] != old {
			t.Fatal("rewrote frozen prefix")
		}
	}
	bad := append([]migrations.Step(nil), refs...)
	bad[0].SQL += " "
	if _, err := ExtendWorkerIndexPlan(base, bad); err == nil {
		t.Fatal("substituted upgrade accepted")
	}
	bad = append([]migrations.Step(nil), refs...)
	bad[0].Before = ""
	if _, err := ExtendWorkerIndexPlan(base, bad); err == nil {
		t.Fatal("missing reference accepted")
	}
}
