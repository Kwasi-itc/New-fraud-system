package migrationaudit

import (
	"errors"
	"fmt"
	"strings"
)

type Manifest struct {
	Format       string          `json:"format"`
	Status       string          `json:"status"`
	Scope        string          `json:"scope"`
	Source       Catalog         `json:"source_catalog"`
	Files        []SourceFile    `json:"source_files"`
	Queries      []QueryContract `json:"query_contracts"`
	Tables       []TableMapping  `json:"table_mappings"`
	Routines     []ObjectMapping `json:"routine_mappings"`
	Triggers     []ObjectMapping `json:"trigger_mappings"`
	Dependencies []Dependency    `json:"cross_owner_contracts"`
	Limitations  []string        `json:"limitations"`
}
type SourceFile struct{ Path, SHA256 string }
type QueryContract struct {
	ID, File                         string
	Line                             int
	Access, References, SQLFragments []string
	Contract, Limitations            string
}
type TableMapping struct {
	Source, Target, Purpose, Status string
	Columns                         []ColumnMapping
	AdditionalColumns               []string
	Constraints                     []ObjectMapping
	Indexes                         []ObjectMapping
	QueryCandidates                 []string
	SourceOwnerDML                  []string
	RuntimeGrants, Preflight        []string
}
type ColumnMapping struct {
	Source, TargetObject, TargetColumn, TargetType, Representation, Collation, Rule, Reverse, DefaultRule, Status string
	SourceNullable, TargetValueNullable                                                                           bool
	QueryCandidates                                                                                               []string
	Preflight, Verification                                                                                       []string
}
type ObjectMapping struct {
	Source, Target, Strategy, Status, KeyBudget string
	QueryCandidates, Verification               []string
}
type Dependency struct{ Owner, Source, TargetContract, RequiredBefore, Status string }

var purposes = map[string]string{
	"scenarios":                 "Scenario identity and unique tenant-scoped name",
	"scenario_iterations":       "Versioned scenario configuration, trigger and thresholds",
	"scenario_publications":     "Publish/unpublish history",
	"rules":                     "Ordered rule formulas and score modifiers",
	"decisions":                 "Persisted evaluation outcome, score and request",
	"rule_executions":           "Per-rule evidence belonging to a decision",
	"test_runs":                 "Live/phantom comparison run and expiry",
	"phantom_decisions":         "Comparison evaluation result",
	"phantom_rule_executions":   "Comparison per-rule evidence",
	"workflows":                 "Legacy workflow configuration and ordered allowed outcomes",
	"workflow_rules":            "Structured workflow parent, order and soft deletion",
	"workflow_conditions":       "Ordered structured conditions",
	"workflow_actions":          "Ordered structured actions",
	"workflow_authoring_events": "Trusted actor and original authoring snapshot",
	"workflow_executions":       "Durable workflow effect and historical identities",
	"rule_snoozes":              "Time-bounded rule-group suppression for a record",
	"outbox_events":             "Optional integration publication, not all required work",
	"scheduled_executions":      "Scheduled/manual/recurring evaluation and domain retries",
	"async_decision_executions": "Queued evaluation, result, callback and retry state",
	"screening_configs":         "Provider configuration, allowed outcomes and review inbox hint",
	"screening_executions":      "Decision-owned provider/projection lifecycle",
	"scoring_configs":           "External scoring configuration and allowed outcomes",
	"scoring_requests":          "Durable scoring effect/result",
	"custom_lists":              "Case-insensitive tenant-scoped list name and metadata",
	"custom_list_entries":       "Exact list values with optional list identity",
	"record_tags":               "Append/read exact record tags",
	"risk_snapshots":            "Append/read latest exact record risk",
	"ip_flags":                  "Append/read exact textual IP flag (not INET)",
	"investigator_reviews":      "Immutable initial review, operation receipt and case effects",
}

func platformTable(table string) bool {
	return table == "record_tags" || table == "risk_snapshots" || table == "ip_flags"
}

