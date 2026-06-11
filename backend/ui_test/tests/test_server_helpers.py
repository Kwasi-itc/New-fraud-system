from __future__ import annotations

import copy
import importlib.util
import sys
from pathlib import Path


SERVER_PATH = Path(__file__).resolve().parents[1] / "server.py"
SPEC = importlib.util.spec_from_file_location("ui_test_server", SERVER_PATH)
server = importlib.util.module_from_spec(SPEC)
assert SPEC and SPEC.loader
sys.modules[SPEC.name] = server
SPEC.loader.exec_module(server)


def test_generated_record_tokens_replace_longest_first():
    row = server.resolve_generated_tokens(
        {
            "object_id": "item_$index",
            "account_id": "acct_$index_mod_10_$index_mod_2",
        },
        12,
    )

    assert row["object_id"] == "item_12"
    assert row["account_id"] == "acct_2_0"


def test_apply_edits_changes_only_supported_business_fields():
    case = {
        "input": {"object_id": "old", "fields": {"object_id": "old", "amount": 100, "merchant": "Shop"}},
        "thresholds": {"review": 1, "block_and_review": 50, "decline": 100},
        "generated_records": [{"count": 3}],
        "rules": [{"name": "amount_rule", "score_modifier": 1}],
        "expected": {"triggered": True, "score": 1, "rules": {"amount_rule": "hit"}},
    }

    server.apply_edits(
        case,
        {
            "input_fields": {"object_id": "new", "amount": 200, "unknown": "ignored"},
            "thresholds": {"review": 2, "other": 9},
            "generated_counts": [4],
            "rule_scores": {"amount_rule": 5},
            "expected": {"score": 5},
            "expected_rules": {"amount_rule": "no_hit", "bad": "invalid"},
            "raw": {"not": "allowed"},
        },
    )

    assert case["input"]["object_id"] == "new"
    assert case["input"]["fields"]["object_id"] == "new"
    assert case["input"]["fields"]["amount"] == 200
    assert "unknown" not in case["input"]["fields"]
    assert case["thresholds"] == {"review": 2, "block_and_review": 50, "decline": 100}
    assert case["generated_records"][0]["count"] == 4
    assert case["rules"][0]["score_modifier"] == 5
    assert case["expected"]["score"] == 5
    assert case["expected"]["rules"]["amount_rule"] == "no_hit"
    assert "bad" not in case["expected"]["rules"]


def test_apply_edits_coerces_input_fields_to_original_fixture_types():
    case = {
        "input": {"object_id": "old", "fields": {"object_id": "old", "amount": 100, "note": None}},
    }

    server.apply_edits(case, {"input_fields": {"object_id": "new", "amount": "250", "note": ""}})

    assert case["input"]["object_id"] == "new"
    assert case["input"]["fields"]["amount"] == 250
    assert isinstance(case["input"]["fields"]["amount"], int)
    assert case["input"]["fields"]["note"] is None


def test_compare_result_reports_rule_diffs():
    result = {
        "triggered": True,
        "decision": {"score": 2, "outcome": "review"},
        "rule_executions": [{"rule_name": "one", "outcome": "hit"}],
    }
    expected = {"triggered": True, "score": 2, "outcome": "review", "rules": {"one": "no_hit"}}
    rules = [{"name": "one", "formula": {"function": "constant", "constant": True}}]

    comparison = server.compare_result(result, expected, rules)

    assert comparison["matches"] is False
    assert comparison["rules"][0]["name"] == "one"
    assert comparison["rules"][0]["actual"] == "hit"
    assert comparison["rules"][0]["expected"] == "no_hit"


def test_materialize_case_resolves_fixture_placeholders():
    raw = {
        "name": "case",
        "object_type": "$transactions",
        "variables": {"object_id": "txn"},
        "input": {"object_id": "$object_id", "fields": {"object_id": "$object_id", "event_time": "$now"}},
    }
    tenant_model = {
        "tenant_id": "tenant-1",
        "transactions": {"name": "transactions_demo"},
        "accounts": {"name": "accounts_demo"},
    }

    materialized = server.materialize_case(copy.deepcopy(raw), tenant_model, {})

    assert materialized["tenant_id"] == "tenant-1"
    assert materialized["object_type"] == "transactions_demo"
    assert materialized["input"]["object_id"].startswith("txn_")
    assert materialized["input"]["fields"]["event_time"].endswith("Z")


