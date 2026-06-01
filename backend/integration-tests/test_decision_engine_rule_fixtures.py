from __future__ import annotations

import copy
import json
from pathlib import Path
from typing import Any
from datetime import datetime, timedelta, timezone

import pytest

from conftest import assert_status, create_tenant_model, require_key, unique_name


pytestmark = pytest.mark.integration


FIXTURE_ROOT = Path(__file__).resolve().parent / "decision_engine_rule_fixtures"


@pytest.fixture(scope="module")
def rule_tenant_model(data_model) -> dict[str, Any]:
    return create_tenant_model(data_model)


def load_rule_fixtures() -> list[tuple[str, dict[str, Any]]]:
    cases: list[tuple[str, dict[str, Any]]] = []
    for path in sorted(FIXTURE_ROOT.rglob("*.json")):
        with path.open(encoding="utf-8") as handle:
            cases.append((path.relative_to(FIXTURE_ROOT).as_posix(), json.load(handle)))
    return cases


def fixture_id(case: tuple[str, dict[str, Any]]) -> str:
    path, payload = case
    return payload.get("name") or path


@pytest.mark.parametrize("fixture_case", load_rule_fixtures(), ids=fixture_id)
def test_decision_engine_rule_fixture_contract(decision_engine, ingestion, rule_tenant_model, fixture_case):
    """Run JSON rule fixtures through the real decision-engine HTTP API and assert evaluator output."""
    _, raw_case = fixture_case
    case = materialize_case(raw_case, rule_tenant_model)

    for seed in case.get("seed_decisions", []):
        seed_bundle = create_published_scenario(decision_engine, case["tenant_id"], seed)
        evaluate_case(decision_engine, case["tenant_id"], seed_bundle, seed["input"])

    for record in case.get("records", []):
        assert_status(
            ingestion.post(
                f"/v1/tenants/{case['tenant_id']}/ingest/{record['object_type']}",
                json=record["body"],
            ),
            200,
        )
    for batch in case.get("generated_records", []):
        rows = build_generated_records(batch)
        for chunk in chunks(rows, 500):
            results = require_key(
                assert_status(
                    ingestion.post(
                        f"/v1/tenants/{case['tenant_id']}/ingest/{batch['object_type']}/batch",
                        json=chunk,
                    ),
                    200,
                ),
                "results",
            )
            assert len(results) == len(chunk)

    bundle = create_draft_with_rules(decision_engine, case["tenant_id"], case)
    validation = assert_status(
        decision_engine.post(
            f"/v1/tenants/{case['tenant_id']}/scenarios/{bundle['scenario']['id']}/iterations/{bundle['iteration']['id']}/validate"
        ),
        {200, 422},
    )
    validation_result = validation_payload(validation)

    expected = case["expected"]
    if expected.get("validation_valid") is False:
        assert validation_result["valid"] is False
        assert_expected_error(validation_result, expected.get("validation_error_contains"))
        return

    assert validation_result["valid"] is True, validation_result
    commit_and_publish(decision_engine, case["tenant_id"], bundle)

    result = evaluate_case(decision_engine, case["tenant_id"], bundle, case["input"])
    assert_successful_result(decision_engine, case["tenant_id"], bundle["scenario"]["id"], result, expected)


def materialize_case(raw_case: dict[str, Any], tenant_model: dict[str, Any]) -> dict[str, Any]:
    case = copy.deepcopy(raw_case)
    now = datetime.now(timezone.utc).replace(microsecond=0)
    context = {
        "transactions": tenant_model["transactions"]["name"],
        "accounts": tenant_model["accounts"]["name"],
        "now": timestamp_text(now),
        "now_minus_30s": timestamp_text(now - timedelta(seconds=30)),
        "now_minus_2m": timestamp_text(now - timedelta(minutes=2)),
        "now_minus_4m": timestamp_text(now - timedelta(minutes=4)),
        "now_minus_10m": timestamp_text(now - timedelta(minutes=10)),
    }
    for key, prefix in case.get("variables", {}).items():
        context[key] = unique_name(prefix)

    case = resolve_placeholders(case, context)
    case["tenant_id"] = tenant_model["tenant_id"]
    if "trigger_override" in case:
        case["trigger"] = case.pop("trigger_override")
    return case


def timestamp_text(value: datetime) -> str:
    return value.isoformat().replace("+00:00", "Z")


def resolve_placeholders(value: Any, context: dict[str, str]) -> Any:
    if isinstance(value, str):
        out = value
        for key in sorted(context, key=len, reverse=True):
            out = out.replace(f"${key}", context[key])
        return out
    if isinstance(value, list):
        return [resolve_placeholders(item, context) for item in value]
    if isinstance(value, dict):
        return {key: resolve_placeholders(item, context) for key, item in value.items()}
    return value


def build_generated_records(spec: dict[str, Any]) -> list[dict[str, Any]]:
    count = spec["count"]
    template = spec["template"]
    rows = []
    for index in range(count):
        row = resolve_generated_tokens(copy.deepcopy(template), index)
        rows.append(row)
    return rows


def resolve_generated_tokens(value: Any, index: int) -> Any:
    if isinstance(value, str):
        return (
            value.replace("$index", str(index))
            .replace("$index_mod_2", str(index % 2))
            .replace("$index_mod_3", str(index % 3))
            .replace("$index_mod_4", str(index % 4))
            .replace("$index_mod_5", str(index % 5))
            .replace("$index_mod_10", str(index % 10))
        )
    if isinstance(value, list):
        return [resolve_generated_tokens(item, index) for item in value]
    if isinstance(value, dict):
        return {key: resolve_generated_tokens(item, index) for key, item in value.items()}
    return value


