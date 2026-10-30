package metadata

import (
	"embed"
	"fmt"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/migrations"
	"strings"
)

//go:embed configuration_sql/*.sql
var configurationSource embed.FS

func ConfigurationStatements(database, definer string) ([]migrations.Step, error) {
	steps, err := HistoryStatements(database, definer)
	if err != nil {
		return nil, err
	}
	schema, _ := mysqlstore.Identifier(database)
	user, _ := mysqlstore.Identifier(definer)
	appendStep := func(kind, name, stmt string) {
		steps = append(steps, migrations.Step{ID: fmt.Sprintf("decision_engine_v001_%04d", len(steps)+1), Kind: kind, Name: name, SQL: stmt})
	}
	objects := []struct{ file, table, child string }{{"001_workflows.sql", "decision_engine_workflows", "decision_engine_workflow_outcomes"}, {"002_screening_configs.sql", "decision_engine_screening_configs", "decision_engine_screening_outcomes"}, {"003_scoring_configs.sql", "decision_engine_scoring_configs", "decision_engine_scoring_outcomes"}}
	for _, o := range objects {
		raw, err := configurationSource.ReadFile("configuration_sql/" + o.file)
		if err != nil {
			return nil, err
		}
		appendStep("TABLE", o.table, strings.ReplaceAll(string(raw), "{{database}}", schema))
		// Positional child identity preserves duplicates, order, empty arrays and
		// SQL NULL elements. No digest or value uniqueness replaces array identity.
		stmt := fmt.Sprintf("CREATE TABLE %s.%s(tenant_id BINARY(16) NOT NULL,config_id BINARY(16) NOT NULL,position INT NOT NULL CHECK(position>=0),value LONGTEXT NULL,CHECK(value IS NULL OR LOCATE(X'00',CAST(value AS BINARY))=0),PRIMARY KEY(tenant_id,config_id,position),FOREIGN KEY(tenant_id,config_id) REFERENCES %s.%s(tenant_id,id) ON DELETE CASCADE) ENGINE=InnoDB ROW_FORMAT=DYNAMIC DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin", schema, o.child, schema, o.table)
		appendStep("TABLE", o.child, stmt)
	}
	for _, o := range objects {
		for _, table := range []string{o.table, o.child} {
			for _, event := range []string{"INSERT", "UPDATE"} {
				body := "IF @@session.unique_checks<>1 OR @@session.foreign_key_checks<>1 THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='decision integrity checks must be enabled'; END IF; "
				if event == "UPDATE" {
					columns := []string{"id", "tenant_id", "scenario_id", "created_at"}
					if table == o.child {
						columns = []string{"tenant_id", "config_id", "position"}
					}
					for _, column := range columns {
						body += fmt.Sprintf("IF NOT(NEW.%s<=>OLD.%s) THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='immutable configuration identity'; END IF; ", column, column)
					}
				}
				name := table + "_" + strings.ToLower(event)
				stmt := fmt.Sprintf("CREATE DEFINER=%s@`localhost` TRIGGER %s.%s BEFORE %s ON %s.%s FOR EACH ROW BEGIN %s END", user, schema, name, event, schema, table, body)
				appendStep("TRIGGER", name, stmt)
			}
		}
	}
	return steps, nil
}
func ConfigurationPlan(database, definer string, reference []migrations.Step) (migrations.Plan, error) {
	steps, err := ConfigurationStatements(database, definer)
	if err != nil {
		return migrations.Plan{}, err
	}
	return bindAuthoringReference(database, steps, reference)
}
func ConfigurationDefinerGrants(database, user string) ([]string, error) {
	grants, err := HistoryDefinerGrants(database, user)
	if err != nil {
		return nil, err
	}
	schema, _ := mysqlstore.Identifier(database)
	account, _ := mysqlstore.Identifier(user)
	for _, table := range []string{"decision_engine_workflows", "decision_engine_workflow_outcomes", "decision_engine_screening_configs", "decision_engine_screening_outcomes", "decision_engine_scoring_configs", "decision_engine_scoring_outcomes"} {
		grants = append(grants, "GRANT SELECT,TRIGGER ON "+schema+"."+table+" TO "+account+"@`localhost`")
	}
	return grants, nil
}
