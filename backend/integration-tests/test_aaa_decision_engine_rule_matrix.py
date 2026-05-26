import pytest

from conftest import assert_status, record_payload, require_key, true_node, unique_name


pytestmark = pytest.mark.integration


SUPPORTED_AST_FUNCTIONS = {
    "field_ref",
    "and",
    "or",
    "not",
    "eq",
    "neq",
    "gt",
    "gte",
    "lt",
    "lte",
    "contains",
    "in",
    "starts_with",
    "ends_with",
    "lower",
    "upper",
    "is_null",
    "coalesce",
    "add",
    "subtract",
    "multiply",
    "divide",
    "in_custom_list",
    "record_has_tag",
    "record_risk_level",
    "has_ip_flag",
    "past_decision_count",
    "related_count",
    "related_field",
}


def constant(value):
    return {"constant": value}


def field(name):
    return {"function": "field_ref", "named_children": {"field": constant(name)}}


def fn(name, *children, **named_children):
    node = {"function": name}
    if children:
        node["children"] = list(children)
    if named_children:
        node["named_children"] = named_children
    return node


def eq(left, right):
    return fn("eq", left, right)


def gte(left, right):
    return fn("gte", left, right)


def account_payload(object_id, account_key, owner_id, account_status="active"):
    return {
        "object_id": object_id,
        "account_key": account_key,
        "owner_id": owner_id,
        "account_status": account_status,
    }


def create_published_scenario(decision_engine, tenant_id, object_type, name, rules, trigger_formula=None):
    scenario = require_key(
        assert_status(
            decision_engine.post(
                f"/v1/tenants/{tenant_id}/scenarios",
                json={"name": unique_name(name), "trigger_object_type": object_type},
            ),
            201,
        ),
        "scenario",
    )
    iteration = require_key(
        assert_status(decision_engine.post(f"/v1/tenants/{tenant_id}/scenarios/{scenario['id']}/iterations"), 201),
        "iteration",
    )
    assert_status(
        decision_engine.put(
            f"/v1/tenants/{tenant_id}/scenarios/{scenario['id']}/iterations/{iteration['id']}",
            json={
                "trigger_formula": trigger_formula or true_node(),
                "score_review_threshold": 1,
                "score_block_and_review_threshold": 50,
                "score_decline_threshold": 100,
                "schedule": "",
            },
        ),
        200,
    )

    created_rules = []
    for index, rule in enumerate(rules, start=1):
        created_rules.append(
            require_key(
                assert_status(
                    decision_engine.post(
                        f"/v1/tenants/{tenant_id}/scenarios/{scenario['id']}/iterations/{iteration['id']}/rules",
                        json={
                            "display_order": index,
                            "name": rule["name"],
                            "description": rule["description"],
                            "formula": rule["formula"],
                            "score_modifier": rule.get("score_modifier", 1),
                            "rule_group": rule.get("rule_group", "function-matrix"),
                            "stable_rule_id": unique_name(rule["name"]),
                        },
                    ),
                    201,
                ),
                "rule",
            )
        )

    validation = assert_status(
        decision_engine.post(f"/v1/tenants/{tenant_id}/scenarios/{scenario['id']}/iterations/{iteration['id']}/validate"),
        200,
    )
    assert not validation.get("result", {}).get("errors"), validation
    assert_status(
        decision_engine.post(f"/v1/tenants/{tenant_id}/scenarios/{scenario['id']}/iterations/{iteration['id']}/commit"),
        200,
    )
    assert_status(
        decision_engine.post(
            f"/v1/tenants/{tenant_id}/scenarios/{scenario['id']}/publications",
            json={"action": "publish", "iteration_id": iteration["id"]},
        ),
        200,
    )
    return {"scenario": scenario, "iteration": iteration, "rules": created_rules}


def test_decision_engine_rule_creation_evaluation_hit_no_hit_and_score(decision_engine, tenant_model):
    """Create a live scenario with both matching and non-matching rules, then verify rule execution outcomes and score calculation."""
    tenant_id = tenant_model["tenant_id"]
    transaction_type = tenant_model["transactions"]["name"]
    payload = record_payload(amount=1500)

    bundle = create_published_scenario(
        decision_engine,
        tenant_id,
        transaction_type,
        "hit no-hit scoring",
        [
            {
                "name": "amount_over_review_limit",
                "description": "Hits when amount is above the configured limit.",
                "formula": fn("gt", field("amount"), constant(1000)),
                "score_modifier": 7,
            },
            {
                "name": "amount_below_small_limit",
                "description": "Does not hit for high-value transactions.",
                "formula": fn("lt", field("amount"), constant(100)),
                "score_modifier": 11,
            },
        ],
    )
    result = require_key(
        assert_status(
            decision_engine.post(
                f"/v1/tenants/{tenant_id}/scenarios/{bundle['scenario']['id']}/evaluate",
                json={"object_id": payload["object_id"], "object_type": transaction_type, "fields": payload},
            ),
            200,
        ),
        "result",
    )

    executions = {execution["rule_name"]: execution for execution in result["rule_executions"]}
    assert result["triggered"] is True
    assert result["decision"]["score"] == 7
    assert result["decision"]["outcome"] == "review"
    assert executions["amount_over_review_limit"]["outcome"] == "hit"
    assert executions["amount_below_small_limit"]["outcome"] == "no_hit"


