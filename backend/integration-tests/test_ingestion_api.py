import pytest

from conftest import assert_status, make_csv_file, record_payload, require_key, unique_name


pytestmark = pytest.mark.integration


def test_ingestion_health(ingestion):
    """Verify ingestion service health and readiness endpoints."""
    assert_status(ingestion.get("/healthz"), 200)
    assert_status(ingestion.get("/readyz"), 200)


def test_single_patch_batch_and_idempotency(ingestion, tenant_model):
    """Verify single ingest, idempotency replay/conflict, patch ingest, and batch ingest."""
    tenant_id = tenant_model["tenant_id"]
    object_type = tenant_model["transactions"]["name"]
    payload = record_payload(amount=1500)
    key = unique_name("idem")

    first = require_key(
        assert_status(
            ingestion.post(
                f"/v1/tenants/{tenant_id}/ingest/{object_type}",
                json=payload,
                headers={"Idempotency-Key": key},
            ),
            200,
        ),
        "result",
    )
    assert first["object_id"] == payload["object_id"]

    replay = require_key(
        assert_status(
            ingestion.post(
                f"/v1/tenants/{tenant_id}/ingest/{object_type}",
                json=payload,
                headers={"Idempotency-Key": key},
            ),
            200,
        ),
        "result",
    )
    assert replay["object_id"] == payload["object_id"]

    changed_payload = {**payload, "amount": 1600}
    assert_status(
        ingestion.post(
            f"/v1/tenants/{tenant_id}/ingest/{object_type}",
            json=changed_payload,
            headers={"Idempotency-Key": key},
        ),
        409,
    )

    patched = require_key(
        assert_status(
            ingestion.patch(
                f"/v1/tenants/{tenant_id}/ingest/{object_type}",
                json={"object_id": payload["object_id"], "status": "pending"},
            ),
            200,
        ),
        "result",
    )
    assert patched["object_id"] == payload["object_id"]

    batch_rows = [record_payload(amount=10), record_payload(amount=20)]
    batch = require_key(
        assert_status(ingestion.post(f"/v1/tenants/{tenant_id}/ingest/{object_type}/batch", json=batch_rows), 200),
        "results",
    )
    assert len(batch) == 2


def test_ingestion_validation_errors(ingestion, tenant_model):
    """Verify validation errors for missing record ids and unknown object types."""
    tenant_id = tenant_model["tenant_id"]
    object_type = tenant_model["transactions"]["name"]
    assert_status(ingestion.post(f"/v1/tenants/{tenant_id}/ingest/{object_type}", json={"amount": 25}), 422)
    assert_status(ingestion.patch(f"/v1/tenants/{tenant_id}/ingest/{object_type}", json={"amount": 25}), 422)
    assert_status(ingestion.post(f"/v1/tenants/{tenant_id}/ingest/missing_object", json=record_payload()), {400, 422})


def test_csv_upload_and_upload_logs(ingestion, tenant_model):
    """Verify CSV upload acceptance plus upload-log list and get endpoints."""
    tenant_id = tenant_model["tenant_id"]
    object_type = tenant_model["transactions"]["name"]
    file_tuple = make_csv_file([record_payload(amount=33), record_payload(amount=44)])
    upload_log = require_key(
        assert_status(
            ingestion.post(
                f"/v1/tenants/{tenant_id}/ingest/{object_type}/csv",
                params={"mode": "create"},
                files={"file": file_tuple},
            ),
            202,
        ),
        "upload_log",
    )
    upload_logs = require_key(
        assert_status(ingestion.get(f"/v1/tenants/{tenant_id}/ingest/{object_type}/upload-logs"), 200),
        "upload_logs",
    )
    assert any(item["id"] == upload_log["id"] for item in upload_logs)
    assert_status(ingestion.get(f"/v1/upload-logs/{upload_log['id']}"), 200)
