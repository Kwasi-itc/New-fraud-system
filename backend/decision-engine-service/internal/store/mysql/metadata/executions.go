package metadata

import (
	"embed"
	"fmt"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/migrations"
)

//go:embed execution_sql/*.sql
var executionSource embed.FS

// ExecutionStatements preserves the 115-step effect-job prefix. These are
// decision-owned objects in the SAME application database, not a per-service DB.
func ExecutionStatements(database, definer string) ([]migrations.Step, error) {
	steps, err := EffectJobsStatements(database, definer)
	if err != nil {
		return nil, err
	}
	schema, _ := mysqlstore.Identifier(database)
	user, _ := mysqlstore.Identifier(definer)
	add := func(kind, name, statement string) {
		steps = append(steps, migrations.Step{ID: fmt.Sprintf("decision_engine_v001_%04d", len(steps)+1), Kind: kind, Name: name, SQL: statement})
	}
	tables := []struct {
		file, name string
		immutable  []string
		history    bool
	}{
		{"001_scheduled.sql", "decision_engine_scheduled_executions", []string{"scenario_id", "scenario_iteration_id", "source", "idempotency_key", "idempotency_key_id", "scheduled_for", "request_body", "max_attempts"}, false},
		{"002_async.sql", "decision_engine_async_executions", []string{"object_type", "idempotency_key", "idempotency_key_id", "request_body", "callback_url", "max_attempts", "callback_max_attempts"}, false},
		{"002a_async_links.sql", "decision_engine_async_scenario_links", []string{"execution_id", "scenario_id"}, false},
		{"003_receipts.sql", "decision_engine_execution_job_receipts", nil, true},
		{"004_deliveries.sql", "decision_engine_execution_job_deliveries", []string{"kind", "queue_name", "max_attempts"}, false},
		{"005_schedules.sql", "decision_engine_execution_schedules", []string{"scenario_id", "scenario_iteration_id", "start_at", "schedule_kind", "interval_us", "calendar_spec", "start_local", "missed_policy", "lateness_us", "max_attempts", "request_body"}, false},
		{"006_occurrences.sql", "decision_engine_schedule_occurrences", nil, true},
	}
	for _, table := range tables {
		raw, err := executionSource.ReadFile("execution_sql/" + table.file)
		if err != nil {
			return nil, err
		}
		add("TABLE", table.name, strings.ReplaceAll(string(raw), "{{database}}", schema))
	}
	for _, table := range tables {
		for _, event := range []string{"INSERT", "UPDATE", "DELETE"} {
			if event == "DELETE" && !table.history {
				continue
			}
			body := "DECLARE v_key BINARY(16); IF @@session.unique_checks<>1 OR @@session.foreign_key_checks<>1 THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='decision integrity checks must be enabled'; END IF; "
			if event == "INSERT" && (table.name == "decision_engine_scheduled_executions" || table.name == "decision_engine_async_executions") {
				domain := "de.scheduled.request"
				if table.name == "decision_engine_async_executions" {
					domain = "de.async.request"
				}
				body += fmt.Sprintf("IF OCTET_LENGTH(NEW.idempotency_key)=0 THEN SET NEW.idempotency_key_id=NULL; ELSE CALL %s.decision_engine_resolve_exact_key(_binary'%s',NEW.tenant_id,CAST(NEW.idempotency_key AS BINARY),v_key); SET NEW.idempotency_key_id=v_key; END IF; ", schema, domain)
			}
			if event == "INSERT" && table.name == "decision_engine_schedule_occurrences" {
				// Tenant FKs alone do not prove that a same-tenant execution
				// belongs to THIS definition and occurrence. Immutable parent
				// bindings permit bounded PK reads without a broad parent lock.
				body += fmt.Sprintf("IF NOT EXISTS(SELECT 1 FROM %s.decision_engine_execution_schedules s JOIN %s.decision_engine_scheduled_executions e ON e.tenant_id=s.tenant_id AND e.scenario_id=s.scenario_id AND e.scenario_iteration_id=s.scenario_iteration_id WHERE s.tenant_id=NEW.tenant_id AND s.id=NEW.schedule_id AND e.id=NEW.execution_id AND e.source=_binary'recurring' AND e.scheduled_for=NEW.scheduled_for) THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='occurrence execution binding mismatch'; END IF; ", schema, schema)
			}
			if event == "UPDATE" || event == "DELETE" {
				if table.history {
					body += "SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='immutable execution receipt'; "
				} else {
					identity := []string{"id", "tenant_id", "created_at"}
					if table.name == "decision_engine_async_scenario_links" {
						identity = []string{"tenant_id"}
					}
					for _, col := range append(identity, table.immutable...) {
						body += fmt.Sprintf("IF NOT(NEW.%s<=>OLD.%s) THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='immutable execution identity'; END IF; ", col, col)
					}
					if table.name == "decision_engine_scheduled_executions" || table.name == "decision_engine_async_executions" {
						body += "IF NEW.generation=OLD.generation AND OLD.cancelled_at IS NOT NULL AND NOT(NEW.cancelled_at<=>OLD.cancelled_at) THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='cancellation timestamp is immutable in generation'; END IF; "
						body += "IF NEW.generation<OLD.generation OR NEW.generation>OLD.generation+1 OR(NEW.generation=OLD.generation AND NEW.attempt_count<OLD.attempt_count) THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='execution attempt cannot regress'; END IF; "
						if table.name == "decision_engine_async_executions" {
							body += "IF NEW.generation=OLD.generation AND OLD.callback_cancelled_at IS NOT NULL AND NOT(NEW.callback_cancelled_at<=>OLD.callback_cancelled_at) THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='callback cancellation timestamp is immutable in generation'; END IF; "
							body += "IF NEW.generation=OLD.generation AND NEW.callback_attempt_count<OLD.callback_attempt_count THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='callback attempt cannot regress'; END IF; "
						}
					}
					if table.name == "decision_engine_execution_schedules" {
						body += "IF NEW.next_at<OLD.next_at OR NEW.next_local<OLD.next_local OR NEW.watermark_version<OLD.watermark_version OR NEW.skipped_count<OLD.skipped_count THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='schedule watermark cannot regress'; END IF; "
					}
				}
			}
			name := table.name + "_" + strings.ToLower(event)
			add("TRIGGER", name, fmt.Sprintf("CREATE DEFINER=%s@`localhost` TRIGGER %s.%s BEFORE %s ON %s.%s FOR EACH ROW BEGIN %s END", user, schema, name, event, schema, table.name, body))
		}
	}
	// Append ALTER steps rather than rewriting the earlier 115-step effect-job
	// definitions. Full before/after table snapshots attest index installation.
	for _, family := range []string{"effect", "execution"} {
		table := "decision_engine_" + family + "_job_deliveries"
		add("TABLE", table, fmt.Sprintf("ALTER TABLE %s.%s ADD INDEX de_%s_delivery_cleanup(tenant_id,kind,queue_name,state,finished_at,id), ALGORITHM=INPLACE, LOCK=NONE", schema, table, family))
	}
	return steps, nil
}