// Build covers each catalog object, but intentionally leaves approval and
// implementation status distinct. A type proposal is NOT executable DDL.
func Build(catalog Catalog, files []SourceFile, queries []QueryContract) (Manifest, error) {
	m := Manifest{Format: "decision-schema-mapping/v1", Status: "needs_review", Scope: "Fresh decision-only PostgreSQL migration lineage through version 22; proposed MySQL mapping, not deployment or production-data profiling", Source: catalog, Files: files, Queries: queries, Dependencies: dependencies()}
	seen := map[string]bool{}
	for _, source := range catalog.Tables {
		name := strings.TrimPrefix(source.Name, "core.")
		purpose, ok := purposes[name]
		if !ok || source.Name != "core."+name || seen[name] {
			return Manifest{}, fmt.Errorf("unreviewed/duplicate decision table %s", source.Name)
		}
		seen[name] = true
		target := "decision_engine_" + name
		table := TableMapping{Source: source.Name, Target: target, Purpose: purpose, Status: "proposed_not_implemented", QueryCandidates: queryIDs(queries, source.Name)}
		if platformTable(name) {
			table.Status = "implemented_staged_runtime_not_selected"
		}
		for _, column := range source.Columns {
			mapping, err := mapColumn(name, column)
			if err != nil {
				return Manifest{}, err
			}
			mapping.QueryCandidates = columnQueryIDs(queries, source.Name, column.Name)
			table.Columns = append(table.Columns, mapping)
		}
		switch name {
		case "scenarios":
			table.AdditionalColumns = []string{"name_key_id BINARY(16): collision-safe exact-name dictionary reference; database guard must bind tenant/raw text/key; pending implementation"}
		case "custom_lists":
			table.AdditionalColumns = []string{"name_key_id BINARY(16): dictionary identity for reviewed source-equivalent lower(name); case-fold contract is unresolved"}
		case "scheduled_executions", "async_decision_executions":
			table.AdditionalColumns = []string{"idempotency_key_id BINARY(16) NULL: nonempty exact text namespace scoped by tenant/execution kind; no unique hash shortcut; receipt protocol still required"}
		case "investigator_reviews":
			table.AdditionalColumns = []string{"resource_key_id BINARY(16): exact text identity retaining source tenant/resource uniqueness; owner separately validates decision UUID binding"}
		}
		if name == "rule_executions" || name == "phantom_rule_executions" {
			table.AdditionalColumns = []string{"tenant_id BINARY(16): derive through owning decision; quarantine missing/cross-tenant rule relationships", "scenario_iteration_id BINARY(16): bind decision iteration and rule iteration with composite FKs; proposed strengthening, not installed"}
		}
		for _, constraint := range source.Constraints {
			mapping, err := mapConstraint(name, constraint)
			if err != nil {
				return Manifest{}, err
			}
			table.Constraints = append(table.Constraints, mapping)
		}
		for _, index := range source.Indexes {
			table.Indexes = append(table.Indexes, mapIndex(name, index, table.QueryCandidates))
		}
		table.RuntimeGrants = []string{"SELECT on explicit owner projection only; tenant authorization/predicates remain mandatory", "Mutation grants must be split by implemented operation; no database-wide or cross-owner DML, DDL or GRANT OPTION"}
		table.SourceOwnerDML = ownerDML(name)
		table.RuntimeGrants = append(table.RuntimeGrants, "Underlying owner executor requires reviewed column-level "+strings.Join(table.SourceOwnerDML, ",")+" for these source operations; do not grant that whole set to every API/worker login")
		if platformTable(name) {
			table.RuntimeGrants = []string{"SELECT,INSERT on this table for the append/read adapter", "No UPDATE,DELETE,DDL,ledger or other-owner privileges; production authorizer/deployment identity still pending"}
		}
		if name == "investigator_reviews" || name == "workflow_authoring_events" {
			table.RuntimeGrants = append(table.RuntimeGrants, "INSERT/SELECT through reviewed owner contract; deny history UPDATE/DELETE; review guard requires database-enforced bypass tests")
		}
		table.Preflight = []string{"Production data not inspected: profile nulls, timestamp range/precision and text/JSON limits", "Validate every source FK/orphan and tenant/scenario/iteration binding before adding stronger composite constraints", "No source tenant FK exists here; do not silently introduce a data-model tenant dependency", "Choose bounded projections/pages and explicit error handling; never drop overflow rows or return a partial successful result"}
		m.Tables = append(m.Tables, table)
	}
	if len(seen) != len(purposes) {
		return Manifest{}, errors.New("decision catalog does not cover every reviewed owner table")
	}
	if len(catalog.Routines) != 2 || len(catalog.Triggers) != 1 {
		return Manifest{}, errors.New("decision routine/trigger catalog is incomplete")
	}
	for _, routine := range catalog.Routines {
		var strategy string
		switch routine.Name {
		case "core.guard_investigator_review":
			strategy = "Preserve INSERT-only review history and current tenant/resource validation; minimally privileged owner guard, immediate FK/write ordering and restricted-account bypass tests. No generic PL/pgSQL text rewrite."
		case "core.reconcile_screening_source":
			strategy = "P-04 owner reconciliation in caller transaction: source-before-queue lock order, exact tenant/decision/config/scenario/idempotency binding, strictly newer version and atomic result. Requires screening source schema and identity/queue contracts."
		default:
			return Manifest{}, fmt.Errorf("unreviewed decision routine %s", routine.Name)
		}
		m.Routines = append(m.Routines, ObjectMapping{Source: routine.Name + "(" + routine.Signature + ")", Target: "decision_engine_" + strings.TrimPrefix(routine.Name, "core."), Strategy: strategy, Status: "blocked_owner_contract_and_security_review", Verification: []string{"Restricted invoker/definer grants and active-transaction enforcement", "Rollback, cross-tenant, concurrent/stale effect and bypass tests"}})
	}
	for _, trigger := range catalog.Triggers {
		if trigger.Name != "guard_investigator_review" || trigger.Table != "core.investigator_reviews" {
			return Manifest{}, errors.New("unreviewed decision trigger")
		}
		m.Triggers = append(m.Triggers, ObjectMapping{Source: trigger.Table + ":" + trigger.Name, Target: "decision_engine_investigator_review_guard", Strategy: "Preserve immutable INSERT-only history and resource/tenant check for direct SQL, not only API validation", Status: "proposed_not_implemented", Verification: []string{"INSERT wrong tenant/resource and UPDATE/DELETE bypass attempts must fail"}})
	}
	m.Limitations = []string{"Catalog is a fresh PG16 owner fixture, not the actual deployed schema/collation/grants or existing-data profile", "Query candidates are source-level associations, not proof an index is selected; computed identifiers and external contracts require the documented manual review", "Three platform adapters only have exploratory MySQL 9.7.1 evidence; frozen 8.4.8 definitions, full owner migration/grant installation and Aurora acceptance are not supplied", "MySQL type/default/index proposals and tenant-strengthening designs require review before scenario/rule storage implementation", "Owner schema_migrations_decision_engine becomes a new decision_engine_schema_migrations lineage, not copied history; shared River tables are not decision-owned and must never be rolled back by this owner"}
	return m, nil
}

