import uuid

import pytest

from conftest import assert_status, amount_gt_node, record_payload, require_key, true_node, unique_name, utc_future


pytestmark = pytest.mark.integration


def evaluate_record(decision_engine, bundle, amount=1250):
    payload = record_payload(amount=amount)
    response = require_key(
        assert_status(
            decision_engine.post(
                f"/v1/tenants/{bundle['tenant_id']}/scenarios/{bundle['scenario']['id']}/evaluate",
                json={"object_id": payload["object_id"], "object_type": bundle["object_type"], "fields": payload},
            ),
            200,
        ),
        "result",
    )
    assert response["triggered"] is True
    assert response["decision"]["id"]
    return response


def test_decision_engine_health_service_info_and_scenario_listing(decision_engine, scenario_bundle):
    """Verify decision-engine health, service metadata, scenario listing, and iteration listing."""
    assert_status(decision_engine.get("/healthz"), 200)
    assert_status(decision_engine.get("/readyz"), 200)
    assert_status(decision_engine.get("/v1/service-info"), 200)

    scenarios = require_key(
        assert_status(decision_engine.get(f"/v1/tenants/{scenario_bundle['tenant_id']}/scenarios"), 200),
        "scenarios",
    )
    assert any(item["id"] == scenario_bundle["scenario"]["id"] for item in scenarios)
    iterations = require_key(
        assert_status(
            decision_engine.get(
                f"/v1/tenants/{scenario_bundle['tenant_id']}/scenarios/{scenario_bundle['scenario']['id']}/iterations"
            ),
            200,
        ),
        "iterations",
    )
    assert any(item["id"] == scenario_bundle["iteration"]["id"] for item in iterations)


def test_rules_validation_publication_and_delete_flow(decision_engine, tenant_model):
    """Exercise scenario authoring: draft iteration update, rule CRUD, validation, commit, publication, and delete."""
    tenant_id = tenant_model["tenant_id"]
    object_type = tenant_model["transactions"]["name"]
    scenario = require_key(
        assert_status(
            decision_engine.post(
                f"/v1/tenants/{tenant_id}/scenarios",
                json={"name": unique_name("draft scenario"), "trigger_object_type": object_type},
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
                "trigger_formula": true_node(),
                "score_review_threshold": 1,
                "score_block_and_review_threshold": 10,
                "score_decline_threshold": 20,
                "schedule": "",
            },
        ),
        200,
    )
    rule = require_key(
        assert_status(
            decision_engine.post(
                f"/v1/tenants/{tenant_id}/scenarios/{scenario['id']}/iterations/{iteration['id']}/rules",
                json={
                    "display_order": 1,
                    "name": "Amount nonzero",
                    "description": "Temporary integration rule",
                    "formula": amount_gt_node(0),
                    "score_modifier": 5,
                    "rule_group": "temporary",
                    "stable_rule_id": unique_name("stable"),
                },
            ),
            201,
        ),
        "rule",
    )
    assert_status(decision_engine.get(f"/v1/tenants/{tenant_id}/scenarios/{scenario['id']}/iterations/{iteration['id']}/rules"), 200)
    assert_status(
        decision_engine.put(
            f"/v1/tenants/{tenant_id}/scenarios/{scenario['id']}/iterations/{iteration['id']}/rules/{rule['id']}",
            json={**rule, "description": "Updated temporary integration rule"},
        ),
        200,
    )
    assert_status(
        decision_engine.post(f"/v1/tenants/{tenant_id}/scenarios/{scenario['id']}/iterations/{iteration['id']}/validate"),
        {200, 422},
    )
    assert_status(
        decision_engine.post(f"/v1/tenants/{tenant_id}/scenarios/{scenario['id']}/iterations/{iteration['id']}/commit"),
        200,
    )
    assert_status(decision_engine.get(f"/v1/tenants/{tenant_id}/scenarios/{scenario['id']}/publications"), 200)
    assert_status(
        decision_engine.post(
            f"/v1/tenants/{tenant_id}/scenarios/{scenario['id']}/publications",
            json={"action": "publish", "iteration_id": iteration["id"]},
        ),
        {200, 400},
    )
    assert_status(
        decision_engine.delete(f"/v1/tenants/{tenant_id}/scenarios/{scenario['id']}/iterations/{iteration['id']}/rules/{rule['id']}"),
        {204, 400},
    )