func ExecutionPlan(database, definer string, reference []migrations.Step) (migrations.Plan, error) {
	steps, err := ExecutionStatements(database, definer)
	if err != nil {
		return migrations.Plan{}, err
	}
	return bindExecutionReference(database, steps, reference)
}

func bindExecutionReference(database string, steps, reference []migrations.Step) (migrations.Plan, error) {
	if len(steps) != len(reference) {
		return migrations.Plan{}, fmt.Errorf("execution reference length mismatch")
	}
	latest := map[string]string{}
	for i, s := range steps {
		r := reference[i]
		before := ""
		if strings.HasPrefix(s.SQL, "ALTER ") {
			before = latest[s.Kind+":"+s.Name]
			if before == "" {
				return migrations.Plan{}, fmt.Errorf("execution alteration lacks reviewed predecessor")
			}
		}
		if r.ID != s.ID || r.Kind != s.Kind || r.Name != s.Name || r.SQL != s.SQL || r.Before != before {
			return migrations.Plan{}, fmt.Errorf("execution reference differs at %d", i+1)
		}
		steps[i].Before, steps[i].After = before, r.After
		latest[s.Kind+":"+s.Name] = r.After
	}
	plan := migrations.Plan{Database: database, Steps: steps}
	return plan, plan.Validate()
}

func ExecutionDefinerGrants(database, user string) ([]string, error) {
	grants, err := EffectJobsDefinerGrants(database, user)
	if err != nil {
		return nil, err
	}
	schema, _ := mysqlstore.Identifier(database)
	account, _ := mysqlstore.Identifier(user)
	for _, table := range []string{"scheduled_executions", "async_executions", "async_scenario_links", "execution_job_receipts", "execution_job_deliveries", "execution_schedules", "schedule_occurrences"} {
		grants = append(grants, "GRANT SELECT,TRIGGER ON "+schema+".decision_engine_"+table+" TO "+account+"@`localhost`")
	}
	for _, table := range []string{"scheduled_executions", "async_executions"} {
		grants = append(grants, "GRANT UPDATE(idempotency_key_id) ON "+schema+".decision_engine_"+table+" TO "+account+"@`localhost`")
	}
	return grants, nil
}
