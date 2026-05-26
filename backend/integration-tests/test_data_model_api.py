import pytest

from conftest import assert_status, require_key, unique_name


pytestmark = pytest.mark.integration


def test_data_model_health_and_tenant_endpoints(data_model, tenant_model):
    """Verify data-model health/readiness and tenant list/get/provision endpoints."""
    assert_status(data_model.get("/healthz"), 200)
    assert_status(data_model.get("/readyz"), 200)

    tenant_id = tenant_model["tenant_id"]
    assert require_key(assert_status(data_model.get("/v1/tenants"), 200), "tenants")
    tenant = require_key(assert_status(data_model.get(f"/v1/tenants/{tenant_id}"), 200), "tenant")
    assert tenant["id"] == tenant_id
    assert_status(data_model.post(f"/v1/tenants/{tenant_id}/provision"), 200)


def test_data_model_tables_fields_options_and_assembled_model(data_model, tenant_model):
    """Exercise table, field, table-options, and assembled model reads for a provisioned tenant."""
    tenant_id = tenant_model["tenant_id"]
    table_id = tenant_model["transactions"]["id"]
    amount_id = tenant_model["fields"]["amount"]["id"]
    status_id = tenant_model["fields"]["status"]["id"]

    assert any(t["id"] == table_id for t in require_key(assert_status(data_model.get(f"/v1/tenants/{tenant_id}/tables"), 200), "tables"))

    updated_table = require_key(
        assert_status(
            data_model.patch(
                f"/v1/tables/{table_id}",
                json={"description": "Updated by integration tests", "alias": "Transactions IT"},
            ),
            200,
        ),
        "table",
    )
    assert updated_table["alias"] == "Transactions IT"

    fields = require_key(assert_status(data_model.get(f"/v1/tables/{table_id}/fields"), 200), "fields")
    assert {field["name"] for field in fields} >= {"amount", "status", "account_id", "ip"}

    updated_field = require_key(
        assert_status(data_model.patch(f"/v1/fields/{amount_id}", json={"description": "Transaction amount"}), 200),
        "field",
    )
    assert updated_field["description"] == "Transaction amount"

    options = assert_status(
        data_model.put(
            f"/v1/tables/{table_id}/options",
            json={"displayed_fields": [amount_id, status_id], "field_order": [status_id, amount_id]},
        ),
        200,
    )
    assert options["table_id"] == table_id
    assert_status(data_model.get(f"/v1/tables/{table_id}/options"), 200)

    assembled = require_key(assert_status(data_model.get(f"/v1/tenants/{tenant_id}/data-model"), 200), "data_model")
    assert assembled["ingestion_contract"]["writable"] is True
    assert tenant_model["transactions"]["name"] in assembled["tables"]


def test_data_model_enum_values(data_model, tenant_model):
    """Create, list, update, and delete managed enum values on an enum field."""
    status_id = tenant_model["fields"]["status"]["id"]
    enum_value = require_key(
        assert_status(
            data_model.post(
                f"/v1/fields/{status_id}/enum-values",
                json={"value": unique_name("review"), "label": "Review", "sort_order": 20},
            ),
            201,
        ),
        "enum_value",
    )
    enum_values = require_key(assert_status(data_model.get(f"/v1/fields/{status_id}/enum-values"), 200), "enum_values")
    assert any(item["id"] == enum_value["id"] for item in enum_values)

    assert_status(
        data_model.patch(f"/v1/enum-values/{enum_value['id']}", json={"label": "Manual Review", "sort_order": 30}),
        200,
    )
    assert_status(data_model.delete(f"/v1/enum-values/{enum_value['id']}"), 204)


