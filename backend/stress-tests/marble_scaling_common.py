from __future__ import annotations

import asyncio
import json
import os
import platform
import statistics
import subprocess
import sys
import time
import uuid
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Callable, Awaitable

import httpx


SUPPORTED_VARIANTS = [
    "baseline_payload",
    "nested_payload",
    "custom_list",
    "related_field",
    "aggregate_count_pushdown",
    "mixed_heavy",
]


def utc_now() -> str:
    return datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")


def unique_name(prefix: str) -> str:
    return f"{prefix}_{uuid.uuid4().hex[:12]}"


def percentile(values: list[float], pct: float) -> float | None:
    if not values:
        return None
    ordered = sorted(values)
    position = (pct / 100.0) * (len(ordered) - 1)
    lower = int(position)
    upper = min(lower + 1, len(ordered) - 1)
    if lower == upper:
        return ordered[lower]
    weight = position - lower
    return ordered[lower] + (ordered[upper] - ordered[lower]) * weight


def format_optional(value: float | None) -> str:
    return "n/a" if value is None else f"{value:.2f}"


def run_command(args: list[str], timeout_seconds: float = 5.0) -> str | None:
    try:
        result = subprocess.run(args, capture_output=True, text=True, timeout=timeout_seconds, check=False)
    except (OSError, subprocess.SubprocessError):
        return None
    output = (result.stdout or result.stderr).strip()
    return output or None


def environment_metadata(api_url: str) -> dict[str, Any]:
    return {
        "captured_at": utc_now(),
        "command": sys.argv,
        "python_version": platform.python_version(),
        "platform": platform.platform(),
        "processor": platform.processor(),
        "cpu_count": os.cpu_count(),
        "git_sha": run_command(["git", "rev-parse", "HEAD"]),
        "git_status_short": run_command(["git", "status", "--short"]),
        "api_url": api_url,
    }


def const(value: Any) -> dict[str, Any]:
    return {"constant": value}


def node(name: str, *children: dict[str, Any], **named_children: dict[str, Any]) -> dict[str, Any]:
    out: dict[str, Any] = {"name": name}
    if children:
        out["children"] = list(children)
    if named_children:
        out["named_children"] = named_children
    return out


def payload(field_name: str) -> dict[str, Any]:
    return node("Payload", const(field_name))


def true_node() -> dict[str, Any]:
    return node("=", const(1), const(1))


def value_gt_node(threshold: float) -> dict[str, Any]:
    return node(">", payload("value"), const(threshold))


def filter_node(table_name: str, field_name: str, operator: str, value: dict[str, Any] | None = None) -> dict[str, Any]:
    named = {
        "tableName": const(table_name),
        "fieldName": const(field_name),
        "operator": const(operator),
    }
    if value is not None:
        named["value"] = value
    return node("Filter", **named)


def list_node(*children: dict[str, Any]) -> dict[str, Any]:
    return node("List", *children)


@dataclass(frozen=True)
class VariantDefinition:
    name: str
    description: str
    formula: dict[str, Any]
    seed_account: bool = False
    seed_custom_list: bool = False
    seed_related_records: bool = False


@dataclass
class Metrics:
    attempted: int = 0
    completed: int = 0
    successes: int = 0
    failures: int = 0
    timeouts: int = 0
    skipped_decisions: int = 0
    latencies_ms: list[float] | None = None
    errors: list[str] | None = None
    per_vu_successes: dict[int, int] | None = None

    def __post_init__(self) -> None:
        self.latencies_ms = [] if self.latencies_ms is None else self.latencies_ms
        self.errors = [] if self.errors is None else self.errors
        self.per_vu_successes = {} if self.per_vu_successes is None else self.per_vu_successes


def add_error(metrics: Metrics, error: str) -> None:
    assert metrics.errors is not None
    if len(metrics.errors) < 20:
        metrics.errors.append(error)


