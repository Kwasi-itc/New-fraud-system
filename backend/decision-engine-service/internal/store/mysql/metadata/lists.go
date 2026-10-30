package metadata

import (
	"embed"
	"fmt"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/migrations"
	"strings"
)

//go:embed lists_sql/*.sql
var listsSource embed.FS

func ListsStatements(database, definer string) ([]migrations.Step, error) {
	steps, err := WorkflowStatements(database, definer)
	if err != nil {
		return nil, err
	}
	schema, _ := mysqlstore.Identifier(database)
	user, _ := mysqlstore.Identifier(definer)
	appendStep := func(kind, name, stmt string) {
		steps = append(steps, migrations.Step{ID: fmt.Sprintf("decision_engine_v001_%04d", len(steps)+1), Kind: kind, Name: name, SQL: stmt})
	}
	for _, o := range []struct{ file, name string }{{"001_lists.sql", "decision_engine_custom_lists"}, {"002_entries.sql", "decision_engine_custom_list_entries"}} {
		raw, err := listsSource.ReadFile("lists_sql/" + o.file)
		if err != nil {
			return nil, err
		}
		appendStep("TABLE", o.name, strings.ReplaceAll(string(raw), "{{database}}", schema))
	}
	for _, table := range []string{"decision_engine_custom_lists", "decision_engine_custom_list_entries"} {
		for _, event := range []string{"INSERT", "UPDATE"} {
			body := "DECLARE v_key BINARY(16); DECLARE v_name LONGTEXT; IF @@session.unique_checks<>1 OR @@session.foreign_key_checks<>1 THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='decision integrity checks must be enabled'; END IF; "
			if event == "UPDATE" {
				columns := []string{"id", "tenant_id", "created_at"}
				if table == "decision_engine_custom_list_entries" {
					columns = append(columns, "list_id")
				}
				for _, column := range columns {
					body += fmt.Sprintf("IF NOT(NEW.%s<=>OLD.%s) THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='immutable custom list identity'; END IF; ", column, column)
				}
			}
			if table == "decision_engine_custom_lists" {
				body += fmt.Sprintf("CALL %s.decision_engine_resolve_exact_key(_binary'de.custom_list.name',NEW.tenant_id,CAST(LOWER(NEW.name COLLATE utf8mb4_0900_as_cs) AS BINARY),v_key); SET NEW.name_key_id=v_key;", schema)
			} else {
				body += fmt.Sprintf("IF NEW.list_id IS NOT NULL THEN SELECT name INTO v_name FROM %s.decision_engine_custom_lists WHERE tenant_id=NEW.tenant_id AND id=NEW.list_id FOR SHARE; IF v_name IS NULL THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='custom list owner missing'; END IF; SET NEW.list_name=v_name; END IF;", schema)
			}
			name := table + "_" + strings.ToLower(event)
			stmt := fmt.Sprintf("CREATE DEFINER=%s@`localhost` TRIGGER %s.%s BEFORE %s ON %s.%s FOR EACH ROW BEGIN %s END", user, schema, name, event, schema, table, body)
			appendStep("TRIGGER", name, stmt)
		}
	}
	return steps, nil
}
func ListsPlan(database, definer string, reference []migrations.Step) (migrations.Plan, error) {
	steps, err := ListsStatements(database, definer)
	if err != nil {
		return migrations.Plan{}, err
	}
	return bindAuthoringReference(database, steps, reference)
}
func ListsDefinerGrants(database, user string) ([]string, error) {
	grants, err := WorkflowDefinerGrants(database, user)
	if err != nil {
		return nil, err
	}
	schema, _ := mysqlstore.Identifier(database)
	account, _ := mysqlstore.Identifier(user)
	for _, table := range []string{"decision_engine_custom_lists", "decision_engine_custom_list_entries"} {
		grants = append(grants, "GRANT SELECT,TRIGGER ON "+schema+"."+table+" TO "+account+"@`localhost`")
	}
	grants = append(grants, "GRANT UPDATE(name_key_id) ON "+schema+".decision_engine_custom_lists TO "+account+"@`localhost`", "GRANT UPDATE(list_name) ON "+schema+".decision_engine_custom_list_entries TO "+account+"@`localhost`")
	return grants, nil
}
