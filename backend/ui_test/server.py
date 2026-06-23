from __future__ import annotations

import copy
import json
import mimetypes
import os
import re
import sys
import time
import uuid
from dataclasses import dataclass, field
from datetime import datetime, timedelta, timezone
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from typing import Any
from urllib.parse import parse_qs, unquote, urlparse

try:
    import httpx
except ImportError:  # pragma: no cover
    print("Missing dependency: httpx. Install with `python3 -m pip install -r ui_test/requirements.txt`.")
    raise


ROOT = Path(__file__).resolve().parents[1]
UI_ROOT = Path(__file__).resolve().parent
STATIC_ROOT = UI_ROOT / "static"
FIXTURE_ROOT = ROOT / "integration-tests" / "decision_engine_rule_fixtures"

DATA_MODEL_URL = os.getenv("DATA_MODEL_URL", "http://localhost:8080").rstrip("/")
INGESTION_URL = os.getenv("INGESTION_URL", "http://localhost:8081").rstrip("/")
DECISION_ENGINE_URL = os.getenv("DECISION_ENGINE_URL", "http://localhost:8082").rstrip("/")
SERVICE_AUTH_TOKEN = os.getenv("SERVICE_AUTH_TOKEN", "")

STAGE_DEFINITIONS = [
    ("tenant", "Tenant", "Create, provision, and retrieve the tenant."),
    ("tables", "Tables", "Create transaction/account tables and retrieve the table list."),
    ("fields", "Fields", "Create fields and links, then retrieve fields, links, and assembled model."),
    ("records", "Records", "Ingest seed/input records and retrieve stored records."),
    ("scenario", "Scenario", "Create scenario and draft iteration."),
    ("rules", "Rules", "Edit, create, and retrieve rules."),
    ("validation", "Validation", "Validate the draft iteration."),
    ("publish", "Publish", "Commit and publish when validation is valid."),
    ("evaluation", "Evaluation", "Evaluate the input payload."),
    ("history", "History", "Retrieve persisted decision and scenario history."),
]

COMPATIBLE_OPERATOR_GROUPS = [
    {"eq", "neq", "gt", "gte", "lt", "lte", ">", ">=", "<", "<=", "="},
    {"and", "or"},
    {"add", "subtract", "multiply", "divide"},
    {"contains", "in", "starts_with", "ends_with"},
]

DEMO_FIXTURE_ALLOWLIST = (
    "core_ast/simple_amount_threshold.json",
    "core_ast/boolean_logical_operations.json",
    "aggregation/velocity_and_account_takeover.json",
    "marble_compat/payload_time_fuzzy.json",
    "core_ast/basic_rule_matrix.json",
    "core_ast/list_null_numeric_edge_cases.json",
)
DEMO_FIXTURE_ORDER = {fixture_id: index for index, fixture_id in enumerate(DEMO_FIXTURE_ALLOWLIST)}


class DemoError(Exception):
    def __init__(self, message: str, status: int = 400, detail: Any | None = None):
        super().__init__(message)
        self.status = status
        self.detail = detail


class ApiClient:
    def __init__(self, name: str, base_url: str):
        headers = {}
        if SERVICE_AUTH_TOKEN:
            headers["Authorization"] = f"Bearer {SERVICE_AUTH_TOKEN}"
        self.name = name
        self.client = httpx.Client(base_url=base_url, timeout=httpx.Timeout(30.0), headers=headers)

    def request(self, session: "Session", method: str, path: str, **kwargs: Any) -> httpx.Response:
        started = time.monotonic()
        response = self.client.request(method, path, **kwargs)
        session.timeline.append(
            {
                "service": self.name,
                "method": method.upper(),
                "path": path,
                "status": response.status_code,
                "duration_ms": int((time.monotonic() - started) * 1000),
                "response": response_snippet(response),
            }
        )
        return response

    def get(self, session: "Session", path: str, **kwargs: Any) -> httpx.Response:
        return self.request(session, "GET", path, **kwargs)

    def post(self, session: "Session", path: str, **kwargs: Any) -> httpx.Response:
        return self.request(session, "POST", path, **kwargs)

    def put(self, session: "Session", path: str, **kwargs: Any) -> httpx.Response:
        return self.request(session, "PUT", path, **kwargs)


DATA_MODEL = ApiClient("data-model", DATA_MODEL_URL)
INGESTION = ApiClient("ingestion", INGESTION_URL)
DECISION_ENGINE = ApiClient("decision-engine", DECISION_ENGINE_URL)


@dataclass
class StageState:
    key: str
    label: str
    description: str
    status: str = "pending"
    summary: str = ""
    definition: dict[str, Any] = field(default_factory=dict)
    created: dict[str, Any] = field(default_factory=dict)
    retrieved: dict[str, Any] = field(default_factory=dict)
    error: str = ""


@dataclass
class Session:
    id: str
    fixture_id: str
    raw_fixture: dict[str, Any]
    edits: dict[str, Any]
    tenant: dict[str, Any] | None = None
    tenant_id: str | None = None
    transactions: dict[str, Any] | None = None
    accounts: dict[str, Any] | None = None
    fields: dict[str, dict[str, Any]] = field(default_factory=dict)
    account_key: dict[str, Any] | None = None
    account_status: dict[str, Any] | None = None
    account_owner_id: dict[str, Any] | None = None
    account_link: dict[str, Any] | None = None
    materialized: dict[str, Any] | None = None
    bundle: dict[str, Any] | None = None
    validation: dict[str, Any] | None = None
    evaluation: dict[str, Any] | None = None
    inspection: dict[str, Any] | None = None
    timeline: list[dict[str, Any]] = field(default_factory=list)
    stages: dict[str, StageState] = field(default_factory=dict)
    created_at: str = field(default_factory=lambda: datetime.now(timezone.utc).isoformat())

    def __post_init__(self) -> None:
        if not self.stages:
            self.stages = {key: StageState(key, label, desc) for key, label, desc in STAGE_DEFINITIONS}


SESSIONS: dict[str, Session] = {}


def main() -> None:
    port = int(os.getenv("UI_TEST_PORT", "8101"))
    server = ThreadingHTTPServer(("127.0.0.1", port), Handler)
    print(f"Interactive Demo: http://127.0.0.1:{port}")
    print("Uses DATA_MODEL_URL, INGESTION_URL, DECISION_ENGINE_URL, SERVICE_AUTH_TOKEN when set.")
    server.serve_forever()