func mapColumn(table string, c Column) (ColumnMapping, error) {
	v := ColumnMapping{Source: c.Name, TargetObject: "decision_engine_" + table, TargetColumn: c.Name, SourceNullable: c.Nullable, TargetValueNullable: c.Nullable, Representation: "scalar", Status: "proposed_not_implemented", DefaultRule: "No SQL default; owner binds explicit value", Collation: "not_applicable", Verification: []string{"Codec and cross-engine round trip", "Invalid/null input fails explicitly, never successful no-match"}}
	if platformTable(table) {
		v.Status = "implemented_staged_runtime_not_selected"
	}
	switch c.Type {
	case "uuid":
		v.TargetType = "BINARY(16)"
		v.Rule = "Standard network UUID byte order, no swap; preserve NULL distinctly"
		v.Reverse = "Format the same 16 bytes as canonical UUID text"
		v.Preflight = []string{"Profile zero/noncanonical textual API identities; do not change stored UUID identity"}
	case "integer":
		v.TargetType = "INT"
		v.Rule = "Signed 32-bit source range; use Int32/NullInt32, do not widen score/version semantics"
		v.Reverse = "Exact signed integer"
	case "bigint":
		v.TargetType = "BIGINT"
		v.Rule = "Signed 64-bit counters/versions; preserve overflow errors"
		v.Reverse = "Exact signed integer"
	case "boolean":
		v.TargetType = "BOOLEAN with enforced 0/1 CHECK"
		v.Rule = "Bool/NullBool codec plus storage CHECK; numeric alias alone is insufficient"
		v.Reverse = "Exact Boolean"
	case "text":
		v.TargetType = "LONGTEXT"
		v.Collation = "utf8mb4_0900_bin plus explicit binary equality where required"
		v.Rule = "Preserve full UTF-8 text, no NUL, truncation, case folding or normalization; indexes/uniqueness use separate reviewed strategies"
		v.Reverse = "Same Unicode text; do not normalize spaces or case"
		v.Preflight = []string{"Profile lengths, server packet/resource budgets and source deployment collation", "Preserve source text identifiers as text, even when they resemble UUIDs"}
	case "jsonb":
		v.TargetType = "JSON"
		v.Rule = "JSON/NullJSON storage codecs preserve SQL NULL versus JSON null; no float64 decoding for storage"
		v.Reverse = "Semantic JSON, not byte-exact formatting"
		v.Preflight = []string{"Profile numeric precision/range, depth, Unicode escape compatibility and scalar JSON null"}
	case "timestamp with time zone":
		v.TargetType = "DATETIME(6) UTC"
		v.Rule = "DateTime/NullDateTime; explicit microsecond precision and UTC, never silently round expiry/version tokens"
		v.Reverse = "UTC instant; original timezone formatting is not stored"
		v.Preflight = []string{"Profile infinities and years outside 1000..9999; quarantine, do not coerce zero dates"}
	case "text[]":
		v.TargetValueNullable = true
		v.TargetType = "Ordered child rows (ordinal INT,value LONGTEXT nullable)"
		v.Representation = "ordered_child_relation"
		v.TargetObject = "decision_engine_" + table + "_allowed_outcomes"
		v.TargetColumn = "value"
		v.Collation = "utf8mb4_0900_bin"
		v.Rule = "Parent plus zero-based ordinal; preserve order, duplicates and NULL elements. Empty array is zero rows. Source outer array is NOT NULL."
		v.Reverse = "Aggregate child rows in explicit ordinal order; no implicit JSON aggregate order"
		v.Preflight = []string{"Profile array dimensions/lower bounds and NULL elements; non-1D or nonstandard lower bounds require explicit compatibility handling", "Composite tenant/parent FK and ordinal key required; batch reads in one snapshot, no N+1"}
	default:
		return ColumnMapping{}, fmt.Errorf("unreviewed type %s on %s.%s", c.Type, table, c.Name)
	}
	if c.Default != nil {
		d := *c.Default
		switch {
		case d == "0" || d == "3" || d == "true" || d == "false":
			v.DefaultRule = "Preserve literal default " + d
		case strings.HasSuffix(d, "::text"):
			v.DefaultRule = "Preserve exact source text literal " + d + " with explicit MySQL text expression default; no truncation"
		case strings.HasSuffix(d, "::jsonb"):
			v.DefaultRule = "Preserve source JSON default " + d + " using a reviewed MySQL JSON expression default"
		case d == "now()":
			v.DefaultRule = "Owner supplies explicit transaction/operation time; PostgreSQL NOW is transaction-start, MySQL UTC_TIMESTAMP is statement-start. Do not claim they are interchangeable."
		case d == "clock_timestamp()":
			v.DefaultRule = "Owner supplies current UTC microsecond time at the reviewed transition after lock waits; not transaction-start time"
		case d == "gen_random_uuid()":
			v.DefaultRule = "Allocate UUID before INSERT and preserve it through operation-receipt reconciliation; no MySQL UUID byte swapping"
		default:
			return ColumnMapping{}, fmt.Errorf("unreviewed default %s on %s.%s", d, table, c.Name)
		}
	}
	if table == "scenarios" && c.Name == "live_iteration_id" {
		v.TargetValueNullable = false
		v.TargetObject = "decision_engine_scenario_live_iterations"
		v.TargetColumn = "iteration_id"
		v.Representation = "optional_relationship_row"
		v.Rule = "Absence represents source NULL; composite (tenant,scenario,iteration) FK ensures correct scenario and tenant. Relationship cascades away on iteration deletion without SET NULL on a non-null tenant column."
		v.Reverse = "LEFT JOIN yields original nullable live_iteration_id"
		v.Status = "proposed_relationship_redesign_requires_review"
	}
	if table == "async_decision_executions" && c.Name == "scenario_id" {
		v.TargetValueNullable = false
		v.TargetObject = "decision_engine_async_execution_scenarios"
		v.TargetColumn = "scenario_id"
		v.Representation = "optional_relationship_row"
		v.Rule = "Absent relationship represents NULL; relationship row cascades on scenario deletion while retaining async execution, preserving source SET NULL without nullable tenant identity."
		v.Reverse = "LEFT JOIN yields original nullable scenario_id"
		v.Status = "proposed_relationship_redesign_requires_review"
	}
	if table == "ip_flags" && c.Name == "ip_address" {
		v.Rule = "Source is TEXT, not INET. Preserve exact textual IPv4/IPv6 spelling; no address canonicalization."
	}
	return v, nil
}

