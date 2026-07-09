from __future__ import annotations

import asyncio
import json
import os
import platform
import random
import statistics
import subprocess
import sys
import time
import uuid
from dataclasses import dataclass
from datetime import datetime, timedelta, timezone
from pathlib import Path
from typing import Any, Callable, Awaitable

import httpx


SUPPORTED_VARIANTS = [
    "baseline_payload",
    "nested_payload",
    "custom_list_related_fuzzy",
    "related_field",
    "aggregate_count",
    "aggregate_velocity",
    "mixed_heavy",
]

PROCESSORS = ["genpay", "uniwallet"]
CHANNELS = ["card", "wallet", "bank"]
MERCHANT_BLACKLIST = ["Sankofa Betting House", "Volta Crypto Exchange", "Accra Gold Deals"]
VELOCITY_WINDOW_DAYS = 7
VELOCITY_AMOUNT_THRESHOLD = 5_000.0
DOMAIN_ACCOUNTS = [
    {"account_ref": "2332416370369", "customer_name": "Ama Mensah", "account_status": "active", "risk_tier": "standard"},
    {"account_ref": "2332448891021", "customer_name": "Kojo Boateng", "account_status": "active", "risk_tier": "high"},
    {"account_ref": "2332094517780", "customer_name": "Esi Owusu", "account_status": "active", "risk_tier": "standard"},
]
DOMAIN_MERCHANTS = [
    {
        "merchant_id": "dbb82c30-d9df-4c9c-bf96-5be052f644e8",
        "merchant_name": "Sankofa Betting House Ltd",
        "merchant_category": "gaming",
        "merchant_status": "active",
        "risk_tier": "high",
    },
    {
        "merchant_id": "87399a0b-ad5b-4c37-a6bb-49fca821184f",
        "merchant_name": "Makola Grocery Mart",
        "merchant_category": "retail",
        "merchant_status": "active",
        "risk_tier": "standard",
    },
    {
        "merchant_id": "0ba8209b-92dc-4ef9-93a8-dcebb067fe09",
        "merchant_name": "Volta Crypto Exchange GH",
        "merchant_category": "crypto",
        "merchant_status": "active",
        "risk_tier": "high",
    },
]
DOMAIN_PRODUCTS = [
    {
        "product_id": "2ae50a9e-0487-4436-a11a-cb486a04c168",
        "product_name": "Mobile Wallet Cashout",
        "product_type": "wallet",
        "risk_category": "medium",
        "status": "active",
        "daily_limit": 5_000.0,
    },
    {
        "product_id": "68fcb1f7-6bf8-466f-9a89-a4d2c5c00f48",
        "product_name": "Merchant Card Collection",
        "product_type": "card",
        "risk_category": "standard",
        "status": "active",
        "daily_limit": 10_000.0,
    },
    {
        "product_id": "c1349cef-e96f-4af8-82b4-4e83990843f7",
        "product_name": "Instant Bank Transfer",
        "product_type": "bank",
        "risk_category": "high",
        "status": "active",
        "daily_limit": 20_000.0,
    },
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
    return node(">", payload("amount"), const(threshold))


def filter_node(table_name: str, field_name: str, operator: str, value: dict[str, Any] | None = None) -> dict[str, Any]:
    named = {
        "tableName": const(table_name),
        "fieldName": const(field_name),
        "operator": const(operator),
    }
    if value is not None:
        named["value"] = value
    return node("Filter", **named)


def time_add_node(timestamp_field: dict[str, Any], duration: str, sign: str) -> dict[str, Any]:
    return node("TimeAdd", timestampField=timestamp_field, duration=const(duration), sign=const(sign))


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
        api_key: str | None,
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
        public_headers = {"Content-Type": "application/json"}
        if api_key:
            public_headers["X-API-KEY"] = api_key
        self.public = httpx.AsyncClient(
            base_url=self.api_url,
            timeout=timeout,
            headers=public_headers,
            limits=limits,
        )

    async def close(self) -> None:
        await self.admin.aclose()
        await self.public.aclose()

    def set_admin_token(self, token: str) -> None:
        self.admin_token = token
        self.admin.headers.update({"Authorization": f"Bearer {token}", "Content-Type": "application/json"})

    def set_api_key(self, api_key: str) -> None:
        self.api_key = api_key
        self.public.headers.update({"X-API-KEY": api_key, "Content-Type": "application/json"})

    async def create_api_key(self) -> str:
        created = await self.request(
            self.admin,
            "POST",
            "/apikeys",
            {200, 201},
            json={"description": unique_name("stress_api_key"), "role": "API_CLIENT"},
        )
        api_key = created["api_key"]["key"]
        self.set_api_key(api_key)
        return api_key

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


def transaction_payload(
    transaction_id: str,
    account_ref: str,
    merchant_id: str,
    product_id: str,
    value: float,
    processor: str,
    channel: str,
    happened_at: str,
) -> dict[str, Any]:
    return {
        "object_id": transaction_id,
        "account_ref": account_ref,
        "processor": processor,
        "merchant_id": merchant_id,
        "product_id": product_id,
        "transaction_id": f"{channel}__{processor}__{transaction_id}",
        "date": happened_at,
        "amount": value,
        "currency": "GHS",
        "country": "GH",
        "channel": channel,
        "updated_at": happened_at,
    }


def account_payload(account: dict[str, Any]) -> dict[str, Any]:
    now = utc_now()
    return {
        "object_id": account["account_ref"],
        **account,
        "created_at": now,
        "updated_at": now,
    }


def merchant_payload(merchant: dict[str, Any]) -> dict[str, Any]:
    return {"object_id": merchant["merchant_id"], "country": "GH", "updated_at": utc_now(), **merchant}


def product_payload(product: dict[str, Any]) -> dict[str, Any]:
    return {"object_id": product["product_id"], "updated_at": utc_now(), **product}


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
        self.merchant_table = f"stress_merchant_{self.suffix}"
        self.product_table = f"stress_product_{self.suffix}"
        self.account_link_name = f"stress_account_link_{self.suffix}"
        self.merchant_link_name = f"stress_merchant_link_{self.suffix}"
        self.product_link_name = f"stress_product_link_{self.suffix}"
        self.link_name = self.account_link_name
        self.primary_merchant_id = DOMAIN_MERCHANTS[0]["merchant_id"]
        self.scenario_ids: list[str] = []
        self.rule_ids: list[str] = []
        self.custom_list_id: str | None = None
        self.seeded_counts: dict[str, int] = {}
        self._sequence = 0
        self.domain_accounts = DOMAIN_ACCOUNTS
        self.domain_merchants = DOMAIN_MERCHANTS
        self.domain_products = DOMAIN_PRODUCTS
        self.current_time = datetime(2025, 1, 30, 12, 0, 28, tzinfo=timezone.utc)
        self.random = random.Random(f"{self.suffix}:{related_seed_count}")

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
        merchant_id = (await self.client.request(
            self.client.admin,
            "POST",
            "/data-model/tables",
            200,
            json={"name": self.merchant_table, "description": "Stress test merchants"},
        ))["id"]
        product_id = (await self.client.request(
            self.client.admin,
            "POST",
            "/data-model/tables",
            200,
            json={"name": self.product_table, "description": "Stress test products"},
        ))["id"]

        tx_fields = await self._default_field_ids(self.transaction_table)
        acct_fields = await self._default_field_ids(self.account_table)
        merchant_fields = await self._default_field_ids(self.merchant_table)
        product_fields = await self._default_field_ids(self.product_table)

        for field_name, field_type, nullable in [
            ("account_ref", "String", False),
            ("processor", "String", False),
            ("merchant_id", "String", False),
            ("product_id", "String", False),
            ("transaction_id", "String", False),
            ("date", "Timestamp", False),
            ("amount", "Float", False),
            ("currency", "String", False),
            ("country", "String", False),
            ("channel", "String", False),
        ]:
            tx_fields[field_name] = await self.create_field(tx_id, self.transaction_table, field_name, field_type, nullable, field_name == "object_id")

        for field_name, field_type, nullable in [
            ("account_ref", "String", False),
            ("customer_name", "String", False),
            ("account_status", "String", False),
            ("created_at", "Timestamp", False),
            ("risk_tier", "String", False),
        ]:
            acct_fields[field_name] = await self.create_field(acct_id, self.account_table, field_name, field_type, nullable, field_name in {"object_id", "account_ref"})

        for field_name, field_type, nullable in [
            ("merchant_id", "String", False),
            ("merchant_name", "String", False),
            ("merchant_category", "String", False),
            ("merchant_status", "String", False),
            ("risk_tier", "String", False),
            ("country", "String", False),
        ]:
            merchant_fields[field_name] = await self.create_field(merchant_id, self.merchant_table, field_name, field_type, nullable, field_name == "merchant_id")

        for field_name, field_type, nullable in [
            ("product_id", "String", False),
            ("product_name", "String", False),
            ("product_type", "String", False),
            ("risk_category", "String", False),
            ("status", "String", False),
            ("daily_limit", "Float", False),
        ]:
            product_fields[field_name] = await self.create_field(product_id, self.product_table, field_name, field_type, nullable, field_name == "product_id")

        await self.client.request(
            self.client.admin,
            "POST",
            "/data-model/links",
            204,
            json={
                "name": self.account_link_name,
                "parent_table_id": acct_id,
                "parent_field_id": acct_fields["account_ref"],
                "child_table_id": tx_id,
                "child_field_id": tx_fields["account_ref"],
            },
        )
        await self.client.request(
            self.client.admin,
            "POST",
            "/data-model/links",
            204,
            json={
                "name": self.merchant_link_name,
                "parent_table_id": merchant_id,
                "parent_field_id": merchant_fields["merchant_id"],
                "child_table_id": tx_id,
                "child_field_id": tx_fields["merchant_id"],
            },
        )
        await self.client.request(
            self.client.admin,
            "POST",
            "/data-model/links",
            204,
            json={
                "name": self.product_link_name,
                "parent_table_id": product_id,
                "parent_field_id": product_fields["product_id"],
                "child_table_id": tx_id,
                "child_field_id": tx_fields["product_id"],
            },
        )

    async def _default_field_ids(self, table_name: str) -> dict[str, str]:
        data_model = await self.client.request(self.client.admin, "GET", "/data-model", 200)
        table = data_model["data_model"]["tables"][table_name]
        return {
            "object_id": table["fields"]["object_id"]["id"],
            "updated_at": table["fields"]["updated_at"]["id"],
        }

    async def create_field(
        self,
        table_id: str,
        table_name: str,
        field_name: str,
        field_type: str,
        nullable: bool,
        is_unique: bool,
    ) -> str:
        payload = {
            "name": field_name,
            "description": field_name,
            "type": field_type,
            "nullable": nullable,
            "is_enum": False,
            "is_unique": is_unique,
        }
        deadline = time.monotonic() + 120.0
        last_error = ""
        while time.monotonic() < deadline:
            existing = await self.find_field(table_name, field_name)
            if existing:
                return existing

            try:
                response = await self.client.admin.post(f"/data-model/tables/{table_id}/fields", json=payload)
            except (httpx.ReadTimeout, httpx.ConnectTimeout, httpx.RemoteProtocolError, httpx.ConnectError) as exc:
                last_error = f"{type(exc).__name__}: {exc}"
                await asyncio.sleep(1.0)
                continue
            if response.status_code == 200:
                return response.json()["id"]

            last_error = f"status {response.status_code}: {response.text}"
            if response.status_code not in {408, 409, 422}:
                raise RuntimeError(
                    f"POST {response.request.url} returned {response.status_code}, "
                    f"expected [200]: {response.text}"
                )

            for _ in range(5):
                await asyncio.sleep(1.0)
                existing = await self.find_field(table_name, field_name)
                if existing:
                    return existing

        raise RuntimeError(f"field {table_name}.{field_name} was not created within 120s; last response: {last_error}")

    async def find_field(self, table_name: str, field_name: str) -> str | None:
        deadline = time.monotonic() + 60.0
        last_error: Exception | None = None
        while time.monotonic() < deadline:
            try:
                data_model = await self.client.request(self.client.admin, "GET", "/data-model", 200)
                break
            except (httpx.ReadTimeout, httpx.ConnectTimeout, httpx.RemoteProtocolError, httpx.ConnectError) as exc:
                last_error = exc
                await asyncio.sleep(1.0)
        else:
            raise RuntimeError(f"GET /data-model did not complete within 60s while looking for {table_name}.{field_name}: {last_error}")
        table = data_model["data_model"]["tables"].get(table_name, {})
        field = table.get("fields", {}).get(field_name)
        if field and field.get("id"):
            return field["id"]
        return None

    async def seed_custom_list(self) -> None:
        created = await self.client.request(
            self.client.admin,
            "POST",
            "/custom-lists",
            201,
            json={"name": unique_name("blocked_merchant_names"), "description": "Blocked merchant names", "kind": "text"},
        )
        self.custom_list_id = created["custom_list"]["id"]
        for value in MERCHANT_BLACKLIST:
            await self.client.request(
                self.client.admin,
                "POST",
                f"/custom-lists/{self.custom_list_id}/values",
                201,
                json={"value": value},
            )
        self.seeded_counts["custom_list_entries"] = len(MERCHANT_BLACKLIST)

    async def seed_account(self) -> None:
        if "accounts" not in self.seeded_counts:
            for account in self.domain_accounts:
                await self.client.request(
                    self.client.public,
                    "POST",
                    f"/v1/ingest/{self.account_table}",
                    {200, 201},
                    json=account_payload(account),
                )
            self.seeded_counts["accounts"] = len(self.domain_accounts)
        if "merchants" not in self.seeded_counts:
            for merchant in self.domain_merchants:
                await self.client.request(
                    self.client.public,
                    "POST",
                    f"/v1/ingest/{self.merchant_table}",
                    {200, 201},
                    json=merchant_payload(merchant),
                )
            self.seeded_counts["merchants"] = len(self.domain_merchants)
        if "products" not in self.seeded_counts:
            for product in self.domain_products:
                await self.client.request(
                    self.client.public,
                    "POST",
                    f"/v1/ingest/{self.product_table}",
                    {200, 201},
                    json=product_payload(product),
                )
            self.seeded_counts["products"] = len(self.domain_products)

    async def seed_related_records(self) -> None:
        await self.seed_account()
        rows = []
        for idx in range(self.related_seed_count):
            rows.append(self.next_transaction_payload(value=100.0 + idx, merchant_id=self.primary_merchant_id))
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

    def next_domain_time(self) -> str:
        self.current_time += timedelta(seconds=self.random.randint(60 * 60, 2 * 24 * 60 * 60))
        return self.current_time.isoformat().replace("+00:00", "Z")

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
                    node("Or", node("=", payload("processor"), const("genpay")), node("=", payload("processor"), const("uniwallet"))),
                    node("IsInList", payload("channel"), list_node(const("card"), const("wallet"), const("bank"))),
                    node("=", payload("currency"), const("GHS")),
                    node("=", payload("country"), const("GH")),
                    node("StringContains", payload("transaction_id"), payload("processor")),
                ),
            )
        if name == "custom_list_related_fuzzy":
            if self.custom_list_id is None:
                raise RuntimeError("custom list variant requires seeded custom list")
            return VariantDefinition(
                name,
                "Related merchant name fuzzy match against merchant blacklist custom list.",
                node(
                    ">=",
                    node(
                        "FuzzyMatchAnyOf",
                        node(
                            "DatabaseAccess",
                            tableName=const(self.transaction_table),
                            fieldName=const("merchant_name"),
                            path=const([self.merchant_link_name]),
                        ),
                        list_node(*(const(value) for value in MERCHANT_BLACKLIST)),
                        algorithm=const("bag_of_words_similarity"),
                    ),
                    const(80),
                ),
                seed_custom_list=True,
                seed_account=True,
            )
        if name == "related_field":
            return VariantDefinition(
                name,
                "Related account, merchant, and product traversal and field reads.",
                node(
                    "And",
                    node(
                        "=",
                        node(
                            "DatabaseAccess",
                            tableName=const(self.transaction_table),
                            fieldName=const("account_status"),
                            path=const([self.account_link_name]),
                        ),
                        const("active"),
                    ),
                    node(
                        "=",
                        node(
                            "DatabaseAccess",
                            tableName=const(self.transaction_table),
                            fieldName=const("merchant_status"),
                            path=const([self.merchant_link_name]),
                        ),
                        const("active"),
                    ),
                    node(
                        "=",
                        node(
                            "DatabaseAccess",
                            tableName=const(self.transaction_table),
                            fieldName=const("status"),
                            path=const([self.product_link_name]),
                        ),
                        const("active"),
                    ),
                ),
                seed_account=True,
            )
        if name == "aggregate_count":
            return VariantDefinition(
                name,
                "Aggregate count of seeded transactions for the same merchant.",
                node(
                    ">=",
                    node(
                        "Aggregator",
                        tableName=const(self.transaction_table),
                        fieldName=const("transaction_id"),
                        aggregator=const("COUNT"),
                        filters=list_node(filter_node(self.transaction_table, "merchant_id", "=", const(self.primary_merchant_id))),
                        label=const("Merchant transaction count"),
                    ),
                    const(self.related_seed_count),
                ),
                seed_related_records=True,
            )
        if name == "aggregate_velocity":
            return VariantDefinition(
                name,
                "Aggregate sum of merchant transaction amount over a seven-day velocity window.",
                node(
                    ">=",
                    node(
                        "Aggregator",
                        tableName=const(self.transaction_table),
                        fieldName=const("amount"),
                        aggregator=const("SUM"),
                        filters=list_node(
                            filter_node(self.transaction_table, "merchant_id", "=", const(self.primary_merchant_id)),
                            filter_node(
                                self.transaction_table,
                                "date",
                                ">=",
                                time_add_node(payload("date"), f"P{VELOCITY_WINDOW_DAYS}D", "-"),
                            ),
                        ),
                        label=const("Merchant weekly velocity"),
                    ),
                    const(VELOCITY_AMOUNT_THRESHOLD),
                ),
                seed_related_records=True,
            )
        if name == "mixed_heavy":
            if self.custom_list_id is None:
                raise RuntimeError("mixed_heavy variant requires seeded custom list")
            return VariantDefinition(
                name,
                "Nested payload, merchant blacklist fuzzy match, related fields, aggregate count, and velocity.",
                node(
                    "And",
                    self.variant("nested_payload").formula,
                    self.variant("custom_list_related_fuzzy").formula,
                    self.variant("related_field").formula,
                    self.variant("aggregate_count").formula,
                    self.variant("aggregate_velocity").formula,
                ),
                seed_account=True,
                seed_custom_list=True,
                seed_related_records=True,
            )
        raise ValueError(f"unknown variant {name!r}; expected one of {', '.join(SUPPORTED_VARIANTS)}")

    async def seed_for_variant(self, name: str) -> None:
        await self.seed_account()
        needs_custom_list = name in {"custom_list_related_fuzzy", "mixed_heavy"}
        needs_related = name in {"aggregate_count", "aggregate_velocity", "mixed_heavy"}
        if needs_custom_list and self.custom_list_id is None:
            await self.seed_custom_list()
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

    def next_transaction_payload(self, value: float | None = None, merchant_id: str | None = None) -> dict[str, Any]:
        self._sequence += 1
        account = self.random.choice(self.domain_accounts)
        merchant = next(
            (item for item in self.domain_merchants if item["merchant_id"] == merchant_id),
            self.random.choice(self.domain_merchants),
        )
        product = self.random.choice(self.domain_products)
        processor = self.random.choice(PROCESSORS)
        channel = self.random.choice(CHANNELS)
        return transaction_payload(
            unique_name(f"tx_{self._sequence}"),
            account["account_ref"],
            merchant["merchant_id"],
            product["product_id"],
            self.transaction_value if value is None else value,
            processor,
            channel,
            self.next_domain_time(),
        )


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
