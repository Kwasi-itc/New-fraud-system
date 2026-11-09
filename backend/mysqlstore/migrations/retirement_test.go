package migrations

import "testing"

func TestOnlyExplicitPhysicalTriggerRetirement(t *testing.T) {
	p := Plan{Database: "shared_app", Steps: []Step{{ID: "retire_guard", Kind: "TRIGGER", Name: "record_guard", SQL: "DROP TRIGGER `shared_app`.`record_guard`", Before: `{"definition":"guard","type":"trigger"}`}}}
	if err := p.Validate(); err == nil {
		t.Fatal("ordinary migration accepted DROP")
	}
	if err := p.ValidateWithTriggerRetirement(); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []Step{
		{ID: "retire_guard", Kind: "TABLE", Name: "record_guard", SQL: "DROP TABLE `shared_app`.`record_guard`", Before: p.Steps[0].Before},
		{ID: "retire_guard", Kind: "TRIGGER", Name: "record_guard", SQL: "DROP TRIGGER `another_app`.`record_guard`", Before: p.Steps[0].Before},
		{ID: "retire_guard", Kind: "TRIGGER", Name: "record_guard", SQL: p.Steps[0].SQL},
	} {
		next := p
		next.Steps = []Step{bad}
		if err := next.ValidateWithTriggerRetirement(); err == nil {
			t.Fatal("accepted unscoped retirement")
		}
	}
}