class MarbleClient:
    def __init__(
        self,
        api_url: str,
        api_key: str,
        admin_token: str | None,
        timeout_seconds: float,
        vus: int,
    ) -> None:
        limits = httpx.Limits(max_connections=max(vus * 2, 100), max_keepalive_connections=max(vus, 20))
        timeout = httpx.Timeout(timeout_seconds)
        self.api_url = api_url.rstrip("/")
        self.api_key = api_key
        self.admin_token = admin_token
        self.admin = httpx.AsyncClient(base_url=self.api_url, timeout=timeout, limits=limits)
        self.public = httpx.AsyncClient(
            base_url=self.api_url,
            timeout=timeout,
            headers={"X-API-KEY": api_key, "Content-Type": "application/json"},
            limits=limits,
        )

    async def close(self) -> None:
        await self.admin.aclose()
        await self.public.aclose()

    def set_admin_token(self, token: str) -> None:
        self.admin_token = token
        self.admin.headers.update({"Authorization": f"Bearer {token}", "Content-Type": "application/json"})

    async def request(
        self,
        client: httpx.AsyncClient,
        method: str,
        path: str,
        expected: int | set[int],
        **kwargs: Any,
    ) -> dict[str, Any]:
        response = await client.request(method, path, **kwargs)
        expected_set = {expected} if isinstance(expected, int) else expected
        if response.status_code not in expected_set:
            raise RuntimeError(
                f"{method} {response.request.url} returned {response.status_code}, "
                f"expected {sorted(expected_set)}: {response.text}"
            )
        if not response.content:
            return {}
        return response.json()

    async def wait_ready(self) -> None:
        deadline = time.monotonic() + 30.0
        last_error: Exception | str | None = None
        while time.monotonic() < deadline:
            try:
                response = await self.public.get("/v1/-/version")
                if response.status_code == 200:
                    return
                last_error = f"status {response.status_code}"
            except (httpx.HTTPError, OSError) as exc:
                last_error = exc
            await asyncio.sleep(0.5)
        raise RuntimeError(f"API was not ready: {last_error}")


async def firebase_login(
    api_url: str,
    firebase_auth_url: str,
    firebase_api_key: str,
    admin_email: str,
    admin_password: str,
    timeout_seconds: float,
) -> str:
    async with httpx.AsyncClient(timeout=httpx.Timeout(timeout_seconds)) as client:
        sign_in = await client.post(
            f"{firebase_auth_url.rstrip('/')}/identitytoolkit.googleapis.com/v1/accounts:signInWithPassword",
            params={"key": firebase_api_key},
            json={"email": admin_email, "password": admin_password, "returnSecureToken": True},
        )
        if sign_in.status_code != 200:
            raise RuntimeError(f"firebase sign-in returned {sign_in.status_code}: {sign_in.text}")
        id_token = sign_in.json()["idToken"]
        token_response = await client.post(
            f"{api_url.rstrip('/')}/token",
            headers={"Authorization": f"Bearer {id_token}"},
        )
        if token_response.status_code != 200:
            raise RuntimeError(f"POST /token returned {token_response.status_code}: {token_response.text}")
        return token_response.json()["access_token"]


def transaction_payload(transaction_id: str, account_id: str, owner_id: str, value: float) -> dict[str, Any]:
    now = utc_now()
    return {
        "object_id": transaction_id,
        "value": value,
        "status": "PENDING",
        "payment_method": "TRANSFER",
        "direction": "PAYOUT",
        "account_id": account_id,
        "owner_id": owner_id,
        "email": "risk@example.com",
        "country": "DE",
        "merchant": "ITC Market",
        "transaction_at": now,
        "created_at": now,
        "updated_at": now,
        "card_payment_currency": "EUR",
        "card_payment_type": "ECOMMERCE",
        "card_is_3ds": False,
        "counterparty_iban": "FR7630006000011234567890189",
        "counterparty_bic": "AGRIFRPPXXX",
        "category": "6012",
        "is_recuring": False,
        "card_merchant_name": "ITC Market",
        "card_payment_country": "DE",
        "creditor_identifier": "stress-creditor",
        "scheme": "CORE",
        "card_merchant_id": "stress-merchant",
    }