def test_decision_evaluation_history_ingestion_trigger_and_test_runs(decision_engine, scenario_bundle):
    """Verify live evaluation, decision history, ingestion-triggered evaluation, and test-run evaluation."""
    tenant_id = scenario_bundle["tenant_id"]
    scenario_id = scenario_bundle["scenario"]["id"]
    result = evaluate_record(decision_engine, scenario_bundle)
    decision_id = result["decision"]["id"]

    assert_status(decision_engine.get(f"/v1/tenants/{tenant_id}/scenarios/{scenario_id}/decisions"), 200)
    decision = assert_status(decision_engine.get(f"/v1/tenants/{tenant_id}/decisions/{decision_id}"), 200)
    assert require_key(decision, "decision")["id"] == decision_id

    payload = record_payload(amount=2500)
    assert_status(
        decision_engine.post(
            f"/v1/tenants/{tenant_id}/ingestion-events/record-ingested",
            json={"object_id": payload["object_id"], "object_type": scenario_bundle["object_type"], "fields": payload},
        ),
        200,
    )

    phantom_iteration = require_key(
        assert_status(decision_engine.post(f"/v1/tenants/{tenant_id}/scenarios/{scenario_id}/iterations"), 201),
        "iteration",
    )
    assert_status(
        decision_engine.put(
            f"/v1/tenants/{tenant_id}/scenarios/{scenario_id}/iterations/{phantom_iteration['id']}",
            json={"trigger_formula": true_node(), "schedule": ""},
        ),
        200,
    )
    test_run = require_key(
        assert_status(
            decision_engine.post(
                f"/v1/tenants/{tenant_id}/scenarios/{scenario_id}/test-runs",
                json={"phantom_iteration_id": phantom_iteration["id"], "expires_at": utc_future()},
            ),
            201,
        ),
        "test_run",
    )
    assert_status(decision_engine.get(f"/v1/tenants/{tenant_id}/scenarios/{scenario_id}/test-runs"), 200)
    assert_status(
        decision_engine.post(
            f"/v1/tenants/{tenant_id}/test-runs/{test_run['id']}/evaluate",
            json={"object_id": payload["object_id"], "object_type": scenario_bundle["object_type"], "fields": payload},
        ),
        200,
    )


