from __future__ import annotations

import argparse
import asyncio
import importlib.util
import json
import os
import statistics
import time
from dataclasses import asdict, dataclass
from pathlib import Path
from typing import Any

import httpx

from decision_throughput_limit import (
    Config as BaseConfig,
    Metrics,
    ThroughputHarness,
    add_error,
    environment_metadata,
    format_optional,
    percentile,
    record_payload,
    true_node,
    unique_name,
    utc_now,
)


DEFAULT_VARIANTS = [
    "baseline_payload",
    "nested_payload",
    "custom_list",
    "related_field",
    "decision_history",
    "related_records_count",
    "aggregate_count_pushdown",
    "mixed_heavy",
]


@dataclass(frozen=True)
class Config:
    variants: list[str]
    vus: list[int]
    duration_seconds: float
    amount: int
    timeout_seconds: float
    output_dir: str
    related_seed_count: int
    data_model_url: str
    ingestion_url: str
    decision_engine_url: str
    auth_token: str | None
    scenario_threshold: int
    ingestion_database_url: str | None
    history_object_pool_size: int


@dataclass(frozen=True)
class VariantDefinition:
    name: str
    description: str
    formula: dict[str, Any]
    stable_object_id: bool = False
    seed_account: bool = False
    seed_custom_list: bool = False
    seed_decision_history: bool = False
    seed_related_records: bool = False


@dataclass
class ClosedLoopMetrics(Metrics):
    per_vu_successes: dict[int, int] | None = None

    def __post_init__(self) -> None:
        super().__post_init__()
        if self.per_vu_successes is None:
            self.per_vu_successes = {}


def field(name: str) -> dict[str, Any]:
    return {"function": "field_ref", "named_children": {"field": {"constant": name}}}


def const(value: Any) -> dict[str, Any]:
    return {"constant": value}


def fn(name: str, *children: dict[str, Any], **named_children: dict[str, Any]) -> dict[str, Any]:
    node: dict[str, Any] = {"function": name}
    if children:
        node["children"] = list(children)
    if named_children:
        node["named_children"] = named_children
    return node


def gt(left: dict[str, Any], right: dict[str, Any]) -> dict[str, Any]:
    return fn("gt", left, right)


def gte(left: dict[str, Any], right: dict[str, Any]) -> dict[str, Any]:
    return fn("gte", left, right)


def eq(left: dict[str, Any], right: dict[str, Any]) -> dict[str, Any]:
    return fn("eq", left, right)


def amount_gt_node(limit: int) -> dict[str, Any]:
    return gt(field("amount"), const(limit))


def list_node(*children: dict[str, Any]) -> dict[str, Any]:
    return fn("List", *children)


def filter_node(table_name: str, field_name: str, operator: str, value: dict[str, Any] | None = None) -> dict[str, Any]:
    named = {
        "tableName": const(table_name),
        "fieldName": const(field_name),
        "operator": const(operator),
    }
    if value is not None:
        named["value"] = value
    return fn("Filter", **named)


def related_records_node(object_type: str, owner_id: str, limit: int) -> dict[str, Any]:
    return fn(
        "related_records",
        object_type=const(object_type),
        match_field=const("owner_id"),
        equals=const(owner_id),
        limit=const(limit),
    )


def aggregate_count_node(object_type: str, owner_id: str) -> dict[str, Any]:
    return fn(
        "Aggregator",
        tableName=const(object_type),
        fieldName=const("object_id"),
        aggregator=const("COUNT"),
        filters=list_node(),
    )


def nested_payload_formula(limit: int) -> dict[str, Any]:
    return fn(
        "and",
        gt(field("amount"), const(limit)),
        fn(
            "or",
            eq(field("status"), const("pending")),
            eq(field("status"), const("review")),
        ),
        fn("not", eq(field("country"), const("blocked"))),
        fn("contains", fn("lower", field("email")), const("@example.com")),
        fn("starts_with", field("merchant"), const("ITC")),
        fn("ends_with", fn("lower", field("email")), const(".com")),
    )


def custom_list_formula() -> dict[str, Any]:
    return fn("in_custom_list", list=const("blocked_emails"), value=fn("lower", field("email")))


def related_field_formula() -> dict[str, Any]:
    return eq(fn("related_field", path=const("account"), field=const("account_status")), const("active"))


def decision_history_formula() -> dict[str, Any]:
    return gte(fn("past_decision_count", outcome=const("review")), const(1))


def related_records_count_formula(object_type: str, owner_id: str, seed_count: int) -> dict[str, Any]:
    return gte(fn("list_count", related_records_node(object_type, owner_id, seed_count + 10)), const(seed_count))


def aggregate_count_formula(object_type: str, owner_id: str, seed_count: int) -> dict[str, Any]:
    return gte(aggregate_count_node(object_type, owner_id), const(seed_count))


