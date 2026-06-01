from __future__ import annotations

import csv
import json
import io
import os
import time
import uuid
from contextvars import ContextVar
from dataclasses import dataclass
from datetime import datetime, timedelta, timezone
from pathlib import Path
from typing import Any

import httpx
import pytest
import yaml


ROOT = Path(__file__).resolve().parent
CURRENT_TEST: ContextVar[str | None] = ContextVar("current_integration_test", default=None)
HTTP_CALLS_BY_TEST: dict[str, list[dict[str, Any]]] = {}
TEST_PURPOSES: dict[str, str] = {}


def unique_name(prefix: str) -> str:
    return f"{prefix}_{uuid.uuid4().hex[:12]}"


def utc_future(minutes: int = 30) -> str:
    return (datetime.now(timezone.utc) + timedelta(minutes=minutes)).isoformat().replace("+00:00", "Z")


def true_node() -> dict[str, Any]:
    return {"function": "eq", "children": [{"constant": 1}, {"constant": 1}]}


def amount_gt_node(limit: int) -> dict[str, Any]:
    return {
        "function": "gt",
        "children": [
            {"function": "field_ref", "named_children": {"field": {"constant": "amount"}}},
            {"constant": limit},
        ],
    }


def record_payload(object_id: str | None = None, amount: int = 1250) -> dict[str, Any]:
    oid = object_id or f"txn_{uuid.uuid4().hex[:10]}"
    return {
        "object_id": oid,
        "amount": amount,
        "status": "pending",
        "account_id": f"acct_{uuid.uuid4().hex[:8]}",
        "ip": "1.2.3.4",
        "merchant": "ITC Market",
        "email": "Risk@Example.com",
        "country": "gh",
        "owner_id": f"owner_{uuid.uuid4().hex[:8]}",
        "note": None,
    }


@dataclass(frozen=True)
class ApiClient:
    name: str
    base_url: str
    client: httpx.Client

    def request(self, method: str, path: str, **kwargs: Any) -> httpx.Response:
        response = self.client.request(method, path, **kwargs)
        record_http_call(self.name, method, path, response)
        return response

    def get(self, path: str, **kwargs: Any) -> httpx.Response:
        return self.request("GET", path, **kwargs)

    def post(self, path: str, **kwargs: Any) -> httpx.Response:
        return self.request("POST", path, **kwargs)

    def put(self, path: str, **kwargs: Any) -> httpx.Response:
        return self.request("PUT", path, **kwargs)

    def patch(self, path: str, **kwargs: Any) -> httpx.Response:
        return self.request("PATCH", path, **kwargs)

    def delete(self, path: str, **kwargs: Any) -> httpx.Response:
        return self.request("DELETE", path, **kwargs)


def assert_status(response: httpx.Response, expected: int | set[int] | tuple[int, ...]) -> dict[str, Any]:
    expected_set = {expected} if isinstance(expected, int) else set(expected)
    if response.status_code not in expected_set:
        pytest.fail(
            f"{response.request.method} {response.request.url} returned {response.status_code}, "
            f"expected {sorted(expected_set)}: {response.text}"
        )
    if response.content:
        return response.json()
    return {}


def require_key(payload: dict[str, Any], key: str) -> Any:
    assert key in payload, f"missing response key {key!r}: {payload}"
    return payload[key]


def extract_id(payload: dict[str, Any], envelope: str) -> str:
    item = require_key(payload, envelope)
    assert isinstance(item, dict), item
    assert item.get("id"), item
    return item["id"]


def wait_until_ready(api: ApiClient, timeout_seconds: float = 30.0) -> None:
    deadline = time.monotonic() + timeout_seconds
    last_error: Exception | None = None
    while time.monotonic() < deadline:
        try:
            if api.get("/readyz").status_code == 200:
                return
        except (httpx.ConnectError, httpx.ReadTimeout, httpx.RemoteProtocolError) as exc:
            last_error = exc
        time.sleep(0.5)
    detail = f": {last_error}" if last_error else ""
    pytest.fail(f"{api.name} was not ready at {api.base_url}{detail}")


