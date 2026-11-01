package mysqlrelease

import (
	"errors"
	"sort"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql/metadata"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
)

// OwnerGrants describes ONLY decision-owned DML for an existing dedicated
// account. Account provisioning/TLS/passwords, foreign-owner contract grants,
// deployment inspection and installer privileges are separate reviewed work.
// There are no wildcard, dictionary, routine, DDL or GRANT OPTION privileges.
// Table privileges are not row authorization: trusted caller-TX checks remain
// mandatory and runtime credentials must never be exposed to end users.
func OwnerGrants(database, user, host, role string) ([]string, error) {
	schema, err := mysqlstore.Identifier(database)
	if err != nil {
		return nil, err
	}
	account, err := mysqlstore.Identifier(user)
	if err != nil || len(user) > 32 || user == "root" {
		return nil, errors.New("dedicated runtime account required")
	}
	// Explicit hosts only; '%' must not accidentally become a deployment default.
	if host == "" || len(host) > 255 || strings.ContainsAny(host, "'`\\\x00%_\n\r") {
		return nil, errors.New("explicit literal runtime host required")
	}
	if role != "api" && role != "worker" {
		return nil, errors.New("role must be api or worker")
	}
	permissions := map[string]string{}
	add := func(privileges string, tables ...string) {
		for _, table := range tables {
			permissions["decision_engine_"+table] = privileges
		}
	}
	add("SELECT,INSERT", "record_tags", "risk_snapshots", "ip_flags", "decisions", "rule_executions", "phantom_decisions", "phantom_rule_executions", "rule_snoozes")
	// MySQL FOR UPDATE also requires a mutation privilege. Grant UPDATE only
	// on the immutable receipt key, NOT receipt bodies; owner triggers reject
	// key changes (an unchanged-key statement need not fail). This permits fences without
	// allowing DELETE, LOCK TABLES or general receipt mutation.
	add("SELECT,INSERT,UPDATE(id)", "effect_job_receipts", "execution_job_receipts")
	add("SELECT,INSERT,UPDATE", "test_runs", "scheduled_executions", "async_executions", "effect_job_deliveries", "execution_job_deliveries", "workflow_executions", "screening_executions", "scoring_requests", "outbox_events")
	add("SELECT", "schema_migrations", "scenarios", "scenario_iterations", "scenario_live_iterations", "rules", "scenario_publications", "scenario_versions", "iteration_creations", "workflows", "workflow_outcomes", "screening_configs", "screening_outcomes", "scoring_configs", "scoring_outcomes", "workflow_rules", "workflow_conditions", "workflow_actions", "workflow_authoring_events", "workflow_authoring_guards", "custom_lists", "custom_list_entries", "async_scenario_links", "effect_cancellations")
	if role == "api" {
		add("SELECT,INSERT,UPDATE,DELETE", "scenarios", "scenario_iterations", "rules", "workflows", "workflow_outcomes", "screening_configs", "screening_outcomes", "scoring_configs", "scoring_outcomes", "workflow_conditions", "workflow_actions", "custom_lists", "custom_list_entries")
		add("SELECT,INSERT,UPDATE", "scenario_live_iterations", "scenario_versions", "workflow_rules", "workflow_authoring_guards", "execution_schedules")
		add("SELECT,INSERT", "scenario_publications", "iteration_creations", "workflow_authoring_events", "async_scenario_links", "effect_cancellations")
		add("SELECT", "schedule_occurrences")
		add("SELECT,UPDATE(receipt_id)", "dispatch_intents")
	} else {
		add("SELECT,UPDATE", "execution_schedules")
		add("SELECT,INSERT", "schedule_occurrences")
		add("SELECT,INSERT,UPDATE", "dispatch_intents")
		// Only delivery copies are retained/cleaned. Permanent owner records,
		// receipts and intents are deliberately not deletable by workers.
		add("SELECT,INSERT,UPDATE,DELETE", "effect_job_deliveries", "execution_job_deliveries")
	}
	// Exhaustive ownership classification: a future table cannot silently gain
	// broad runtime access or disappear from the release grant review.
	steps, err := metadata.DispatchStatements(database, "de_grant_review")
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{"decision_engine_schema_migrations": true}
	for _, step := range steps {
		if step.Kind != "TABLE" {
			continue
		}
		seen[step.Name] = true
		if step.Name == "decision_engine_exact_keys" || step.Name == "decision_engine_exact_key_buckets" {
			continue
		}
		if permissions[step.Name] == "" {
			return nil, errors.New("unclassified decision table in runtime grants: " + step.Name)
		}
	}
	names := make([]string, 0, len(permissions))
	for name := range permissions {
		if !seen[name] {
			return nil, errors.New("grant names a non-owner table: " + name)
		}
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]string, 0, len(names))
	for _, name := range names {
		out = append(out, "GRANT "+permissions[name]+" ON "+schema+"."+name+" TO "+account+"@'"+host+"'")
	}
	return out, nil
}