def mixed_heavy_formula(object_type: str, owner_id: str, seed_count: int) -> dict[str, Any]:
    return fn(
        "and",
        nested_payload_formula(1000),
        custom_list_formula(),
        related_field_formula(),
        gte(aggregate_count_node(object_type, owner_id), const(seed_count)),
    )


def pg_type(data_type: str) -> str:
    return {
        "bool": "BOOLEAN",
        "boolean": "BOOLEAN",
        "int": "INTEGER",
        "integer": "INTEGER",
        "float": "DOUBLE PRECISION",
        "number": "DOUBLE PRECISION",
        "string": "TEXT",
        "timestamp": "TIMESTAMPTZ",
        "ip_address": "INET",
        "json": "JSONB",
    }.get(data_type, "TEXT")


def to_base_config(config: Config, output: str, vus: int) -> BaseConfig:
    return BaseConfig(
        rate=0,
        vus=vus,
        duration_seconds=config.duration_seconds,
        warmup_duration_seconds=0,
        amount=config.amount,
        timeout_seconds=config.timeout_seconds,
        output=output,
        data_model_url=config.data_model_url,
        ingestion_url=config.ingestion_url,
        decision_engine_url=config.decision_engine_url,
        auth_token=config.auth_token,
        scenario_threshold=config.scenario_threshold,
    )