class Handler(BaseHTTPRequestHandler):
    server_version = "FixtureDemo/0.2"

    def do_GET(self) -> None:
        self.route()

    def do_POST(self) -> None:
        self.route()

    def log_message(self, fmt: str, *args: Any) -> None:
        sys.stderr.write("%s - %s\n" % (self.log_date_time_string(), fmt % args))

    def route(self) -> None:
        try:
            parsed = urlparse(self.path)
            if parsed.path.startswith("/api/"):
                self.handle_api(parsed.path, parse_qs(parsed.query))
                return
            self.serve_static(parsed.path)
        except DemoError as exc:
            self.send_json({"error": str(exc), "detail": exc.detail}, exc.status)
        except httpx.HTTPError as exc:
            self.send_json({"error": "Service request failed", "detail": str(exc)}, 502)
        except Exception as exc:  # pragma: no cover
            self.send_json({"error": "Unexpected server error", "detail": str(exc)}, 500)

    def handle_api(self, path: str, query: dict[str, list[str]]) -> None:
        if self.command == "GET" and path == "/api/health":
            self.send_json({"ok": True, "services": service_config()})
            return
        if self.command == "GET" and path == "/api/fixtures":
            self.send_json({"fixtures": list_fixtures()})
            return
        if self.command == "GET" and path.startswith("/api/fixtures/"):
            self.send_json({"fixture": fixture_preview(unquote(path.removeprefix("/api/fixtures/")))})
            return
        if self.command == "POST" and path == "/api/sessions":
            payload = self.read_json()
            session = create_session(payload.get("fixture_id", ""), payload.get("edits") or {})
            self.send_json({"session": serialize_session(session)})
            return
        if path.startswith("/api/sessions/"):
            parts = [unquote(part) for part in path.split("/") if part]
            if len(parts) < 3:
                raise DemoError("Missing session id", 404)
            session = require_session(parts[2])
            if self.command == "GET" and len(parts) == 3:
                self.send_json({"session": serialize_session(session)})
                return
            if self.command == "GET" and len(parts) == 5 and parts[3] == "stages":
                self.send_json({"stage": vars(session.stages[parts[4]])})
                return
            if self.command != "POST":
                raise DemoError("Unsupported route", 405)
            if len(parts) == 4 and parts[3] == "run-all":
                run_all(session)
                self.send_json({"session": serialize_session(session)})
                return
            if len(parts) == 4 and parts[3] == "reset":
                reset_session(session)
                self.send_json({"session": serialize_session(session)})
                return
            if len(parts) == 5 and parts[3] == "steps":
                run_stage(session, parts[4])
                self.send_json({"session": serialize_session(session)})
                return
            if len(parts) == 6 and parts[3] == "stages" and parts[5] == "run":
                run_stage(session, parts[4])
                self.send_json({"session": serialize_session(session)})
                return
        raise DemoError("Not found", 404)

    def serve_static(self, path: str) -> None:
        rel = "index.html" if path in {"", "/"} else path.lstrip("/")
        target = (STATIC_ROOT / rel).resolve()
        if STATIC_ROOT.resolve() not in target.parents and target != STATIC_ROOT.resolve():
            raise DemoError("Not found", 404)
        if not target.exists() or not target.is_file():
            target = STATIC_ROOT / "index.html"
        data = target.read_bytes()
        self.send_response(200)
        self.send_header("Content-Type", mimetypes.guess_type(str(target))[0] or "application/octet-stream")
        self.send_header("Content-Length", str(len(data)))
        self.send_header("Cache-Control", "no-store")
        self.end_headers()
        self.wfile.write(data)

    def read_json(self) -> dict[str, Any]:
        length = int(self.headers.get("content-length", "0") or "0")
        return json.loads(self.rfile.read(length)) if length else {}

    def send_json(self, payload: dict[str, Any], status: int = 200) -> None:
        data = json.dumps(payload, indent=2, sort_keys=True, default=str).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)


def list_fixtures() -> list[dict[str, Any]]:
    fixtures = []
    for path in sorted(FIXTURE_ROOT.rglob("*.json")):
        raw = load_json(path)
        fixture_id = path.relative_to(FIXTURE_ROOT).as_posix()
        if fixture_id not in DEMO_FIXTURE_ORDER:
            continue
        complexity = fixture_complexity(raw)
        fixtures.append(
            {
                "id": fixture_id,
                "name": raw.get("name", path.stem.replace("_", " ")),
                "category": path.relative_to(FIXTURE_ROOT).parts[0],
                "rule_count": len(raw.get("rules", [])),
                "generated_record_count": sum(item.get("count", 0) for item in raw.get("generated_records", [])),
                "record_count": len(raw.get("records", [])),
                "invalid_expected": raw.get("expected", {}).get("validation_valid") is False,
                "description": summarize_fixture(raw),
                "complexity_score": complexity,
                "complexity_label": complexity_label(complexity),
                "demo_visible": True,
                "demo_order": DEMO_FIXTURE_ORDER[fixture_id],
            }
        )
    return sorted(fixtures, key=lambda item: item["demo_order"])


def fixture_preview(fixture_id: str) -> dict[str, Any]:
    raw = load_fixture(fixture_id)
    complexity = fixture_complexity(raw)
    return {
        "id": fixture_id,
        "name": raw.get("name", fixture_id),
        "category": fixture_id.split("/", 1)[0],
        "description": summarize_fixture(raw),
        "complexity_score": complexity,
        "complexity_label": complexity_label(complexity),
        "demo_visible": fixture_id in DEMO_FIXTURE_ORDER,
        "demo_order": DEMO_FIXTURE_ORDER.get(fixture_id),
        "editable": editable_view(raw),
        "rules": rules_view(raw.get("rules", [])),
        "rule_edit_model": rule_edit_model(raw.get("rules", [])),
        "rule_diagram_model": rule_diagram_model(raw.get("rules", [])),
        "expected": raw.get("expected", {}),
        "seed_summary": seed_summary(raw),
        "raw": raw,
    }


def editable_view(raw: dict[str, Any]) -> dict[str, Any]:
    fields = raw.get("input", {}).get("fields", {})
    expected = raw.get("expected", {})
    return {
        "input_fields": {key: value for key, value in fields.items() if editable_scalar(value)},
        "thresholds": raw.get("thresholds", {}),
        "rule_scores": {rule["name"]: rule.get("score_modifier", 1) for rule in raw.get("rules", [])},
        "generated_counts": [item.get("count", 0) for item in raw.get("generated_records", [])],
        "expected": {key: expected[key] for key in ("triggered", "score", "outcome", "validation_valid", "validation_error_contains") if key in expected},
        "expected_rules": expected.get("rules", {}),
        "rule_edits": {"constants": {}, "operators": {}},
        "stage_config": {
            "tenant": {"name_prefix": "demo tenant", "external_key_prefix": "demo_ext"},
            "tables": {
                "transactions_name_prefix": "transactions",
                "transactions_alias": "Transactions",
                "transactions_description": "Demo transactions table",
                "accounts_name_prefix": "accounts",
                "accounts_alias": "Accounts",
                "accounts_description": "Demo accounts table",
            },
        },
    }


def sanitize_edits(edits: dict[str, Any]) -> dict[str, Any]:
    allowed = {"input_fields", "thresholds", "rule_scores", "generated_counts", "expected", "expected_rules", "rule_edits", "stage_config"}
    return {key: copy.deepcopy(value) for key, value in edits.items() if key in allowed}


