from __future__ import annotations

import argparse
import asyncio
import importlib.util
import json
import os
import sys
from dataclasses import asdict, dataclass
from datetime import datetime, timedelta, timezone
from pathlib import Path
from typing import Any

from decision_rule_complexity_scaling import pg_type
from decision_throughput_limit import Config as BaseConfig
from decision_throughput_limit import ThroughputHarness, true_node, unique_name, utc_now


ACCOUNT_REF = "2332416370369"
ACCOUNT_CLEAN = "2332400000001"
ACCOUNT_HIGH_AMOUNT = "2332400000002"
ACCOUNT_BURST = "2332400000003"
ACCOUNT_WEEKLY = "2332400000004"
ACCOUNT_SHARED = "2332400000005"
ACCOUNT_FULL_RISK = "2332400000006"
ALT_ACCOUNT_REFS = ["2332448891021", "2332094517780", "2335521199000", "2335511188776"]
PRODUCT_ID = "wallet-transfer-standard"
BENEFICIARY_ID = "ben-wallet-001"
NEW_BENEFICIARY_ID = "ben-wallet-new"
KNOWN_DEVICE_ID = "dev-ama-known-001"
NEW_DEVICE_ID = "dev-ama-new-001"
SHARED_DEVICE_ID = "dev-shared-risk-001"
NORMAL_IP = "102.176.10.20"
HIGH_RISK_IP = "45.155.205.99"
BASE_TIME = datetime(2026, 7, 6, 12, 0, 0, tzinfo=timezone.utc)


@dataclass(frozen=True)
class Config:
    timeout_seconds: float
    output: str
    data_model_url: str
    ingestion_url: str
    decision_engine_url: str
    auth_token: str | None
    ingestion_database_url: str | None


@dataclass(frozen=True)
class RuleDef:
    name: str
    group: str
    description: str
    score: int
    formula: dict[str, Any]


@dataclass(frozen=True)
class DemoCase:
    name: str
    object_id: str
    fields: dict[str, Any]
    expected_rules: set[str]


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


def list_node(*children: dict[str, Any]) -> dict[str, Any]:
    return fn("List", *children)


def gt(left: dict[str, Any], right: dict[str, Any]) -> dict[str, Any]:
    return fn("gt", left, right)


def gte(left: dict[str, Any], right: dict[str, Any]) -> dict[str, Any]:
    return fn("gte", left, right)


def lt(left: dict[str, Any], right: dict[str, Any]) -> dict[str, Any]:
    return fn("lt", left, right)


def neq(left: dict[str, Any], right: dict[str, Any]) -> dict[str, Any]:
    return fn("neq", left, right)


def related(path: str, related_field: str) -> dict[str, Any]:
    return fn("related_field", path=const(path), field=const(related_field))


def filter_node(table_name: str, field_name: str, operator: str, value: dict[str, Any]) -> dict[str, Any]:
    return fn(
        "Filter",
        tableName=const(table_name),
        fieldName=const(field_name),
        operator=const(operator),
        value=value,
    )


def time_add(timestamp_field: dict[str, Any], duration: str, sign: str = "-") -> dict[str, Any]:
    return fn("TimeAdd", timestampField=timestamp_field, duration=const(duration), sign=const(sign))


def aggregator(
    table_name: str,
    field_name: str,
    aggregator_name: str,
    *filters: dict[str, Any],
) -> dict[str, Any]:
    return fn(
        "Aggregator",
        tableName=const(table_name),
        fieldName=const(field_name),
        aggregator=const(aggregator_name),
        filters=list_node(*filters),
    )


def iso(value: datetime) -> str:
    return value.isoformat().replace("+00:00", "Z")


def base_config(config: Config) -> BaseConfig:
    return BaseConfig(
        rate=0,
        vus=1,
        duration_seconds=0,
        warmup_duration_seconds=0,
        amount=0,
        timeout_seconds=config.timeout_seconds,
        output=config.output,
        data_model_url=config.data_model_url,
        ingestion_url=config.ingestion_url,
        decision_engine_url=config.decision_engine_url,
        auth_token=config.auth_token,
        scenario_threshold=0,
    )