def load_spec(name: str) -> dict[str, Any]:
    with (ROOT / f"{name}.yaml").open() as handle:
        return yaml.safe_load(handle)


def make_csv_file(rows: list[dict[str, Any]]) -> tuple[str, bytes, str]:
    buffer = io.StringIO()
    writer = csv.DictWriter(buffer, fieldnames=list(rows[0].keys()))
    writer.writeheader()
    writer.writerows(rows)
    return "records.csv", buffer.getvalue().encode(), "text/csv"


def pytest_addoption(parser: pytest.Parser) -> None:
    parser.addoption(
        "--endpoint-summary",
        action="store_true",
        help="Print purpose, endpoint list, status codes, and response snippets for each passing integration test.",
    )
    parser.addoption(
        "--endpoint-summary-file",
        action="store",
        default=None,
        help="Write the endpoint summary to this file when the full run passes.",
    )
    parser.addoption(
        "--endpoint-output-limit",
        action="store",
        type=int,
        default=0,
        help="Maximum response-body characters to show per endpoint; 0 means no truncation.",
    )


def pytest_collection_modifyitems(items: list[pytest.Item]) -> None:
    for item in items:
        TEST_PURPOSES[item.nodeid] = test_purpose(item)


def pytest_runtest_setup(item: pytest.Item) -> None:
    CURRENT_TEST.set(item.nodeid)
    HTTP_CALLS_BY_TEST.setdefault(item.nodeid, [])


def pytest_runtest_call(item: pytest.Item) -> None:
    CURRENT_TEST.set(item.nodeid)
    HTTP_CALLS_BY_TEST.setdefault(item.nodeid, [])


def pytest_runtest_teardown(item: pytest.Item) -> None:
    CURRENT_TEST.set(None)


def pytest_terminal_summary(terminalreporter: Any, exitstatus: int, config: pytest.Config) -> None:
    endpoint_summary = config.getoption("--endpoint-summary", default=False)
    output_path = config.getoption("--endpoint-summary-file", default=None)
    if not endpoint_summary and not output_path:
        return
    if exitstatus != 0:
        if endpoint_summary:
            terminalreporter.section("endpoint summary skipped")
            terminalreporter.write_line("Endpoint summary is printed only when the full run passes.")
        return

    limit = config.getoption("--endpoint-output-limit", default=0)
    passed = terminalreporter.stats.get("passed", [])
    summary = build_endpoint_summary(passed, limit)
    if endpoint_summary:
        terminalreporter.section("integration endpoint summary")
        terminalreporter.write_line(summary)
    if output_path:
        path = Path(output_path)
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(summary + "\n")
        terminalreporter.write_line(f"endpoint summary written to {path}")


def build_endpoint_summary(passed_reports: list[Any], limit: int) -> str:
    lines = []
    for report in passed_reports:
        nodeid = report.nodeid
        calls = HTTP_CALLS_BY_TEST.get(nodeid, [])
        lines.append("")
        lines.append(nodeid)
        lines.append(f"  Purpose: {TEST_PURPOSES.get(nodeid, 'No purpose recorded.')}")
        if not calls:
            lines.append("  Endpoints: none")
            continue
        lines.append("  Endpoints:")
        for call in calls:
            response = truncate(call["response"], limit)
            lines.append(f"    - {call['service']} {call['method']} {call['path']} -> {call['status_code']}")
            if response:
                lines.append(f"      output: {response}")
    return "\n".join(lines).lstrip()


def record_http_call(service: str, method: str, path: str, response: httpx.Response) -> None:
    nodeid = CURRENT_TEST.get()
    if nodeid is None:
        return
    HTTP_CALLS_BY_TEST.setdefault(nodeid, []).append(
        {
            "service": service,
            "method": method.upper(),
            "path": display_path(path),
            "status_code": response.status_code,
            "response": response_snippet(response),
        }
    )