def create_session(fixture_id: str, edits: dict[str, Any]) -> Session:
    if not fixture_id:
        raise DemoError("fixture_id is required")
    raw = load_fixture(fixture_id)
    validate_rule_edits(raw, edits.get("rule_edits") or {})
    session = Session(str(uuid.uuid4()), fixture_id, raw, sanitize_edits(edits))
    SESSIONS[session.id] = session
    return session


def reset_session(session: Session) -> None:
    session.raw_fixture = load_fixture(session.fixture_id)
    validate_rule_edits(session.raw_fixture, session.edits.get("rule_edits") or {})
    edits = copy.deepcopy(session.edits)
    fresh = Session(session.id, session.fixture_id, session.raw_fixture, edits, created_at=session.created_at)
    session.__dict__.update(fresh.__dict__)


def require_session(session_id: str) -> Session:
    if session_id not in SESSIONS:
        raise DemoError("Session not found", 404)
    return SESSIONS[session_id]


def run_all(session: Session) -> None:
    for key, _, _ in STAGE_DEFINITIONS:
        if session.stages[key].status in {"passed", "skipped"}:
            continue
        run_stage(session, key)
        if session.stages[key].status == "failed":
            break


def run_stage(session: Session, key: str) -> None:
    if key not in session.stages:
        raise DemoError(f"Unknown stage {key}", 404)
    ensure_prior_stages(session, key)
    stage = session.stages[key]
    if stage.status in {"passed", "skipped"}:
        return
    stage.status = "running"
    stage.error = ""
    try:
        definition, created, retrieved, summary, skipped = STAGE_RUNNERS[key](session)
        stage.definition = definition
        stage.created = created
        stage.retrieved = retrieved
        stage.summary = summary
        stage.status = "skipped" if skipped else "passed"
    except Exception as exc:
        stage.status = "failed"
        stage.error = str(exc)
        raise


def ensure_prior_stages(session: Session, key: str) -> None:
    keys = [item[0] for item in STAGE_DEFINITIONS]
    for prior in keys[: keys.index(key)]:
        if session.stages[prior].status == "pending":
            raise DemoError(f"Run '{session.stages[prior].label}' before this stage")
        if session.stages[prior].status == "failed":
            raise DemoError(f"Cannot continue because '{session.stages[prior].label}' failed")


def stage_tenant(session: Session) -> tuple[dict[str, Any], dict[str, Any], dict[str, Any], str, bool]:
    cfg = stage_config(session, "tenant")
    name_prefix = str(cfg.get("name_prefix") or "demo tenant")
    external_key_prefix = str(cfg.get("external_key_prefix") or "demo_ext")
    tenant = require_key(assert_status(DATA_MODEL.post(session, "/v1/tenants", json={"name": unique_name(name_prefix), "external_key": unique_name(external_key_prefix)}), 201), "tenant")
    session.tenant = tenant
    session.tenant_id = tenant["id"]
    assert_status(DATA_MODEL.post(session, f"/v1/tenants/{session.tenant_id}/provision"), 200)
    retrieved = require_key(assert_status(DATA_MODEL.get(session, f"/v1/tenants/{session.tenant_id}"), 200), "tenant")
    return {"tenant_name_prefix": name_prefix, "external_key_prefix": external_key_prefix}, {"tenant": tenant, "provisioned": True}, {"tenant": retrieved}, f"Created and retrieved tenant {tenant['name']}.", False


def stage_tables(session: Session) -> tuple[dict[str, Any], dict[str, Any], dict[str, Any], str, bool]:
    tenant_id = require_tenant(session)
    cfg = stage_config(session, "tables")
    session.transactions = create_table(
        session,
        tenant_id,
        str(cfg.get("transactions_name_prefix") or "transactions"),
        str(cfg.get("transactions_alias") or "Transactions"),
        str(cfg.get("transactions_description") or "Demo transactions table"),
    )
    session.accounts = create_table(
        session,
        tenant_id,
        str(cfg.get("accounts_name_prefix") or "accounts"),
        str(cfg.get("accounts_alias") or "Accounts"),
        str(cfg.get("accounts_description") or "Demo accounts table"),
    )
    tables = require_key(assert_status(DATA_MODEL.get(session, f"/v1/tenants/{tenant_id}/tables"), 200), "tables")
    return {"tables": [session.transactions.get("alias"), session.accounts.get("alias")]}, {"transactions": session.transactions, "accounts": session.accounts}, {"tables": tables}, "Created and retrieved transaction/account tables.", False


def stage_fields(session: Session) -> tuple[dict[str, Any], dict[str, Any], dict[str, Any], str, bool]:
    tenant_id = require_tenant(session)
    if not session.transactions or not session.accounts:
        raise DemoError("Run Tables first")
    for spec in transaction_field_specs():
        created = require_key(assert_status(DATA_MODEL.post(session, f"/v1/tables/{session.transactions['id']}/fields", json=spec), 201), "field")
        session.fields[created["name"]] = created
    session.account_key = create_account_field(session, {"name": "account_key", "data_type": "string", "nullable": False, "is_unique": True})
    session.account_status = create_account_field(session, {"name": "account_status", "data_type": "string", "nullable": False})
    session.account_owner_id = create_account_field(session, {"name": "owner_id", "data_type": "string", "nullable": True})
    session.account_link = require_key(
        assert_status(
            DATA_MODEL.post(
                session,
                f"/v1/tenants/{tenant_id}/links",
                json={
                    "name": "account",
                    "parent_table_id": session.accounts["id"],
                    "parent_field_id": session.account_key["id"],
                    "child_table_id": session.transactions["id"],
                    "child_field_id": session.fields["account_id"]["id"],
                },
            ),
            201,
        ),
        "link",
    )
    model_context = tenant_model_context(session)
    session.materialized = materialize_case(session.raw_fixture, model_context, session.edits)
    txn_fields = require_key(assert_status(DATA_MODEL.get(session, f"/v1/tables/{session.transactions['id']}/fields"), 200), "fields")
    account_fields = require_key(assert_status(DATA_MODEL.get(session, f"/v1/tables/{session.accounts['id']}/fields"), 200), "fields")
    links = require_key(assert_status(DATA_MODEL.get(session, f"/v1/tenants/{tenant_id}/links"), 200), "links")
    assembled = require_key(assert_status(DATA_MODEL.get(session, f"/v1/tenants/{tenant_id}/data-model"), 200), "data_model")
    return (
        {"transaction_fields": [item["name"] for item in transaction_field_specs()], "account_fields": ["account_key", "account_status", "owner_id"]},
        {"fields": session.fields, "account_link": session.account_link},
        {"transaction_fields": txn_fields, "account_fields": account_fields, "links": links, "data_model": summarized_model(assembled)},
        "Created fields/link and retrieved assembled data model.",
        False,
    )


