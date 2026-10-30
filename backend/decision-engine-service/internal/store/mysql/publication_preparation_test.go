package mysql

import (
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

func TestPublicationRegistryIncludesIntermediateRelatedTables(t *testing.T) {
	snapshot := PublicationSnapshot{Scenario: scenario.Scenario{TriggerObjectType: "wallets"}, Iteration: scenario.Iteration{TriggerFormula: []byte(`{"function":"related_field","named_children":{"path":{"constant":"owner.profile"},"field":{"constant":"active"}}}`)}}
	model := ports.TenantModel{Tables: map[string]ports.TenantModelTable{
		"wallets":   {Name: "wallets", LinksToSingle: map[string]ports.TenantModelLink{"owner": {ParentTableName: "customers"}}},
		"customers": {Name: "customers", LinksToSingle: map[string]ports.TenantModelLink{"profile": {ParentTableName: "profiles"}}},
		"profiles":  {Name: "profiles"},
	}}
	names, err := publicationRelatedTables(snapshot, model, 4096)
	if err != nil || len(names) != 3 || !names["customers"] || !names["profiles"] {
		t.Fatal(names, err)
	}
	delete(model.Tables, "customers")
	if _, err := publicationRelatedTables(snapshot, model, 4096); err == nil {
		t.Fatal("missing intermediate owner accepted")
	}
}
