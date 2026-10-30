package metadata

import (
	"embed"
	"errors"
	"fmt"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/migrations"
)

//go:embed scenario_sql/*.sql
var scenarioSource embed.FS

// ScenarioFoundationStatements appends to the initial platform lineage. The
// supplied definer is a separate locked local account, never an application
// login or root. Its creation/grants are privileged deployment operations;
// this function only describes objects and does not change any database.
func ScenarioFoundationStatements(database, definerUser string) ([]migrations.Step, error) {
	schema, err := mysqlstore.Identifier(database)
	if err != nil {
		return nil, err
	}
	user, err := mysqlstore.Identifier(definerUser)
	if err != nil || len(definerUser) > 32 || strings.EqualFold(definerUser, "root") {
		return nil, errors.New("dedicated bounded decision definer required")
	}
	definer := user + "@`localhost`"
	steps, err := PlatformStatements(database)
	if err != nil {
		return nil, err
	}
	objects := []struct{ file, kind, name string }{
		{"001_buckets.sql", "TABLE", "decision_engine_exact_key_buckets"},
		{"002_keys.sql", "TABLE", "decision_engine_exact_keys"},
		{"003_digest.sql", "FUNCTION", "decision_engine_exact_key_digest"},
		{"004_resolve.sql", "PROCEDURE", "decision_engine_resolve_exact_key"},
		{"005_scenarios.sql", "TABLE", "decision_engine_scenarios"},
		{"006_iterations.sql", "TABLE", "decision_engine_scenario_iterations"},
		{"007_live_links.sql", "TABLE", "decision_engine_scenario_live_iterations"},
		{"008_rules.sql", "TABLE", "decision_engine_rules"},
		{"009_publications.sql", "TABLE", "decision_engine_scenario_publications"},
		{"010_versions.sql", "TABLE", "decision_engine_scenario_versions"},
		{"011_receipts.sql", "TABLE", "decision_engine_iteration_creations"},
	}
	for _, o := range objects {
		raw, err := scenarioSource.ReadFile("scenario_sql/" + o.file)
		if err != nil {
			return nil, err
		}
		stmt := strings.NewReplacer("{{database}}", schema, "{{definer}}", definer).Replace(string(raw))
		steps = append(steps, migrations.Step{ID: fmt.Sprintf("decision_engine_v001_%04d", len(steps)+1), Kind: o.kind, Name: o.name, SQL: stmt})
	}
	for _, event := range []string{"INSERT", "UPDATE"} {
		body := "DECLARE v_key BINARY(16); IF @@session.unique_checks<>1 OR @@session.foreign_key_checks<>1 THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='decision integrity checks must be enabled'; END IF; "
		if event == "UPDATE" {
			body += "IF NOT (NEW.id <=> OLD.id) OR NOT (NEW.tenant_id <=> OLD.tenant_id) THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='immutable scenario identity'; END IF; "
		}
		body += fmt.Sprintf("CALL %s.decision_engine_resolve_exact_key(_binary'de.scenario.name',NEW.tenant_id,CAST(NEW.name AS BINARY),v_key); SET NEW.name_key_id=v_key;", schema)
		name := "decision_engine_scenario_name_" + strings.ToLower(event)
		stmt := fmt.Sprintf("CREATE DEFINER=%s TRIGGER %s.%s BEFORE %s ON %s.decision_engine_scenarios FOR EACH ROW BEGIN %s END", definer, schema, name, event, schema, body)
		steps = append(steps, migrations.Step{ID: fmt.Sprintf("decision_engine_v001_%04d", len(steps)+1), Kind: "TRIGGER", Name: name, SQL: stmt})
	}
	// Match the existing MySQL owner pattern: a runtime session cannot bypass
	// relational integrity by disabling checks. Parent/resource identity never
	// moves between tenants. History/receipts are immutable; parent FK cascades
	// intentionally retain the source deletion semantics (cascades skip triggers).
	for _, guard := range scenarioGuards {
		for _, event := range []string{"INSERT", "UPDATE"} {
			body := "IF @@session.unique_checks<>1 OR @@session.foreign_key_checks<>1 THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='decision integrity checks must be enabled'; END IF; "
			if event == "UPDATE" {
				if guard.history {
					body += "SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='immutable decision history'; "
				} else {
					for _, column := range guard.immutable {
						body += fmt.Sprintf("IF NOT (NEW.%s <=> OLD.%s) THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='immutable decision identity'; END IF; ", column, column)
					}
				}
			}
			name := guard.table + "_" + strings.ToLower(event)
			stmt := fmt.Sprintf("CREATE DEFINER=%s TRIGGER %s.%s BEFORE %s ON %s.%s FOR EACH ROW BEGIN %s END", definer, schema, name, event, schema, guard.table, body)
			steps = append(steps, migrations.Step{ID: fmt.Sprintf("decision_engine_v001_%04d", len(steps)+1), Kind: "TRIGGER", Name: name, SQL: stmt})
		}
		if guard.history {
			name := guard.table + "_delete"
			stmt := fmt.Sprintf("CREATE DEFINER=%s TRIGGER %s.%s BEFORE DELETE ON %s.%s FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='immutable decision history'", definer, schema, name, schema, guard.table)
			steps = append(steps, migrations.Step{ID: fmt.Sprintf("decision_engine_v001_%04d", len(steps)+1), Kind: "TRIGGER", Name: name, SQL: stmt})
		}
	}
	return steps, nil
}

