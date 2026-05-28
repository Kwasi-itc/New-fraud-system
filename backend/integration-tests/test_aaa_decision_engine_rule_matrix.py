from datetime import datetime, timedelta, timezone

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


def gt(left, right):
    return fn("gt", left, right)


def lt(left, right):
    return fn("lt", left, right)


def payload(name):
    return fn("Payload", constant(name))


def list_node(*children):
    return fn("List", *children)


def related_records(object_type, owner_id, **named_children):
    children = {
        "object_type": constant(object_type),
        "match_field": constant("owner_id"),
        "equals": constant(owner_id),
    }
    children.update(named_children)
    return fn("related_records", **children)


def map_field(items, name):
    return fn("map_field", items=items, field=constant(name))


def filter_eq(items, name, value):
    return fn("filter_eq", items=items, field=constant(name), value=constant(value))


def marble_filter(field_name, operator, value=None, table_name=None):
    children = {
        "tableName": constant(table_name or "transactions"),
        "fieldName": constant(field_name),
        "operator": constant(operator),
    }
    if value is not None:
        children["value"] = value
    return fn("Filter", **children)


def marble_aggregator(table_name, field_name, aggregator, *filters, percentile=None):
    children = {
        "tableName": constant(table_name),
        "fieldName": constant(field_name),
        "aggregator": constant(aggregator),
    }
    if filters:
        children["filters"] = list_node(*filters)
    if percentile is not None:
        children["percentile"] = constant(percentile)
    return fn("Aggregator", **children)


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