def account_payload(account_id: str, owner_id: str) -> dict[str, Any]:
    now = utc_now()
    return {
        "object_id": account_id,
        "account_status": "active",
        "owner_id": owner_id,
        "created_at": now,
        "updated_at": now,
        "balance": 200000.0,
        "past_balance": 200000.0,
        "iban": f"DE893704004405{uuid.uuid4().int % 10**10:010d}",
        "bic": "COBADEFFXXX",
    }


class MarbleScalingHarness:
    def __init__(
        self,
        client: MarbleClient,
        transaction_value: float,
        scenario_threshold: float,
        related_seed_count: int,
    ) -> None:
        self.client = client
        self.transaction_value = transaction_value
        self.scenario_threshold = scenario_threshold
        self.related_seed_count = related_seed_count
        self.suffix = uuid.uuid4().hex[:8]
        self.transaction_table = f"stress_tx_{self.suffix}"
        self.account_table = f"stress_acct_{self.suffix}"
        self.link_name = f"stress_link_{self.suffix}"
        self.owner_id = unique_name("owner")
        self.account_id = unique_name("account")
        self.scenario_ids: list[str] = []
        self.rule_ids: list[str] = []
        self.custom_list_id: str | None = None
        self.seeded_counts: dict[str, int] = {}
        self._sequence = 0

    async def bootstrap_model(self) -> None:
        tx_id = (await self.client.request(
            self.client.admin,
            "POST",
            "/data-model/tables",
            200,
            json={"name": self.transaction_table, "description": "Stress test transactions"},
        ))["id"]
        acct_id = (await self.client.request(
            self.client.admin,
            "POST",
            "/data-model/tables",
            200,
            json={"name": self.account_table, "description": "Stress test accounts"},
        ))["id"]

        tx_fields = await self._default_field_ids(self.transaction_table)
        acct_fields = await self._default_field_ids(self.account_table)

        for field_name, field_type, nullable in [
            ("value", "Float", False),
            ("status", "String", False),
            ("payment_method", "String", False),
            ("direction", "String", False),
            ("account_id", "String", False),
            ("owner_id", "String", False),
            ("email", "String", False),
            ("country", "String", False),
            ("merchant", "String", False),
            ("transaction_at", "Timestamp", False),
            ("created_at", "Timestamp", False),
            ("card_payment_currency", "String", False),
            ("card_payment_type", "String", False),
            ("card_is_3ds", "Bool", False),
            ("counterparty_iban", "String", False),
            ("counterparty_bic", "String", False),
            ("category", "String", False),
            ("is_recuring", "Bool", False),
            ("card_merchant_name", "String", False),
            ("card_payment_country", "String", False),
            ("creditor_identifier", "String", False),
            ("scheme", "String", False),
            ("card_merchant_id", "String", False),
        ]:
            tx_fields[field_name] = (await self.client.request(
                self.client.admin,
                "POST",
                f"/data-model/tables/{tx_id}/fields",
                200,
                json={
                    "name": field_name,
                    "description": field_name,
                    "type": field_type,
                    "nullable": nullable,
                    "is_enum": False,
                    "is_unique": field_name == "object_id",
                },
            ))["id"]

        for field_name, field_type, nullable in [
            ("account_status", "String", False),
            ("owner_id", "String", False),
            ("created_at", "Timestamp", False),
            ("balance", "Float", False),
            ("past_balance", "Float", False),
            ("iban", "String", False),
            ("bic", "String", False),
        ]:
            acct_fields[field_name] = (await self.client.request(
                self.client.admin,
                "POST",
                f"/data-model/tables/{acct_id}/fields",
                200,
                json={
                    "name": field_name,
                    "description": field_name,
                    "type": field_type,
                    "nullable": nullable,
                    "is_enum": False,
                    "is_unique": field_name == "object_id",
                },
            ))["id"]

        await self.client.request(
            self.client.admin,
            "POST",
            "/data-model/links",
            204,
            json={
                "name": self.link_name,
                "parent_table_id": acct_id,
                "parent_field_id": acct_fields["object_id"],
                "child_table_id": tx_id,
                "child_field_id": tx_fields["account_id"],
            },
        )

    async def _default_field_ids(self, table_name: str) -> dict[str, str]:
        data_model = await self.client.request(self.client.admin, "GET", "/data-model", 200)
        table = data_model["data_model"]["tables"][table_name]
        return {
            "object_id": table["fields"]["object_id"]["id"],
            "updated_at": table["fields"]["updated_at"]["id"],
        }

    async def seed_custom_list(self) -> None:
        created = await self.client.request(
            self.client.admin,
            "POST",
            "/custom-lists",
            201,
            json={"name": unique_name("blocked_emails"), "description": "Stress list", "kind": "text"},
        )
        self.custom_list_id = created["custom_list"]["id"]
        await self.client.request(
            self.client.admin,
            "POST",
            f"/custom-lists/{self.custom_list_id}/values",
            201,
            json={"value": "risk@example.com"},
        )
        self.seeded_counts["custom_list_entries"] = 1

    async def seed_account(self) -> None:
        await self.client.request(
            self.client.public,
            "POST",
            f"/v1/ingest/{self.account_table}",
            {200, 201},
            json=account_payload(self.account_id, self.owner_id),
        )
        self.seeded_counts["accounts"] = 1

    async def seed_related_records(self) -> None:
        rows = [
            transaction_payload(unique_name("seed_tx"), self.account_id, self.owner_id, 100.0 + idx)
            for idx in range(self.related_seed_count)
        ]
        batch_size = 100
        for start in range(0, len(rows), batch_size):
            await self.client.request(
                self.client.public,
                "POST",
                f"/v1/ingest/{self.transaction_table}/batch",
                {200, 201},
                json=rows[start:start + batch_size],
            )
        self.seeded_counts["related_records"] = len(rows)

    def variant(self, name: str) -> VariantDefinition:
        if name == "baseline_payload":
            return VariantDefinition(name, "Simple value threshold payload rule.", value_gt_node(self.scenario_threshold))
        if name == "nested_payload":
            return VariantDefinition(
                name,
                "Nested payload-only logical rule.",
                node(
                    "And",
                    value_gt_node(self.scenario_threshold),
                    node("Or", node("=", payload("status"), const("PENDING")), node("=", payload("status"), const("REVIEW"))),
                    node("Not", node("=", payload("country"), const("blocked"))),
                    node("StringContains", payload("email"), const("@example.com")),
                    node("StringStartsWith", payload("merchant"), const("ITC")),
                    node("StringEndsWith", payload("email"), const(".com")),
                ),
            )
        if name == "custom_list":
            if self.custom_list_id is None:
                raise RuntimeError("custom list variant requires seeded custom list")
            return VariantDefinition(
                name,
                "Custom list membership lookup against email.",
                node("IsInList", payload("email"), node("CustomListAccess", customListId=const(self.custom_list_id))),
                seed_custom_list=True,
            )
        if name == "related_field":
            return VariantDefinition(
                name,
                "Related account traversal and field read.",
                node(
                    "=",
                    node(
                        "DatabaseAccess",
                        tableName=const(self.transaction_table),
                        fieldName=const("account_status"),
                        path=const([self.link_name]),
                    ),
                    const("active"),
                ),
                seed_account=True,
            )
        if name == "aggregate_count_pushdown":
            return VariantDefinition(
                name,
                "Aggregate count over seeded transaction records.",
                node(
                    ">=",
                    node(
                        "Aggregator",
                        tableName=const(self.transaction_table),
                        fieldName=const("object_id"),
                        aggregator=const("COUNT"),
                        filters=list_node(),
                        label=const("Owner pending transaction count"),
                    ),
                    const(self.related_seed_count),
                ),
                seed_related_records=True,
            )
        if name == "mixed_heavy":
            if self.custom_list_id is None:
                raise RuntimeError("mixed_heavy variant requires seeded custom list")
            return VariantDefinition(
                name,
                "Nested payload, custom list, related field, and aggregate count.",
                node(
                    "And",
                    self.variant("nested_payload").formula,
                    self.variant("custom_list").formula,
                    self.variant("related_field").formula,
                    self.variant("aggregate_count_pushdown").formula,
                ),
                seed_account=True,
                seed_custom_list=True,
                seed_related_records=True,
            )
        raise ValueError(f"unknown variant {name!r}; expected one of {', '.join(SUPPORTED_VARIANTS)}")

    async def seed_for_variant(self, name: str) -> None:
        needs_custom_list = name in {"custom_list", "mixed_heavy"}
        needs_account = name in {"related_field", "mixed_heavy"}
        needs_related = name in {"aggregate_count_pushdown", "mixed_heavy"}
        if needs_custom_list and self.custom_list_id is None:
            await self.seed_custom_list()
        if needs_account and "accounts" not in self.seeded_counts:
            await self.seed_account()
        if needs_related and "related_records" not in self.seeded_counts:
            await self.seed_related_records()

    async def create_scenario(self, variant: VariantDefinition, rules_per_scenario: int = 1) -> str:
        scenario = await self.client.request(
            self.client.admin,
            "POST",
            "/scenarios",
            200,
            json={
                "name": unique_name(f"stress_{variant.name}"),
                "description": variant.description,
                "trigger_object_type": self.transaction_table,
            },
        )
        scenario_id = scenario["id"]
        rules = [
            {
                "scenario_iteration_id": "",
                "display_order": idx,
                "name": f"{variant.name}_{idx + 1}",
                "description": variant.description,
                "formula_ast_expression": variant.formula,
                "score_modifier": 10,
                "rule_group": "stress",
            }
            for idx in range(rules_per_scenario)
        ]
        iteration = await self.client.request(
            self.client.admin,
            "POST",
            "/scenario-iterations",
            200,
            json={
                "scenario_id": scenario_id,
                "body": {
                    "trigger_condition_ast_expression": true_node(),
                    "rules": rules,
                    "score_review_threshold": 10,
                    "score_block_and_review_threshold": 30,
                    "score_decline_threshold": 60,
                    "schedule": "",
                },
            },
        )
        iteration_id = iteration["id"]
        validation = await self.client.request(
            self.client.admin,
            "POST",
            f"/scenario-iterations/{iteration_id}/validate",
            200,
            json={},
        )
        errors = collect_validation_errors(validation.get("scenario_validation", {}))
        if errors:
            raise RuntimeError(f"scenario validation failed: {json.dumps(errors, default=str)}")
        committed = await self.client.request(
            self.client.admin,
            "POST",
            f"/scenario-iterations/{iteration_id}/commit",
            200,
            json={},
        )
        committed_iteration_id = committed.get("iteration", {}).get("id", iteration_id)
        await self.prepare_iteration_for_publication(committed_iteration_id)
        await self.client.request(
            self.client.admin,
            "POST",
            "/scenario-publications",
            200,
            json={"scenario_iteration_id": committed_iteration_id, "publication_action": "publish"},
        )
        self.scenario_ids.append(scenario_id)
        self.rule_ids.extend(rule["id"] for rule in iteration.get("body", {}).get("rules", []) if "id" in rule)
        return scenario_id

    async def prepare_iteration_for_publication(self, iteration_id: str) -> None:
        status = await self.publication_preparation_status(iteration_id)
        if status.get("preparation_status") == "ready_to_activate":
            return
        if status.get("preparation_status") != "required":
            raise RuntimeError(f"unexpected publication preparation status: {status}")

        await self.start_publication_preparation(iteration_id)
        deadline = time.monotonic() + 120.0
        while time.monotonic() < deadline:
            status = await self.publication_preparation_status(iteration_id)
            if status.get("preparation_status") == "ready_to_activate":
                return
            await asyncio.sleep(1.0)
        raise RuntimeError(
            "scenario publication preparation did not complete within 120s; "
            "make sure the Marble worker is running so queued index-creation tasks are processed"
        )

    async def publication_preparation_status(self, iteration_id: str) -> dict[str, Any]:
        return await self.client.request(
            self.client.admin,
            "GET",
            "/scenario-publications/preparation",
            200,
            params={"scenario_iteration_id": iteration_id},
        )

    async def start_publication_preparation(self, iteration_id: str) -> None:
        while True:
            try:
                await self.client.request(
                    self.client.admin,
                    "POST",
                    "/scenario-publications/preparation",
                    202,
                    json={"scenario_iteration_id": iteration_id},
                )
                return
            except RuntimeError as exc:
                if "data_preparation_service_unavailable" not in str(exc):
                    raise
                await asyncio.sleep(1.0)

    def next_transaction_payload(self) -> dict[str, Any]:
        self._sequence += 1
        return transaction_payload(unique_name(f"tx_{self._sequence}"), self.account_id, self.owner_id, self.transaction_value)