def test_rule_edit_model_exposes_constants_and_compatible_operators():
    rules = [
        {
            "name": "amount_over_limit",
            "formula": {
                "function": "gt",
                "children": [
                    {"function": "field_ref", "named_children": {"field": {"constant": "amount"}}},
                    {"constant": 1000},
                ],
            },
        }
    ]

    model = server.rule_edit_model(rules)
    nodes = {node["path"]: node for node in model[0]["nodes"]}

    assert "gte" in nodes["rules[0].formula"]["editable_operator"]
    assert nodes["rules[0].formula.children[1]"]["editable_constant"] is True


def test_rule_diagram_model_flattens_boolean_rules_into_readable_conditions():
    rules = [
        {
            "name": "core_boolean_and",
            "formula": {
                "function": "and",
                "children": [
                    {
                        "function": "gt",
                        "children": [
                            {"function": "field_ref", "named_children": {"field": {"constant": "amount"}}},
                            {"constant": 1000},
                        ],
                    },
                    {
                        "function": "neq",
                        "children": [
                            {"function": "field_ref", "named_children": {"field": {"constant": "country"}}},
                            {"constant": "us"},
                        ],
                    },
                ],
            },
        }
    ]

    model = server.rule_diagram_model(rules)
    conditions = model[0]["conditions"]

    assert [condition["left"] for condition in conditions] == ["amount", "country"]
    assert conditions[1]["joiner"] == "AND"
    assert conditions[0]["operator_edit"]["path"] == "rules[0].formula.children[0]"
    assert conditions[0]["constant_edits"][0]["path"] == "rules[0].formula.children[0].children[1]"


def test_list_fixtures_hides_complex_demo_cases_and_uses_hardcoded_order():
    fixtures = server.list_fixtures()
    ids = [fixture["id"] for fixture in fixtures]

    assert ids == [
        "core_ast/simple_amount_threshold.json",
        "core_ast/boolean_logical_operations.json",
        "aggregation/velocity_and_account_takeover.json",
        "marble_compat/payload_time_fuzzy.json",
        "core_ast/basic_rule_matrix.json",
        "core_ast/list_null_numeric_edge_cases.json",
    ]
    assert fixtures[0]["name"] == "simple amount threshold"
    assert [fixture["demo_order"] for fixture in fixtures] == list(range(len(fixtures)))


def test_apply_rule_edits_updates_constants_and_operators():
    case = {
        "rules": [
            {
                "name": "amount_over_limit",
                "formula": {
                    "function": "gt",
                    "children": [
                        {"function": "field_ref", "named_children": {"field": {"constant": "amount"}}},
                        {"constant": 1000},
                    ],
                },
            }
        ]
    }

    server.apply_rule_edits(
        case,
        {
            "operators": {"rules[0].formula": "gte"},
            "constants": {"rules[0].formula.children[1]": "1500"},
        },
    )

    assert case["rules"][0]["formula"]["function"] == "gte"
    assert case["rules"][0]["formula"]["children"][1]["constant"] == 1500


def test_validate_rule_edits_rejects_incompatible_operator_swap():
    case = {
        "rules": [
            {
                "name": "amount_over_limit",
                "formula": {
                    "function": "gt",
                    "children": [
                        {"function": "field_ref", "named_children": {"field": {"constant": "amount"}}},
                        {"constant": 1000},
                    ],
                },
            }
        ]
    }

    try:
        server.validate_rule_edits(case, {"operators": {"rules[0].formula": "and"}})
    except server.DemoError as exc:
        assert "Cannot change operator" in str(exc)
    else:
        raise AssertionError("expected DemoError")


def test_editable_view_includes_stage_config_defaults():
    raw = {
        "input": {"fields": {}},
        "thresholds": {},
        "rules": [],
        "expected": {},
    }

    editable = server.editable_view(raw)

    assert editable["stage_config"]["tenant"]["name_prefix"] == "demo tenant"
    assert editable["stage_config"]["tables"]["transactions_alias"] == "Transactions"


def test_sanitize_edits_preserves_stage_config_and_drops_unknowns():
    edits = {
        "stage_config": {"tenant": {"name_prefix": "Acme"}},
        "unexpected": {"raw": True},
    }

    sanitized = server.sanitize_edits(edits)

    assert sanitized == {"stage_config": {"tenant": {"name_prefix": "Acme"}}}
