package mysqlworkflow

import (
	"fmt"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/mysqlaccess"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/migrations"
	"strings"
)

func CombinedStatements(database, definer string) ([]migrations.Step, error) {
	prefix, err := mysqlaccess.Statements(database, definer)
	if err != nil {
		return nil, err
	}
	next, err := Statements(database, definer)
	if err != nil {
		return nil, err
	}
	return append(prefix, next...), nil
}

// Plan extends the existing case owner's ledger prefix. Trusted before/after
// definitions must come from a reviewed pinned-engine artifact, never inferred
// from an application database during deployment.
func Plan(database, definer string, reference []migrations.Step) (migrations.Plan, error) {
	steps, err := CombinedStatements(database, definer)
	if err != nil {
		return migrations.Plan{}, err
	}
	if len(steps) != len(reference) {
		return migrations.Plan{}, fmt.Errorf("case workflow reference length mismatch")
	}
	for i, s := range steps {
		r := reference[i]
		if s.ID != r.ID || s.Kind != r.Kind || s.Name != r.Name || s.SQL != r.SQL || strings.HasPrefix(s.SQL, "ALTER ") != (r.Before != "") {
			return migrations.Plan{}, fmt.Errorf("case workflow reference mismatch at %d", i)
		}
		steps[i].Before = r.Before
		steps[i].After = r.After
	}
	p := migrations.Plan{Database: database, Owner: "case_manager", Steps: steps}
	return p, p.Validate()
}