def stage_records(session: Session) -> tuple[dict[str, Any], dict[str, Any], dict[str, Any], str, bool]:
    case = require_materialized(session)
    tenant_id = case["tenant_id"]
    seed_decisions = 0
    records = 0
    generated = 0
    samples: list[dict[str, Any]] = []
    for seed in case.get("seed_decisions", []):
        seed_bundle = create_published_scenario(session, tenant_id, seed)
        evaluate_case(session, tenant_id, seed_bundle, seed["input"])
        seed_decisions += 1
    for record in case.get("records", []):
        assert_status(INGESTION.post(session, f"/v1/tenants/{tenant_id}/ingest/{record['object_type']}", json=record["body"]), 200)
        records += 1
        samples.append(get_record(session, tenant_id, record["object_type"], record["body"].get("object_id")))
    for batch in case.get("generated_records", []):
        rows = build_generated_records(batch)
        for chunk in chunks(rows, 500):
            payload = assert_status(INGESTION.post(session, f"/v1/tenants/{tenant_id}/ingest/{batch['object_type']}/batch", json=chunk), 200)
            generated += len(require_key(payload, "results"))
        if rows:
            samples.append(get_record(session, tenant_id, batch["object_type"], rows[0].get("object_id")))
    input_record = case.get("input", {}).get("fields")
    input_object_type = case.get("input", {}).get("object_type")
    if input_record and input_object_type and case.get("expected", {}).get("validation_valid") is not False:
        assert_status(INGESTION.post(session, f"/v1/tenants/{tenant_id}/ingest/{input_object_type}", json=input_record), 200)
        samples.append(get_record(session, tenant_id, input_object_type, input_record.get("object_id")))
    listed = {}
    if input_object_type:
        listed = assert_status(INGESTION.get(session, f"/v1/tenants/{tenant_id}/records/{input_object_type}", params={"limit": 20}), 200)
    return (
        seed_summary(case),
        {"seed_decisions": seed_decisions, "records": records, "generated_records": generated, "input_record_ingested": bool(input_record)},
        {"samples": samples, "list": listed},
        f"Seeded {seed_decisions} decisions, {records} records, {generated} generated rows, and input record.",
        False,
    )


def stage_scenario(session: Session) -> tuple[dict[str, Any], dict[str, Any], dict[str, Any], str, bool]:
    case = require_materialized(session)
    scenario = require_key(assert_status(DECISION_ENGINE.post(session, f"/v1/tenants/{case['tenant_id']}/scenarios", json={"name": unique_name(case["name"]), "trigger_object_type": case["object_type"]}), 201), "scenario")
    iteration = require_key(assert_status(DECISION_ENGINE.post(session, f"/v1/tenants/{case['tenant_id']}/scenarios/{scenario['id']}/iterations"), 201), "iteration")
    thresholds = case.get("thresholds", {})
    iteration = require_key(
        assert_status(
            DECISION_ENGINE.put(
                session,
                f"/v1/tenants/{case['tenant_id']}/scenarios/{scenario['id']}/iterations/{iteration['id']}",
                json={
                    "trigger_formula": case["trigger"],
                    "score_review_threshold": thresholds.get("review", 1),
                    "score_block_and_review_threshold": thresholds.get("block_and_review", 50),
                    "score_decline_threshold": thresholds.get("decline", 100),
                    "schedule": case.get("schedule", ""),
                },
            ),
            200,
        ),
        "iteration",
    )
    session.bundle = {"scenario": scenario, "iteration": iteration, "rules": []}
    scenarios = require_key(assert_status(DECISION_ENGINE.get(session, f"/v1/tenants/{case['tenant_id']}/scenarios"), 200), "scenarios")
    iterations = require_key(assert_status(DECISION_ENGINE.get(session, f"/v1/tenants/{case['tenant_id']}/scenarios/{scenario['id']}/iterations"), 200), "iterations")
    return {"name": case["name"], "thresholds": thresholds}, {"scenario": scenario, "iteration": iteration}, {"scenarios": scenarios, "iterations": iterations}, f"Created scenario draft {scenario['name']}.", False


def stage_rules(session: Session) -> tuple[dict[str, Any], dict[str, Any], dict[str, Any], str, bool]:
    case = require_materialized(session)
    bundle = require_bundle(session)
    rules = []
    for index, rule in enumerate(case.get("rules", []), start=1):
        created = require_key(
            assert_status(
                DECISION_ENGINE.post(
                    session,
                    f"/v1/tenants/{case['tenant_id']}/scenarios/{bundle['scenario']['id']}/iterations/{bundle['iteration']['id']}/rules",
                    json={
                        "display_order": index,
                        "name": rule["name"],
                        "description": rule.get("description", rule["name"]),
                        "formula": rule["formula"],
                        "score_modifier": rule.get("score_modifier", 1),
                        "rule_group": rule.get("rule_group", "fixture-demo"),
                        "stable_rule_id": unique_name(rule["name"]),
                    },
                ),
                201,
            ),
            "rule",
        )
        rules.append(created)
    bundle["rules"] = rules
    retrieved = assert_status(DECISION_ENGINE.get(session, f"/v1/tenants/{case['tenant_id']}/scenarios/{bundle['scenario']['id']}/iterations/{bundle['iteration']['id']}/rules"), 200)
    return {"rules": rules_view(case.get("rules", [])), "flowchart": rule_diagram_model(case.get("rules", []))}, {"rules": rules}, retrieved, f"Created and retrieved {len(rules)} rules.", False


def stage_validation(session: Session) -> tuple[dict[str, Any], dict[str, Any], dict[str, Any], str, bool]:
    case = require_materialized(session)
    bundle = require_bundle(session)
    payload = assert_status(DECISION_ENGINE.post(session, f"/v1/tenants/{case['tenant_id']}/scenarios/{bundle['scenario']['id']}/iterations/{bundle['iteration']['id']}/validate"), {200, 422})
    session.validation = validation_payload(payload)
    comparison = compare_validation(session.validation, case.get("expected", {}))
    return {"expected": case.get("expected", {})}, {"validation": session.validation}, {"comparison": comparison}, f"Validation is {'valid' if session.validation.get('valid') else 'invalid'}.", False


def stage_publish(session: Session) -> tuple[dict[str, Any], dict[str, Any], dict[str, Any], str, bool]:
    case = require_materialized(session)
    bundle = require_bundle(session)
    if (session.validation or {}).get("valid") is False:
        return {}, {}, {"reason": "Validation is invalid."}, "Skipped publish because validation is invalid.", True
    assert_status(DECISION_ENGINE.post(session, f"/v1/tenants/{case['tenant_id']}/scenarios/{bundle['scenario']['id']}/iterations/{bundle['iteration']['id']}/commit"), 200)
    publication = assert_status(DECISION_ENGINE.post(session, f"/v1/tenants/{case['tenant_id']}/scenarios/{bundle['scenario']['id']}/publications", json={"action": "publish", "iteration_id": bundle["iteration"]["id"]}), 200)
    publications = assert_status(DECISION_ENGINE.get(session, f"/v1/tenants/{case['tenant_id']}/scenarios/{bundle['scenario']['id']}/publications"), 200)
    return {"action": "publish"}, publication, publications, "Published scenario iteration.", False