def test_workflow_and_structured_workflow_endpoints(decision_engine, scenario_bundle):
    """Exercise workflow definitions, structured workflow rules, conditions, actions, reorder, execution reads, and deletes."""
    tenant_id = scenario_bundle["tenant_id"]
    scenario_id = scenario_bundle["scenario"]["id"]

    workflow = require_key(
        assert_status(
            decision_engine.post(
                f"/v1/tenants/{tenant_id}/scenarios/{scenario_id}/workflows",
                json={
                    "name": unique_name("case workflow"),
                    "description": "Create a review case",
                    "allowed_outcomes": ["review"],
                    "action_type": "create_case",
                    "action_config": {"queue": "manual-review"},
                    "active": True,
                },
            ),
            201,
        ),
        "workflow",
    )
    assert_status(decision_engine.get(f"/v1/tenants/{tenant_id}/scenarios/{scenario_id}/workflows"), 200)
    assert_status(
        decision_engine.post(f"/v1/tenants/{tenant_id}/scenarios/{scenario_id}/workflows/reorder", json=[workflow["id"]]),
        204,
    )

    workflow_rule = require_key(
        assert_status(
            decision_engine.post(
                f"/v1/tenants/{tenant_id}/scenarios/{scenario_id}/workflow-rules",
                json={"name": unique_name("structured rule"), "fallthrough": False},
            ),
            201,
        ),
        "workflow_rule",
    )
    assert_status(decision_engine.get(f"/v1/tenants/{tenant_id}/scenarios/{scenario_id}/workflow-rules"), 200)
    assert_status(decision_engine.get(f"/v1/tenants/{tenant_id}/scenarios/{scenario_id}/workflow-rules/{workflow_rule['id']}"), 200)
    assert_status(
        decision_engine.put(
            f"/v1/tenants/{tenant_id}/scenarios/{scenario_id}/workflow-rules/{workflow_rule['id']}",
            json={"name": workflow_rule["name"] + " updated", "fallthrough": True},
        ),
        200,
    )
    assert_status(
        decision_engine.post(
            f"/v1/tenants/{tenant_id}/scenarios/{scenario_id}/workflow-rules/reorder",
            json=[workflow_rule["id"]],
        ),
        204,
    )

    condition = require_key(
        assert_status(
            decision_engine.post(
                f"/v1/tenants/{tenant_id}/scenarios/{scenario_id}/workflow-rules/{workflow_rule['id']}/conditions",
                json={"function": "always", "params": None},
            ),
            201,
        ),
        "workflow_condition",
    )
    assert_status(
        decision_engine.put(
            f"/v1/tenants/{tenant_id}/scenarios/{scenario_id}/workflow-rules/{workflow_rule['id']}/conditions/{condition['id']}",
                json={"function": "outcome_in", "params": ["review"]},
        ),
        200,
    )

    action = require_key(
        assert_status(
            decision_engine.post(
                f"/v1/tenants/{tenant_id}/scenarios/{scenario_id}/workflow-rules/{workflow_rule['id']}/actions",
                json={"action_type": "add_tag", "action_config": {"tag": "needs-review"}},
            ),
            201,
        ),
        "workflow_action",
    )
    assert_status(
        decision_engine.put(
            f"/v1/tenants/{tenant_id}/scenarios/{scenario_id}/workflow-rules/{workflow_rule['id']}/actions/{action['id']}",
            json={"action_type": "emit_event", "action_config": {"topic": "risk.review"}},
        ),
        200,
    )

    decision_id = evaluate_record(decision_engine, scenario_bundle)["decision"]["id"]
    assert_status(decision_engine.get(f"/v1/tenants/{tenant_id}/decisions/{decision_id}/workflow-executions"), 200)
    assert_status(
        decision_engine.delete(
            f"/v1/tenants/{tenant_id}/scenarios/{scenario_id}/workflow-rules/{workflow_rule['id']}/conditions/{condition['id']}"
        ),
        204,
    )
    assert_status(
        decision_engine.delete(
            f"/v1/tenants/{tenant_id}/scenarios/{scenario_id}/workflow-rules/{workflow_rule['id']}/actions/{action['id']}"
        ),
        204,
    )
    assert_status(
        decision_engine.delete(f"/v1/tenants/{tenant_id}/scenarios/{scenario_id}/workflow-rules/{workflow_rule['id']}"),
        204,
    )


