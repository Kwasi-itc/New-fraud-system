package mysql

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/decision"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/scenario"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/service"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/postgres"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type scalarGoldenCase struct {
	name          string
	formula       map[string]any
	matched       bool
	errorContains string
}

func scalarConstant(v any) map[string]any { return map[string]any{"constant": v} }
func scalarCall(name string, children ...map[string]any) map[string]any {
	return map[string]any{"function": name, "children": children}
}
func scalarNamed(name string, values map[string]any) map[string]any {
	children := map[string]any{}
	for k, v := range values {
		children[k] = scalarConstant(v)
	}
	return map[string]any{"function": name, "named_children": children}
}

// Golden expectations are explicit, rather than derived from either evaluator.
// The shared fixture persists and publishes the same rules on both engines.
func TestActualDecisionServicePostgresMySQLScalarGoldenParity(t *testing.T) {
	cases := scalarGoldenCorpus()
	for start := 0; start < len(cases); start += 24 {
		end := min(start+24, len(cases))
		t.Run(fmt.Sprintf("batch_%02d", start/24), func(t *testing.T) { actualDecisionServiceParity(t, false, cases[start:end]...) })
	}
}

func scalarGoldenCorpus() []scalarGoldenCase {
	c, f := scalarConstant, scalarCall
	var cases []scalarGoldenCase
	add := func(name string, formula map[string]any, matched bool) {
		cases = append(cases, scalarGoldenCase{name, formula, matched, ""})
	}
	bad := func(name string, formula map[string]any, message string) {
		cases = append(cases, scalarGoldenCase{name, formula, false, message})
	}
	eq := func(a map[string]any, b any) map[string]any { return f("eq", a, c(b)) }
	add("payload_present_zero", eq(f("payload", c("amount")), 0), true)
	add("payload_missing", f("is_null", f("payload", c("absent"))), true)
	add("field_ref_present", eq(scalarNamed("field_ref", map[string]any{"field": "amount"}), 2), true)
	add("field_ref_missing", f("is_null", scalarNamed("field_ref", map[string]any{"field": "absent"})), true)
	bad("field_ref_invalid", scalarNamed("field_ref", map[string]any{"field": 12}), "must be a string")
	for _, v := range []any{nil, false, 0, ""} {
		add(fmt.Sprintf("coalesce_%v", v), eq(f("coalesce", c(v), c("fallback")), func() any {
			if v == nil {
				return "fallback"
			}
			return v
		}()), true)
	}
	add("coalesce_all_null", f("is_null", f("coalesce", c(nil), c(nil))), true)
	// The evidence evaluator returns nil for an empty coalesce; the boolean
	// rule contract rejects it. Preserve this source behavior in parity tests.
	bad("coalesce_empty", f("coalesce"), "boolean children")
	add("list_empty", eq(f("list_count", f("list")), 0), true)
	add("list_null_false_zero", eq(f("list_count", f("list", c(nil), c(false), c(0))), 3), true)
	add("and_short_circuit", f("and", c(false), f("divide", c(1), c(0))), false)
	add("or_short_circuit", f("or", c(true), f("divide", c(1), c(0))), true)
	add("not_false", f("not", c(false)), true)
	bad("and_invalid", f("and", c(1)), "boolean")
	bad("or_invalid", f("or", c(1)), "boolean")
	bad("not_invalid", f("not", c(nil)), "boolean")
	for _, op := range []string{"eq", "neq", "gt", "gte", "lt", "lte"} {
		add(op+"_boundary", f(op, c(1.5), c(1.5)), op == "eq" || op == "gte" || op == "lte")
		add(op+"_null", f(op, c(nil), c(1)), op == "neq")
		bad(op+"_mixed", f(op, c(true), c("true")), "boolean operands")
	}
	add("null_equals_null", f("eq", c(nil), c(nil)), true)
	for _, tc := range []struct {
		name, fn string
		a, b     any
		want     bool
	}{
		{"contains_unicode", "contains", "café", "fé", true},
		{"contains_case", "contains", "ABC", "abc", false},
		{"string_not_contain", "string_not_contain", "abc", "z", true},
		{"in_list", "in", "a", []any{"a", "b"}, true},
		{"in_empty", "in", "a", []any{}, false},
		{"contains_any_case", "contains_any", "ABC", []any{"ab"}, true},
		{"contains_none_empty", "contains_none", "abc", []any{}, true},
		{"starts_with_unicode", "starts_with", "éclair", "é", true},
		{"ends_with_empty", "ends_with", "abc", "", true},
		{"is_in_list", "is_in_list", 0, []any{0, 1}, true},
		{"is_not_in_list", "is_not_in_list", "A", []any{"a"}, true},
	} {
		add(tc.name, f(tc.fn, c(tc.a), c(tc.b)), tc.want)
	}
	add("lower_unicode", eq(f("lower", c("ÉCOLE")), "école"), true)
	add("upper_unicode", eq(f("upper", c("école")), "ÉCOLE"), true)
	add("is_empty_null", f("is_empty", c(nil)), true)
	add("is_empty_string", f("is_empty", c("")), true)
	add("is_not_empty", f("is_not_empty", c("x")), true)
	add("is_null_false", f("is_null", c(false)), false)
	bad("lower_invalid", f("lower", c(42)), "string")
	bad("contains_any_invalid", f("contains_any", c("abc"), c("a")), "list")
	bad("in_invalid", f("in", c("a"), c(1)), "list")
	add("concat_null_number", eq(f("string_concat", c("a"), c(nil), c(2)), "a2"), true)
	bad("concat_invalid", f("string_concat", c(true)), "not supported")
	add("template_literal", eq(f("string_template", c("café")), "café"), true)
	bad("template_empty", f("string_template", c("")), "non-empty")
	for _, tc := range []struct {
		fn         string
		a, b, want any
	}{
		{"add", -2, 0.5, -1.5}, {"subtract", 0, 2, -2}, {"multiply", -2, 0, 0}, {"divide", 1, 4, 0.25},
	} {
		add(tc.fn+"_numeric", eq(f(tc.fn, c(tc.a), c(tc.b)), tc.want), true)
		bad(tc.fn+"_invalid", f(tc.fn, c("bad"), c(2)), "numeric")
	}
	bad("divide_zero", f("divide", c(1), c(0)), "zero")
	add("multiple_negative", scalarNamed("is_multiple_of", map[string]any{"value": -6, "divider": 3}), true)
	add("multiple_fraction", scalarNamed("is_multiple_of", map[string]any{"value": 1.5, "divider": 3}), false)
	bad("multiple_zero", scalarNamed("is_multiple_of", map[string]any{"value": 1, "divider": 0}), "non-zero integer")
	add("fixed_clock", eq(f("time_now"), "2026-11-02T12:00:00.123456Z"), true)
	add("parse_timezone_boundary", eq(f("parse_time", c("2026-11-03T00:30:00+02:00")), "2026-11-02T22:30:00Z"), true)
	bad("parse_malformed", f("parse_time", c("yesterday")), "RFC3339")
	add("extract_null", f("is_null", scalarNamed("timestamp_extract", map[string]any{"timestamp": nil, "part": "hour"})), true)
	add("extract_year", eq(scalarNamed("timestamp_extract", map[string]any{"timestamp": "2026-11-02T22:30:00Z", "part": "year"}), 2026), true)
	bad("extract_bad_unit", scalarNamed("timestamp_extract", map[string]any{"timestamp": "2026-11-02T22:30:00Z", "part": "fortnight"}), "not supported")
	bad("extract_bad_time", scalarNamed("timestamp_extract", map[string]any{"timestamp": "bad", "part": "year"}), "timestamp")
	add("add_time_boundary", eq(scalarNamed("time_add", map[string]any{"timestampField": "2026-11-02T23:30:00Z", "duration": "1h", "sign": "+"}), "2026-11-03T00:30:00Z"), true)
	add("subtract_time_boundary", eq(scalarNamed("time_add", map[string]any{"timestampField": "2026-11-03T00:30:00Z", "duration": "1h", "sign": "-"}), "2026-11-02T23:30:00Z"), true)
	bad("add_time_bad_sign", scalarNamed("time_add", map[string]any{"timestampField": "2026-11-02T23:30:00Z", "duration": "1h", "sign": "?"}), "sign")
	bad("add_time_bad_duration", scalarNamed("time_add", map[string]any{"timestampField": "2026-11-02T23:30:00Z", "duration": "nonsense", "sign": "+"}), "duration")
	get := func(object map[string]any, key string) map[string]any {
		return map[string]any{"function": "object_get", "named_children": map[string]any{"object": object, "key": c(key)}}
	}
	add("nested_object", eq(get(get(c(map[string]any{"outer": map[string]any{"inner": false}}), "outer"), "inner"), false), true)
	add("object_missing", f("is_null", get(c(map[string]any{}), "missing")), true)
	bad("object_invalid", get(c(1), "missing"), "object")
	for _, op := range []string{"sum", "avg", "min", "max"} {
		add(op+"_empty", eq(f(op, c([]any{})), 0), true)
		want := map[string]any{"sum": 3, "avg": 1, "min": -1, "max": 2.5}[op]
		add(op+"_fraction", eq(f(op, c([]any{-1, 1.5, 2.5})), want), true)
		bad(op+"_invalid", f(op, c([]any{1, "bad"})), "numeric")
	}
	items := []any{map[string]any{"group": "a", "value": 2}, map[string]any{"group": "a", "value": 3}, map[string]any{"group": "b", "value": -1}}
	add("filter_eq_duplicates", eq(f("list_count", scalarNamed("filter_eq", map[string]any{"items": items, "field": "group", "value": "a"})), 2), true)
	add("filter_eq_empty", eq(f("list_count", scalarNamed("filter_eq", map[string]any{"items": []any{}, "field": "group", "value": "a"})), 0), true)
	bad("filter_eq_invalid", scalarNamed("filter_eq", map[string]any{"items": 1, "field": "group", "value": "a"}), "list")
	add("map_field", eq(f("sum", scalarNamed("map_field", map[string]any{"items": items, "field": "value"})), 4), true)
	add("map_field_missing", eq(f("list_count", scalarNamed("map_field", map[string]any{"items": []any{map[string]any{}, nil}, "field": "value"})), 2), true)
	add("group_count_duplicates", eq(get(scalarNamed("group_count", map[string]any{"items": items, "field": "group"}), "a"), 2), true)
	add("group_sum_duplicates", eq(get(scalarNamed("group_sum", map[string]any{"items": items, "group_field": "group", "value_field": "value"}), "a"), 5), true)
	add("group_count_empty", f("is_null", get(scalarNamed("group_count", map[string]any{"items": []any{}, "field": "group"}), "a")), true)
	add("group_sum_empty", f("is_null", get(scalarNamed("group_sum", map[string]any{"items": []any{}, "group_field": "group", "value_field": "value"}), "a")), true)
	bad("group_sum_missing_value", scalarNamed("group_sum", map[string]any{"items": []any{map[string]any{"group": "a"}}, "group_field": "group", "value_field": "value"}), "numeric")
	add("filter_value", f("not", f("is_null", scalarNamed("filter", map[string]any{"tableName": "wallets", "fieldName": "amount", "operator": "=", "value": 0}))), true)
	bad("filter_malformed", scalarNamed("filter", map[string]any{}), "tableName")
	score := func(hit any) map[string]any {
		n := scalarNamed("score_computation", map[string]any{"modifier": 7, "floor": 2})
		n["children"] = []map[string]any{c(hit)}
		return n
	}
	add("score_modifier", eq(get(score(true), "modifier"), 7), true)
	add("score_miss_zero", eq(get(score(false), "modifier"), 0), true)
	bad("score_invalid_child", score("bad"), "boolean")
	switchNode := scalarNamed("switch", map[string]any{"field": "selected"})
	switchNode["children"] = []map[string]any{score(false), score(true)}
	add("switch_first_matching_branch", eq(get(switchNode, "branch"), 1), true)
	bad("switch_missing_branch", scalarNamed("switch", map[string]any{"field": "selected"}), "at least one branch")
	invalidSwitch := scalarNamed("switch", map[string]any{"field": "selected"})
	invalidSwitch["children"] = []map[string]any{c(true)}
	bad("switch_invalid_branch", invalidSwitch, "score_computation")
	for _, fn := range []string{"fuzzy_match", "fuzzy_match_any_of"} {
		var right any = "CAFÉ"
		if fn == "fuzzy_match_any_of" {
			right = []any{"other", "CAFÉ"}
		}
		n := scalarNamed(fn, map[string]any{"algorithm": "ratio"})
		n["children"] = []map[string]any{c("café"), c(right)}
		add(fn+"_unicode", eq(n, 100), true)
		invalid := scalarNamed(fn, map[string]any{"algorithm": "unsupported"})
		invalid["children"] = []map[string]any{c("a"), c(right)}
		bad(fn+"_invalid_algorithm", invalid, "unknown fuzzy match algorithm")
	}
	options := scalarNamed("fuzzy_match_filter_options", map[string]any{"algorithm": "ratio", "threshold": 75, "value": "café"})
	add("fuzzy_options_threshold", eq(get(options, "threshold"), 0.75), true)
	bad("fuzzy_options_invalid_threshold", scalarNamed("fuzzy_match_filter_options", map[string]any{"algorithm": "ratio", "threshold": 101, "value": "a"}), "between 0 and 100")
	// Exercise the public aliases using the same independently specified golden
	// outcomes, including errors. Clone nodes so the canonical cases stay intact.
	aliases := map[string]string{"eq": "=", "neq": "!=", "gt": ">", "gte": ">=", "lt": "<", "lte": "<=", "add": "+", "subtract": "-", "multiply": "*", "divide": "/", "payload": "Payload", "time_now": "TimeNow", "parse_time": "ParseTime", "time_add": "TimeAdd", "timestamp_extract": "TimestampExtract", "contains": "StringContains", "string_not_contain": "StringNotContain", "starts_with": "StringStartsWith", "ends_with": "StringEndsWith", "contains_any": "ContainsAnyOf", "contains_none": "ContainsNoneOf", "is_in_list": "IsInList", "is_not_in_list": "IsNotInList", "is_empty": "IsEmpty", "is_not_empty": "IsNotEmpty", "is_multiple_of": "IsMultipleOf", "string_concat": "StringConcat", "string_template": "StringTemplate", "score_computation": "ScoreComputation", "fuzzy_match": "FuzzyMatch", "fuzzy_match_any_of": "FuzzyMatchAnyOf", "fuzzy_match_filter_options": "FuzzyMatchOptions"}
	for _, tc := range append([]scalarGoldenCase(nil), cases...) {
		// These two expect the selected request's case index, not a fixed input.
		if tc.name == "payload_present_zero" || tc.name == "field_ref_present" {
			continue
		}
		clone, changed := scalarAliasNode(tc.formula, aliases)
		if changed {
			tc.name = "alias_" + tc.name
			tc.formula = clone
			cases = append(cases, tc)
		}
	}
	return cases
}