func mapConstraint(table string, c Constraint) (ObjectMapping, error) {
	v := ObjectMapping{Source: c.Name, Target: "decision_engine_" + table + ":" + strings.Join(c.Columns, ","), Status: "proposed_not_implemented", KeyBudget: "Compute from final native/dictionary key parts before DDL; never index full unbounded text", Verification: []string{"Direct SQL constraint rejection, rollback and tenant-binding tests"}}
	switch c.Kind {
	case "p":
		v.Strategy = "Preserve primary key order/uniqueness; UUID is 16 bytes per part"
		v.KeyBudget = fmt.Sprintf("%d bytes for UUID-only source primary-key parts", 16*len(c.Columns))
	case "u":
		v.Strategy = "Preserve full uniqueness; text resource_id requires collision-safe exact-key dictionary with database-enforced raw/key binding, not a UNIQUE hash or prefix"
	case "f":
		if c.ReferencedTable == nil {
			return ObjectMapping{}, errors.New("foreign key missing parent")
		}
		v.Target += " -> decision_engine_" + strings.TrimPrefix(*c.ReferencedTable, "core.") + ":" + strings.Join(c.ReferencedColumns, ",")
		v.Strategy = "Preserve referenced identity and delete behavior, with reviewed tenant-composite parent keys/FKs and owner scenario/iteration binding. Source ID-only FKs do not prove tenant consistency; validate existing data first."
		if strings.Contains(c.Definition, "SET NULL") {
			v.Strategy = "Use proposed optional relationship row for nullable pointer. Cascade relationship deletion preserves API NULL without clearing non-null tenant/scope columns. Requires owner query/delete-path review."
			v.Status = "blocked_optional_relationship_design_review"
			if table == "scenarios" {
				v.Target = "decision_engine_scenario_live_iterations:(tenant_id,scenario_id,iteration_id) -> decision_engine_scenario_iterations:(tenant_id,scenario_id,id)"
			}
			if table == "async_decision_executions" {
				v.Target = "decision_engine_async_execution_scenarios:(tenant_id,scenario_id) -> decision_engine_scenarios:(tenant_id,id)"
			}
		}
	case "c":
		v.Strategy = "Translate this exact stored CHECK expression, not a handler-only approximation; retain byte length versus character count and PostgreSQL btrim space semantics"
	default:
		return ObjectMapping{}, fmt.Errorf("unreviewed constraint kind %s", c.Kind)
	}
	if c.Deferrable {
		v.Status = "blocked_immediate_constraint_order_review"
		v.Strategy += "; InnoDB checks are immediate: reorder dependent owner writes, never disable FK checks"
	}
	if platformTable(table) {
		v.Status = "implemented_staged_runtime_not_selected"
	}
	return v, nil
}

