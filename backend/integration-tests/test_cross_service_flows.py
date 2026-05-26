import pytest

from conftest import assert_status, record_payload, require_key


pytestmark = pytest.mark.integration


def test_data_model_ingestion_and_decision_engine_end_to_end(data_model, ingestion, decision_engine, tenant_model, scenario_bundle):
    """Verify a full flow: read model, ingest a record, evaluate it, read decision history, and trigger callback evaluation."""
    tenant_id = tenant_model["tenant_id"]
    object_type = tenant_model["transactions"]["name"]
    record = record_payload(amount=5000)

    model = require_key(assert_status(data_model.get(f"/v1/tenants/{tenant_id}/data-model"), 200), "data_model")
    assert object_type in model["tables"]

    ingest_result = require_key(
        assert_status(ingestion.post(f"/v1/tenants/{tenant_id}/ingest/{object_type}", json=record), 200),
        "result",
    )
    assert ingest_result["object_id"] == record["object_id"]

    evaluation = require_key(
        assert_status(
            decision_engine.post(
                f"/v1/tenants/{tenant_id}/scenarios/{scenario_bundle['scenario']['id']}/evaluate",
                json={"object_id": record["object_id"], "object_type": object_type, "fields": record},
            ),
            200,
        ),
        "result",
    )
    assert evaluation["triggered"] is True
    assert evaluation["decision"]["object_id"] == record["object_id"]

    decisions = require_key(
        assert_status(decision_engine.get(f"/v1/tenants/{tenant_id}/scenarios/{scenario_bundle['scenario']['id']}/decisions"), 200),
        "decisions",
    )
    assert any(item["id"] == evaluation["decision"]["id"] for item in decisions)

    assert_status(
        decision_engine.post(
            f"/v1/tenants/{tenant_id}/ingestion-events/record-ingested",
            json={"object_id": record["object_id"], "object_type": object_type, "fields": record, "source": "integration-test"},
        ),
        200,
    )