class RuleComplexityHarness(ThroughputHarness):
    def __init__(
        self,
        config: BaseConfig,
        variant_name: str,
        related_seed_count: int,
        ingestion_database_url: str | None,
        history_object_pool_size: int = 100,
    ) -> None:
        super().__init__(config)
        self.variant_name = variant_name
        self.related_seed_count = related_seed_count
        self.ingestion_database_url = ingestion_database_url
        self.history_object_pool_size = history_object_pool_size
        self.account_object_type = ""
        self.owner_id = f"owner_{variant_name}_{unique_name('seed')}"
        self.stable_object_id = f"{variant_name}_target_{unique_name('object')}"
        self.history_object_ids: list[str] = []
        self.history_object_index = 0
        self.seeded_counts: dict[str, int] = {}
        self.variant: VariantDefinition | None = None
        self.model_fields_by_table: dict[str, list[dict[str, Any]]] = {}

    async def bootstrap(self) -> None:
        await self.wait_until_ready(self.data_model, "data-model")
        await self.wait_until_ready(self.ingestion, "ingestion")
        await self.wait_until_ready(self.decision_engine, "decision-engine")

        tenant = (
            await self.request(
                self.data_model,
                "POST",
                "/v1/tenants",
                201,
                json={"name": unique_name("rule_complexity_tenant"), "external_key": unique_name("rule_complexity_ext")},
            )
        )["tenant"]
        self.tenant_id = tenant["id"]
        await self.request(self.data_model, "POST", f"/v1/tenants/{self.tenant_id}/provision", 200)
        await self.bootstrap_model()
        self.variant = build_variant(self.variant_name, self.object_type, self.owner_id, self.related_seed_count, self.config.scenario_threshold)
        if self.ingestion_database_url and (self.variant.seed_account or self.variant.seed_related_records):
            await asyncio.to_thread(self.materialize_ingestion_schema, self.ingestion_database_url)
        await self.seed_variant_data(self.variant)
        if self.variant.seed_decision_history:
            await self.seed_prior_review_decision()
        await self.bootstrap_scenario(self.variant)

    async def bootstrap_model(self) -> None:
        transactions = (
            await self.request(
                self.data_model,
                "POST",
                f"/v1/tenants/{self.tenant_id}/tables",
                201,
                json={
                    "name": unique_name("transactions"),
                    "description": "Rule complexity transaction table",
                    "alias": "Transactions",
                    "semantic_type": "entity",
                },
            )
        )["table"]
        accounts = (
            await self.request(
                self.data_model,
                "POST",
                f"/v1/tenants/{self.tenant_id}/tables",
                201,
                json={
                    "name": unique_name("accounts"),
                    "description": "Rule complexity account table",
                    "alias": "Accounts",
                    "semantic_type": "entity",
                },
            )
        )["table"]
        self.object_type = transactions["name"]
        self.account_object_type = accounts["name"]

        fields: dict[str, dict[str, Any]] = {}
        for item in [
            {"name": "amount", "data_type": "int", "nullable": False},
            {
                "name": "status",
                "data_type": "string",
                "nullable": False,
                "is_enum": True,
                "enum_values": [
                    {"value": "pending", "label": "Pending", "sort_order": 10},
                    {"value": "review", "label": "Review", "sort_order": 20},
                ],
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
            created = (
                await self.request(self.data_model, "POST", f"/v1/tables/{transactions['id']}/fields", 201, json=item)
            )["field"]
            fields[created["name"]] = created

        account_key = (
            await self.request(
                self.data_model,
                "POST",
                f"/v1/tables/{accounts['id']}/fields",
                201,
                json={"name": "account_key", "data_type": "string", "nullable": False, "is_unique": True},
            )
        )["field"]
        await self.request(
            self.data_model,
            "POST",
            f"/v1/tables/{accounts['id']}/fields",
            201,
            json={"name": "account_status", "data_type": "string", "nullable": False},
        )
        await self.request(
            self.data_model,
            "POST",
            f"/v1/tables/{accounts['id']}/fields",
            201,
            json={"name": "owner_id", "data_type": "string", "nullable": True},
        )
        await self.request(
            self.data_model,
            "POST",
            f"/v1/tenants/{self.tenant_id}/links",
            201,
            json={
                "name": "account",
                "parent_table_id": accounts["id"],
                "parent_field_id": account_key["id"],
                "child_table_id": transactions["id"],
                "child_field_id": fields["account_id"]["id"],
            },
        )
        self.model_fields_by_table = {
            self.object_type: [
                {"name": "amount", "data_type": "int", "is_unique": False},
                {"name": "status", "data_type": "string", "is_unique": False},
                {"name": "account_id", "data_type": "string", "is_unique": False},
                {"name": "ip", "data_type": "ip_address", "is_unique": False},
                {"name": "merchant", "data_type": "string", "is_unique": False},
                {"name": "email", "data_type": "string", "is_unique": False},
                {"name": "country", "data_type": "string", "is_unique": False},
                {"name": "owner_id", "data_type": "string", "is_unique": False},
                {"name": "event_time", "data_type": "timestamp", "is_unique": False},
                {"name": "note", "data_type": "string", "is_unique": False},
            ],
            self.account_object_type: [
                {"name": "account_key", "data_type": "string", "is_unique": True},
                {"name": "account_status", "data_type": "string", "is_unique": False},
                {"name": "owner_id", "data_type": "string", "is_unique": False},
            ],
        }

    def materialize_ingestion_schema(self, database_url: str) -> None:
        if importlib.util.find_spec("psycopg") is None:
            raise RuntimeError("--ingestion-database-url requires the psycopg Python package")
        import psycopg
        from psycopg import sql

        schema_name = "tenant_" + self.tenant_id.replace("-", "")
        with psycopg.connect(database_url) as conn:
            with conn.cursor() as cur:
                cur.execute(sql.SQL("CREATE SCHEMA IF NOT EXISTS {}").format(sql.Identifier(schema_name)))
                for table_name, fields in self.model_fields_by_table.items():
                    table_ident = sql.Identifier(schema_name, table_name)
                    cur.execute(sql.SQL("""
                        CREATE TABLE IF NOT EXISTS {} (
                            id UUID NOT NULL PRIMARY KEY,
                            object_id TEXT NOT NULL,
                            updated_at TIMESTAMPTZ NOT NULL,
                            valid_from TIMESTAMPTZ NOT NULL DEFAULT NOW(),
                            valid_until TIMESTAMPTZ NOT NULL DEFAULT 'INFINITY'
                        )
                    """).format(table_ident))
                    for field_def in fields:
                        cur.execute(
                            sql.SQL("ALTER TABLE {} ADD COLUMN IF NOT EXISTS {} {}").format(
                                table_ident,
                                sql.Identifier(field_def["name"]),
                                sql.SQL(pg_type(field_def["data_type"])),
                            )
                        )
                    cur.execute(
                        sql.SQL("CREATE UNIQUE INDEX IF NOT EXISTS {} ON {} ({})").format(
                            sql.Identifier(f"{table_name}_object_id_uidx"),
                            table_ident,
                            sql.Identifier("object_id"),
                        )
                    )
                    for field_def in fields:
                        if field_def.get("is_unique"):
                            cur.execute(
                                sql.SQL("CREATE UNIQUE INDEX IF NOT EXISTS {} ON {} ({})").format(
                                    sql.Identifier(f"{table_name}_{field_def['name']}_uidx"),
                                    table_ident,
                                    sql.Identifier(field_def["name"]),
                                )
                            )
            conn.commit()

    async def seed_variant_data(self, variant: VariantDefinition) -> None:
        account_key = self.account_key()
        if variant.seed_account:
            await self.ingest(self.account_object_type, {
                "object_id": account_key,
                "account_key": account_key,
                "account_status": "active",
                "owner_id": self.owner_id,
            })
            self.seeded_counts["accounts"] = 1
        if variant.seed_custom_list:
            await self.request(
                self.decision_engine,
                "POST",
                f"/v1/tenants/{self.tenant_id}/platform/custom-list-entries",
                201,
                json={"list_name": "blocked_emails", "value": "risk@example.com"},
            )
            self.seeded_counts["custom_list_entries"] = 1
        if variant.seed_related_records:
            rows = []
            for index in range(self.related_seed_count):
                payload = self.payload(f"seed_{index}_{unique_name('txn')}")
                payload["amount"] = 100 + index
                payload["owner_id"] = self.owner_id
                payload["status"] = "pending"
                rows.append(payload)
            await self.ingest_batch(self.object_type, rows)
            self.seeded_counts["related_records"] = len(rows)

    async def bootstrap_scenario(self, variant: VariantDefinition) -> None:
        scenario = (
            await self.request(
                self.decision_engine,
                "POST",
                f"/v1/tenants/{self.tenant_id}/scenarios",
                201,
                json={"name": unique_name(f"rule_complexity_{variant.name}"), "trigger_object_type": self.object_type},
            )
        )["scenario"]
        self.scenario_id = scenario["id"]
        iteration = (
            await self.request(
                self.decision_engine,
                "POST",
                f"/v1/tenants/{self.tenant_id}/scenarios/{self.scenario_id}/iterations",
                201,
            )
        )["iteration"]
        await self.request(
            self.decision_engine,
            "PUT",
            f"/v1/tenants/{self.tenant_id}/scenarios/{self.scenario_id}/iterations/{iteration['id']}",
            200,
            json={
                "trigger_formula": true_node(),
                "score_review_threshold": 10,
                "score_block_and_review_threshold": 30,
                "score_decline_threshold": 60,
                "schedule": "",
            },
        )
        rule = (
            await self.request(
                self.decision_engine,
                "POST",
                f"/v1/tenants/{self.tenant_id}/scenarios/{self.scenario_id}/iterations/{iteration['id']}/rules",
                201,
                json={
                    "display_order": 1,
                    "name": variant.name,
                    "description": variant.description,
                    "formula": variant.formula,
                    "score_modifier": 25,
                    "rule_group": "rule_complexity",
                    "stable_rule_id": unique_name(f"rule_complexity_{variant.name}"),
                },
            )
        )["rule"]
        self.rule_id = rule["id"]
        validation = await self.request(
            self.decision_engine,
            "POST",
            f"/v1/tenants/{self.tenant_id}/scenarios/{self.scenario_id}/iterations/{iteration['id']}/validate",
            200,
        )
        if validation.get("validation", {}).get("valid") is not True:
            raise RuntimeError(f"iteration validation failed for {variant.name}: {json.dumps(validation, default=str)}")
        await self.request(
            self.decision_engine,
            "POST",
            f"/v1/tenants/{self.tenant_id}/scenarios/{self.scenario_id}/iterations/{iteration['id']}/commit",
            200,
        )
        await self.request(
            self.decision_engine,
            "POST",
            f"/v1/tenants/{self.tenant_id}/scenarios/{self.scenario_id}/publications",
            200,
            json={"action": "publish", "iteration_id": iteration["id"]},
        )

    async def seed_prior_review_decision(self) -> None:
        seed_variant = VariantDefinition(
            name="history_seed",
            description="Temporary scenario used to create a prior review decision.",
            formula=amount_gt_node(0),
        )
        original_scenario_id = self.scenario_id
        original_rule_id = self.rule_id
        await self.bootstrap_scenario(seed_variant)
        seed_scenario_id = self.scenario_id
        pool_size = max(1, self.history_object_pool_size)
        self.history_object_ids = [
            f"{self.variant_name}_history_{index}_{unique_name('object')}"
            for index in range(pool_size)
        ]
        for object_id in self.history_object_ids:
            payload = self.payload(object_id)
            await self.request(
                self.decision_engine,
                "POST",
                f"/v1/tenants/{self.tenant_id}/scenarios/{seed_scenario_id}/evaluate",
                200,
                json={"object_id": object_id, "object_type": self.object_type, "fields": payload},
            )
        self.scenario_id = original_scenario_id
        self.rule_id = original_rule_id
        self.seeded_counts["prior_decisions"] = len(self.history_object_ids)
        self.seeded_counts["history_object_pool_size"] = len(self.history_object_ids)
        self.seeded_counts["seed_scenarios"] = self.seeded_counts.get("seed_scenarios", 0) + 1

    def account_key(self) -> str:
        return f"acct_{self.owner_id}"

    def payload(self, object_id: str) -> dict[str, Any]:
        payload = record_payload(object_id=object_id, amount=self.config.amount)
        payload["account_id"] = self.account_key()
        payload["owner_id"] = self.owner_id
        payload["email"] = "risk@example.com"
        payload["merchant"] = "ITC Market"
        payload["country"] = "gh"
        payload["status"] = "pending"
        return payload

    async def ingest(self, object_type: str, payload: dict[str, Any]) -> None:
        await self.ingest_with_schema_retry(f"/v1/tenants/{self.tenant_id}/ingest/{object_type}", payload)

    async def ingest_batch(self, object_type: str, rows: list[dict[str, Any]]) -> None:
        batch_size = 500
        for start in range(0, len(rows), batch_size):
            await self.ingest_with_schema_retry(
                f"/v1/tenants/{self.tenant_id}/ingest/{object_type}/batch",
                rows[start:start + batch_size],
            )

    async def ingest_with_schema_retry(self, path: str, payload: Any) -> None:
        last_error: RuntimeError | None = None
        for attempt in range(12):
            try:
                await self.request(self.ingestion, "POST", path, 200, json=payload)
                return
            except RuntimeError as exc:
                if "SQLSTATE 42P01" not in str(exc) and "does not exist" not in str(exc):
                    raise
                last_error = exc
                await asyncio.sleep(min(0.25 * (attempt + 1), 2.0))
        assert last_error is not None
        raise RuntimeError(
            f"{last_error}\n"
            "Ingestion could not find the tenant table. If data-model and ingestion use separate Postgres databases, "
            "rerun with --ingestion-database-url pointing at ingestion's database, or run both services against the same DATABASE_URL."
        ) from last_error

    async def evaluate_once(self) -> None:
        assert self.variant is not None
        if self.variant.seed_decision_history and self.history_object_ids:
            object_id = self.history_object_ids[self.history_object_index % len(self.history_object_ids)]
            self.history_object_index += 1
        elif self.variant.stable_object_id:
            object_id = self.stable_object_id
        else:
            object_id = self.next_object_id()
        payload = self.payload(object_id)
        await self.request(
            self.decision_engine,
            "POST",
            f"/v1/tenants/{self.tenant_id}/scenarios/{self.scenario_id}/evaluate",
            200,
            json={"object_id": object_id, "object_type": self.object_type, "fields": payload},
        )


def build_variant(name: str, object_type: str, owner_id: str, related_seed_count: int, threshold: int) -> VariantDefinition:
    variants = {
        "baseline_payload": VariantDefinition(
            name="baseline_payload",
            description="Simple amount threshold payload rule.",
            formula=amount_gt_node(threshold),
        ),
        "nested_payload": VariantDefinition(
            name="nested_payload",
            description="Nested payload-only logical rule.",
            formula=nested_payload_formula(threshold),
        ),
        "custom_list": VariantDefinition(
            name="custom_list",
            description="Custom list membership lookup against email.",
            formula=custom_list_formula(),
            seed_custom_list=True,
        ),
        "related_field": VariantDefinition(
            name="related_field",
            description="Related account traversal and field read.",
            formula=related_field_formula(),
            seed_account=True,
        ),
        "decision_history": VariantDefinition(
            name="decision_history",
            description="Decision history lookup for the evaluated object.",
            formula=decision_history_formula(),
            stable_object_id=True,
            seed_decision_history=True,
        ),
        "related_records_count": VariantDefinition(
            name="related_records_count",
            description="List related records and count owner matches.",
            formula=related_records_count_formula(object_type, owner_id, related_seed_count),
            seed_related_records=True,
        ),
        "aggregate_count_pushdown": VariantDefinition(
            name="aggregate_count_pushdown",
            description="Remote aggregate count over seeded transaction records.",
            formula=aggregate_count_formula(object_type, owner_id, related_seed_count),
            seed_related_records=True,
        ),
        "mixed_heavy": VariantDefinition(
            name="mixed_heavy",
            description="Nested payload, custom list, related field, and aggregate count.",
            formula=mixed_heavy_formula(object_type, owner_id, related_seed_count),
            seed_account=True,
            seed_custom_list=True,
            seed_related_records=True,
        ),
    }
    if name not in variants:
        raise ValueError(f"unknown variant {name!r}; expected one of {', '.join(DEFAULT_VARIANTS)}")
    return variants[name]


async def run_one_request(harness: RuleComplexityHarness, metrics: ClosedLoopMetrics, vu_id: int) -> None:
    started_at = time.perf_counter()
    metrics.attempted += 1
    try:
        await harness.evaluate_once()
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
        metrics.successes += 1
        assert metrics.latencies_ms is not None
        metrics.latencies_ms.append((time.perf_counter() - started_at) * 1000.0)
        assert metrics.per_vu_successes is not None
        metrics.per_vu_successes[vu_id] = metrics.per_vu_successes.get(vu_id, 0) + 1


async def worker(
    vu_id: int,
    harness: RuleComplexityHarness,
    metrics: ClosedLoopMetrics,
    start_gate: asyncio.Event,
    deadline: float,
) -> None:
    await start_gate.wait()
    while time.perf_counter() < deadline:
        await run_one_request(harness, metrics, vu_id)


async def run_closed_loop(harness: RuleComplexityHarness, vus: int, duration_seconds: float) -> tuple[ClosedLoopMetrics, float]:
    metrics = ClosedLoopMetrics()
    start_gate = asyncio.Event()
    started_at = time.perf_counter()
    deadline = started_at + duration_seconds
    tasks = [asyncio.create_task(worker(vu_id, harness, metrics, start_gate, deadline)) for vu_id in range(vus)]
    start_gate.set()
    await asyncio.gather(*tasks)
    return metrics, time.perf_counter() - started_at


def trial_output_path(config: Config, variant: str, vus: int) -> str:
    duration_label = f"{config.duration_seconds:g}s"
    return str(Path(config.output_dir) / f"trial-{variant}-{vus}-vus-{duration_label}.json")


def summarize_trial(
    config: Config,
    variant: VariantDefinition,
    harness: RuleComplexityHarness,
    vus: int,
    output: str,
    metrics: ClosedLoopMetrics,
    elapsed_seconds: float,
) -> dict[str, Any]:
    latencies = metrics.latencies_ms or []
    per_vu_counts = list((metrics.per_vu_successes or {}).values())
    achieved_eps = metrics.successes / elapsed_seconds if elapsed_seconds > 0 else 0.0
    return {
        "summary_version": 1,
        "test": {
            "name": "decision_engine_rule_complexity_scaling",
            "objective": "Measure how direct decision evaluation performance changes as rule complexity changes.",
            "system_under_test": "POST /v1/tenants/{tenantId}/scenarios/{scenarioId}/evaluate",
            "load_model": "Closed loop: each VU sends a new request immediately after its previous request completes.",
            "sustainability_definition": "0% errors and 0% timeouts during measured run.",
        },
        "environment": environment_metadata(to_base_config(config, output, vus)),
        "setup": {
            "variant": variant.name,
            "variant_description": variant.description,
            "tenant_id": harness.tenant_id,
            "object_type": harness.object_type,
            "account_object_type": harness.account_object_type,
            "scenario_id": harness.scenario_id,
            "rule_id": harness.rule_id,
            "scenario_count": 1,
            "rules_per_scenario": 1,
            "seeded_counts": harness.seeded_counts,
            "related_seed_count": config.related_seed_count,
            "stable_object_id": variant.stable_object_id,
            "history_object_pool_size": config.history_object_pool_size if variant.seed_decision_history else 0,
        },
        "run": {
            "variant": variant.name,
            "configured_vus": vus,
            "duration_seconds": config.duration_seconds,
            "timeout_seconds": config.timeout_seconds,
            "amount": config.amount,
            "output": output,
            "auth_token": "set" if config.auth_token else None,
        },
        "workload_counts": {
            "configured_vus": vus,
            "completed_evaluations": metrics.completed,
            "successful_evaluations": metrics.successes,
            "failed_evaluations": metrics.failures,
            "timeouts": metrics.timeouts,
            "dropped_requests": 0,
        },
        "rates": {
            "achieved_successful_evaluations_per_second": achieved_eps,
            "success_rate": metrics.successes / metrics.completed if metrics.completed else 0.0,
            "error_rate": metrics.failures / metrics.completed if metrics.completed else 0.0,
            "timeout_rate": metrics.timeouts / metrics.completed if metrics.completed else 0.0,
        },
        "per_vu_successes": {
            "min": min(per_vu_counts) if per_vu_counts else 0,
            "max": max(per_vu_counts) if per_vu_counts else 0,
            "avg": statistics.fmean(per_vu_counts) if per_vu_counts else 0,
        },
        "latency_ms": {
            "avg": statistics.fmean(latencies) if latencies else None,
            "p50": percentile(latencies, 50),
            "p95": percentile(latencies, 95),
            "p99": percentile(latencies, 99),
            "max": max(latencies) if latencies else None,
        },
        "result": {
            "sustainable": metrics.failures == 0 and metrics.timeouts == 0,
            "elapsed_seconds": elapsed_seconds,
            "requested_duration_seconds": config.duration_seconds,
        },
        "sample_errors": metrics.errors or [],
    }


def apply_baseline_ratios(trials: list[dict[str, Any]]) -> None:
    baseline_by_vus = {
        trial["run"]["configured_vus"]: trial
        for trial in trials
        if trial["run"]["variant"] == "baseline_payload"
    }
    for trial in trials:
        baseline = baseline_by_vus.get(trial["run"]["configured_vus"])
        ratios: dict[str, float | None] = {}
        if baseline is not None:
            for key in ["p50", "p95", "p99", "max", "avg"]:
                value = trial["latency_ms"].get(key)
                base = baseline["latency_ms"].get(key)
                ratios[f"latency_{key}_ratio"] = (value / base) if value is not None and base else None
            eps = trial["rates"]["achieved_successful_evaluations_per_second"]
            base_eps = baseline["rates"]["achieved_successful_evaluations_per_second"]
            ratios["achieved_eps_ratio"] = (eps / base_eps) if base_eps else None
        trial["baseline_comparison"] = {
            "baseline_variant": "baseline_payload",
            "baseline_available": baseline is not None,
            **ratios,
        }


def aggregate_summary(config: Config, trials: list[dict[str, Any]]) -> dict[str, Any]:
    apply_baseline_ratios(trials)
    run_config = asdict(config)
    run_config["auth_token"] = "set" if config.auth_token else None
    run_config["ingestion_database_url"] = "set" if config.ingestion_database_url else None
    ranked_by_p95 = sorted(
        trials,
        key=lambda item: item.get("baseline_comparison", {}).get("latency_p95_ratio") or 0,
        reverse=True,
    )
    ranked_by_eps_drop = sorted(
        trials,
        key=lambda item: item.get("baseline_comparison", {}).get("achieved_eps_ratio") or 1,
    )
    return {
        "summary_version": 1,
        "test": {
            "name": "decision_engine_rule_complexity_scaling",
            "objective": "Compare direct decision evaluation performance across rule complexity variants.",
        },
        "environment": environment_metadata(to_base_config(config, str(Path(config.output_dir) / "summary.json"), max(config.vus))),
        "run": run_config,
        "trial_count": len(trials),
        "trials": trials,
        "rankings": {
            "highest_p95_latency_ratio": [
                compact_ranking_item(item, "latency_p95_ratio")
                for item in ranked_by_p95
                if item.get("baseline_comparison", {}).get("latency_p95_ratio") is not None
            ],
            "lowest_achieved_eps_ratio": [
                compact_ranking_item(item, "achieved_eps_ratio")
                for item in ranked_by_eps_drop
                if item.get("baseline_comparison", {}).get("achieved_eps_ratio") is not None
            ],
        },
    }


def compact_ranking_item(trial: dict[str, Any], ratio_key: str) -> dict[str, Any]:
    return {
        "variant": trial["run"]["variant"],
        "vus": trial["run"]["configured_vus"],
        ratio_key: trial.get("baseline_comparison", {}).get(ratio_key),
        "achieved_eps": trial["rates"]["achieved_successful_evaluations_per_second"],
        "p95_ms": trial["latency_ms"]["p95"],
        "p99_ms": trial["latency_ms"]["p99"],
        "max_ms": trial["latency_ms"]["max"],
        "failures": trial["workload_counts"]["failed_evaluations"],
        "timeouts": trial["workload_counts"]["timeouts"],
    }


def print_trial_summary(summary: dict[str, Any]) -> None:
    latency = summary["latency_ms"]
    rates = summary["rates"]
    counts = summary["workload_counts"]
    print(
        f"  {summary['run']['variant']} @ {summary['run']['configured_vus']} VUs: "
        f"{rates['achieved_successful_evaluations_per_second']:.2f} EPS, "
        f"p95 {format_optional(latency['p95'])} ms, "
        f"p99 {format_optional(latency['p99'])} ms, "
        f"failures {counts['failed_evaluations']}, timeouts {counts['timeouts']}"
    )


def parse_csv_list(value: str, valid: set[str] | None = None) -> list[str]:
    items = [item.strip() for item in value.split(",") if item.strip()]
    if valid is not None:
        unknown = [item for item in items if item not in valid]
        if unknown:
            raise SystemExit(f"unknown values: {', '.join(unknown)}; expected one of {', '.join(sorted(valid))}")
    if not items:
        raise SystemExit("list must not be empty")
    return items


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="Decision-engine rule complexity scaling stress test.")
    parser.add_argument("--variants", default=",".join(DEFAULT_VARIANTS), help="Comma-separated variant names.")
    parser.add_argument("--vus", default="5,10,30", help="Comma-separated closed-loop VU levels.")
    parser.add_argument("--duration", type=float, default=60.0, help="Measured run duration in seconds.")
    parser.add_argument("--amount", type=int, default=1800, help="Amount value used in generated payloads.")
    parser.add_argument("--timeout", type=float, default=30.0, help="Per-request timeout in seconds.")
    parser.add_argument("--scenario-threshold", type=int, default=1000, help="Simple amount rule threshold.")
    parser.add_argument("--related-seed-count", type=int, default=100, help="Seeded related transaction count.")
    parser.add_argument(
        "--history-object-pool-size",
        type=int,
        default=100,
        help="Number of preseeded objects used by decision-history variants.",
    )
    parser.add_argument("--output-dir", help="Output directory. Defaults to timestamped stress-tests/rule-complexity-runs folder.")
    parser.add_argument("--data-model-url", default=os.getenv("DATA_MODEL_URL", "http://127.0.0.1:8080"))
    parser.add_argument("--ingestion-url", default=os.getenv("INGESTION_URL", "http://127.0.0.1:8081"))
    parser.add_argument("--decision-engine-url", default=os.getenv("DECISION_ENGINE_URL", "http://127.0.0.1:8082"))
    parser.add_argument("--auth-token", default=os.getenv("SERVICE_AUTH_TOKEN"))
    parser.add_argument(
        "--ingestion-database-url",
        default=os.getenv("INGESTION_DATABASE_URL"),
        help="Optional Postgres URL used to materialize tenant tables when ingestion uses a separate DB.",
    )
    return parser


def parse_config() -> Config:
    args = build_parser().parse_args()
    variants = parse_csv_list(args.variants, set(DEFAULT_VARIANTS))
    vus = [int(item) for item in parse_csv_list(args.vus)]
    if "baseline_payload" not in variants:
        variants = ["baseline_payload", *variants]
    if any(item <= 0 for item in vus):
        raise SystemExit("--vus values must be greater than 0")
    if args.duration <= 0:
        raise SystemExit("--duration must be greater than 0")
    if args.timeout <= 0:
        raise SystemExit("--timeout must be greater than 0")
    if args.related_seed_count <= 0:
        raise SystemExit("--related-seed-count must be greater than 0")
    if args.history_object_pool_size <= 0:
        raise SystemExit("--history-object-pool-size must be greater than 0")
    output_dir = args.output_dir or str(Path("stress-tests/rule-complexity-runs") / utc_now().replace(":", "").replace(".", "-"))
    return Config(
        variants=variants,
        vus=vus,
        duration_seconds=args.duration,
        amount=args.amount,
        timeout_seconds=args.timeout,
        output_dir=output_dir,
        related_seed_count=args.related_seed_count,
        data_model_url=args.data_model_url.rstrip("/"),
        ingestion_url=args.ingestion_url.rstrip("/"),
        decision_engine_url=args.decision_engine_url.rstrip("/"),
        auth_token=args.auth_token,
        scenario_threshold=args.scenario_threshold,
        ingestion_database_url=args.ingestion_database_url,
        history_object_pool_size=args.history_object_pool_size,
    )


async def run_trial(config: Config, variant_name: str, vus: int) -> dict[str, Any]:
    output = trial_output_path(config, variant_name, vus)
    harness = RuleComplexityHarness(
        to_base_config(config, output, vus),
        variant_name,
        config.related_seed_count,
        config.ingestion_database_url,
        config.history_object_pool_size,
    )
    try:
        print(f"bootstrapping variant {variant_name} for {vus} VUs...")
        await harness.bootstrap()
        assert harness.variant is not None
        print(f"running {variant_name} for {config.duration_seconds:.0f}s with {vus} VUs...")
        metrics, elapsed = await run_closed_loop(harness, vus, config.duration_seconds)
        summary = summarize_trial(config, harness.variant, harness, vus, output, metrics, elapsed)
        output_path = Path(output)
        output_path.parent.mkdir(parents=True, exist_ok=True)
        output_path.write_text(json.dumps(summary, indent=2, default=str) + "\n")
        print_trial_summary(summary)
        return summary
    finally:
        await harness.close()


async def async_main() -> int:
    config = parse_config()
    trials = []
    Path(config.output_dir).mkdir(parents=True, exist_ok=True)
    for vus in config.vus:
        for variant in config.variants:
            trials.append(await run_trial(config, variant, vus))
    summary = aggregate_summary(config, trials)
    for trial in trials:
        Path(trial["run"]["output"]).write_text(json.dumps(trial, indent=2, default=str) + "\n")
    summary_path = Path(config.output_dir) / "summary.json"
    summary_path.write_text(json.dumps(summary, indent=2, default=str) + "\n")
    print("")
    print("Rule Complexity Scaling Summary")
    print(f"  trials: {len(trials)}")
    print(f"  output: {summary_path}")
    print("  highest p95 latency ratios:")
    for item in summary["rankings"]["highest_p95_latency_ratio"][:10]:
        print(f"    {item['variant']} @ {item['vus']} VUs: {item['latency_p95_ratio']:.2f}x, p95 {format_optional(item['p95_ms'])} ms")
    return 0 if all(trial["result"]["sustainable"] for trial in trials) else 1


def main() -> None:
    raise SystemExit(asyncio.run(async_main()))


if __name__ == "__main__":
    main()
