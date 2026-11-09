package mysqlruntime

import (
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/migrations"
)

func TestDataModelContractAndWorkerIndexLineage(t *testing.T) {
	base := migrations.Step{ID: "data_model_v006_contract_lookup"}
	first := migrations.Step{ID: "data_model_v007_jobs_table_scope", Kind: "TABLE", Name: "data_model_index_jobs", SQL: "ALTER TABLE `fraud`.data_model_index_jobs ADD INDEX data_model_jobs_table_scope (tenant_id,table_id,requested_at DESC,id DESC)", Before: "retained reference"}
	second := migrations.Step{ID: "data_model_v007_intent_epoch", Kind: "TABLE", Name: "data_model_schema_change_intents", SQL: "ALTER TABLE `fraud`.data_model_schema_change_intents ADD INDEX data_model_intent_epoch (tenant_id,table_id,expected_epoch,state)", Before: "retained reference"}
	changed := first
	changed.SQL += ", ADD COLUMN unexpected INT"
	for _, tc := range []struct {
		name  string
		steps []migrations.Step
		valid bool
	}{
		{"v006", []migrations.Step{base}, true},
		{"v007", []migrations.Step{base, first, second}, true},
		{"missing contract", []migrations.Step{first, second}, false},
		{"partial upgrade", []migrations.Step{base, first}, false},
		{"reordered upgrade", []migrations.Step{base, second, first}, false},
		{"changed SQL", []migrations.Step{base, changed, second}, false},
		{"unknown upgrade", []migrations.Step{base, first, second, {ID: "data_model_v008_unknown"}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateDataModelLineage(migrations.Plan{Database: "fraud", Steps: tc.steps})
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%t error=%v", tc.valid, err)
			}
		})
	}
}