func mapIndex(table string, i Index, queries []string) ObjectMapping {
	v := ObjectMapping{Source: i.Name, Target: "decision_engine_" + table + ":" + strings.Join(i.Keys, ","), Status: "candidate_not_measured", QueryCandidates: queries, KeyBudget: "Final native key must fit 3072-byte InnoDB budget at 16KiB pages, including utf8mb4 bytes and all parts; full LONGTEXT is not indexable", Verification: []string{"EXPLAIN ANALYZE on selective/hot keys and actual filter/order/page predicates; table-reference association alone proves no index use"}}
	switch {
	case i.Primary:
		v.Strategy = "Same primary key with standard binary UUID order"
		v.Status = "structural_mapping_requires_review"
	case i.Name == "scenarios_tenant_name_idx":
		v.Strategy = "Exact-name dictionary scoped by tenant; full raw bytes and collision-safe owner guard; UNIQUE tenant/name-key ID"
	case i.Name == "custom_lists_tenant_name_idx":
		v.Strategy = "Source lower(name) uniqueness requires reviewed source-locale Unicode case-fold equivalence, then collision-safe exact-key dictionary; MySQL LOWER/default case-insensitive collation is not assumed equivalent"
		v.Status = "blocked_casefold_contract"
	case strings.Contains(i.Name, "idempotency"):
		v.Strategy = "Nullable exact-key dictionary identity per tenant and execution kind; NULL and exactly empty text have no reservation, whitespace remains significant; uniqueness and request/result receipt protocol required"
		v.Status = "blocked_exact_key_and_receipt_contract"
	case i.Name == "scheduled_executions_recurring_unique_idx":
		v.Strategy = "Unique tenant/scenario/instant reservation only for exact source='recurring'; generated nullable scope or occurrence relation, with database-side atomic job/watermark protocol"
		v.Status = "blocked_schedule_occurrence_contract"
	case strings.Contains(i.Name, "object_id_prefix"):
		v.Strategy = "Non-unique bounded prefix candidate plus full escaped prefix predicate; hash cannot serve prefix/ILIKE search. Review case/wildcard/collation semantics and residual/order plan."
	case strings.Contains(i.Name, "active_order"):
		v.Strategy = "Candidate tenant,scenario,deleted_at,priority,created_at,id path for active rows; no blind partial-index-to-boolean-prefix translation"
	case i.Unique:
		v.Strategy = "Preserve all unique key expressions/predicates and NULL behavior; use dictionary for unbounded text, never hash-only uniqueness"
		v.Status = "blocked_unique_key_design_review"
	default:
		v.Strategy = "Native bounded UUID/numeric/time leading parts; text equality candidates use non-unique SHA accelerators plus full binary predicates, then required sort/page keys. Expression/partial semantics need explicit target query review."
	}
	if platformTable(table) {
		v.Status = "platform_candidate_exploratory_9_7_1_plans"
		v.Strategy = "See implemented platform DDL: global UUID primary key and non-unique SHA lookup + full binary residual + created_at,id ordering. Positive 5000-row plans do not establish every candidate index benefit."
		v.KeyBudget = "16-byte tenant + at most three 32-byte digest parts; ordered lookup also has 8-byte DATETIME(6) and 16-byte ID, comfortably below native key limit"
	}
	return v
}

