package metadata

import (
	"fmt"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/migrations"
)

// AuthoringStatements never rewrites the 30-step foundation prefix. These
// additional guards enforce draft/commit serialization even for direct DML.
func AuthoringStatements(database, definerUser string) ([]migrations.Step, error) {
	steps, err := ScenarioFoundationStatements(database, definerUser)
	if err != nil {
		return nil, err
	}
	schema, _ := mysqlstore.Identifier(database)
	user, _ := mysqlstore.Identifier(definerUser)
	definer := user + "@`localhost`"
	for _, event := range []string{"INSERT", "UPDATE", "DELETE"} {
		image := "NEW"
		if event == "DELETE" {
			image = "OLD"
		}
		body := fmt.Sprintf("DECLARE v_status LONGTEXT; SELECT status INTO v_status FROM %s.decision_engine_scenario_iterations WHERE tenant_id=%s.tenant_id AND id=%s.iteration_id FOR SHARE; IF v_status IS NULL OR BINARY v_status<>_binary'draft' THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='rules require a draft iteration'; END IF;", schema, image, image)
		name := "decision_engine_rules_draft_" + strings.ToLower(event)
		stmt := fmt.Sprintf("CREATE DEFINER=%s TRIGGER %s.%s BEFORE %s ON %s.decision_engine_rules FOR EACH ROW BEGIN %s END", definer, schema, name, event, schema, body)
		steps = append(steps, migrations.Step{ID: fmt.Sprintf("decision_engine_v001_%04d", len(steps)+1), Kind: "TRIGGER", Name: name, SQL: stmt})
	}
	name := "decision_engine_committed_iteration_update"
	stmt := fmt.Sprintf("CREATE DEFINER=%s TRIGGER %s.%s BEFORE UPDATE ON %s.decision_engine_scenario_iterations FOR EACH ROW BEGIN IF BINARY OLD.status=_binary'committed' THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='committed iteration is immutable'; END IF; END", definer, schema, name, schema)
	steps = append(steps, migrations.Step{ID: fmt.Sprintf("decision_engine_v001_%04d", len(steps)+1), Kind: "TRIGGER", Name: name, SQL: stmt})
	return steps, nil
}

func AuthoringPlan(database, definer string, reference []migrations.Step) (migrations.Plan, error) {
	steps, err := AuthoringStatements(database, definer)
	if err != nil {
		return migrations.Plan{}, err
	}
	return bindAuthoringReference(database, steps, reference)
}

func bindAuthoringReference(database string, steps, reference []migrations.Step) (migrations.Plan, error) {
	if len(steps) != len(reference) {
		return migrations.Plan{}, fmt.Errorf("authoring reference length mismatch")
	}
	for i, s := range steps {
		r := reference[i]
		if r.ID != s.ID || r.Kind != s.Kind || r.Name != s.Name || r.SQL != s.SQL || r.Before != "" {
			return migrations.Plan{}, fmt.Errorf("authoring reference differs at %d", i+1)
		}
		steps[i].After = r.After
	}
	plan := migrations.Plan{Database: database, Steps: steps}
	return plan, plan.Validate()
}