def response_snippet(response: httpx.Response) -> str:
    if not response.content:
        return ""
    content_type = response.headers.get("content-type", "")
    if "json" in content_type:
        try:
            return json.dumps(response.json(), sort_keys=True, default=str)
        except ValueError:
            return response.text
    return response.text


def display_path(path: str) -> str:
    if path.startswith("http://") or path.startswith("https://"):
        return httpx.URL(path).raw_path.decode()
    return path


def truncate(value: str, limit: int) -> str:
    value = " ".join(value.split())
    if limit <= 0:
        return value
    if len(value) <= limit:
        return value
    return value[: max(limit - 3, 0)] + "..."


def test_purpose(item: pytest.Item) -> str:
    doc = getattr(item.obj, "__doc__", None)
    if doc:
        return " ".join(doc.split())
    name = item.name.removeprefix("test_").replace("_", " ")
    return name[:1].upper() + name[1:] + "."


@pytest.fixture(scope="session")
def data_model() -> ApiClient:
    headers = {}
    token = os.getenv("SERVICE_AUTH_TOKEN")
    if token:
        headers["Authorization"] = f"Bearer {token}"
    client = httpx.Client(
        base_url=os.getenv("DATA_MODEL_URL", "http://localhost:8080"),
        timeout=httpx.Timeout(20.0),
        headers=headers,
    )
    api = ApiClient("data-model", str(client.base_url).rstrip("/"), client)
    wait_until_ready(api)
    yield api
    client.close()


@pytest.fixture(scope="session")
def ingestion() -> ApiClient:
    headers = {}
    token = os.getenv("SERVICE_AUTH_TOKEN")
    if token:
        headers["Authorization"] = f"Bearer {token}"
    client = httpx.Client(
        base_url=os.getenv("INGESTION_URL", "http://localhost:8081"),
        timeout=httpx.Timeout(30.0),
        headers=headers,
    )
    api = ApiClient("ingestion", str(client.base_url).rstrip("/"), client)
    wait_until_ready(api)
    yield api
    client.close()


@pytest.fixture(scope="session")
def decision_engine() -> ApiClient:
    headers = {}
    token = os.getenv("SERVICE_AUTH_TOKEN")
    if token:
        headers["Authorization"] = f"Bearer {token}"
    client = httpx.Client(
        base_url=os.getenv("DECISION_ENGINE_URL", "http://localhost:8082"),
        timeout=httpx.Timeout(30.0),
        headers=headers,
    )
    api = ApiClient("decision-engine", str(client.base_url).rstrip("/"), client)
    wait_until_ready(api)
    yield api
    client.close()