def test_decision_engine_aggregation_rules_with_filters_and_many_ingests(decision_engine, ingestion, tenant_model):
    """Seed a large transaction history, then verify list-style and Marble-style aggregation rules for count, distinct count, sum, avg, min, max, median, percentile, stddev, windows, and filter operators."""
    tenant_id = tenant_model["tenant_id"]
    transaction_type = tenant_model["transactions"]["name"]
    owner_id = unique_name("agg_owner")
    other_owner_id = unique_name("agg_other_owner")
    target_object_id = unique_name("agg_target")
    now = datetime.now(timezone.utc).replace(microsecond=0)
    recent_time = (now - timedelta(hours=1)).isoformat().replace("+00:00", "Z")
    old_time = (now - timedelta(hours=48)).isoformat().replace("+00:00", "Z")
    account_ids = [unique_name("agg_acct_a"), unique_name("agg_acct_b"), unique_name("agg_acct_c")]

    def transaction_row(index, amount, country, merchant, owner, event_time, note=None):
        return {
            **record_payload(object_id=unique_name(f"agg_txn_{index}"), amount=amount),
            "status": "pending",
            "account_id": account_ids[index % len(account_ids)] if owner == owner_id else unique_name("distractor_acct"),
            "merchant": merchant,
            "email": f"agg{index}@example.com",
            "country": country,
            "owner_id": owner,
            "note": note,
            "event_time": event_time,
        }

    rows = []
    for index in range(12):
        rows.append(transaction_row(index, 100, "gh", "ITC Market", owner_id, recent_time, note=None))
    for index in range(12, 20):
        rows.append(transaction_row(index, 200, "us", "Other Shop", owner_id, recent_time, note="reviewed"))
    for index in range(20, 25):
        rows.append(transaction_row(index, 300, "gh", "Legacy Market", owner_id, old_time, note="legacy"))
    for index in range(25, 30):
        rows.append(transaction_row(index, 400, "us", "Archive Shop", owner_id, old_time, note="archive"))
    for index in range(30, 40):
        rows.append(transaction_row(index, 9000, "gh", "ITC Distractor", other_owner_id, recent_time, note=None))

    batch = require_key(
        assert_status(ingestion.post(f"/v1/tenants/{tenant_id}/ingest/{transaction_type}/batch", json=rows), 200),
        "results",
    )
    assert len(batch) == len(rows)

    target = {
        **record_payload(object_id=target_object_id, amount=50),
        "account_id": account_ids[0],
        "owner_id": owner_id,
        "merchant": "Target Evaluation",
        "email": "target@example.com",
        "country": "gh",
        "event_time": now.isoformat().replace("+00:00", "Z"),
        "note": "target",
    }

    all_owner_records = related_records(transaction_type, owner_id)
    recent_owner_records = related_records(
        transaction_type,
        owner_id,
        timestamp_field=constant("event_time"),
        within_hours=constant(24),
    )
    itc_records = filter_eq(all_owner_records, "merchant", "ITC Market")
    owner_filter = marble_filter("owner_id", "=", payload("owner_id"), table_name=transaction_type)

    rules = [
        {"name": "related_records_count_all", "description": "Counts all seeded target-owner records.", "formula": eq(fn("list_count", all_owner_records), constant(30))},
        {"name": "related_records_count_recent_window", "description": "Counts only target-owner records inside the last 24 hours.", "formula": eq(fn("list_count", recent_owner_records), constant(20))},
        {"name": "related_records_sum_amount", "description": "Sums all target-owner amounts.", "formula": eq(fn("sum", map_field(all_owner_records, "amount")), constant(6300))},
        {"name": "related_records_avg_amount", "description": "Averages all target-owner amounts.", "formula": eq(fn("avg", map_field(all_owner_records, "amount")), constant(210))},
        {"name": "related_records_min_amount", "description": "Finds the minimum target-owner amount.", "formula": eq(fn("min", map_field(all_owner_records, "amount")), constant(100))},
        {"name": "related_records_max_amount", "description": "Finds the maximum target-owner amount.", "formula": eq(fn("max", map_field(all_owner_records, "amount")), constant(400))},
        {"name": "filtered_list_count_itc_market", "description": "Filters related records by merchant before counting.", "formula": eq(fn("list_count", itc_records), constant(12))},
        {"name": "filtered_list_sum_itc_market", "description": "Filters related records by merchant before summing.", "formula": eq(fn("sum", map_field(itc_records, "amount")), constant(1200))},
        {"name": "filtered_list_avg_itc_market", "description": "Filters related records by merchant before averaging.", "formula": eq(fn("avg", map_field(itc_records, "amount")), constant(100))},
        {"name": "marble_count_owner", "description": "Marble Aggregator COUNT with owner equality filter.", "formula": eq(marble_aggregator(transaction_type, "object_id", "COUNT", owner_filter), constant(30))},
        {"name": "marble_count_distinct_accounts", "description": "Marble Aggregator COUNT_DISTINCT with owner equality filter.", "formula": eq(marble_aggregator(transaction_type, "account_id", "COUNT_DISTINCT", owner_filter), constant(3))},
        {"name": "marble_sum_owner", "description": "Marble Aggregator SUM with owner equality filter.", "formula": eq(marble_aggregator(transaction_type, "amount", "SUM", owner_filter), constant(6300))},
        {"name": "marble_avg_owner", "description": "Marble Aggregator AVG with owner equality filter.", "formula": eq(marble_aggregator(transaction_type, "amount", "AVG", owner_filter), constant(210))},
        {"name": "marble_min_owner", "description": "Marble Aggregator MIN with owner equality filter.", "formula": eq(marble_aggregator(transaction_type, "amount", "MIN", owner_filter), constant(100))},
        {"name": "marble_max_owner", "description": "Marble Aggregator MAX with owner equality filter.", "formula": eq(marble_aggregator(transaction_type, "amount", "MAX", owner_filter), constant(400))},
        {"name": "marble_median_owner", "description": "Marble Aggregator MEDIAN with owner equality filter.", "formula": eq(marble_aggregator(transaction_type, "amount", "MEDIAN", owner_filter), constant(200))},
        {"name": "marble_percentile_owner", "description": "Marble Aggregator PCTILE with owner equality filter.", "formula": eq(marble_aggregator(transaction_type, "amount", "PCTILE", owner_filter, percentile=90), constant(400))},
        {
            "name": "marble_stddev_owner_lower_bound",
            "description": "Marble Aggregator STDDEV lower bound with owner equality filter.",
            "formula": gt(marble_aggregator(transaction_type, "amount", "STDDEV", owner_filter), constant(110)),
        },
        {
            "name": "marble_stddev_owner_upper_bound",
            "description": "Marble Aggregator STDDEV upper bound with owner equality filter.",
            "formula": lt(marble_aggregator(transaction_type, "amount", "STDDEV", owner_filter), constant(111)),
        },
        {
            "name": "marble_filter_not_equal_country",
            "description": "Marble Aggregator COUNT with != filter.",
            "formula": eq(
                marble_aggregator(transaction_type, "object_id", "COUNT", owner_filter, marble_filter("country", "!=", constant("gh"), table_name=transaction_type)),
                constant(13),
            ),
        },
        {
            "name": "marble_filter_amount_gt",
            "description": "Marble Aggregator COUNT with > filter.",
            "formula": eq(
                marble_aggregator(transaction_type, "object_id", "COUNT", owner_filter, marble_filter("amount", ">", constant(250), table_name=transaction_type)),
                constant(10),
            ),
        },
        {
            "name": "marble_filter_amount_gte",
            "description": "Marble Aggregator COUNT with >= filter.",
            "formula": eq(
                marble_aggregator(transaction_type, "object_id", "COUNT", owner_filter, marble_filter("amount", ">=", constant(300), table_name=transaction_type)),
                constant(10),
            ),
        },
        {
            "name": "marble_filter_amount_lte",
            "description": "Marble Aggregator COUNT with <= filter.",
            "formula": eq(
                marble_aggregator(transaction_type, "object_id", "COUNT", owner_filter, marble_filter("amount", "<=", constant(200), table_name=transaction_type)),
                constant(20),
            ),
        },
        {
            "name": "marble_filter_country_in_list",
            "description": "Marble Aggregator SUM with IsInList filter.",
            "formula": eq(
                marble_aggregator(transaction_type, "amount", "SUM", owner_filter, marble_filter("country", "IsInList", list_node(constant("gh")), table_name=transaction_type)),
                constant(2700),
            ),
        },
        {
            "name": "marble_filter_country_not_in_list",
            "description": "Marble Aggregator SUM with IsNotInList filter.",
            "formula": eq(
                marble_aggregator(transaction_type, "amount", "SUM", owner_filter, marble_filter("country", "IsNotInList", list_node(constant("gh")), table_name=transaction_type)),
                constant(3600),
            ),
        },
        {
            "name": "marble_filter_note_empty",
            "description": "Marble Aggregator COUNT with IsEmpty filter.",
            "formula": eq(
                marble_aggregator(transaction_type, "object_id", "COUNT", owner_filter, marble_filter("note", "IsEmpty", table_name=transaction_type)),
                constant(12),
            ),
        },
        {
            "name": "marble_filter_note_not_empty",
            "description": "Marble Aggregator COUNT with IsNotEmpty filter.",
            "formula": eq(
                marble_aggregator(transaction_type, "object_id", "COUNT", owner_filter, marble_filter("note", "IsNotEmpty", table_name=transaction_type)),
                constant(18),
            ),
        },
        {
            "name": "marble_filter_merchant_starts_with",
            "description": "Marble Aggregator SUM with StringStartsWith filter.",
            "formula": eq(
                marble_aggregator(transaction_type, "amount", "SUM", owner_filter, marble_filter("merchant", "StringStartsWith", constant("ITC"), table_name=transaction_type)),
                constant(1200),
            ),
        },
        {
            "name": "marble_filter_email_ends_with",
            "description": "Marble Aggregator COUNT with StringEndsWith filter.",
            "formula": eq(
                marble_aggregator(transaction_type, "object_id", "COUNT", owner_filter, marble_filter("email", "StringEndsWith", constant("@example.com"), table_name=transaction_type)),
                constant(30),
            ),
        },
        {
            "name": "marble_filter_recent_time_window_count",
            "description": "Marble Aggregator COUNT with dynamic 24-hour timestamp filter.",
            "formula": eq(
                marble_aggregator(
                    transaction_type,
                    "object_id",
                    "COUNT",
                    owner_filter,
                    marble_filter(
                        "event_time",
                        ">=",
                        fn("TimeAdd", timestampField=payload("event_time"), duration=constant("PT24H"), sign=constant("-")),
                        table_name=transaction_type,
                    ),
                ),
                constant(20),
            ),
        },
        {
            "name": "marble_filter_recent_time_window_sum",
            "description": "Marble Aggregator SUM with dynamic 24-hour timestamp filter.",
            "formula": eq(
                marble_aggregator(
                    transaction_type,
                    "amount",
                    "SUM",
                    owner_filter,
                    marble_filter(
                        "event_time",
                        ">=",
                        fn("TimeAdd", timestampField=payload("event_time"), duration=constant("PT24H"), sign=constant("-")),
                        table_name=transaction_type,
                    ),
                ),
                constant(2800),
            ),
        },
        {
            "name": "marble_filter_recent_time_window_avg",
            "description": "Marble Aggregator AVG with dynamic 24-hour timestamp filter.",
            "formula": eq(
                marble_aggregator(
                    transaction_type,
                    "amount",
                    "AVG",
                    owner_filter,
                    marble_filter(
                        "event_time",
                        ">=",
                        fn("TimeAdd", timestampField=payload("event_time"), duration=constant("PT24H"), sign=constant("-")),
                        table_name=transaction_type,
                    ),
                ),
                constant(140),
            ),
        },
    ]

    bundle = create_published_scenario(
        decision_engine,
        tenant_id,
        transaction_type,
        "aggregation matrix",
        rules,
        trigger_formula=eq(field("owner_id"), constant(owner_id)),
    )
    result = require_key(
        assert_status(
            decision_engine.post(
                f"/v1/tenants/{tenant_id}/scenarios/{bundle['scenario']['id']}/evaluate",
                json={"object_id": target_object_id, "object_type": transaction_type, "fields": target},
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
