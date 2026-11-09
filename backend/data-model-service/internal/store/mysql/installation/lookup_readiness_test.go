package installation

import (
	"database/sql"
	"encoding/json"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/migrations"
	"testing"
)

func TestCompleteLookupReadinessBindsAllSourceAndDefiners(t *testing.T) {
	defs := []Definer{{"local_resolver", "localhost"}, {"local_creation", "localhost"}, {"local_result", "localhost"}, {"local_record", "localhost"}, {"local_contract", "localhost"}}
	steps, err := Statements("fraud", defs[0])
	if err != nil {
		t.Fatal(err)
	}
	upgrade, err := DeliveryRetryUpgradeStatement("fraud")
	if err != nil {
		t.Fatal(err)
	}
	steps = append(steps, upgrade)
	for i, builder := range []func(string, Definer) ([]migrations.Step, error){CreationReceiptUpgradeStatements, OwnerResultUpgradeStatements, RecordLookupUpgradeStatements, ContractLookupUpgradeStatements} {
		more, err := builder("fraud", defs[i+1])
		if err != nil {
			t.Fatal(err)
		}
		steps = append(steps, more...)
	}
	// Synthetic definitions exercise constructor binding only. The local
	// installer independently checks real server definitions and exact grants.
	latest := map[string]string{}
	for i := range steps {
		key := steps[i].Kind + ":" + steps[i].Name
		steps[i].Before = latest[key]
		b, _ := json.Marshal(map[string]string{"object": steps[i].Name, "definition": steps[i].ID})
		steps[i].After = string(b)
		latest[key] = string(b)
	}
	plan := migrations.Plan{Database: "fraud", Owner: "data_model", Steps: steps}
	bind := func(p migrations.Plan, ds []Definer) (*ExactKeyReadiness, error) {
		return NewExactKeyReadinessWithLookups(new(sql.DB), p, ds[0], ds[1], ds[2], ds[3], ds[4])
	}
	if _, err := bind(plan, defs); err != nil {
		t.Fatal(err)
	}
	if _, err := NewExactKeyReadinessWithOwnerResults(new(sql.DB), plan, defs[0], defs[1], defs[2]); err == nil {
		t.Fatal("older constructor silently accepted new lineage")
	}
	for _, index := range []int{0, len(steps) - 2, len(steps) - 1} {
		bad := plan
		bad.Steps = append([]migrations.Step(nil), steps...)
		bad.Steps[index].SQL += " "
		if _, err := bind(bad, defs); err == nil {
			t.Fatal("substituted source accepted", index)
		}
	}
	for i := 1; i < len(defs); i++ {
		bad := append([]Definer(nil), defs...)
		bad[i] = Definer{defs[0].User, "LOCALHOST"}
		if _, err := bind(plan, bad); err == nil {
			t.Fatal("shared account alias accepted")
		}
	}
	plan.Steps = steps[:len(steps)-1]
	if _, err := bind(plan, defs); err == nil {
		t.Fatal("partial lookup lineage accepted")
	}
}