def create_tenant_model(data_model: ApiClient) -> dict[str, Any]:
    tenant_payload = {"name": unique_name("itc tenant"), "external_key": unique_name("ext")}
    tenant = require_key(assert_status(data_model.post("/v1/tenants", json=tenant_payload), 201), "tenant")
    tenant_id = tenant["id"]
    assert_status(data_model.post(f"/v1/tenants/{tenant_id}/provision"), 200)

    transactions = require_key(
        assert_status(
            data_model.post(
                f"/v1/tenants/{tenant_id}/tables",
                json={
                    "name": unique_name("transactions"),
                    "description": "Integration transaction table",
                    "alias": "Transactions",
                    "semantic_type": "entity",
                },
            ),
            201,
        ),
        "table",
    )
    accounts = require_key(
        assert_status(
            data_model.post(
                f"/v1/tenants/{tenant_id}/tables",
                json={
                    "name": unique_name("accounts"),
                    "description": "Integration account table",
                    "alias": "Accounts",
                    "semantic_type": "entity",
                },
            ),
            201,
        ),
        "table",
    )

    fields: dict[str, dict[str, Any]] = {}
    for field in [
        {"name": "amount", "data_type": "int", "nullable": False},
        {
            "name": "status",
            "data_type": "string",
            "nullable": False,
            "is_enum": True,
            "enum_values": [{"value": "pending", "label": "Pending", "sort_order": 10}],
        },
        {"name": "account_id", "data_type": "string", "nullable": True},
        {"name": "ip", "data_type": "ip_address", "nullable": True},
        {"name": "merchant", "data_type": "string", "nullable": False},
        {"name": "email", "data_type": "string", "nullable": False},
        {"name": "country", "data_type": "string", "nullable": False},
        {"name": "owner_id", "data_type": "string", "nullable": True},
        {"name": "event_time", "data_type": "timestamp", "nullable": True},
        {"name": "note", "data_type": "string", "nullable": True},
    ]:
        created = require_key(
            assert_status(data_model.post(f"/v1/tables/{transactions['id']}/fields", json=field), 201),
            "field",
        )
        fields[created["name"]] = created

    account_key = require_key(
        assert_status(
            data_model.post(
                f"/v1/tables/{accounts['id']}/fields",
                json={"name": "account_key", "data_type": "string", "nullable": False, "is_unique": True},
            ),
            201,
        ),
        "field",
    )
    account_status = require_key(
        assert_status(
            data_model.post(
                f"/v1/tables/{accounts['id']}/fields",
                json={"name": "account_status", "data_type": "string", "nullable": False},
            ),
            201,
        ),
        "field",
    )
    account_owner_id = require_key(
        assert_status(
            data_model.post(
                f"/v1/tables/{accounts['id']}/fields",
                json={"name": "owner_id", "data_type": "string", "nullable": True},
            ),
            201,
        ),
        "field",
    )
    account_link = require_key(
        assert_status(
            data_model.post(
                f"/v1/tenants/{tenant_id}/links",
                json={
                    "name": "account",
                    "parent_table_id": accounts["id"],
                    "parent_field_id": account_key["id"],
                    "child_table_id": transactions["id"],
                    "child_field_id": fields["account_id"]["id"],
                },
            ),
            201,
        ),
        "link",
    )

    return {
        "tenant": tenant,
        "tenant_id": tenant_id,
        "transactions": transactions,
        "accounts": accounts,
        "fields": fields,
        "account_key": account_key,
        "account_status": account_status,
        "account_owner_id": account_owner_id,
        "account_link": account_link,
    }


@pytest.fixture(scope="session")
def tenant_model(data_model: ApiClient) -> dict[str, Any]:
    return create_tenant_model(data_model)


@pytest.fixture(scope="session")
def scenario_bundle(decision_engine: ApiClient, tenant_model: dict[str, Any]) -> dict[str, Any]:
    tenant_id = tenant_model["tenant_id"]
    object_type = tenant_model["transactions"]["name"]
    scenario = require_key(
        assert_status(
            decision_engine.post(
                f"/v1/tenants/{tenant_id}/scenarios",
                json={"name": unique_name("high amount"), "trigger_object_type": object_type},
            ),
            201,
        ),
        "scenario",
    )
    iteration = require_key(
        assert_status(decision_engine.post(f"/v1/tenants/{tenant_id}/scenarios/{scenario['id']}/iterations"), 201),
        "iteration",
    )
    iteration = require_key(
        assert_status(
            decision_engine.put(
                f"/v1/tenants/{tenant_id}/scenarios/{scenario['id']}/iterations/{iteration['id']}",
                json={
                    "trigger_formula": true_node(),
                    "score_review_threshold": 10,
                    "score_block_and_review_threshold": 30,
                    "score_decline_threshold": 60,
                    "schedule": "",
                },
            ),
            200,
        ),
        "iteration",
    )
    rule = require_key(
        assert_status(
            decision_engine.post(
                f"/v1/tenants/{tenant_id}/scenarios/{scenario['id']}/iterations/{iteration['id']}/rules",
                json={
                    "display_order": 1,
                    "name": "Amount over limit",
                    "description": "Flags records over the integration-test limit",
                    "formula": amount_gt_node(1000),
                    "score_modifier": 25,
                    "rule_group": "default",
                    "stable_rule_id": unique_name("stable_rule"),
                },
            ),
            201,
        ),
        "rule",
    )
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
    return {
        "tenant_id": tenant_id,
        "object_type": object_type,
        "scenario": scenario,
        "iteration": iteration,
        "rule": rule,
    }