def test_data_model_links_pivots_navigation_and_deletes(data_model, tenant_model):
    """Create relationship metadata, pivot metadata, navigation options, and then delete them."""
    tenant_id = tenant_model["tenant_id"]
    transactions = tenant_model["transactions"]
    accounts = tenant_model["accounts"]
    account_id = tenant_model["fields"]["account_id"]
    account_key = tenant_model["account_key"]
    amount_id = tenant_model["fields"]["amount"]["id"]

    link = require_key(
        assert_status(
            data_model.post(
                f"/v1/tenants/{tenant_id}/links",
                json={
                    "name": unique_name("txn_to_account"),
                    "parent_table_id": accounts["id"],
                    "parent_field_id": account_key["id"],
                    "child_table_id": transactions["id"],
                    "child_field_id": account_id["id"],
                },
            ),
            201,
        ),
        "link",
    )
    assert any(item["id"] == link["id"] for item in require_key(assert_status(data_model.get(f"/v1/tenants/{tenant_id}/links"), 200), "links"))

    pivot = require_key(
        assert_status(
            data_model.post(
                f"/v1/tenants/{tenant_id}/pivots",
                json={"base_table_id": transactions["id"], "path_link_ids": [link["id"]]},
            ),
            201,
        ),
        "pivot",
    )
    assert any(item["id"] == pivot["id"] for item in require_key(assert_status(data_model.get(f"/v1/tenants/{tenant_id}/pivots"), 200), "pivots"))

    navigation = require_key(
        assert_status(
            data_model.post(
                f"/v1/tables/{accounts['id']}/navigation-options",
                json={
                    "source_field_id": account_key["id"],
                    "target_table_id": transactions["id"],
                    "filter_field_id": account_id["id"],
                    "ordering_field_id": amount_id,
                },
            ),
            201,
        ),
        "navigation_option",
    )
    assert_status(data_model.get(f"/v1/tables/{accounts['id']}/navigation-options"), 200)

    assert_status(data_model.delete(f"/v1/navigation-options/{navigation['id']}"), 204)
    assert_status(data_model.delete(f"/v1/pivots/{pivot['id']}"), 200)
    assert_status(data_model.delete(f"/v1/links/{link['id']}"), 200)


def test_data_model_index_jobs_logs_migrations_reconcile_and_delete_paths(data_model, tenant_model):
    """Verify schema logs, migrations, index job lifecycle, reconcile, dry-run deletes, and hard deletes."""
    tenant_id = tenant_model["tenant_id"]
    table_id = tenant_model["transactions"]["id"]

    assert_status(data_model.get(f"/v1/tenants/{tenant_id}/schema-change-log"), 200)
    assert_status(data_model.get(f"/v1/tenants/{tenant_id}/schema-migrations"), 200)

    job = require_key(
        assert_status(
            data_model.post(
                f"/v1/tenants/{tenant_id}/index-jobs",
                json={"table_id": table_id, "index_type": "search", "columns": ["amount"]},
            ),
            201,
        ),
        "index_job",
    )
    assert_status(data_model.get(f"/v1/tenants/{tenant_id}/index-jobs"), 200)
    assert_status(data_model.get(f"/v1/index-jobs/{job['id']}"), 200)
    assert_status(data_model.post(f"/v1/index-jobs/{job['id']}/retry"), {200, 400, 409})
    assert_status(data_model.get("/v1/admin/reconcile"), 200)

    disposable = require_key(
        assert_status(
            data_model.post(
                f"/v1/tenants/{tenant_id}/tables",
                json={"name": unique_name("disposable"), "description": "delete me"},
            ),
            201,
        ),
        "table",
    )
    disposable_field = require_key(
        assert_status(
            data_model.post(
                f"/v1/tables/{disposable['id']}/fields",
                json={"name": "note", "data_type": "string", "nullable": True},
            ),
            201,
        ),
        "field",
    )
    assert_status(data_model.delete(f"/v1/fields/{disposable_field['id']}?dry_run=true"), 200)
    assert_status(data_model.delete(f"/v1/fields/{disposable_field['id']}"), 200)
    assert_status(data_model.delete(f"/v1/tables/{disposable['id']}?dry_run=true"), 200)
    assert_status(data_model.delete(f"/v1/tables/{disposable['id']}"), 200)