def collect_validation_errors(validation: dict[str, Any]) -> list[dict[str, Any]]:
    errors: list[dict[str, Any]] = []
    errors.extend(validation.get("trigger", {}).get("errors", []))
    errors.extend(validation.get("rules", {}).get("errors", []))
    errors.extend(validation.get("decision", {}).get("errors", []))
    for rule_validation in validation.get("rules", {}).get("rules", {}).values():
        errors.extend(rule_validation.get("errors", []))
    return errors


async def run_one_request(
    action: Callable[[], Awaitable[bool]],
    metrics: Metrics,
    vu_id: int,
) -> None:
    started_at = time.perf_counter()
    metrics.attempted += 1
    try:
        decision_created = await action()
    except httpx.TimeoutException as exc:
        metrics.completed += 1
        metrics.failures += 1
        metrics.timeouts += 1
        add_error(metrics, f"timeout: {exc}")
    except Exception as exc:
        metrics.completed += 1
        metrics.failures += 1
        add_error(metrics, str(exc))
    else:
        metrics.completed += 1
        if decision_created:
            metrics.successes += 1
            assert metrics.latencies_ms is not None
            metrics.latencies_ms.append((time.perf_counter() - started_at) * 1000.0)
            assert metrics.per_vu_successes is not None
            metrics.per_vu_successes[vu_id] = metrics.per_vu_successes.get(vu_id, 0) + 1
        else:
            metrics.failures += 1
            metrics.skipped_decisions += 1
            add_error(metrics, "scenario trigger condition did not match")


