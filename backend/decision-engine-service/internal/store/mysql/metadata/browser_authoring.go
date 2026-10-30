package metadata

import (
	"fmt"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/migrations"
)

// A dedicated authoring guard avoids FOR UPDATE on the scenario parent used by
// unrelated decision/configuration FKs. It is not a tenant-wide/global mutex.
func BrowserAuthoringStatements(database, definer string) ([]migrations.Step, error) {
	steps, err := EffectsStatements(database, definer)
	if err != nil {
		return nil, err
	}
	schema, _ := mysqlstore.Identifier(database)
	user, _ := mysqlstore.Identifier(definer)
	appendStep := func(kind, name, statement string) {
		steps = append(steps, migrations.Step{ID: fmt.Sprintf("decision_engine_v001_%04d", len(steps)+1), Kind: kind, Name: name, SQL: statement})
	}
	name := "decision_engine_workflow_authoring_guards"
	appendStep("TABLE", name, fmt.Sprintf("CREATE TABLE %s.%s(tenant_id BINARY(16) NOT NULL,scenario_id BINARY(16) NOT NULL,PRIMARY KEY(tenant_id,scenario_id),FOREIGN KEY(tenant_id,scenario_id) REFERENCES %s.decision_engine_scenarios(tenant_id,id) ON DELETE CASCADE) ENGINE=InnoDB", schema, name, schema))
	for _, event := range []string{"INSERT", "UPDATE"} {
		body := "IF @@session.unique_checks<>1 OR @@session.foreign_key_checks<>1 THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='decision integrity checks must be enabled'; END IF; "
		if event == "UPDATE" {
			body += "IF NOT(NEW.tenant_id<=>OLD.tenant_id) OR NOT(NEW.scenario_id<=>OLD.scenario_id) THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='immutable authoring guard identity'; END IF; "
		}
		trigger := name + "_" + map[string]string{"INSERT": "insert", "UPDATE": "update"}[event]
		appendStep("TRIGGER", trigger, fmt.Sprintf("CREATE DEFINER=%s@`localhost` TRIGGER %s.%s BEFORE %s ON %s.%s FOR EACH ROW BEGIN %s END", user, schema, trigger, event, schema, name, body))
	}
	return steps, nil
}
func BrowserAuthoringPlan(database, definer string, reference []migrations.Step) (migrations.Plan, error) {
	steps, err := BrowserAuthoringStatements(database, definer)
	if err != nil {
		return migrations.Plan{}, err
	}
	return bindAuthoringReference(database, steps, reference)
}
func BrowserAuthoringDefinerGrants(database, user string) ([]string, error) {
	grants, err := EffectsDefinerGrants(database, user)
	if err != nil {
		return nil, err
	}
	schema, _ := mysqlstore.Identifier(database)
	account, _ := mysqlstore.Identifier(user)
	return append(grants, "GRANT SELECT,TRIGGER ON "+schema+".decision_engine_workflow_authoring_guards TO "+account+"@`localhost`"), nil
}