def chunks(items: list[dict[str, Any]], size: int) -> list[list[dict[str, Any]]]:
    return [items[index : index + size] for index in range(0, len(items), size)]


def create_published_scenario(decision_engine, tenant_id: str, spec: dict[str, Any]) -> dict[str, Any]:
    bundle = create_draft_with_rules(decision_engine, tenant_id, spec)
    validation = assert_status(
        decision_engine.post(
            f"/v1/tenants/{tenant_id}/scenarios/{bundle['scenario']['id']}/iterations/{bundle['iteration']['id']}/validate"
        ),
        {200, 422},
    )
    assert validation_payload(validation)["valid"] is True, validation
    commit_and_publish(decision_engine, tenant_id, bundle)
    return bundle


def create_draft_with_rules(decision_engine, tenant_id: str, spec: dict[str, Any]) -> dict[str, Any]:
    scenario = require_key(
        assert_status(
            decision_engine.post(
                f"/v1/tenants/{tenant_id}/scenarios",
                json={
                    "name": unique_name(spec["name"]),
                    "trigger_object_type": spec["object_type"],
                },
            ),
            201,
        ),
        "scenario",
    )
    iteration = require_key(
        assert_status(decision_engine.post(f"/v1/tenants/{tenant_id}/scenarios/{scenario['id']}/iterations"), 201),
        "iteration",
    )
    thresholds = spec.get("thresholds", {})
    iteration = require_key(
        assert_status(
            decision_engine.put(
                f"/v1/tenants/{tenant_id}/scenarios/{scenario['id']}/iterations/{iteration['id']}",
                json={
                    "trigger_formula": spec["trigger"],
                    "score_review_threshold": thresholds.get("review", 1),
                    "score_block_and_review_threshold": thresholds.get("block_and_review", 50),
                    "score_decline_threshold": thresholds.get("decline", 100),
                    "schedule": spec.get("schedule", ""),
                },
            ),
            200,
        ),
        "iteration",
    )

    rules = []
    for index, rule in enumerate(spec.get("rules", []), start=1):
        created = require_key(
            assert_status(
                decision_engine.post(
                    f"/v1/tenants/{tenant_id}/scenarios/{scenario['id']}/iterations/{iteration['id']}/rules",
                    json={
                        "display_order": index,
                        "name": rule["name"],
                        "description": rule.get("description", rule["name"]),
                        "formula": rule["formula"],
                        "score_modifier": rule.get("score_modifier", 1),
                        "rule_group": rule.get("rule_group", "fixture"),
                        "stable_rule_id": unique_name(rule["name"]),
                    },
                ),
                201,
            ),
            "rule",
        )
        rules.append(created)

    return {"scenario": scenario, "iteration": iteration, "rules": rules}


def commit_and_publish(decision_engine, tenant_id: str, bundle: dict[str, Any]) -> None:
    scenario_id = bundle["scenario"]["id"]
    iteration_id = bundle["iteration"]["id"]
    assert_status(
        decision_engine.post(f"/v1/tenants/{tenant_id}/scenarios/{scenario_id}/iterations/{iteration_id}/commit"),
        200,
    )
    assert_status(
        decision_engine.post(
            f"/v1/tenants/{tenant_id}/scenarios/{scenario_id}/publications",
            json={"action": "publish", "iteration_id": iteration_id},
        ),
        200,
    )


def evaluate_case(decision_engine, tenant_id: str, bundle: dict[str, Any], input_payload: dict[str, Any]) -> dict[str, Any]:
    return require_key(
        assert_status(
            decision_engine.post(
                f"/v1/tenants/{tenant_id}/scenarios/{bundle['scenario']['id']}/evaluate",
                json=input_payload,
            ),
            200,
        ),
        "result",
    )


def assert_successful_result(
    decision_engine,
    tenant_id: str,
    scenario_id: str,
    result: dict[str, Any],
    expected: dict[str, Any],
) -> None:
    assert result["triggered"] is expected["triggered"]
    if not expected["triggered"]:
        assert "decision" not in result or result["decision"] is None
        return

    decision = result["decision"]
    executions_by_name = {execution["rule_name"]: execution for execution in result["rule_executions"]}
    actual_rule_outcomes = {name: execution["outcome"] for name, execution in executions_by_name.items()}
    assert decision["score"] == expected["score"], actual_rule_outcomes
    assert decision["outcome"] == expected["outcome"], actual_rule_outcomes

    assert set(executions_by_name) == set(expected["rules"])
    for rule_name, outcome in expected["rules"].items():
        assert executions_by_name[rule_name]["outcome"] == outcome

    persisted = assert_status(decision_engine.get(f"/v1/tenants/{tenant_id}/decisions/{decision['id']}"), 200)
    persisted_decision = require_key(persisted, "decision")
    assert persisted_decision["id"] == decision["id"]
    assert persisted_decision["score"] == expected["score"]
    assert persisted_decision["outcome"] == expected["outcome"]

    scenario_decisions = require_key(
        assert_status(decision_engine.get(f"/v1/tenants/{tenant_id}/scenarios/{scenario_id}/decisions"), 200),
        "decisions",
    )
    assert any(item["id"] == decision["id"] for item in scenario_decisions)


def assert_expected_error(validation_result: dict[str, Any], expected_fragment: str | None) -> None:
    if not expected_fragment:
        return
    haystack = json.dumps(validation_result, sort_keys=True)
    assert expected_fragment in haystack, validation_result


def validation_payload(payload: dict[str, Any]) -> dict[str, Any]:
    if "validation" in payload:
        return payload["validation"]
    return require_key(payload, "result")