def test_decision_engine_rule_function_matrix_and_related_count_ingests(decision_engine, ingestion, tenant_model):
    """Create and evaluate rules covering every supported decision-engine AST function, including platform lookups, past decisions, related-field traversal, and related-count aggregation seeded by multiple account ingests."""
    tenant_id = tenant_model["tenant_id"]
    transaction_type = tenant_model["transactions"]["name"]
    account_type = tenant_model["accounts"]["name"]
    object_id = unique_name("matrix_txn")
    account_key = unique_name("acct")
    owner_id = unique_name("owner")
    blocked_list = unique_name("blocked_emails")

    transaction = {
        **record_payload(object_id=object_id, amount=1500),
        "account_id": account_key,
        "owner_id": owner_id,
        "note": None,
    }

    seed_scenario = create_published_scenario(
        decision_engine,
        tenant_id,
        transaction_type,
        "prior decision seed",
        [
            {
                "name": "seed_amount_gt_zero",
                "description": "Creates one prior review decision for past_decision_count.",
                "formula": fn("gt", field("amount"), constant(0)),
            }
        ],
        trigger_formula=eq(field("owner_id"), constant(owner_id)),
    )
    assert_status(
        decision_engine.post(
            f"/v1/tenants/{tenant_id}/scenarios/{seed_scenario['scenario']['id']}/evaluate",
            json={"object_id": object_id, "object_type": transaction_type, "fields": transaction},
        ),
        200,
    )

    for index in range(3):
        assert_status(
            ingestion.post(
                f"/v1/tenants/{tenant_id}/ingest/{account_type}",
                json=account_payload(
                    object_id=f"{account_key}_owned_{index}",
                    account_key=account_key if index == 0 else f"{account_key}_{index}",
                    owner_id=owner_id,
                ),
            ),
            200,
        )
    assert_status(
        ingestion.post(
            f"/v1/tenants/{tenant_id}/ingest/{account_type}",
            json=account_payload(f"{account_key}_other_owner", f"{account_key}_other", unique_name("other_owner")),
        ),
        200,
    )
    assert_status(ingestion.post(f"/v1/tenants/{tenant_id}/ingest/{transaction_type}", json=transaction), 200)

    assert_status(
        decision_engine.post(
            f"/v1/tenants/{tenant_id}/platform/custom-list-entries",
            json={"list_name": blocked_list, "value": "risk@example.com"},
        ),
        201,
    )
    assert_status(
        decision_engine.post(
            f"/v1/tenants/{tenant_id}/platform/record-tags",
            json={"object_type": transaction_type, "object_id": object_id, "tag": "high_value"},
        ),
        201,
    )
    assert_status(
        decision_engine.post(
            f"/v1/tenants/{tenant_id}/platform/risk-snapshots",
            json={"object_type": transaction_type, "object_id": object_id, "risk_level": "high"},
        ),
        201,
    )
    assert_status(
        decision_engine.post(
            f"/v1/tenants/{tenant_id}/platform/ip-flags",
            json={"ip_address": "1.2.3.4", "flag": "tor"},
        ),
        201,
    )

    rules = [
        {"name": "field_ref_eq", "description": "field_ref plus eq", "formula": eq(field("status"), constant("pending"))},
        {"name": "and_true", "description": "and with two true children", "formula": fn("and", eq(field("status"), constant("pending")), fn("gt", field("amount"), constant(1000)))},
        {"name": "or_true", "description": "or short-circuit truth", "formula": fn("or", eq(field("status"), constant("declined")), fn("gt", field("amount"), constant(1000)))},
        {"name": "not_true", "description": "not over a false child", "formula": fn("not", eq(field("status"), constant("declined")))},
        {"name": "neq_true", "description": "neq comparison", "formula": fn("neq", field("status"), constant("declined"))},
        {"name": "gt_true", "description": "gt numeric comparison", "formula": fn("gt", field("amount"), constant(1499))},
        {"name": "gte_true", "description": "gte numeric comparison", "formula": fn("gte", field("amount"), constant(1500))},
        {"name": "lt_true", "description": "lt numeric comparison", "formula": fn("lt", field("amount"), constant(1501))},
        {"name": "lte_true", "description": "lte numeric comparison", "formula": fn("lte", field("amount"), constant(1500))},
        {"name": "contains_string", "description": "contains on a string", "formula": fn("contains", field("merchant"), constant("Market"))},
        {"name": "contains_list", "description": "contains on a constant list", "formula": fn("contains", constant(["pending", "approved"]), field("status"))},
        {"name": "in_list", "description": "in against a constant list", "formula": fn("in", field("country"), constant(["gh", "us"]))},
        {"name": "starts_with_true", "description": "starts_with string helper", "formula": fn("starts_with", field("merchant"), constant("ITC"))},
        {"name": "ends_with_true", "description": "ends_with string helper", "formula": fn("ends_with", field("email"), constant(".com"))},
        {"name": "lower_true", "description": "lower string transform", "formula": eq(fn("lower", field("email")), constant("risk@example.com"))},
        {"name": "upper_true", "description": "upper string transform", "formula": eq(fn("upper", field("country")), constant("GH"))},
        {"name": "is_null_true", "description": "is_null over a nullable field", "formula": fn("is_null", field("note"))},
        {"name": "coalesce_true", "description": "coalesce nullable field to fallback", "formula": eq(fn("coalesce", field("note"), constant("fallback")), constant("fallback"))},
        {"name": "add_true", "description": "add arithmetic", "formula": eq(fn("add", field("amount"), constant(5)), constant(1505))},
        {"name": "subtract_true", "description": "subtract arithmetic", "formula": eq(fn("subtract", field("amount"), constant(500)), constant(1000))},
        {"name": "multiply_true", "description": "multiply arithmetic", "formula": eq(fn("multiply", field("amount"), constant(2)), constant(3000))},
        {"name": "divide_true", "description": "divide arithmetic", "formula": eq(fn("divide", field("amount"), constant(2)), constant(750))},
        {"name": "in_custom_list_true", "description": "in_custom_list platform lookup", "formula": fn("in_custom_list", list=constant(blocked_list), value=fn("lower", field("email")))},
        {"name": "record_has_tag_true", "description": "record_has_tag platform lookup", "formula": fn("record_has_tag", tag=constant("high_value"))},
        {"name": "record_risk_level_true", "description": "record_risk_level platform lookup", "formula": eq(fn("record_risk_level"), constant("high"))},
        {"name": "has_ip_flag_true", "description": "has_ip_flag platform lookup", "formula": fn("has_ip_flag", ip=constant("1.2.3.4"), flag=constant("tor"))},
        {"name": "past_decision_count_all", "description": "past_decision_count without outcome filter", "formula": gte(fn("past_decision_count"), constant(1))},
        {"name": "past_decision_count_review", "description": "past_decision_count with review outcome filter", "formula": gte(fn("past_decision_count", outcome=constant("review")), constant(1))},
        {"name": "related_count_dynamic_equals", "description": "related_count over multiple ingested accounts using a dynamic equals field", "formula": gte(fn("related_count", object_type=constant(account_type), field=constant("owner_id"), equals=field("owner_id")), constant(3))},
        {"name": "related_count_non_nil", "description": "related_count without equals counts populated account owner_id values", "formula": gte(fn("related_count", object_type=constant(account_type), field=constant("owner_id")), constant(4))},
        {"name": "related_field_account_status", "description": "related_field traverses the account link and reads account_status", "formula": eq(fn("related_field", path=constant("account"), field=constant("account_status")), constant("active"))},
    ]
    covered_functions = {
        "field_ref",
        "and",
        "or",
        "not",
        "eq",
        "neq",
        "gt",
        "gte",
        "lt",
        "lte",
        "contains",
        "in",
        "starts_with",
        "ends_with",
        "lower",
        "upper",
        "is_null",
        "coalesce",
        "add",
        "subtract",
        "multiply",
        "divide",
        "in_custom_list",
        "record_has_tag",
        "record_risk_level",
        "has_ip_flag",
        "past_decision_count",
        "related_count",
        "related_field",
    }
    assert covered_functions == SUPPORTED_AST_FUNCTIONS

    matrix = create_published_scenario(
        decision_engine,
        tenant_id,
        transaction_type,
        "rule function matrix",
        rules,
        trigger_formula=eq(field("owner_id"), constant(owner_id)),
    )
    result = require_key(
        assert_status(
            decision_engine.post(
                f"/v1/tenants/{tenant_id}/scenarios/{matrix['scenario']['id']}/evaluate",
                json={"object_id": object_id, "object_type": transaction_type, "fields": transaction},
            ),
            200,
        ),
        "result",
    )

    executions_by_name = {execution["rule_name"]: execution for execution in result["rule_executions"]}
    assert result["triggered"] is True
    assert result["decision"]["score"] == len(rules)
    assert result["decision"]["outcome"] == "review"
    assert set(executions_by_name) == {rule["name"] for rule in rules}
    assert all(execution["outcome"] == "hit" for execution in executions_by_name.values())