class WalletTransferDemoHarness(ThroughputHarness):
    def __init__(self, config: Config) -> None:
        super().__init__(base_config(config))
        self.demo_config = config
        self.account_object_type = ""
        self.product_object_type = ""
        self.beneficiary_object_type = ""
        self.transaction_table_id = ""
        self.account_table_id = ""
        self.product_table_id = ""
        self.beneficiary_table_id = ""
        self.model_fields_by_table: dict[str, list[dict[str, Any]]] = {}
        self.rule_defs: list[RuleDef] = []

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
                json={"name": unique_name("wallet_demo_tenant"), "external_key": unique_name("wallet_demo_ext")},
            )
        )["tenant"]
        self.tenant_id = tenant["id"]
        await self.request(self.data_model, "POST", f"/v1/tenants/{self.tenant_id}/provision", 200)
        await self.bootstrap_model()
        if self.demo_config.ingestion_database_url:
            await asyncio.to_thread(self.materialize_ingestion_schema, self.demo_config.ingestion_database_url)
        await self.seed_reference_data()
        await self.seed_history()
        await self.seed_platform_lists()
        await self.bootstrap_scenario()

    async def create_table(self, name: str, alias: str, description: str) -> dict[str, Any]:
        return (
            await self.request(
                self.data_model,
                "POST",
                f"/v1/tenants/{self.tenant_id}/tables",
                201,
                json={
                    "name": unique_name(name),
                    "description": description,
                    "alias": alias,
                    "semantic_type": "entity",
                },
            )
        )["table"]

    async def create_field(self, table_id: str, payload: dict[str, Any]) -> dict[str, Any]:
        return (await self.request(self.data_model, "POST", f"/v1/tables/{table_id}/fields", 201, json=payload))["field"]

    async def bootstrap_model(self) -> None:
        transactions = await self.create_table("wallet_transactions", "Wallet Transactions", "Wallet transfer demo transactions")
        accounts = await self.create_table("wallet_accounts", "Accounts", "Wallet transfer demo accounts")
        products = await self.create_table("wallet_products", "Products", "Wallet transfer demo products")
        beneficiaries = await self.create_table("wallet_beneficiaries", "Beneficiaries", "Wallet transfer demo beneficiaries")
        self.object_type = transactions["name"]
        self.account_object_type = accounts["name"]
        self.product_object_type = products["name"]
        self.beneficiary_object_type = beneficiaries["name"]
        self.transaction_table_id = transactions["id"]
        self.account_table_id = accounts["id"]
        self.product_table_id = products["id"]
        self.beneficiary_table_id = beneficiaries["id"]

        tx_fields = {}
        for item in [
            {"name": "account_ref", "data_type": "string", "nullable": False},
            {"name": "processor", "data_type": "string", "nullable": False},
            {"name": "product_id", "data_type": "string", "nullable": False},
            {"name": "beneficiary_id", "data_type": "string", "nullable": False},
            {"name": "transaction_id", "data_type": "string", "nullable": False, "is_unique": True},
            {"name": "date", "data_type": "timestamp", "nullable": False},
            {"name": "amount", "data_type": "int", "nullable": False},
            {"name": "currency", "data_type": "string", "nullable": False},
            {"name": "country", "data_type": "string", "nullable": False},
            {"name": "channel", "data_type": "string", "nullable": False},
            {"name": "device_id", "data_type": "string", "nullable": False},
            {"name": "ip", "data_type": "string", "nullable": False},
            {"name": "ip_region", "data_type": "string", "nullable": False},
            {"name": "required_kyc_level", "data_type": "int", "nullable": False},
        ]:
            created = await self.create_field(transactions["id"], item)
            tx_fields[created["name"]] = created

        account_key = await self.create_field(
            accounts["id"],
            {"name": "account_ref", "data_type": "string", "nullable": False, "is_unique": True},
        )
        for item in [
            {"name": "customer_name", "data_type": "string", "nullable": False},
            {"name": "kyc_level", "data_type": "int", "nullable": False},
            {"name": "usual_ip_region", "data_type": "string", "nullable": False},
            {"name": "known_device_ids", "data_type": "string", "nullable": False},
        ]:
            await self.create_field(accounts["id"], item)

        product_key = await self.create_field(
            products["id"],
            {"name": "product_id", "data_type": "string", "nullable": False, "is_unique": True},
        )
        for item in [
            {"name": "product_name", "data_type": "string", "nullable": False},
            {"name": "max_amount", "data_type": "int", "nullable": False},
            {"name": "status", "data_type": "string", "nullable": False},
        ]:
            await self.create_field(products["id"], item)

        beneficiary_key = await self.create_field(
            beneficiaries["id"],
            {"name": "beneficiary_id", "data_type": "string", "nullable": False, "is_unique": True},
        )
        for item in [
            {"name": "account_ref", "data_type": "string", "nullable": False},
            {"name": "beneficiary_name", "data_type": "string", "nullable": False},
            {"name": "first_seen_at", "data_type": "timestamp", "nullable": False},
        ]:
            await self.create_field(beneficiaries["id"], item)

        await self.create_link("account", accounts["id"], account_key["id"], transactions["id"], tx_fields["account_ref"]["id"])
        await self.create_link("product", products["id"], product_key["id"], transactions["id"], tx_fields["product_id"]["id"])
        await self.create_link("beneficiary", beneficiaries["id"], beneficiary_key["id"], transactions["id"], tx_fields["beneficiary_id"]["id"])

        self.model_fields_by_table = {
            self.object_type: [
                {"name": name, "data_type": data_type, "is_unique": name == "transaction_id"}
                for name, data_type in [
                    ("account_ref", "string"),
                    ("processor", "string"),
                    ("product_id", "string"),
                    ("beneficiary_id", "string"),
                    ("transaction_id", "string"),
                    ("date", "timestamp"),
                    ("amount", "int"),
                    ("currency", "string"),
                    ("country", "string"),
                    ("channel", "string"),
                    ("device_id", "string"),
                    ("ip", "string"),
                    ("ip_region", "string"),
                    ("required_kyc_level", "int"),
                ]
            ],
            self.account_object_type: [
                {"name": "account_ref", "data_type": "string", "is_unique": True},
                {"name": "customer_name", "data_type": "string", "is_unique": False},
                {"name": "kyc_level", "data_type": "int", "is_unique": False},
                {"name": "usual_ip_region", "data_type": "string", "is_unique": False},
                {"name": "known_device_ids", "data_type": "string", "is_unique": False},
            ],
            self.product_object_type: [
                {"name": "product_id", "data_type": "string", "is_unique": True},
                {"name": "product_name", "data_type": "string", "is_unique": False},
                {"name": "max_amount", "data_type": "int", "is_unique": False},
                {"name": "status", "data_type": "string", "is_unique": False},
            ],
            self.beneficiary_object_type: [
                {"name": "beneficiary_id", "data_type": "string", "is_unique": True},
                {"name": "account_ref", "data_type": "string", "is_unique": False},
                {"name": "beneficiary_name", "data_type": "string", "is_unique": False},
                {"name": "first_seen_at", "data_type": "timestamp", "is_unique": False},
            ],
        }

    async def create_link(
        self,
        name: str,
        parent_table_id: str,
        parent_field_id: str,
        child_table_id: str,
        child_field_id: str,
    ) -> None:
        await self.request(
            self.data_model,
            "POST",
            f"/v1/tenants/{self.tenant_id}/links",
            201,
            json={
                "name": name,
                "parent_table_id": parent_table_id,
                "parent_field_id": parent_field_id,
                "child_table_id": child_table_id,
                "child_field_id": child_field_id,
            },
        )

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
                    cur.execute(
                        sql.SQL(
                            """
                            CREATE TABLE IF NOT EXISTS {} (
                                id UUID NOT NULL PRIMARY KEY,
                                object_id TEXT NOT NULL,
                                updated_at TIMESTAMPTZ NOT NULL,
                                valid_from TIMESTAMPTZ NOT NULL DEFAULT NOW(),
                                valid_until TIMESTAMPTZ NOT NULL DEFAULT 'INFINITY'
                            )
                            """
                        ).format(table_ident)
                    )
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
                    if table_name == self.object_type:
                        for index_name, columns in {
                            "acct_date_idx": ["account_ref", "date"],
                            "device_date_idx": ["device_id", "date"],
                        }.items():
                            cur.execute(
                                sql.SQL("CREATE INDEX IF NOT EXISTS {} ON {} ({})").format(
                                    sql.Identifier(f"{table_name}_{index_name}"),
                                    table_ident,
                                    sql.SQL(", ").join(sql.Identifier(column) for column in columns),
                                )
                            )
            conn.commit()

    async def seed_reference_data(self) -> None:
        accounts = [
            {
                "account_ref": ACCOUNT_REF,
                "customer_name": "Ama Mensah",
                "kyc_level": 2,
                "usual_ip_region": "Greater Accra",
                "known_device_ids": f",{KNOWN_DEVICE_ID},{SHARED_DEVICE_ID},",
            },
            {
                "account_ref": ACCOUNT_CLEAN,
                "customer_name": "Clean Demo Customer",
                "kyc_level": 3,
                "usual_ip_region": "Greater Accra",
                "known_device_ids": f",{KNOWN_DEVICE_ID},",
            },
            {
                "account_ref": ACCOUNT_HIGH_AMOUNT,
                "customer_name": "High Amount Demo Customer",
                "kyc_level": 3,
                "usual_ip_region": "Greater Accra",
                "known_device_ids": f",{KNOWN_DEVICE_ID},",
            },
            {
                "account_ref": ACCOUNT_BURST,
                "customer_name": "Burst Demo Customer",
                "kyc_level": 3,
                "usual_ip_region": "Greater Accra",
                "known_device_ids": f",{KNOWN_DEVICE_ID},",
            },
            {
                "account_ref": ACCOUNT_WEEKLY,
                "customer_name": "Weekly Velocity Demo Customer",
                "kyc_level": 3,
                "usual_ip_region": "Greater Accra",
                "known_device_ids": f",{KNOWN_DEVICE_ID},",
            },
            {
                "account_ref": ACCOUNT_SHARED,
                "customer_name": "Shared Device Demo Customer",
                "kyc_level": 3,
                "usual_ip_region": "Greater Accra",
                "known_device_ids": f",{SHARED_DEVICE_ID},",
            },
            {
                "account_ref": ACCOUNT_FULL_RISK,
                "customer_name": "Full Risk Demo Customer",
                "kyc_level": 2,
                "usual_ip_region": "Greater Accra",
                "known_device_ids": f",{KNOWN_DEVICE_ID},",
            },
            *[
                {
                    "account_ref": account_ref,
                    "customer_name": f"Shared Device Customer {index + 1}",
                    "kyc_level": 3,
                    "usual_ip_region": "Greater Accra",
                    "known_device_ids": f",{SHARED_DEVICE_ID},",
                }
                for index, account_ref in enumerate(ALT_ACCOUNT_REFS)
            ],
        ]
        for account in accounts:
            await self.ingest(self.account_object_type, {"object_id": account["account_ref"], **account})

        await self.ingest(
            self.product_object_type,
            {
                "object_id": PRODUCT_ID,
                "product_id": PRODUCT_ID,
                "product_name": "Mobile Wallet Transfer",
                "max_amount": 5_000,
                "status": "active",
            },
        )
        for beneficiary_id, first_seen_at in [
            (BENEFICIARY_ID, BASE_TIME - timedelta(days=30)),
            (NEW_BENEFICIARY_ID, BASE_TIME - timedelta(hours=2)),
        ]:
            await self.ingest(
                self.beneficiary_object_type,
                {
                    "object_id": beneficiary_id,
                    "beneficiary_id": beneficiary_id,
                    "account_ref": ACCOUNT_REF,
                    "beneficiary_name": f"Beneficiary {beneficiary_id}",
                    "first_seen_at": iso(first_seen_at),
                },
            )

    async def seed_history(self) -> None:
        rows: list[dict[str, Any]] = []
        for account_ref in [ACCOUNT_CLEAN, ACCOUNT_BURST, ACCOUNT_WEEKLY, ACCOUNT_SHARED, ACCOUNT_REF]:
            for index in range(10):
                rows.append(
                    self.transaction_payload(
                        f"hist_neutral_{account_ref}_{index}",
                        amount=3_000,
                        account_ref=account_ref,
                        at=BASE_TIME - timedelta(days=20, minutes=index),
                    )
                )
        for index in range(20):
            rows.append(
                self.transaction_payload(
                    f"hist_high_avg_{index}",
                    amount=100,
                    account_ref=ACCOUNT_HIGH_AMOUNT,
                    at=BASE_TIME - timedelta(days=10, minutes=index),
                )
            )
        for index in range(11):
            rows.append(
                self.transaction_payload(
                    f"hist_burst_{index}",
                    amount=100,
                    account_ref=ACCOUNT_BURST,
                    at=BASE_TIME - timedelta(minutes=50 - index),
                )
            )
        for index in range(5):
            rows.append(
                self.transaction_payload(
                    f"hist_weekly_{index}",
                    amount=9_000,
                    account_ref=ACCOUNT_WEEKLY,
                    at=BASE_TIME - timedelta(days=2, minutes=index),
                )
            )
        rows.append(
            self.transaction_payload(
                "hist_weekly_5",
                amount=9_000,
                account_ref=ACCOUNT_WEEKLY,
                at=BASE_TIME - timedelta(days=3),
            )
        )
        for index in range(20):
            rows.append(
                self.transaction_payload(
                    f"hist_full_avg_{index}",
                    amount=100,
                    account_ref=ACCOUNT_FULL_RISK,
                    at=BASE_TIME - timedelta(days=10, minutes=index),
                )
            )
        for index in range(11):
            rows.append(
                self.transaction_payload(
                    f"hist_full_burst_{index}",
                    amount=100,
                    account_ref=ACCOUNT_FULL_RISK,
                    at=BASE_TIME - timedelta(minutes=40 - index),
                )
            )
        for index in range(6):
            rows.append(
                self.transaction_payload(
                    f"hist_full_weekly_{index}",
                    amount=9_000,
                    account_ref=ACCOUNT_FULL_RISK,
                    at=BASE_TIME - timedelta(days=2, minutes=20 + index),
                )
            )
        for index, account_ref in enumerate(ALT_ACCOUNT_REFS):
            rows.append(
                self.transaction_payload(
                    f"hist_shared_device_{index}",
                    amount=100,
                    account_ref=account_ref,
                    device_id=SHARED_DEVICE_ID,
                    at=BASE_TIME - timedelta(hours=12, minutes=index),
                )
            )
        await self.ingest_batch(self.object_type, rows)

    async def seed_platform_lists(self) -> None:
        custom_list = (
            await self.request(
                self.decision_engine,
                "POST",
                f"/v1/tenants/{self.tenant_id}/platform/custom-lists",
                201,
                json={
                    "name": "high_risk_ips",
                    "description": "High-risk IP watchlist for wallet transfer demo.",
                    "kind": "ip",
                },
            )
        )["custom_list"]
        await self.request(
            self.decision_engine,
            "POST",
            f"/v1/tenants/{self.tenant_id}/platform/custom-lists/{custom_list['id']}/entries",
            201,
            json={"value": HIGH_RISK_IP},
        )

    def transaction_payload(
        self,
        suffix: str,
        *,
        amount: int = 500,
        account_ref: str = ACCOUNT_REF,
        product_id: str = PRODUCT_ID,
        beneficiary_id: str = BENEFICIARY_ID,
        device_id: str = KNOWN_DEVICE_ID,
        ip: str = NORMAL_IP,
        ip_region: str = "Greater Accra",
        required_kyc_level: int = 2,
        at: datetime = BASE_TIME,
    ) -> dict[str, Any]:
        return {
            "object_id": f"wallet_demo_{suffix}",
            "account_ref": account_ref,
            "processor": "uniwallet",
            "product_id": product_id,
            "beneficiary_id": beneficiary_id,
            "transaction_id": f"wallet__uniwallet__{suffix}",
            "date": iso(at),
            "amount": amount,
            "currency": "GHS",
            "country": "GH",
            "channel": "wallet",
            "device_id": device_id,
            "ip": ip,
            "ip_region": ip_region,
            "required_kyc_level": required_kyc_level,
        }

    def build_rule_defs(self) -> list[RuleDef]:
        account_filter = filter_node(self.object_type, "account_ref", "=", field("account_ref"))
        return [
            RuleDef(
                "High Transfer Amount",
                "Transaction Based Rules",
                "transaction.amount is greater than 3x the AVG of transaction.amount for the same account_ref between Now and 30 days before Now.",
                25,
                gt(
                    field("amount"),
                    fn(
                        "multiply",
                        aggregator(
                            self.object_type,
                            "amount",
                            "AVG",
                            account_filter,
                            filter_node(self.object_type, "date", ">=", time_add(field("date"), "P30D")),
                        ),
                        const(3),
                    ),
                ),
            ),
            RuleDef(
                "Product Limit Breach",
                "Transaction Based Rules",
                "transaction.amount is greater than product.max_amount.",
                35,
                gt(field("amount"), related("product", "max_amount")),
            ),
            RuleDef(
                "One Hour Transfer Burst",
                "Velocity Rules",
                "COUNT of transaction.transaction_id for the same account_ref between Now and 1 hour before Now is greater than 10.",
                30,
                gt(
                    aggregator(
                        self.object_type,
                        "transaction_id",
                        "COUNT",
                        account_filter,
                        filter_node(self.object_type, "date", ">=", time_add(field("date"), "PT1H")),
                    ),
                    const(10),
                ),
            ),
            RuleDef(
                "Weekly Transfer Velocity",
                "Velocity Rules",
                "SUM of transaction.amount for the same account_ref between Now and 7 days before Now is greater than 50000.",
                35,
                gt(
                    aggregator(
                        self.object_type,
                        "amount",
                        "SUM",
                        account_filter,
                        filter_node(self.object_type, "date", ">=", time_add(field("date"), "P7D")),
                    ),
                    const(50_000),
                ),
            ),
            RuleDef(
                "New Device Transfer",
                "Device Fingerprinting",
                "transaction.device_id is not in the known device list for account.account_ref.",
                25,
                fn("not", fn("contains", related("account", "known_device_ids"), field("device_id"))),
            ),
            RuleDef(
                "Shared Device Risk",
                "Device Fingerprinting",
                "COUNT_DISTINCT of transaction.account_ref using the same device_id between Now and 24 hours before Now is greater than 3.",
                30,
                gt(
                    aggregator(
                        self.object_type,
                        "account_ref",
                        "COUNT_DISTINCT",
                        filter_node(self.object_type, "device_id", "=", field("device_id")),
                        filter_node(self.object_type, "date", ">=", time_add(field("date"), "P1D")),
                    ),
                    const(3),
                ),
            ),
            RuleDef(
                "Unusual IP Region",
                "Geolocation and IP Monitoring",
                "transaction.ip_region is not equal to account.usual_ip_region.",
                20,
                neq(field("ip_region"), related("account", "usual_ip_region")),
            ),
            RuleDef(
                "High Risk IP",
                "Geolocation and IP Monitoring",
                "transaction.ip is in the high-risk IP watchlist.",
                35,
                fn("in_custom_list", list=const("high_risk_ips"), value=field("ip")),
            ),
            RuleDef(
                "New Beneficiary",
                "High Transfer Beneficiary Risk Rules",
                "beneficiary.first_seen_at is within 24 hours before Now, and transaction.amount is greater than 2000.",
                30,
                fn(
                    "and",
                    gte(related("beneficiary", "first_seen_at"), time_add(field("date"), "P1D")),
                    gt(field("amount"), const(2_000)),
                ),
            ),
            RuleDef(
                "Low KYC",
                "High Transfer KYC and Account Profile Rules",
                "account.kyc_level is less than required KYC level for transaction.amount.",
                35,
                lt(related("account", "kyc_level"), field("required_kyc_level")),
            ),
        ]

    async def bootstrap_scenario(self) -> None:
        self.rule_defs = self.build_rule_defs()
        scenario = (
            await self.request(
                self.decision_engine,
                "POST",
                f"/v1/tenants/{self.tenant_id}/scenarios",
                201,
                json={"name": unique_name("wallet_transfer_fraud_screening"), "trigger_object_type": self.object_type},
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
                "score_review_threshold": 30,
                "score_block_and_review_threshold": 60,
                "score_decline_threshold": 90,
                "schedule": "",
            },
        )
        for index, rule in enumerate(self.rule_defs, start=1):
            created = (
                await self.request(
                    self.decision_engine,
                    "POST",
                    f"/v1/tenants/{self.tenant_id}/scenarios/{self.scenario_id}/iterations/{iteration['id']}/rules",
                    201,
                    json={
                        "display_order": index,
                        "name": rule.name,
                        "description": rule.description,
                        "formula": rule.formula,
                        "score_modifier": rule.score,
                        "rule_group": rule.group,
                        "stable_rule_id": unique_name("wallet_rule"),
                    },
                )
            )["rule"]
            if index == 1:
                self.rule_id = created["id"]

        validation = await self.request(
            self.decision_engine,
            "POST",
            f"/v1/tenants/{self.tenant_id}/scenarios/{self.scenario_id}/iterations/{iteration['id']}/validate",
            200,
        )
        if validation.get("validation", {}).get("valid") is not True:
            raise RuntimeError(f"iteration validation failed: {json.dumps(validation, default=str)}")
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

    def build_cases(self) -> list[DemoCase]:
        definitions = [
            ("Clean Wallet Transfer", "clean", {"account_ref": ACCOUNT_CLEAN}, set()),
            (
                "High Transfer Amount Only",
                "high_amount",
                {"account_ref": ACCOUNT_HIGH_AMOUNT, "amount": 400},
                {"High Transfer Amount"},
            ),
            (
                "Product Limit Breach Only",
                "product_limit",
                {"account_ref": ACCOUNT_CLEAN, "amount": 5_500},
                {"Product Limit Breach"},
            ),
            ("One Hour Transfer Burst", "hour_burst", {"account_ref": ACCOUNT_BURST}, {"One Hour Transfer Burst"}),
            ("Weekly Transfer Velocity", "weekly_velocity", {"account_ref": ACCOUNT_WEEKLY}, {"Weekly Transfer Velocity"}),
            (
                "New Device Transfer",
                "new_device",
                {"account_ref": ACCOUNT_CLEAN, "device_id": NEW_DEVICE_ID},
                {"New Device Transfer"},
            ),
            (
                "Shared Device Risk",
                "shared_device",
                {"account_ref": ACCOUNT_SHARED, "device_id": SHARED_DEVICE_ID},
                {"Shared Device Risk"},
            ),
            (
                "Unusual IP Region",
                "ip_region",
                {"account_ref": ACCOUNT_CLEAN, "ip_region": "Ashanti"},
                {"Unusual IP Region"},
            ),
            ("High Risk IP", "high_risk_ip", {"account_ref": ACCOUNT_CLEAN, "ip": HIGH_RISK_IP}, {"High Risk IP"}),
            (
                "New Beneficiary High Transfer",
                "new_beneficiary",
                {"account_ref": ACCOUNT_CLEAN, "beneficiary_id": NEW_BENEFICIARY_ID, "amount": 2_500},
                {"New Beneficiary"},
            ),
            ("Low KYC Transfer", "low_kyc", {"account_ref": ACCOUNT_REF, "required_kyc_level": 3}, {"Low KYC"}),
            (
                "Full Risk Showcase",
                "full_risk",
                {
                    "account_ref": ACCOUNT_FULL_RISK,
                    "amount": 6_000,
                    "beneficiary_id": NEW_BENEFICIARY_ID,
                    "device_id": SHARED_DEVICE_ID,
                    "ip": HIGH_RISK_IP,
                    "ip_region": "Ashanti",
                    "required_kyc_level": 3,
                },
                {rule.name for rule in self.rule_defs},
            ),
        ]
        return [
            DemoCase(
                name=name,
                object_id=f"wallet_demo_case_{suffix}",
                fields=self.transaction_payload(f"case_{suffix}", at=BASE_TIME) | overrides,
                expected_rules=expected,
            )
            for name, suffix, overrides, expected in definitions
        ]

    async def evaluate_case(self, case: DemoCase) -> dict[str, Any]:
        response = await self.request(
            self.decision_engine,
            "POST",
            f"/v1/tenants/{self.tenant_id}/scenarios/{self.scenario_id}/evaluate",
            200,
            json={"object_id": case.object_id, "object_type": self.object_type, "fields": case.fields},
        )
        result = response.get("result", response)
        triggered_rules = {
            item["rule_name"]
            for item in result.get("rule_executions", [])
            if item.get("outcome") == "hit"
        }
        decision = result.get("decision") or {}
        unexpected = sorted(triggered_rules - case.expected_rules)
        missing = sorted(case.expected_rules - triggered_rules)
        passed = not unexpected and not missing
        return {
            "case": case.name,
            "object_id": case.object_id,
            "expected_rules": sorted(case.expected_rules),
            "triggered_rules": sorted(triggered_rules),
            "missing_rules": missing,
            "unexpected_rules": unexpected,
            "score": decision.get("score"),
            "outcome": decision.get("outcome"),
            "passed": passed,
            "transaction": case.fields,
        }

    async def ingest(self, object_type: str, payload: dict[str, Any]) -> None:
        await self.ingest_with_schema_retry(f"/v1/tenants/{self.tenant_id}/ingest/{object_type}", payload)

    async def ingest_batch(self, object_type: str, rows: list[dict[str, Any]]) -> None:
        for start in range(0, len(rows), 500):
            await self.ingest_with_schema_retry(f"/v1/tenants/{self.tenant_id}/ingest/{object_type}/batch", rows[start:start + 500])

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