async def run_closed_loop(action: Callable[[], Awaitable[bool]], vus: int, duration_seconds: float) -> tuple[Metrics, float]:
    metrics = Metrics()
    start_gate = asyncio.Event()
    started_at = time.perf_counter()
    deadline = started_at + duration_seconds

    async def worker(vu_id: int) -> None:
        await start_gate.wait()
        while time.perf_counter() < deadline:
            await run_one_request(action, metrics, vu_id)

    tasks = [asyncio.create_task(worker(vu_id)) for vu_id in range(vus)]
    start_gate.set()
    await asyncio.gather(*tasks)
    return metrics, time.perf_counter() - started_at


def latency_summary(values: list[float]) -> dict[str, float | None]:
    return {
        "avg": statistics.fmean(values) if values else None,
        "p50": percentile(values, 50),
        "p95": percentile(values, 95),
        "p99": percentile(values, 99),
        "max": max(values) if values else None,
    }


def per_vu_summary(metrics: Metrics) -> dict[str, float | int]:
    counts = list((metrics.per_vu_successes or {}).values())
    return {
        "min": min(counts) if counts else 0,
        "max": max(counts) if counts else 0,
        "avg": statistics.fmean(counts) if counts else 0,
    }


def parse_csv_list(value: str, valid: set[str] | None = None) -> list[str]:
    items = [item.strip() for item in value.split(",") if item.strip()]
    if valid is not None:
        unknown = [item for item in items if item not in valid]
        if unknown:
            raise SystemExit(f"unknown values: {', '.join(unknown)}; expected one of {', '.join(sorted(valid))}")
    if not items:
        raise SystemExit("list must not be empty")
    return items


def parse_int_list(value: str, name: str) -> list[int]:
    try:
        items = [int(item) for item in parse_csv_list(value)]
    except ValueError as exc:
        raise SystemExit(f"{name} values must be integers") from exc
    if any(item <= 0 for item in items):
        raise SystemExit(f"{name} values must be greater than 0")
    return items