def stage_evaluation(session: Session) -> tuple[dict[str, Any], dict[str, Any], dict[str, Any], str, bool]:
    case = require_materialized(session)
    if case.get("expected", {}).get("validation_valid") is False:
        return {}, {}, {"reason": "Fixture expects validation failure."}, "Skipped evaluation for validation-failure fixture.", True
    result = evaluate_case(session, case["tenant_id"], require_bundle(session), case["input"])
    session.evaluation = result
    comparison = compare_result(result, case.get("expected", {}), case.get("rules", []))
    return {"input": case["input"], "expected": case.get("expected", {})}, {"result": result}, {"comparison": comparison}, comparison["summary"], False


def stage_history(session: Session) -> tuple[dict[str, Any], dict[str, Any], dict[str, Any], str, bool]:
    case = require_materialized(session)
    bundle = require_bundle(session)
    decision = (session.evaluation or {}).get("decision")
    if not decision:
        return {}, {}, {"reason": "No decision was created."}, "Skipped history because there is no decision.", True
    persisted = assert_status(DECISION_ENGINE.get(session, f"/v1/tenants/{case['tenant_id']}/decisions/{decision['id']}"), 200)
    decisions = require_key(assert_status(DECISION_ENGINE.get(session, f"/v1/tenants/{case['tenant_id']}/scenarios/{bundle['scenario']['id']}/decisions"), 200), "decisions")
    session.inspection = {"persisted": persisted, "listing_count": len(decisions), "in_listing": any(item["id"] == decision["id"] for item in decisions)}
    return {"decision_id": decision["id"]}, {}, session.inspection, f"Retrieved persisted decision {decision['id']}.", False


STAGE_RUNNERS = {
    "tenant": stage_tenant,
    "tables": stage_tables,
    "fields": stage_fields,
    "records": stage_records,
    "scenario": stage_scenario,
    "rules": stage_rules,
    "validation": stage_validation,
    "publish": stage_publish,
    "evaluation": stage_evaluation,
    "history": stage_history,
}


def create_table(session: Session, tenant_id: str, name_prefix: str, alias: str, description: str) -> dict[str, Any]:
    return require_key(
        assert_status(DATA_MODEL.post(session, f"/v1/tenants/{tenant_id}/tables", json={"name": unique_name(name_prefix), "description": description, "alias": alias, "semantic_type": "entity"}), 201),
        "table",
    )


def create_account_field(session: Session, spec: dict[str, Any]) -> dict[str, Any]:
    if not session.accounts:
        raise DemoError("Run Tables first")
    return require_key(assert_status(DATA_MODEL.post(session, f"/v1/tables/{session.accounts['id']}/fields", json=spec), 201), "field")


def create_published_scenario(session: Session, tenant_id: str, spec: dict[str, Any]) -> dict[str, Any]:
    scenario = require_key(assert_status(DECISION_ENGINE.post(session, f"/v1/tenants/{tenant_id}/scenarios", json={"name": unique_name(spec["name"]), "trigger_object_type": spec["object_type"]}), 201), "scenario")
    iteration = require_key(assert_status(DECISION_ENGINE.post(session, f"/v1/tenants/{tenant_id}/scenarios/{scenario['id']}/iterations"), 201), "iteration")
    thresholds = spec.get("thresholds", {})
    iteration = require_key(
        assert_status(
            DECISION_ENGINE.put(
                session,
                f"/v1/tenants/{tenant_id}/scenarios/{scenario['id']}/iterations/{iteration['id']}",
                json={"trigger_formula": spec["trigger"], "score_review_threshold": thresholds.get("review", 1), "score_block_and_review_threshold": thresholds.get("block_and_review", 50), "score_decline_threshold": thresholds.get("decline", 100), "schedule": spec.get("schedule", "")},
            ),
            200,
        ),
        "iteration",
    )
    rules = []
    for index, rule in enumerate(spec.get("rules", []), start=1):
        rules.append(
            require_key(
                assert_status(
                    DECISION_ENGINE.post(session, f"/v1/tenants/{tenant_id}/scenarios/{scenario['id']}/iterations/{iteration['id']}/rules", json={"display_order": index, "name": rule["name"], "description": rule.get("description", rule["name"]), "formula": rule["formula"], "score_modifier": rule.get("score_modifier", 1), "rule_group": rule.get("rule_group", "fixture-demo"), "stable_rule_id": unique_name(rule["name"])}),
                    201,
                ),
                "rule",
            )
        )
    validation = validation_payload(assert_status(DECISION_ENGINE.post(session, f"/v1/tenants/{tenant_id}/scenarios/{scenario['id']}/iterations/{iteration['id']}/validate"), {200, 422}))
    if validation.get("valid") is not True:
        raise DemoError("Seed decision scenario did not validate", 502, validation)
    assert_status(DECISION_ENGINE.post(session, f"/v1/tenants/{tenant_id}/scenarios/{scenario['id']}/iterations/{iteration['id']}/commit"), 200)
    assert_status(DECISION_ENGINE.post(session, f"/v1/tenants/{tenant_id}/scenarios/{scenario['id']}/publications", json={"action": "publish", "iteration_id": iteration["id"]}), 200)
    return {"scenario": scenario, "iteration": iteration, "rules": rules}


def evaluate_case(session: Session, tenant_id: str, bundle: dict[str, Any], input_payload: dict[str, Any]) -> dict[str, Any]:
    return require_key(assert_status(DECISION_ENGINE.post(session, f"/v1/tenants/{tenant_id}/scenarios/{bundle['scenario']['id']}/evaluate", json=input_payload), 200), "result")


def get_record(session: Session, tenant_id: str, object_type: str, object_id: Any) -> dict[str, Any]:
    if not object_id:
        return {}
    payload = assert_status(INGESTION.get(session, f"/v1/tenants/{tenant_id}/records/{object_type}/{object_id}"), 200)
    return payload.get("record", payload)


def materialize_case(raw_case: dict[str, Any], tenant_model: dict[str, Any], edits: dict[str, Any]) -> dict[str, Any]:
    case = copy.deepcopy(raw_case)
    apply_edits(case, edits)
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