func scalarAliasNode(node map[string]any, aliases map[string]string) (map[string]any, bool) {
	copy := make(map[string]any, len(node))
	changed := false
	for k, v := range node {
		switch k {
		case "function":
			if alias, ok := aliases[v.(string)]; ok {
				v = alias
				changed = true
			}
		case "children":
			children := v.([]map[string]any)
			out := make([]map[string]any, len(children))
			for i, child := range children {
				c, yes := scalarAliasNode(child, aliases)
				out[i] = c
				changed = changed || yes
			}
			v = out
		case "named_children":
			out := map[string]any{}
			for name, child := range v.(map[string]any) {
				c, yes := scalarAliasNode(child.(map[string]any), aliases)
				out[name] = c
				changed = changed || yes
			}
			v = out
		}
		copy[k] = v
	}
	return copy, changed
}

func runScalarGoldenParity(t *testing.T, ctx context.Context, source, target service.DecisionService, pg *pgxpool.Pool, history *DecisionStore, tenant, parent uuid.UUID, rules []scenario.Rule, cases []scalarGoldenCase) {
	persisted := 0
	for index, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			request := service.DecisionEvaluationRequest{ObjectID: tc.name, ObjectType: "wallets", Fields: map[string]any{"amount": index}}
			left, le := source.EvaluateScenario(ctx, tenant.String(), parent.String(), request)
			right, re := target.EvaluateScenario(ctx, tenant.String(), parent.String(), request)
			if tc.errorContains != "" {
				for _, e := range []error{le, re} {
					if e == nil || !strings.Contains(e.Error(), tc.errorContains) {
						t.Fatalf("wanted error containing %q, got %v", tc.errorContains, e)
					}
				}
				if left.Decision != nil || right.Decision != nil || len(left.RuleExecutions) != 0 || len(right.RuleExecutions) != 0 {
					t.Fatal("failed evaluation exposed provisional output")
				}
			} else {
				if le != nil || re != nil {
					t.Fatal(le, re)
				}
				wantScore := 0
				if tc.matched {
					wantScore = 1
				}
				for _, result := range []service.DecisionEvaluationResult{left, right} {
					if !result.Triggered || result.Decision == nil || result.Decision.Score != wantScore || result.Decision.Outcome != decision.OutcomeApprove || len(result.RuleExecutions) != len(rules) {
						t.Fatalf("wrong golden result: %+v", result)
					}
					seen := map[string]bool{}
					for _, e := range result.RuleExecutions {
						hit := e.RuleID == rules[index].ID && tc.matched
						want := "no_hit"
						if hit {
							want = "hit"
						}
						if seen[e.RuleID] || e.Outcome != want || e.Evaluation == nil || e.Evaluation.ReturnValue != hit {
							t.Fatalf("wrong evidence: %+v", e)
						}
						seen[e.RuleID] = true
					}
					for _, rule := range rules {
						if !seen[rule.ID] {
							t.Fatal("missing rule", rule.ID)
						}
					}
				}
				if !reflect.DeepEqual(normalizedServiceResult(t, left), normalizedServiceResult(t, right)) {
					t.Fatal("returned full evidence differs")
				}
				savedLeft, savedRight := left, right
				ld, e := postgres.NewDecisionRepository(pg).GetByID(ctx, tenant.String(), left.Decision.ID)
				if e != nil {
					t.Fatal(e)
				}
				savedLeft.Decision = &ld
				savedLeft.RuleExecutions, e = postgres.NewRuleExecutionRepository(pg).ListByDecision(ctx, tenant.String(), ld.ID)
				if e != nil {
					t.Fatal(e)
				}
				if e := history.ReadSnapshot(ctx, func(r DecisionRepositories) error {
					rd, e := r.Decisions.GetByID(ctx, tenant.String(), right.Decision.ID)
					if e != nil {
						return e
					}
					savedRight.Decision = &rd
					savedRight.RuleExecutions, e = r.RuleExecutions.ListByDecision(ctx, tenant.String(), rd.ID)
					return e
				}); e != nil {
					t.Fatal(e)
				}
				for _, pair := range [][2]service.DecisionEvaluationResult{{left, savedLeft}, {right, savedRight}, {savedLeft, savedRight}} {
					if !reflect.DeepEqual(normalizedServiceResult(t, pair[0]), normalizedServiceResult(t, pair[1])) {
						t.Fatal("persisted full evidence differs from returned evidence")
					}
				}
				persisted++
			}
			var count int
			if e := pg.QueryRow(ctx, "SELECT COUNT(*) FROM core.decisions WHERE tenant_id=$1 AND scenario_id=$2", tenant, parent).Scan(&count); e != nil || count != persisted {
				t.Fatal("source decision count", count, persisted, e)
			}
			if e := pg.QueryRow(ctx, "SELECT COUNT(*) FROM core.rule_executions r JOIN core.decisions d ON d.id=r.decision_id WHERE d.tenant_id=$1 AND d.scenario_id=$2", tenant, parent).Scan(&count); e != nil || count != persisted*len(rules) {
				t.Fatal("source evidence count", count, e)
			}
			if e := history.ReadSnapshot(ctx, func(r DecisionRepositories) error {
				count, e := r.Decisions.CountByScenario(ctx, tenant.String(), parent.String())
				if e == nil && count != persisted {
					return fmt.Errorf("target persisted %d decisions, want %d", count, persisted)
				}
				return e
			}); e != nil {
				t.Fatal(e)
			}
			p := history.configuration
			if e := p.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+p.schema+".decision_engine_rule_executions r JOIN "+p.schema+".decision_engine_decisions d ON d.tenant_id=r.tenant_id AND d.id=r.decision_id WHERE d.tenant_id=? AND d.scenario_id=?", mysqlstore.UUID(tenant), mysqlstore.UUID(parent)).Scan(&count); e != nil || count != persisted*len(rules) {
				t.Fatal("target evidence count", count, persisted*len(rules), e)
			}
		})
	}
}