def print_case(result: dict[str, Any]) -> None:
    expected = ", ".join(result["expected_rules"]) if result["expected_rules"] else "none"
    triggered = ", ".join(result["triggered_rules"]) if result["triggered_rules"] else "none"
    print(f"\nCase: {result['case']}")
    print(f"Expected: {expected}")
    print(f"Triggered: {triggered}")
    print(f"Score: {result['score']}")
    print(f"Decision: {result['outcome']}")
    print(f"Result: {'PASS' if result['passed'] else 'FAIL'}")
    if result["missing_rules"]:
        print(f"Missing: {', '.join(result['missing_rules'])}")
    if result["unexpected_rules"]:
        print(f"Unexpected: {', '.join(result['unexpected_rules'])}")


def coverage(rule_names: list[str], results: list[dict[str, Any]]) -> dict[str, dict[str, bool]]:
    out: dict[str, dict[str, bool]] = {}
    for rule_name in rule_names:
        out[rule_name] = {
            "triggered": any(rule_name in result["triggered_rules"] for result in results),
            "not_triggered": any(rule_name not in result["triggered_rules"] for result in results),
        }
    return out


def parse_config() -> Config:
    parser = argparse.ArgumentParser(description="Deterministic wallet transfer fraud screening scenario demo.")
    parser.add_argument("--timeout", type=float, default=30.0, help="Per-request timeout in seconds.")
    parser.add_argument("--output", help="Output JSON path. Defaults to a timestamped stress-tests/demo-runs file.")
    parser.add_argument("--data-model-url", default=os.getenv("DATA_MODEL_URL", "http://127.0.0.1:8080"))
    parser.add_argument("--ingestion-url", default=os.getenv("INGESTION_URL", "http://127.0.0.1:8081"))
    parser.add_argument("--decision-engine-url", default=os.getenv("DECISION_ENGINE_URL", "http://127.0.0.1:8082"))
    parser.add_argument("--auth-token", default=os.getenv("SERVICE_AUTH_TOKEN"))
    parser.add_argument(
        "--ingestion-database-url",
        default=os.getenv("INGESTION_DATABASE_URL"),
        help="Optional Postgres URL used to materialize tenant tables when ingestion uses a separate DB.",
    )
    args = parser.parse_args()
    output = args.output or str(Path("stress-tests/demo-runs") / f"wallet-transfer-{utc_now().replace(':', '').replace('.', '-')}.json")
    return Config(
        timeout_seconds=args.timeout,
        output=output,
        data_model_url=args.data_model_url.rstrip("/"),
        ingestion_url=args.ingestion_url.rstrip("/"),
        decision_engine_url=args.decision_engine_url.rstrip("/"),
        auth_token=args.auth_token,
        ingestion_database_url=args.ingestion_database_url,
    )