def apply_edits(case: dict[str, Any], edits: dict[str, Any]) -> None:
    for key, value in (edits.get("input_fields") or {}).items():
        fields = case.get("input", {}).get("fields", {})
        if key in fields and editable_scalar(value):
            case["input"]["fields"][key] = coerce_constant(value, fields[key])
            if key == "object_id":
                case["input"]["object_id"] = case["input"]["fields"][key]
    for key, value in (edits.get("thresholds") or {}).items():
        if key in {"review", "block_and_review", "decline"} and isinstance(value, (int, float)):
            case.setdefault("thresholds", {})[key] = value
    for rule in case.get("rules", []):
        scores = edits.get("rule_scores") or {}
        if rule.get("name") in scores and isinstance(scores[rule["name"]], (int, float)):
            rule["score_modifier"] = scores[rule["name"]]
    apply_rule_edits(case, edits.get("rule_edits") or {})
    for index, count in enumerate(edits.get("generated_counts") or []):
        if index < len(case.get("generated_records", [])) and isinstance(count, int) and 0 <= count <= 5000:
            case["generated_records"][index]["count"] = count
    expected = edits.get("expected") or {}
    for key in ("triggered", "score", "outcome", "validation_valid", "validation_error_contains"):
        if key in expected:
            case.setdefault("expected", {})[key] = expected[key]
    for rule_name, outcome in (edits.get("expected_rules") or {}).items():
        if outcome in {"hit", "no_hit", "error"}:
            case.setdefault("expected", {}).setdefault("rules", {})[rule_name] = outcome


def rule_edit_model(rules: list[dict[str, Any]]) -> list[dict[str, Any]]:
    return [{"rule": rule.get("name"), "score_modifier": rule.get("score_modifier", 1), "formula": humanize_formula(rule.get("formula", {})), "nodes": ast_nodes(rule.get("formula", {}), f"rules[{idx}].formula")} for idx, rule in enumerate(rules)]


def rule_diagram_model(rules: list[dict[str, Any]]) -> list[dict[str, Any]]:
    return [
        {
            "rule": rule.get("name"),
            "score_modifier": rule.get("score_modifier", 1),
            "formula": humanize_formula(rule.get("formula", {})),
            "conditions": diagram_conditions(rule.get("formula", {}), f"rules[{idx}].formula"),
        }
        for idx, rule in enumerate(rules)
    ]


def diagram_conditions(node: Any, path: str, joiner: str = "") -> list[dict[str, Any]]:
    if not isinstance(node, dict):
        return []
    function = node.get("function") or node.get("name")
    children = node.get("children") or []
    if function in {"and", "or"}:
        conditions: list[dict[str, Any]] = []
        for index, child in enumerate(children):
            conditions.extend(diagram_conditions(child, f"{path}.children[{index}]", function.upper() if index else joiner))
        return conditions

    condition = {
        "path": path,
        "joiner": joiner,
        "title": diagram_title(node),
        "left": "",
        "operator": function or "value",
        "operator_symbol": BINARY_OPERATORS.get(function or "", function or "value"),
        "right": "",
        "formula": humanize_formula(node),
        "operator_edit": None,
        "constant_edits": diagram_constant_edits(node, path),
    }
    if function in BINARY_OPERATORS and len(children) >= 2:
        condition["left"] = compact_formula(children[0])
        condition["right"] = compact_formula(children[1])
    else:
        condition["left"] = compact_formula(node)
        condition["right"] = ""
    group = operator_group(function)
    if group:
        condition["operator_edit"] = {"path": path, "options": sorted(group), "value": function}
    return [condition]


def diagram_constant_edits(node: Any, path: str) -> list[dict[str, Any]]:
    edits: list[dict[str, Any]] = []
    collect_constant_edits(node, path, edits)
    return edits[:3]


def collect_constant_edits(node: Any, path: str, edits: list[dict[str, Any]]) -> None:
    if not isinstance(node, dict):
        return
    function = node.get("function") or node.get("name")
    if "constant" in node and (editable_scalar(node["constant"]) or isinstance(node["constant"], list)):
        edits.append({"path": path, "label": "Value", "value": node["constant"]})
        return
    if function in {"field_ref", "Payload", "payload"}:
        return
    for key, value in node.items():
        if isinstance(value, dict):
            collect_constant_edits(value, f"{path}.{key}", edits)
        elif isinstance(value, list):
            for index, child in enumerate(value):
                collect_constant_edits(child, f"{path}.{key}[{index}]", edits)


def diagram_title(node: Any) -> str:
    if not isinstance(node, dict):
        return "Condition"
    function = node.get("function") or node.get("name") or "Condition"
    if function in {"eq", "neq", "gt", "gte", "lt", "lte", ">", ">=", "<", "<=", "="}:
        return "Compare values"
    if function in {"contains", "in", "starts_with", "ends_with"}:
        return "Check text or list"
    if function in {"add", "subtract", "multiply", "divide"}:
        return "Calculate value"
    return function.replace("_", " ").title()


def compact_formula(node: Any) -> str:
    text = humanize_formula(node)
    text = re.sub(r"^\((.*)\)$", r"\1", text)
    text = text.replace("related_records", "related records")
    text = text.replace("map_field", "values from")
    text = text.replace("list_count", "count")
    text = text.replace("FuzzyMatch", "fuzzy match")
    return text


def ast_nodes(node: Any, path: str) -> list[dict[str, Any]]:
    out: list[dict[str, Any]] = []
    if not isinstance(node, dict):
        return out
    function = node.get("function") or node.get("name")
    item = {"path": path, "kind": "node", "label": function or "object", "human": humanize_formula(node), "editable_operator": None, "editable_constant": False, "value": None}
    group = operator_group(function)
    if group:
        item["editable_operator"] = sorted(group)
        item["value"] = function
    if "constant" in node:
        item["kind"] = "constant"
        item["label"] = "constant"
        item["editable_constant"] = editable_scalar(node["constant"]) or isinstance(node["constant"], list)
        item["value"] = node["constant"]
    elif function in {"field_ref", "Payload", "payload"}:
        item["kind"] = "field"
        item["label"] = field_name((node.get("named_children") or {}).get("field") or ((node.get("children") or [None])[0]))
    out.append(item)
    if function in {"field_ref", "Payload", "payload"}:
        return out
    for key, value in node.items():
        if isinstance(value, dict):
            out.extend(ast_nodes(value, f"{path}.{key}"))
        elif isinstance(value, list):
            for index, child in enumerate(value):
                out.extend(ast_nodes(child, f"{path}.{key}[{index}]"))
    return out


def validate_rule_edits(raw_case: dict[str, Any], rule_edits: dict[str, Any]) -> None:
    editable = {}
    for model in rule_edit_model(raw_case.get("rules", [])):
        for node in model["nodes"]:
            editable[node["path"]] = node
    for path in (rule_edits.get("constants") or {}):
        if path not in editable or not editable[path].get("editable_constant"):
            raise DemoError(f"Cannot edit constant at {path}")
    for path, value in (rule_edits.get("operators") or {}).items():
        node = editable.get(path)
        if not node or not node.get("editable_operator") or value not in node["editable_operator"]:
            raise DemoError(f"Cannot change operator at {path} to {value}")


def apply_rule_edits(case: dict[str, Any], rule_edits: dict[str, Any]) -> None:
    validate_rule_edits(case, rule_edits)
    for path, value in (rule_edits.get("constants") or {}).items():
        node = path_get(case, path)
        if not isinstance(node, dict) or "constant" not in node:
            raise DemoError(f"Invalid constant path {path}")
        node["constant"] = coerce_constant(value, node["constant"])
    for path, value in (rule_edits.get("operators") or {}).items():
        node = path_get(case, path)
        if not isinstance(node, dict):
            raise DemoError(f"Invalid operator path {path}")
        if "function" in node:
            node["function"] = value
        elif "name" in node:
            node["name"] = value