var scenarioGuards = []struct {
	table     string
	immutable []string
	history   bool
}{
	{"decision_engine_scenario_iterations", []string{"id", "tenant_id", "scenario_id", "version"}, false},
	{"decision_engine_rules", []string{"id", "tenant_id", "iteration_id"}, false},
	{"decision_engine_scenario_live_iterations", []string{"tenant_id", "scenario_id"}, false},
	{"decision_engine_scenario_publications", nil, true},
	{"decision_engine_scenario_versions", []string{"tenant_id", "scenario_id"}, false},
	{"decision_engine_iteration_creations", nil, true},
}

// ScenarioFoundationPlan requires independent exact reference observations for
// every step. It never adopts definitions from an existing application DB.
func ScenarioFoundationPlan(database, definer string, reference []migrations.Step) (migrations.Plan, error) {
	steps, err := ScenarioFoundationStatements(database, definer)
	if err != nil {
		return migrations.Plan{}, err
	}
	if len(steps) != len(reference) {
		return migrations.Plan{}, errors.New("decision foundation reference length mismatch")
	}
	for i, s := range steps {
		r := reference[i]
		if r.ID != s.ID || r.Kind != s.Kind || r.Name != s.Name || r.SQL != s.SQL || r.Before != "" {
			return migrations.Plan{}, fmt.Errorf("decision foundation reference differs at %d", i+1)
		}
		steps[i].After = r.After
	}
	plan := migrations.Plan{Database: database, Steps: steps}
	return plan, plan.Validate()
}

// ScenarioDefinerGrants is an explicit deployment input, not executed here.
// The account must stay locked, without roles/grant option/other privileges.
// Runtime callers receive no dictionary access or resolver EXECUTE.
func ScenarioDefinerGrants(database, user string) ([]string, error) {
	if _, err := ScenarioFoundationStatements(database, user); err != nil {
		return nil, err
	}
	schema, _ := mysqlstore.Identifier(database)
	account, _ := mysqlstore.Identifier(user)
	account += "@`localhost`"
	grants := []string{
		"GRANT SELECT,INSERT,UPDATE ON " + schema + ".decision_engine_exact_key_buckets TO " + account,
		"GRANT SELECT,INSERT ON " + schema + ".decision_engine_exact_keys TO " + account,
		"GRANT SELECT,TRIGGER ON " + schema + ".decision_engine_scenarios TO " + account,
		"GRANT UPDATE(name_key_id) ON " + schema + ".decision_engine_scenarios TO " + account,
		"GRANT EXECUTE ON FUNCTION " + schema + ".decision_engine_exact_key_digest TO " + account,
		"GRANT EXECUTE ON PROCEDURE " + schema + ".decision_engine_resolve_exact_key TO " + account,
	}
	for _, guard := range scenarioGuards {
		grants = append(grants, "GRANT SELECT,TRIGGER ON "+schema+"."+guard.table+" TO "+account)
	}
	return grants, nil
}