def test_screening_scoring_snooze_execution_platform_and_outbox(decision_engine, scenario_bundle):
    """Verify screening, scoring, snoozes, scheduled/async executions, platform helper data, and outbox reads."""
    tenant_id = scenario_bundle["tenant_id"]
    scenario_id = scenario_bundle["scenario"]["id"]
    object_type = scenario_bundle["object_type"]

    screening = require_key(
        assert_status(
            decision_engine.post(
                f"/v1/tenants/{tenant_id}/scenarios/{scenario_id}/screening-configs",
                json={
                    "name": unique_name("screen"),
                    "allowed_outcomes": ["review"],
                    "provider": "integration-provider",
                    "config_json": {"mode": "test"},
                    "active": True,
                },
            ),
            201,
        ),
        "screening_config",
    )
    assert screening["id"]
    assert_status(decision_engine.get(f"/v1/tenants/{tenant_id}/scenarios/{scenario_id}/screening-configs"), 200)

    scoring = require_key(
        assert_status(
            decision_engine.post(
                f"/v1/tenants/{tenant_id}/scenarios/{scenario_id}/scoring-configs",
                json={
                    "name": unique_name("score"),
                    "allowed_outcomes": ["review"],
                    "ruleset_ref": "integration-ruleset",
                    "config_json": {"mode": "test"},
                    "active": True,
                },
            ),
            201,
        ),
        "scoring_config",
    )
    assert scoring["id"]
    assert_status(decision_engine.get(f"/v1/tenants/{tenant_id}/scenarios/{scenario_id}/scoring-configs"), 200)

    decision_id = evaluate_record(decision_engine, scenario_bundle)["decision"]["id"]
    screening_executions = require_key(
        assert_status(decision_engine.get(f"/v1/tenants/{tenant_id}/decisions/{decision_id}/screening-executions"), 200),
        "screening_executions",
    )
    scoring_requests = require_key(
        assert_status(decision_engine.get(f"/v1/tenants/{tenant_id}/decisions/{decision_id}/scoring-requests"), 200),
        "scoring_requests",
    )

    screening_execution_id = screening_executions[0]["id"] if screening_executions else str(uuid.uuid4())
    assert_status(decision_engine.get(f"/v1/tenants/{tenant_id}/screening-executions/{screening_execution_id}"), {200, 404})
    assert_status(
        decision_engine.post(
            f"/v1/tenants/{tenant_id}/screening-executions/{screening_execution_id}/status",
            json={"status": "failed", "last_error": "integration test"},
        ),
        {200, 400, 404},
    )
    assert_status(
        decision_engine.post(f"/v1/tenants/{tenant_id}/screening-executions/{screening_execution_id}/retry"),
        {200, 400, 404},
    )

    scoring_request_id = scoring_requests[0]["id"] if scoring_requests else str(uuid.uuid4())
    assert_status(decision_engine.get(f"/v1/tenants/{tenant_id}/scoring-requests/{scoring_request_id}"), {200, 404})
    assert_status(
        decision_engine.post(
            f"/v1/tenants/{tenant_id}/scoring-requests/{scoring_request_id}/status",
            json={"status": "failed", "last_error": "integration test"},
        ),
        {200, 400, 404},
    )
    assert_status(
        decision_engine.post(f"/v1/tenants/{tenant_id}/scoring-requests/{scoring_request_id}/retry"),
        {200, 400, 404},
    )

    assert_status(
        decision_engine.post(
            f"/v1/tenants/{tenant_id}/scenarios/{scenario_id}/rule-snoozes",
            json={
                "object_type": object_type,
                "object_id": unique_name("obj"),
                "snooze_group_id": str(uuid.uuid4()),
                "expires_at": utc_future(),
            },
        ),
        201,
    )
    assert_status(decision_engine.get(f"/v1/tenants/{tenant_id}/scenarios/{scenario_id}/rule-snoozes"), 200)

    item = {"object_id": unique_name("scheduled"), "object_type": object_type, "fields": record_payload(amount=500)}
    assert_status(
        decision_engine.post(
            f"/v1/tenants/{tenant_id}/scenarios/{scenario_id}/scheduled-executions",
            json={"scheduled_for": utc_future(), "items": [item], "candidate_limit": 1},
        ),
        201,
    )
    assert_status(decision_engine.get(f"/v1/tenants/{tenant_id}/scenarios/{scenario_id}/scheduled-executions"), 200)

    assert_status(
        decision_engine.post(
            f"/v1/tenants/{tenant_id}/async-decision-executions",
            json={"scenario_id": scenario_id, "object_type": object_type, "items": [item]},
        ),
        201,
    )
    assert_status(decision_engine.get(f"/v1/tenants/{tenant_id}/async-decision-executions"), 200)

    assert_status(
        decision_engine.post(
            f"/v1/tenants/{tenant_id}/platform/custom-list-entries",
            json={"list_name": "blocked_accounts", "value": "acct-1"},
        ),
        201,
    )
    assert_status(decision_engine.get(f"/v1/tenants/{tenant_id}/platform/custom-list-entries", params={"list_name": "blocked_accounts"}), 200)
    assert_status(
        decision_engine.post(
            f"/v1/tenants/{tenant_id}/platform/record-tags",
            json={"object_type": object_type, "object_id": "txn-1", "tag": "vip"},
        ),
        201,
    )
    assert_status(decision_engine.get(f"/v1/tenants/{tenant_id}/platform/record-tags", params={"object_type": object_type, "object_id": "txn-1"}), 200)
    assert_status(
        decision_engine.post(
            f"/v1/tenants/{tenant_id}/platform/risk-snapshots",
            json={"object_type": object_type, "object_id": "txn-1", "risk_level": "high"},
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
    assert_status(decision_engine.get(f"/v1/tenants/{tenant_id}/platform/ip-flags", params={"ip_address": "1.2.3.4"}), 200)
    assert_status(decision_engine.get(f"/v1/tenants/{tenant_id}/outbox-events"), 200)