def path_get(root: Any, path: str) -> Any:
    current = root
    for part in re.finditer(r"([A-Za-z_][A-Za-z0-9_]*)|\[(\d+)\]", path):
        key, index = part.group(1), part.group(2)
        current = current[int(index)] if index is not None else current[key]
    return current


def operator_group(function: str | None) -> set[str] | None:
    for group in COMPATIBLE_OPERATOR_GROUPS:
        if function in group:
            return group
    return None


def coerce_constant(value: Any, old: Any) -> Any:
    if isinstance(old, bool):
        return value if isinstance(value, bool) else str(value).lower() == "true"
    if isinstance(old, int) and not isinstance(old, bool):
        return int(value)
    if isinstance(old, float):
        return float(value)
    if isinstance(old, list):
        return value if isinstance(value, list) else [item.strip() for item in str(value).split(",") if item.strip()]
    if old is None and value == "":
        return None
    return value


def transaction_field_specs() -> list[dict[str, Any]]:
    return [
        {"name": "amount", "data_type": "int", "nullable": False},
        {"name": "status", "data_type": "string", "nullable": False, "is_enum": True, "enum_values": [{"value": "pending", "label": "Pending", "sort_order": 10}]},
        {"name": "account_id", "data_type": "string", "nullable": True},
        {"name": "ip", "data_type": "ip_address", "nullable": True},
        {"name": "merchant", "data_type": "string", "nullable": False},
        {"name": "email", "data_type": "string", "nullable": False},
        {"name": "country", "data_type": "string", "nullable": False},
        {"name": "owner_id", "data_type": "string", "nullable": True},
        {"name": "event_time", "data_type": "timestamp", "nullable": True},
        {"name": "note", "data_type": "string", "nullable": True},
    ]


def tenant_model_context(session: Session) -> dict[str, Any]:
    return {
        "tenant": session.tenant,
        "tenant_id": session.tenant_id,
        "transactions": session.transactions,
        "accounts": session.accounts,
        "fields": session.fields,
        "account_key": session.account_key,
        "account_status": session.account_status,
        "account_owner_id": session.account_owner_id,
        "account_link": session.account_link,
    }


def summarized_model(model: dict[str, Any]) -> dict[str, Any]:
    return {"tables": sorted((model.get("tables") or {}).keys()), "ingestion_contract": model.get("ingestion_contract")}


def require_tenant(session: Session) -> str:
    if not session.tenant_id:
        raise DemoError("Run Tenant first")
    return session.tenant_id


def require_materialized(session: Session) -> dict[str, Any]:
    if not session.materialized:
        raise DemoError("Run Fields first")
    return session.materialized


def require_bundle(session: Session) -> dict[str, Any]:
    if not session.bundle:
        raise DemoError("Run Scenario first")
    return session.bundle


def stage_config(session: Session, stage_key: str) -> dict[str, Any]:
    config = (session.edits.get("stage_config") or {}).get(stage_key)
    return config if isinstance(config, dict) else {}


def service_config() -> dict[str, str]:
    return {"data_model": DATA_MODEL_URL, "ingestion": INGESTION_URL, "decision_engine": DECISION_ENGINE_URL, "auth": "token" if SERVICE_AUTH_TOKEN else "none"}


def fixture_complexity(raw: dict[str, Any]) -> float:
    rules = raw.get("rules", [])
    generated = sum(item.get("count", 0) for item in raw.get("generated_records", []))
    records = len(raw.get("records", []))
    seeds = len(raw.get("seed_decisions", []))
    nodes = sum(count_formula_nodes(rule.get("formula", {})) for rule in rules)
    return round(nodes + (len(rules) * 8) + (generated / 10) + (records * 3) + (seeds * 12), 1)


def count_formula_nodes(value: Any) -> int:
    if isinstance(value, dict):
        return 1 + sum(count_formula_nodes(item) for item in value.values())
    if isinstance(value, list):
        return sum(count_formula_nodes(item) for item in value)
    return 0


def complexity_label(score: float) -> str:
    if score < 25:
        return "starter"
    if score < 60:
        return "focused"
    if score < 100:
        return "intermediate"
    return "advanced"


def summarize_fixture(raw: dict[str, Any]) -> str:
    expected = raw.get("expected", {})
    if expected.get("validation_valid") is False:
        return "Validation failure case"
    if expected.get("triggered") is False:
        return "Trigger miss case with no decision"
    if expected.get("outcome"):
        return f"Expected {expected['outcome'].replace('_', ' ')} decision with score {expected.get('score')}"
    return "Decision-engine rule fixture"


def editable_scalar(value: Any) -> bool:
    return value is None or isinstance(value, (str, int, float, bool))


def rules_view(rules: list[dict[str, Any]]) -> list[dict[str, Any]]:
    return [{"name": rule.get("name"), "description": rule.get("description", ""), "score_modifier": rule.get("score_modifier", 1), "formula": humanize_formula(rule.get("formula", {}))} for rule in rules]


def seed_summary(raw: dict[str, Any]) -> dict[str, Any]:
    return {
        "seed_decisions": len(raw.get("seed_decisions", [])),
        "records": len(raw.get("records", [])),
        "generated_batches": [{"object_type": batch.get("object_type"), "count": batch.get("count", 0)} for batch in raw.get("generated_records", [])],
    }


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
    return [resolve_generated_tokens(copy.deepcopy(spec["template"]), index) for index in range(spec["count"])]


def resolve_generated_tokens(value: Any, index: int) -> Any:
    if isinstance(value, str):
        return value.replace("$index_mod_10", str(index % 10)).replace("$index_mod_5", str(index % 5)).replace("$index_mod_4", str(index % 4)).replace("$index_mod_3", str(index % 3)).replace("$index_mod_2", str(index % 2)).replace("$index", str(index))
    if isinstance(value, list):
        return [resolve_generated_tokens(item, index) for item in value]
    if isinstance(value, dict):
        return {key: resolve_generated_tokens(item, index) for key, item in value.items()}
    return value


def chunks(items: list[dict[str, Any]], size: int) -> list[list[dict[str, Any]]]:
    return [items[index : index + size] for index in range(0, len(items), size)]


def compare_validation(validation: dict[str, Any], expected: dict[str, Any]) -> dict[str, Any]:
    if "validation_valid" not in expected:
        return {"matches": True, "expected": None, "actual": validation.get("valid")}
    matches = validation.get("valid") is expected.get("validation_valid")
    fragment = expected.get("validation_error_contains")
    if fragment:
        matches = matches and fragment in json.dumps(validation, sort_keys=True)
    return {"matches": matches, "expected": expected.get("validation_valid"), "actual": validation.get("valid"), "fragment": fragment}


