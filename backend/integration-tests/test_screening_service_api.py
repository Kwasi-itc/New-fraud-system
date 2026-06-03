import uuid

import pytest

from conftest import assert_status, record_payload, require_key


pytestmark = pytest.mark.integration


def test_screening_service_internal_intake_and_detail(screening_service, tenant_model, scenario_bundle):
    """Verify screening-service readiness, decision intake, idempotent create, decision listing, and detail retrieval."""
    tenant_id = tenant_model["tenant_id"]
    payload = {
        "provider": "opensanctions",
        "decision_id": str(uuid.uuid4()),
        "scenario_id": scenario_bundle["scenario"]["id"],
        "screening_config_id": str(uuid.uuid4()),
        "idempotency_key": f"it-{uuid.uuid4().hex}",
        "object_type": tenant_model["transactions"]["name"],
        "object_id": f"obj_{uuid.uuid4().hex[:10]}",
        "queries": [{"name": "Acme Ltd", "type": "Organization"}],
    }

    assert_status(screening_service.get("/healthz"), 200)
    assert_status(screening_service.get("/readyz"), 200)
    assert_status(screening_service.get("/v1/service-info"), 200)

    created = assert_status(
        screening_service.post(f"/internal/v1/tenants/{tenant_id}/decision-screenings", json=payload),
        201,
    )
    screening_id = require_key(created, "id")

    duplicated = assert_status(
        screening_service.post(f"/internal/v1/tenants/{tenant_id}/decision-screenings", json=payload),
        201,
    )
    assert require_key(duplicated, "id") == screening_id

    items = assert_status(screening_service.get(f"/v1/tenants/{tenant_id}/decisions/{payload['decision_id']}/screenings"), 200)
    assert any(item["id"] == screening_id for item in items)

    detail = assert_status(screening_service.get(f"/v1/tenants/{tenant_id}/screenings/{screening_id}"), 200)
    screening = require_key(detail, "screening")
    assert screening["id"] == screening_id
    assert screening["decision_id"] == payload["decision_id"]


def test_decision_engine_internal_screening_status_callback(decision_engine, scenario_bundle):
    """Verify the decision-engine internal screening callback updates screening execution state over HTTP."""
    tenant_id = scenario_bundle["tenant_id"]
    scenario_id = scenario_bundle["scenario"]["id"]
    object_type = scenario_bundle["object_type"]
    record = record_payload(amount=5000)

    screening_config = require_key(
        assert_status(
            decision_engine.post(
                f"/v1/tenants/{tenant_id}/scenarios/{scenario_id}/screening-configs",
                json={
                    "name": "Merchant screening",
                    "allowed_outcomes": ["review"],
                    "provider": "opensanctions",
                    "config_json": {
                        "entity_type": "Organization",
                        "query_fields": {"name": "merchant"},
                        "provider_config": {"datasets": ["pep"]},
                        "counterparty_id_field": "owner_id",
                    },
                    "active": True,
                },
            ),
            201,
        ),
        "screening_config",
    )

    evaluation = require_key(
        assert_status(
            decision_engine.post(
                f"/v1/tenants/{tenant_id}/scenarios/{scenario_id}/evaluate",
                json={"object_id": record["object_id"], "object_type": object_type, "fields": record},
            ),
            200,
        ),
        "result",
    )
    decision_id = evaluation["decision"]["id"]

    executions = require_key(
        assert_status(decision_engine.get(f"/v1/tenants/{tenant_id}/decisions/{decision_id}/screening-executions"), 200),
        "screening_executions",
    )
    execution = next(item for item in executions if item["config_id"] == screening_config["id"])

    assert_status(
        decision_engine.post(
            "/internal/screening-status-updates",
            json={
                "tenant_id": tenant_id,
                "screening_id": "screening-live-1",
                "decision_id": decision_id,
                "scenario_id": scenario_id,
                "screening_config_id": screening_config["id"],
                "status": "awaiting_review",
                "provider": "opensanctions",
                "object_type": object_type,
                "object_id": record["object_id"],
                "provider_reference": "provider-job-1",
                "match_count": 2,
            },
        ),
        200,
    )

    updated = require_key(
        assert_status(decision_engine.get(f"/v1/tenants/{tenant_id}/screening-executions/{execution['id']}"), 200),
        "screening_execution",
    )
    assert updated["status"] == "completed"
    assert updated["provider_reference"] == "provider-job-1"
