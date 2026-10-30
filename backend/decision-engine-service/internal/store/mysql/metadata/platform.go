// Package metadata describes staged, decision-owned MySQL schema. It does not
// install anything at API startup or authorize a PostgreSQL/MySQL cutover.
package metadata

import (
	"embed"
	"errors"
	"fmt"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/migrations"
)

//go:embed sql/*.sql
var platformSource embed.FS

// PlatformStatements is the initial additive platform slice, not the complete
// decision lineage. No queues, cross-owner objects or grants are implied.
func PlatformStatements(database string) ([]migrations.Step, error) {
	schema, err := mysqlstore.Identifier(database)
	if err != nil {
		return nil, err
	}
	objects := []struct{ file, name string }{
		{"001_record_tags.sql", "decision_engine_record_tags"},
		{"002_risk_snapshots.sql", "decision_engine_risk_snapshots"},
		{"003_ip_flags.sql", "decision_engine_ip_flags"},
	}
	steps := make([]migrations.Step, 0, len(objects))
	for i, object := range objects {
		source, err := platformSource.ReadFile("sql/" + object.file)
		if err != nil {
			return nil, err
		}
		steps = append(steps, migrations.Step{ID: fmt.Sprintf("decision_engine_v001_%04d", i+1), Kind: "TABLE", Name: object.name, SQL: strings.ReplaceAll(string(source), "{{database}}", schema)})
	}
	return steps, nil
}

// PlatformPlan binds independently reviewed reference-server observations to
// this exact source. It must not learn/accept existing production definitions.
// Future decision steps must append to this lineage, not replace its history.
func PlatformPlan(database string, reference []migrations.Step) (migrations.Plan, error) {
	steps, err := PlatformStatements(database)
	if err != nil {
		return migrations.Plan{}, err
	}
	if len(reference) != len(steps) {
		return migrations.Plan{}, errors.New("decision reference lineage length mismatch")
	}
	for i, step := range steps {
		r := reference[i]
		if step.ID != r.ID || step.Kind != r.Kind || step.Name != r.Name || step.SQL != r.SQL || r.Before != "" {
			return migrations.Plan{}, fmt.Errorf("decision reference differs from initial source step %d", i+1)
		}
		steps[i].After = r.After
	}
	plan := migrations.Plan{Database: database, Steps: steps}
	return plan, plan.Validate()
}
