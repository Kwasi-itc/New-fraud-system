package metadata

import (
	"embed"
	"fmt"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/migrations"
	"strings"
)

//go:embed history_sql/*.sql
var historySource embed.FS

func HistoryStatements(database, definer string) ([]migrations.Step, error) {
	steps, err := DecisionStatements(database, definer)
	if err != nil {
		return nil, err
	}
	schema, _ := mysqlstore.Identifier(database)
	user, _ := mysqlstore.Identifier(definer)
	raw, err := historySource.ReadFile("history_sql/001_rule_snoozes.sql")
	if err != nil {
		return nil, err
	}
	table := "decision_engine_rule_snoozes"
	steps = append(steps, migrations.Step{ID: fmt.Sprintf("decision_engine_v001_%04d", len(steps)+1), Kind: "TABLE", Name: table, SQL: strings.ReplaceAll(string(raw), "{{database}}", schema)})
	for _, event := range []string{"INSERT", "UPDATE", "DELETE"} {
		body := "IF @@session.unique_checks<>1 OR @@session.foreign_key_checks<>1 THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='decision integrity checks must be enabled'; END IF; "
		if event != "INSERT" {
			body += "SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='immutable snooze history'; "
		}
		name := table + "_" + strings.ToLower(event)
		stmt := fmt.Sprintf("CREATE DEFINER=%s@`localhost` TRIGGER %s.%s BEFORE %s ON %s.%s FOR EACH ROW BEGIN %s END", user, schema, name, event, schema, table, body)
		steps = append(steps, migrations.Step{ID: fmt.Sprintf("decision_engine_v001_%04d", len(steps)+1), Kind: "TRIGGER", Name: name, SQL: stmt})
	}
	return steps, nil
}
func HistoryPlan(database, definer string, reference []migrations.Step) (migrations.Plan, error) {
	steps, err := HistoryStatements(database, definer)
	if err != nil {
		return migrations.Plan{}, err
	}
	return bindAuthoringReference(database, steps, reference)
}
func HistoryDefinerGrants(database, user string) ([]string, error) {
	grants, err := DecisionDefinerGrants(database, user)
	if err != nil {
		return nil, err
	}
	schema, _ := mysqlstore.Identifier(database)
	account, _ := mysqlstore.Identifier(user)
	return append(grants, "GRANT SELECT,TRIGGER ON "+schema+".decision_engine_rule_snoozes TO "+account+"@`localhost`"), nil
}