func queryIDs(queries []QueryContract, table string) []string {
	result := []string{}
	for _, q := range queries {
		for _, ref := range q.References {
			if ref == table {
				result = append(result, q.ID)
				break
			}
		}
	}
	return result
}

func ownerDML(table string) []string {
	switch table {
	case "scenarios", "rules", "workflows", "workflow_conditions", "workflow_actions", "screening_configs", "scoring_configs", "custom_lists", "custom_list_entries":
		return []string{"SELECT", "INSERT", "UPDATE", "DELETE"}
	case "scenario_iterations", "test_runs", "workflow_rules", "workflow_executions", "outbox_events", "scheduled_executions", "async_decision_executions", "screening_executions", "scoring_requests":
		return []string{"SELECT", "INSERT", "UPDATE"}
	default:
		return []string{"SELECT", "INSERT"}
	}
}

func dependencies() []Dependency {
	return []Dependency{
		{"data_model", "core.model_tables/core.model_fields; datamodel HTTP metadata", "Published registry/contract authorizes tenant table and fields; immutable physical names and schema version/barrier, never guessed identifiers", "Direct MySQL tenant reads, aggregates and payload schema preparation", "owner_integration_pending"},
		{"ingestion", "TenantDataReader and ingestion HTTP aggregate/list/get contracts", "Equivalent typed filter/null/numeric/time contract; bounded direct/HTTP reads from the same authority", "Historical SQL/HTTP parity and decision runtime selection", "owner_integration_pending"},
		{"identity", "identity.authorize_data_model_v1; identity.authorize_case_v1; shared CheckIdentity", "Trusted actor, current user/membership/session checks in caller transaction, user->membership->session lock order; no broad identity writes", "Workflow authoring and investigator review mutations", "owner_sql_and_grants_pending"},
		{"case_manager", "case_manager.cases/inboxes/inbox_users/case_decisions/case_screenings; owner review history routine", "Read/owner routine contract preserves inbox permissions and atomic case event/outbox; no arbitrary case DML; review_inbox_id remains a hint validated by owner", "Case workflow authoring and investigator review/case actions", "owner_sql_and_grants_pending"},
		{"screening", "screening.screenings/screening_matches and projection queue", "P-04 source-first locking, binding and monotonic projection; don't add reverse migration dependency or guess caller transaction semantics", "decision_engine_reconcile_screening_source", "source_schema_and_reconciliation_contract_pending"},
		{"shared_workers", "public River schema and seven decision job kinds", "Typed MySQL enqueue on caller sql.Tx, durable claims/fences/receipts and callback idempotency; no silent missing queues or per-owner shared rollback", "Atomic decision/evidence/effects activation and runtime cutover", "decision_delivery_adapter_and_full_lifecycle_pending"},
		{"deployment", "schema_migrations_decision_engine and owner migration command", "New decision_engine_schema_migrations lineage, checked definitions and dependency bootstrap; restricted installer separate from runtime, no API migrations", "Pinned-version installation, upgrade and production activation", "review_required"},
	}
}