def compare_result(result: dict[str, Any], expected: dict[str, Any], rules: list[dict[str, Any]]) -> dict[str, Any]:
    comparison: dict[str, Any] = {"matches": True, "triggered": {"expected": expected.get("triggered"), "actual": result.get("triggered")}, "score": {"expected": expected.get("score"), "actual": None}, "outcome": {"expected": expected.get("outcome"), "actual": None}, "rules": []}
    if result.get("triggered") != expected.get("triggered"):
        comparison["matches"] = False
    decision = result.get("decision")
    if decision:
        comparison["score"]["actual"] = decision.get("score")
        comparison["outcome"]["actual"] = decision.get("outcome")
        if "score" in expected and decision.get("score") != expected.get("score"):
            comparison["matches"] = False
        if "outcome" in expected and decision.get("outcome") != expected.get("outcome"):
            comparison["matches"] = False
    executions = {item.get("rule_name"): item for item in result.get("rule_executions", [])}
    rules_by_name = {rule.get("name"): rule for rule in rules}
    for rule_name, expected_outcome in expected.get("rules", {}).items():
        actual = executions.get(rule_name, {}).get("outcome")
        matched = actual == expected_outcome
        comparison["rules"].append({"name": rule_name, "expected": expected_outcome, "actual": actual, "matches": matched, "formula": humanize_formula(rules_by_name.get(rule_name, {}).get("formula", {}))})
        if not matched:
            comparison["matches"] = False
    comparison["summary"] = result_summary(comparison)
    return comparison


def result_summary(comparison: dict[str, Any]) -> str:
    status = "matches" if comparison["matches"] else "differs from"
    if comparison.get("outcome", {}).get("actual"):
        return f"Actual {comparison['outcome']['actual'].replace('_', ' ')} decision with score {comparison['score']['actual']} {status} expectations."
    return f"Evaluation {status} expectations."


def validation_payload(payload: dict[str, Any]) -> dict[str, Any]:
    return payload["validation"] if "validation" in payload else require_key(payload, "result")


def assert_status(response: httpx.Response, expected: int | set[int]) -> dict[str, Any]:
    expected_set = {expected} if isinstance(expected, int) else expected
    if response.status_code not in expected_set:
        raise DemoError(f"{response.request.method} {response.request.url} returned {response.status_code}", 502, response.text[:2000])
    return response.json() if response.content else {}


def require_key(payload: dict[str, Any], key: str) -> Any:
    if key not in payload:
        raise DemoError(f"Missing response key {key}", 502, payload)
    return payload[key]


def response_snippet(response: httpx.Response) -> str:
    if not response.content:
        return ""
    try:
        return json.dumps(response.json(), sort_keys=True, default=str)[:2000]
    except ValueError:
        return response.text[:2000]


def load_fixture(fixture_id: str) -> dict[str, Any]:
    path = (FIXTURE_ROOT / fixture_id).resolve()
    if FIXTURE_ROOT.resolve() not in path.parents or path.suffix != ".json" or not path.exists():
        raise DemoError("Fixture not found", 404)
    return load_json(path)


def load_json(path: Path) -> dict[str, Any]:
    return json.loads(path.read_text(encoding="utf-8"))


def unique_name(prefix: str) -> str:
    safe = re.sub(r"[^a-zA-Z0-9_]+", "_", prefix).strip("_") or "demo"
    return f"{safe}_{uuid.uuid4().hex[:12]}"


def serialize_session(session: Session) -> dict[str, Any]:
    materialized_view = None
    if session.materialized:
        materialized_view = {"name": session.materialized.get("name"), "tenant_id": session.materialized.get("tenant_id"), "object_type": session.materialized.get("object_type"), "input": session.materialized.get("input"), "thresholds": session.materialized.get("thresholds", {}), "rules": rules_view(session.materialized.get("rules", [])), "rule_edit_model": rule_edit_model(session.materialized.get("rules", [])), "rule_diagram_model": rule_diagram_model(session.materialized.get("rules", [])), "expected": session.materialized.get("expected", {})}
    return {
        "id": session.id,
        "fixture_id": session.fixture_id,
        "created_at": session.created_at,
        "edits": session.edits,
        "fixture": fixture_preview(session.fixture_id),
        "materialized": materialized_view,
        "stages": [vars(session.stages[key]) for key, _, _ in STAGE_DEFINITIONS],
        "steps": [vars(session.stages[key]) for key, _, _ in STAGE_DEFINITIONS],
        "timeline": session.timeline,
        "result": {"validation": session.validation, "evaluation": session.evaluation, "inspection": session.inspection},
        "services": service_config(),
    }


def humanize_formula(node: Any) -> str:
    if not isinstance(node, dict):
        return repr(node)
    if "constant" in node:
        return humanize_constant(node["constant"])
    function = node.get("function") or node.get("name")
    children = node.get("children") or []
    named = node.get("named_children") or {}
    if function == "constant":
        return humanize_constant(node.get("constant"))
    if function == "field_ref":
        return field_name(named.get("field") or (children[0] if children else None))
    if function in {"Payload", "payload"}:
        return field_name(children[0] if children else named.get("field"))
    if function in BINARY_OPERATORS and len(children) >= 2:
        return f"({humanize_formula(children[0])} {BINARY_OPERATORS[function]} {humanize_formula(children[1])})"
    if function in {"and", "or"}:
        return "(" + f" {function.upper()} ".join(humanize_formula(child) for child in children) + ")"
    if function == "not" and children:
        return f"NOT {humanize_formula(children[0])}"
    if function == "List":
        return "[" + ", ".join(humanize_formula(child) for child in children) + "]"
    if function in {"contains", "starts_with", "ends_with", "in"} and len(children) >= 2:
        return f"{function.replace('_', ' ')}({humanize_formula(children[0])}, {humanize_formula(children[1])})"
    if function and named:
        args = ", ".join(f"{key}={humanize_formula(value)}" for key, value in named.items())
        if children:
            args = f"{', '.join(humanize_formula(child) for child in children)}, {args}"
        return f"{function}({args})"
    if function:
        return f"{function}(" + ", ".join(humanize_formula(child) for child in children) + ")"
    return json.dumps(node, sort_keys=True)


def field_name(node: Any) -> str:
    return str(node["constant"]) if isinstance(node, dict) and "constant" in node else humanize_formula(node)


def humanize_constant(value: Any) -> str:
    if isinstance(value, str):
        return repr(value)
    if value is True:
        return "true"
    if value is False:
        return "false"
    if value is None:
        return "null"
    return str(value)


BINARY_OPERATORS = {"eq": "=", "neq": "!=", "gt": ">", "gte": ">=", "lt": "<", "lte": "<=", "add": "+", "subtract": "-", "multiply": "*", "divide": "/", ">": ">", ">=": ">=", "<": "<", "<=": "<=", "=": "="}


if __name__ == "__main__":
    main()
