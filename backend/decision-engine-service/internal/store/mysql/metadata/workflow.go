package metadata

import (
	"embed"
	"fmt"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/migrations"
	"strings"
)

//go:embed workflow_sql/*.sql
var workflowSource embed.FS

func WorkflowStatements(database, definer string) ([]migrations.Step, error) {
	steps, err := ConfigurationStatements(database, definer)
	if err != nil {
		return nil, err
	}
	schema, _ := mysqlstore.Identifier(database)
	user, _ := mysqlstore.Identifier(definer)
	appendStep := func(kind, name, stmt string) {
		steps = append(steps, migrations.Step{ID: fmt.Sprintf("decision_engine_v001_%04d", len(steps)+1), Kind: kind, Name: name, SQL: stmt})
	}
	objects := []struct {
		file, table string
		immutable   []string
		history     bool
	}{
		{"001_rules.sql", "decision_engine_workflow_rules", []string{"id", "tenant_id", "scenario_id", "created_at"}, false},
		{"002_conditions.sql", "decision_engine_workflow_conditions", []string{"id", "tenant_id", "rule_id", "created_at"}, false},
		{"003_actions.sql", "decision_engine_workflow_actions", []string{"id", "tenant_id", "rule_id", "created_at"}, false},
		{"004_authoring_events.sql", "decision_engine_workflow_authoring_events", nil, true},
	}
	for _, o := range objects {
		raw, err := workflowSource.ReadFile("workflow_sql/" + o.file)
		if err != nil {
			return nil, err
		}
		appendStep("TABLE", o.table, strings.ReplaceAll(string(raw), "{{database}}", schema))
	}
	for _, o := range objects {
		for _, event := range []string{"INSERT", "UPDATE", "DELETE"} {
			if event == "DELETE" && !o.history {
				continue
			}
			body := "IF @@session.unique_checks<>1 OR @@session.foreign_key_checks<>1 THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='decision integrity checks must be enabled'; END IF; "
			if event != "INSERT" {
				if o.history {
					body += "SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='immutable workflow audit'; "
				} else {
					for _, column := range o.immutable {
						body += fmt.Sprintf("IF NOT(NEW.%s<=>OLD.%s) THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='immutable workflow identity'; END IF; ", column, column)
					}
					if o.table == "decision_engine_workflow_rules" {
						body += "IF OLD.deleted_at IS NOT NULL THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='workflow tombstone is immutable'; END IF; "
					}
				}
			}
			name := o.table + "_" + strings.ToLower(event)
			stmt := fmt.Sprintf("CREATE DEFINER=%s@`localhost` TRIGGER %s.%s BEFORE %s ON %s.%s FOR EACH ROW BEGIN %s END", user, schema, name, event, schema, o.table, body)
			appendStep("TRIGGER", name, stmt)
		}
	}
	return steps, nil
}
func WorkflowPlan(database, definer string, reference []migrations.Step) (migrations.Plan, error) {
	steps, err := WorkflowStatements(database, definer)
	if err != nil {
		return migrations.Plan{}, err
	}
	return bindAuthoringReference(database, steps, reference)
}
func WorkflowDefinerGrants(database, user string) ([]string, error) {
	grants, err := ConfigurationDefinerGrants(database, user)
	if err != nil {
		return nil, err
	}
	schema, _ := mysqlstore.Identifier(database)
	account, _ := mysqlstore.Identifier(user)
	for _, table := range []string{"decision_engine_workflow_rules", "decision_engine_workflow_conditions", "decision_engine_workflow_actions", "decision_engine_workflow_authoring_events"} {
		grants = append(grants, "GRANT SELECT,TRIGGER ON "+schema+"."+table+" TO "+account+"@`localhost`")
	}
	return grants, nil
}