async def async_main() -> int:
    config = parse_config()
    harness = WalletTransferDemoHarness(config)
    try:
        print("bootstrapping wallet transfer fraud screening demo...")
        await harness.bootstrap()
        results = []
        print("\nWallet Transfer Fraud Screening Demo")
        for case in harness.build_cases():
            result = await harness.evaluate_case(case)
            results.append(result)
            print_case(result)

        rule_names = [rule.name for rule in harness.rule_defs]
        rule_coverage = coverage(rule_names, results)
        coverage_passed = all(item["triggered"] and item["not_triggered"] for item in rule_coverage.values())
        cases_passed = all(result["passed"] for result in results)
        print("\nRule Coverage")
        for rule_name, item in rule_coverage.items():
            print(f"{rule_name}: triggered {'YES' if item['triggered'] else 'NO'}, not-triggered {'YES' if item['not_triggered'] else 'NO'}")
        print(f"\nOverall: {'PASS' if cases_passed and coverage_passed else 'FAIL'}")

        summary = {
            "summary_version": 1,
            "test": {
                "name": "wallet_transfer_fraud_screening_demo",
                "objective": "Show each wallet transfer fraud screening rule triggering and not triggering in deterministic cases.",
            },
            "run": asdict(config) | {
                "auth_token": "set" if config.auth_token else None,
                "ingestion_database_url": "set" if config.ingestion_database_url else None,
            },
            "setup": {
                "tenant_id": harness.tenant_id,
                "transaction_object_type": harness.object_type,
                "scenario_id": harness.scenario_id,
                "review_threshold": 30,
                "block_and_review_threshold": 60,
                "decline_threshold": 90,
                "rules": [asdict(rule) | {"formula": rule.formula} for rule in harness.rule_defs],
            },
            "cases": results,
            "coverage": rule_coverage,
            "passed": cases_passed and coverage_passed,
        }
        output_path = Path(config.output)
        output_path.parent.mkdir(parents=True, exist_ok=True)
        output_path.write_text(json.dumps(summary, indent=2, default=str) + "\n")
        print(f"output: {output_path}")
        return 0 if summary["passed"] else 1
    finally:
        await harness.close()


def main() -> None:
    if sys.platform == "win32" and sys.version_info < (3, 14):
        asyncio.set_event_loop_policy(asyncio.WindowsSelectorEventLoopPolicy())
    raise SystemExit(asyncio.run(async_main()))


if __name__ == "__main__":
    main()
