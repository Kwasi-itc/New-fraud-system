package metadata

import (
	"embed"
	"fmt"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/migrations"
)

//go:embed decision_sql/*.sql
var decisionSource embed.FS

func DecisionStatements(database, definer string) ([]migrations.Step, error) {
	steps, err := AuthoringStatements(database, definer)
	if err != nil {
		return nil, err
	}
	schema, _ := mysqlstore.Identifier(database)
	user, _ := mysqlstore.Identifier(definer)
	objects := []struct{ file, name string }{
		{"001_decisions.sql", "decision_engine_decisions"}, {"002_rule_evidence.sql", "decision_engine_rule_executions"},
		{"003_test_runs.sql", "decision_engine_test_runs"}, {"004_phantom_decisions.sql", "decision_engine_phantom_decisions"},
		{"005_phantom_evidence.sql", "decision_engine_phantom_rule_executions"},
	}
	for _, o := range objects {
		raw, err := decisionSource.ReadFile("decision_sql/" + o.file)
		if err != nil {
			return nil, err
		}
		steps = append(steps, migrations.Step{ID: fmt.Sprintf("decision_engine_v001_%04d", len(steps)+1), Kind: "TABLE", Name: o.name, SQL: strings.ReplaceAll(string(raw), "{{database}}", schema)})
	}
	for _, o := range objects {
		for _, event := range []string{"INSERT", "UPDATE", "DELETE"} {
			if o.name == "decision_engine_test_runs" && event == "DELETE" {
				continue
			}
			body := "IF @@session.unique_checks<>1 OR @@session.foreign_key_checks<>1 THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='decision integrity checks must be enabled'; END IF; "
			if event != "INSERT" {
				if o.name == "decision_engine_test_runs" {
					for _, column := range []string{"id", "tenant_id", "scenario_id", "live_iteration_id", "phantom_iteration_id", "created_at", "expires_at"} {
						body += fmt.Sprintf("IF NOT (NEW.%s <=> OLD.%s) THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='immutable test run identity'; END IF; ", column, column)
					}
				} else {
					body += "SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='immutable decision evidence'; "
				}
			}
			name := o.name + "_" + strings.ToLower(event)
			sql := fmt.Sprintf("CREATE DEFINER=%s@`localhost` TRIGGER %s.%s BEFORE %s ON %s.%s FOR EACH ROW BEGIN %s END", user, schema, name, event, schema, o.name, body)
			steps = append(steps, migrations.Step{ID: fmt.Sprintf("decision_engine_v001_%04d", len(steps)+1), Kind: "TRIGGER", Name: name, SQL: sql})
		}
	}
	return steps, nil
}
func DecisionPlan(database, definer string, reference []migrations.Step) (migrations.Plan, error) {
	steps, err := DecisionStatements(database, definer)
	if err != nil {
		return migrations.Plan{}, err
	}
	return bindAuthoringReference(database, steps, reference)
}
func DecisionDefinerGrants(database, user string) ([]string, error) {
	grants, err := ScenarioDefinerGrants(database, user)
	if err != nil {
		return nil, err
	}
	schema, _ := mysqlstore.Identifier(database)
	account, _ := mysqlstore.Identifier(user)
	for _, table := range []string{"decision_engine_decisions", "decision_engine_rule_executions", "decision_engine_test_runs", "decision_engine_phantom_decisions", "decision_engine_phantom_rule_executions"} {
		grants = append(grants, "GRANT SELECT,TRIGGER ON "+schema+"."+table+" TO "+account+"@`localhost`")
	}
	return grants, nil
}
