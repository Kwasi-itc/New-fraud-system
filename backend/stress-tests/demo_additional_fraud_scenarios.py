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
from demo_wallet_transfer_fraud_screening import const, field, fn, gt, gte, iso, list_node, lt, neq, related, time_add


DEFAULT_TENANT_NAME = "Fraud Scenario Demo Tenant"
BASE_TIME = datetime(2026, 7, 6, 12, 0, 0, tzinfo=timezone.utc)
HIGH_RISK_IP = "45.155.205.99"
NORMAL_IP = "102.176.10.20"
WATCHLISTED_MERCHANT = "Sankofa Betting House Limited"


@dataclass(frozen=True)
class Config:
    tenant_id: str | None
    tenant_name: str
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
class ScenarioDef:
    name: str
    rules: list[RuleDef]


@dataclass(frozen=True)
class DemoCase:
    scenario: str
    name: str
    object_id: str
    fields: dict[str, Any]
    expected_rules: set[str]


def filter_node(table_name: str, field_name: str, operator: str, value: dict[str, Any]) -> dict[str, Any]:
    return fn("Filter", tableName=const(table_name), fieldName=const(field_name), operator=const(operator), value=value)


def aggregator(table_name: str, field_name: str, name: str, *filters: dict[str, Any]) -> dict[str, Any]:
    return fn("Aggregator", tableName=const(table_name), fieldName=const(field_name), aggregator=const(name), filters=list_node(*filters))


def clean_id(value: str) -> str:
    return "".join(part[:1].upper() + part[1:] for part in value.replace("-", " ").replace("_", " ").split())


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


class AdditionalScenarioDemoHarness(ThroughputHarness):
    def __init__(self, config: Config) -> None:
        super().__init__(base_config(config))
        self.demo_config = config
        self.tenant_id = config.tenant_id or ""
        self.account_object_type = ""
        self.device_object_type = ""
        self.product_object_type = ""
        self.beneficiary_object_type = ""
        self.merchant_object_type = ""
        self.login_object_type = ""
        self.model_fields_by_table: dict[str, list[dict[str, Any]]] = {}
        self.scenario_ids: dict[str, str] = {}
        self.rule_defs_by_scenario: dict[str, list[RuleDef]] = {}
        self.ip_list_name = "High Risk IPs"

    async def bootstrap(self) -> None:
        await self.wait_until_ready(self.data_model, "data-model")
        await self.wait_until_ready(self.ingestion, "ingestion")
        await self.wait_until_ready(self.decision_engine, "decision-engine")
        if not self.tenant_id:
            tenant = (
                await self.request(
                    self.data_model,
                    "POST",
                    "/v1/tenants",
                    201,
                    json={"name": self.demo_config.tenant_name, "external_key": f"fraud-scenario-demo-{utc_now().replace(':', '').replace('.', '-')}"},
                )
            )["tenant"]
            self.tenant_id = tenant["id"]
        await self.request(self.data_model, "POST", f"/v1/tenants/{self.tenant_id}/provision", {200, 409})
        await self.bootstrap_model()
        if self.demo_config.ingestion_database_url:
            await asyncio.to_thread(self.materialize_ingestion_schema, self.demo_config.ingestion_database_url)
        await self.seed_reference_data()
        await self.seed_history()
        await self.seed_platform_lists()
        for scenario in self.build_scenarios():
            await self.bootstrap_scenario(scenario)

    async def create_table(self, name: str, alias: str, description: str) -> dict[str, Any]:
        return (
            await self.request(
                self.data_model,
                "POST",
                f"/v1/tenants/{self.tenant_id}/tables",
                201,
                json={"name": name, "description": description, "alias": alias, "semantic_type": "entity"},
            )
        )["table"]

    async def create_field(self, table_id: str, payload: dict[str, Any]) -> dict[str, Any]:
        return (await self.request(self.data_model, "POST", f"/v1/tables/{table_id}/fields", 201, json=payload))["field"]

    async def bootstrap_model(self) -> None:
        transactions = await self.create_table("transactions", "Transactions", "Demo financial transactions")
        accounts = await self.create_table("accounts", "Accounts", "Demo customer accounts")
        devices = await self.create_table("devices", "Devices", "Demo customer devices")
        products = await self.create_table("products", "Products", "Demo payment products")
        beneficiaries = await self.create_table("beneficiaries", "Beneficiaries", "Demo payment beneficiaries")
        merchants = await self.create_table("merchants", "Merchants", "Demo merchants")
        logins = await self.create_table("loginattempts", "Login Attempts", "Demo authentication attempts")
        self.object_type = transactions["name"]
        self.account_object_type = accounts["name"]
        self.device_object_type = devices["name"]
        self.product_object_type = products["name"]
        self.beneficiary_object_type = beneficiaries["name"]
        self.merchant_object_type = merchants["name"]
        self.login_object_type = logins["name"]

        tx_fields: dict[str, dict[str, Any]] = {}
        for item in [
            ("account_ref", "string", False, False),
            ("merchant_id", "string", False, False),
            ("product_id", "string", False, False),
            ("beneficiary_id", "string", False, False),
            ("device_id", "string", False, False),
            ("transaction_id", "string", False, True),
            ("date", "timestamp", False, False),
            ("amount", "int", False, False),
            ("currency", "string", False, False),
            ("country", "string", False, False),
            ("channel", "string", False, False),
            ("ip", "string", False, False),
            ("ip_location", "string", False, False),
            ("ip_reputation_score", "int", False, False),
            ("previous_ip_distance_km", "int", False, False),
            ("previous_transaction_minutes_ago", "int", False, False),
            ("required_kyc_level", "int", False, False),
            ("direction", "string", False, False),
        ]:
            name, data_type, nullable, is_unique = item
            created = await self.create_field(transactions["id"], {"name": name, "data_type": data_type, "nullable": nullable, "is_unique": is_unique})
            tx_fields[name] = created

        account_key = await self.create_field(accounts["id"], {"name": "account_ref", "data_type": "string", "nullable": False, "is_unique": True})
        for name, data_type in [
            ("customer_name", "string"),
            ("kyc_level", "int"),
            ("last_password_change_at", "timestamp"),
            ("last_profile_change_at", "timestamp"),
        ]:
            await self.create_field(accounts["id"], {"name": name, "data_type": data_type, "nullable": False})

        device_key = await self.create_field(devices["id"], {"name": "device_id", "data_type": "string", "nullable": False, "is_unique": True})
        for name, data_type in [("account_ref", "string"), ("first_seen_at", "timestamp")]:
            await self.create_field(devices["id"], {"name": name, "data_type": data_type, "nullable": False})

        product_key = await self.create_field(products["id"], {"name": "product_id", "data_type": "string", "nullable": False, "is_unique": True})
        for name, data_type in [("product_name", "string"), ("product_category", "string"), ("max_amount", "int")]:
            await self.create_field(products["id"], {"name": name, "data_type": data_type, "nullable": False})

        beneficiary_key = await self.create_field(beneficiaries["id"], {"name": "beneficiary_id", "data_type": "string", "nullable": False, "is_unique": True})
        for name, data_type in [("account_ref", "string"), ("beneficiary_name", "string"), ("created_at", "timestamp")]:
            await self.create_field(beneficiaries["id"], {"name": name, "data_type": data_type, "nullable": False})

        merchant_key = await self.create_field(merchants["id"], {"name": "merchant_id", "data_type": "string", "nullable": False, "is_unique": True})
        for name, data_type in [
            ("merchant_name", "string"),
            ("merchant_category", "string"),
            ("settlement_account_updated_at", "timestamp"),
        ]:
            await self.create_field(merchants["id"], {"name": name, "data_type": data_type, "nullable": False})

        for item in [
            ("login_id", "string", False, True),
            ("account_ref", "string", False, False),
            ("status", "string", False, False),
            ("attempted_at", "timestamp", False, False),
        ]:
            name, data_type, nullable, is_unique = item
            await self.create_field(logins["id"], {"name": name, "data_type": data_type, "nullable": nullable, "is_unique": is_unique})

        await self.create_link("account", accounts["id"], account_key["id"], transactions["id"], tx_fields["account_ref"]["id"])
        await self.create_link("device", devices["id"], device_key["id"], transactions["id"], tx_fields["device_id"]["id"])
        await self.create_link("product", products["id"], product_key["id"], transactions["id"], tx_fields["product_id"]["id"])
        await self.create_link("beneficiary", beneficiaries["id"], beneficiary_key["id"], transactions["id"], tx_fields["beneficiary_id"]["id"])
        await self.create_link("merchant", merchants["id"], merchant_key["id"], transactions["id"], tx_fields["merchant_id"]["id"])

        self.model_fields_by_table = {
            self.object_type: [{"name": k, "data_type": v, "is_unique": k == "transaction_id"} for k, v in [
                ("account_ref", "string"), ("merchant_id", "string"), ("product_id", "string"), ("beneficiary_id", "string"),
                ("device_id", "string"), ("transaction_id", "string"), ("date", "timestamp"), ("amount", "int"),
                ("currency", "string"), ("country", "string"), ("channel", "string"), ("ip", "string"),
                ("ip_location", "string"), ("ip_reputation_score", "int"), ("previous_ip_distance_km", "int"),
                ("previous_transaction_minutes_ago", "int"), ("required_kyc_level", "int"), ("direction", "string"),
            ]],
            self.account_object_type: [{"name": k, "data_type": v, "is_unique": k == "account_ref"} for k, v in [
                ("account_ref", "string"), ("customer_name", "string"), ("kyc_level", "int"),
                ("last_password_change_at", "timestamp"), ("last_profile_change_at", "timestamp"),
            ]],
            self.device_object_type: [{"name": k, "data_type": v, "is_unique": k == "device_id"} for k, v in [
                ("device_id", "string"), ("account_ref", "string"), ("first_seen_at", "timestamp"),
            ]],
            self.product_object_type: [{"name": k, "data_type": v, "is_unique": k == "product_id"} for k, v in [
                ("product_id", "string"), ("product_name", "string"), ("product_category", "string"), ("max_amount", "int"),
            ]],
            self.beneficiary_object_type: [{"name": k, "data_type": v, "is_unique": k == "beneficiary_id"} for k, v in [
                ("beneficiary_id", "string"), ("account_ref", "string"), ("beneficiary_name", "string"), ("created_at", "timestamp"),
            ]],
            self.merchant_object_type: [{"name": k, "data_type": v, "is_unique": k == "merchant_id"} for k, v in [
                ("merchant_id", "string"), ("merchant_name", "string"), ("merchant_category", "string"), ("settlement_account_updated_at", "timestamp"),
            ]],
            self.login_object_type: [{"name": k, "data_type": v, "is_unique": k == "login_id"} for k, v in [
                ("login_id", "string"), ("account_ref", "string"), ("status", "string"), ("attempted_at", "timestamp"),
            ]],
        }

    async def create_link(self, name: str, parent_table_id: str, parent_field_id: str, child_table_id: str, child_field_id: str) -> None:
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
                        cur.execute(sql.SQL("ALTER TABLE {} ADD COLUMN IF NOT EXISTS {} {}").format(
                            table_ident, sql.Identifier(field_def["name"]), sql.SQL(pg_type(field_def["data_type"]))
                        ))
                    cur.execute(sql.SQL("CREATE UNIQUE INDEX IF NOT EXISTS {} ON {} ({})").format(
                        sql.Identifier(f"{table_name}_object_id_uidx"), table_ident, sql.Identifier("object_id")
                    ))
                    for field_def in fields:
                        if field_def.get("is_unique"):
                            cur.execute(sql.SQL("CREATE UNIQUE INDEX IF NOT EXISTS {} ON {} ({})").format(
                                sql.Identifier(f"{table_name}_{field_def['name']}_uidx"), table_ident, sql.Identifier(field_def["name"])
                            ))
                    if table_name == self.object_type:
                        for suffix, columns in {
                            "account_date_idx": ["account_ref", "date"],
                            "merchant_date_idx": ["merchant_id", "date"],
                            "ip_date_idx": ["ip", "date"],
                        }.items():
                            cur.execute(sql.SQL("CREATE INDEX IF NOT EXISTS {} ON {} ({})").format(
                                sql.Identifier(f"{table_name}_{suffix}"), table_ident, sql.SQL(", ").join(sql.Identifier(c) for c in columns)
                            ))
            conn.commit()

    async def seed_reference_data(self) -> None:
        account_specs = {
            "AcctClean": (3, BASE_TIME - timedelta(days=30), BASE_TIME - timedelta(days=30)),
            "AcctRecentPassword": (3, BASE_TIME - timedelta(hours=2), BASE_TIME - timedelta(days=30)),
            "AcctLowKyc": (1, BASE_TIME - timedelta(days=30), BASE_TIME - timedelta(days=30)),
            "AcctRecentProfile": (3, BASE_TIME - timedelta(days=30), BASE_TIME - timedelta(hours=2)),
            "AcctFailedLogin": (3, BASE_TIME - timedelta(days=30), BASE_TIME - timedelta(days=30)),
            "AcctRapid": (3, BASE_TIME - timedelta(days=30), BASE_TIME - timedelta(days=30)),
            "AcctFunding": (3, BASE_TIME - timedelta(days=30), BASE_TIME - timedelta(days=30)),
            "AcctAtoFull": (3, BASE_TIME - timedelta(hours=2), BASE_TIME - timedelta(days=30)),
            "AcctHighValueOnly": (3, BASE_TIME - timedelta(days=30), BASE_TIME - timedelta(days=30)),
            "AcctHvtFull": (1, BASE_TIME - timedelta(days=30), BASE_TIME - timedelta(hours=2)),
        }
        for account_ref, (kyc, password_at, profile_at) in account_specs.items():
            await self.ingest(self.account_object_type, {
                "object_id": account_ref,
                "account_ref": account_ref,
                "customer_name": account_ref.replace("_", " ").title(),
                "kyc_level": kyc,
                "last_password_change_at": iso(password_at),
                "last_profile_change_at": iso(profile_at),
            })
        for device_id, account_ref, first_seen in [
            ("DevOld", "AcctClean", BASE_TIME - timedelta(days=20)),
            ("DevNew", "AcctClean", BASE_TIME - timedelta(hours=2)),
            ("DevRecentPassword", "AcctRecentPassword", BASE_TIME - timedelta(days=20)),
            ("DevLowKyc", "AcctLowKyc", BASE_TIME - timedelta(days=20)),
            ("DevRecentProfile", "AcctRecentProfile", BASE_TIME - timedelta(days=20)),
            ("DevFailedLogin", "AcctFailedLogin", BASE_TIME - timedelta(days=20)),
            ("DevRapid", "AcctRapid", BASE_TIME - timedelta(days=20)),
            ("DevFunding", "AcctFunding", BASE_TIME - timedelta(days=20)),
            ("DevAtoFull", "AcctAtoFull", BASE_TIME - timedelta(hours=2)),
            ("DevHighValueOnly", "AcctHighValueOnly", BASE_TIME - timedelta(days=20)),
            ("DevHvtFull", "AcctHvtFull", BASE_TIME - timedelta(hours=2)),
        ]:
            await self.ingest(self.device_object_type, {"object_id": device_id, "device_id": device_id, "account_ref": account_ref, "first_seen_at": iso(first_seen)})
        for product_id, category, max_amount in [
            ("ProductWallet", "wallet_transfer", 20_000),
            ("ProductLowLimit", "wallet_transfer", 5_000),
            ("ProductGrocery", "grocery", 20_000),
            ("ProductGaming", "gaming", 20_000),
        ]:
            await self.ingest(self.product_object_type, {"object_id": product_id, "product_id": product_id, "product_name": product_id, "product_category": category, "max_amount": max_amount})
        for beneficiary_id, created_at in [("BeneficiaryOld", BASE_TIME - timedelta(days=30)), ("BeneficiaryNew", BASE_TIME - timedelta(hours=2))]:
            await self.ingest(self.beneficiary_object_type, {"object_id": beneficiary_id, "beneficiary_id": beneficiary_id, "account_ref": "AcctClean", "beneficiary_name": beneficiary_id, "created_at": iso(created_at)})
        for merchant_id, name, category, settlement_at in [
            ("MerchantClean", "Makola Grocery Mart", "grocery", BASE_TIME - timedelta(days=30)),
            ("MerchantHighWeekly", "Weekly Volume Merchant", "grocery", BASE_TIME - timedelta(days=30)),
            ("MerchantBurst", "Burst Merchant", "grocery", BASE_TIME - timedelta(days=30)),
            ("MerchantWatch", WATCHLISTED_MERCHANT, "gaming", BASE_TIME - timedelta(days=30)),
            ("MerchantMismatch", "Mismatch Merchant", "gaming", BASE_TIME - timedelta(days=30)),
            ("MerchantRepeat", "Repeat Merchant", "grocery", BASE_TIME - timedelta(days=30)),
            ("MerchantShared", "Shared IP Merchant", "grocery", BASE_TIME - timedelta(days=30)),
            ("MerchantAbnormal", "Abnormal Ticket Merchant", "grocery", BASE_TIME - timedelta(days=30)),
            ("MerchantSettlement", "Settlement Risk Merchant", "grocery", BASE_TIME - timedelta(hours=6)),
            ("MerchantFull", WATCHLISTED_MERCHANT, "gaming", BASE_TIME - timedelta(hours=6)),
            ("MerchantHistory", "History Seed Merchant", "grocery", BASE_TIME - timedelta(days=30)),
            *[(f"MerchantIpCluster{i}", f"IP Cluster Merchant {i}", "grocery", BASE_TIME - timedelta(days=30)) for i in range(6)],
        ]:
            await self.ingest(self.merchant_object_type, {
                "object_id": merchant_id,
                "merchant_id": merchant_id,
                "merchant_name": name,
                "merchant_category": category,
                "settlement_account_updated_at": iso(settlement_at),
            })

    async def seed_history(self) -> None:
        rows: list[dict[str, Any]] = []
        for account_ref in ["AcctClean", "AcctRecentPassword", "AcctLowKyc", "AcctRecentProfile", "AcctFailedLogin", "AcctRapid", "AcctHighValueOnly"]:
            for i in range(10):
                rows.append(self.tx(f"Neutral{account_ref}{i}", account_ref=account_ref, merchant_id="MerchantHistory", amount=4_000, at=BASE_TIME - timedelta(days=20, minutes=i)))
        for i in range(10):
            rows.append(self.tx(f"NeutralAcctFunding{i}", account_ref="AcctFunding", merchant_id="MerchantHistory", amount=6_000, at=BASE_TIME - timedelta(days=20, minutes=i)))
        for i in range(20):
            rows.append(self.tx(f"AcctAbnormalAverage{i}", account_ref="AcctAbnormal", merchant_id="MerchantHistory", amount=100, at=BASE_TIME - timedelta(days=10, minutes=i)))
        for i in range(20):
            rows.append(self.tx(f"AcctAtoFullAverage{i}", account_ref="AcctAtoFull", merchant_id="MerchantHistory", amount=100, at=BASE_TIME - timedelta(days=10, minutes=i)))
        for i in range(20):
            rows.append(self.tx(f"AcctHvtFullAverage{i}", account_ref="AcctHvtFull", merchant_id="MerchantHistory", amount=100, at=BASE_TIME - timedelta(days=10, minutes=i)))
        await self.ingest(self.account_object_type, {"object_id": "AcctAbnormal", "account_ref": "AcctAbnormal", "customer_name": "Abnormal Spend", "kyc_level": 3, "last_password_change_at": iso(BASE_TIME - timedelta(days=30)), "last_profile_change_at": iso(BASE_TIME - timedelta(days=30))})
        await self.ingest(self.device_object_type, {"object_id": "DevAbnormal", "device_id": "DevAbnormal", "account_ref": "AcctAbnormal", "first_seen_at": iso(BASE_TIME - timedelta(days=20))})
        for i in range(4):
            await self.ingest(self.login_object_type, {"object_id": f"LoginFailed{i}", "login_id": f"LoginFailed{i}", "account_ref": "AcctFailedLogin", "status": "failed", "attempted_at": iso(BASE_TIME - timedelta(minutes=50 - i))})
            await self.ingest(self.login_object_type, {"object_id": f"LoginFullFailed{i}", "login_id": f"LoginFullFailed{i}", "account_ref": "AcctAtoFull", "status": "failed", "attempted_at": iso(BASE_TIME - timedelta(minutes=45 - i))})
        for i in range(6):
            rows.append(self.tx(f"RapidPostLogin{i}", account_ref="AcctRapid", merchant_id="MerchantHistory", amount=100, at=BASE_TIME - timedelta(minutes=25 - i)))
            rows.append(self.tx(f"FullRapidPostLogin{i}", account_ref="AcctAtoFull", merchant_id="MerchantHistory", amount=100, at=BASE_TIME - timedelta(minutes=20 - i)))
        for i in range(6):
            rows.append(self.tx(f"Funding{i}", account_ref="AcctFunding", merchant_id="MerchantHistory", amount=2_000, direction="incoming", at=BASE_TIME - timedelta(hours=2, minutes=i)))
            rows.append(self.tx(f"HvtFullFunding{i}", account_ref="AcctHvtFull", merchant_id="MerchantHistory", amount=2_000, direction="incoming", at=BASE_TIME - timedelta(hours=2, minutes=20 + i)))
        for i in range(12):
            rows.append(self.tx(f"MerchantWeekly{i}", merchant_id="MerchantHighWeekly", amount=9_000, at=BASE_TIME - timedelta(days=2, minutes=i)))
        for i in range(51):
            rows.append(self.tx(f"MerchantBurst{i}", merchant_id="MerchantBurst", account_ref=f"MerchantBurstAccount{i}", amount=100, at=BASE_TIME - timedelta(minutes=50 - (i % 50), seconds=i)))
        for i in range(11):
            rows.append(self.tx(f"MerchantRepeat{i}", merchant_id="MerchantRepeat", account_ref="AcctClean", amount=100, at=BASE_TIME - timedelta(hours=2, minutes=i)))
        for i in range(6):
            rows.append(self.tx(f"MerchantIpCluster{i}", merchant_id=f"MerchantIpCluster{i}", ip="102.88.1.77", amount=100, at=BASE_TIME - timedelta(hours=4, minutes=i)))
        for i in range(20):
            rows.append(self.tx(f"MerchantAbnormalAverage{i}", merchant_id="MerchantAbnormal", amount=100, at=BASE_TIME - timedelta(days=10, minutes=i)))
        for merchant_id in ["MerchantClean", "MerchantHighWeekly", "MerchantBurst", "MerchantWatch", "MerchantMismatch", "MerchantRepeat", "MerchantShared", "MerchantSettlement"]:
            for i in range(10):
                rows.append(self.tx(f"MerchantNeutral{merchant_id}{i}", merchant_id=merchant_id, account_ref=f"Neutral{merchant_id}{i}", product_id="ProductGrocery", amount=4_000, at=BASE_TIME - timedelta(days=20, minutes=i)))
        for i in range(3):
            rows.append(self.tx(f"MerchantSettlement{i}", merchant_id="MerchantSettlement", amount=7_000, at=BASE_TIME - timedelta(hours=3, minutes=i)))
        for i in range(12):
            rows.append(self.tx(f"MerchantFullWeekly{i}", merchant_id="MerchantFull", account_ref="AcctMerchantFull", product_id="ProductGrocery", amount=9_000, ip="102.88.1.77", at=BASE_TIME - timedelta(days=2, minutes=i)))
        for i in range(51):
            rows.append(self.tx(f"MerchantFullBurst{i}", merchant_id="MerchantFull", account_ref="AcctMerchantFull", product_id="ProductGrocery", amount=500, ip="102.88.1.77", at=BASE_TIME - timedelta(minutes=50 - (i % 50), seconds=i)))
        await self.ingest_batch(self.object_type, rows)

    async def seed_platform_lists(self) -> None:
        custom_list = (await self.request(
            self.decision_engine,
            "POST",
            f"/v1/tenants/{self.tenant_id}/platform/custom-lists",
            201,
            json={"name": self.ip_list_name, "description": "Demo high-risk IP watchlist.", "kind": "ip"},
        ))["custom_list"]
        await self.request(self.decision_engine, "POST", f"/v1/tenants/{self.tenant_id}/platform/custom-lists/{custom_list['id']}/entries", 201, json={"value": HIGH_RISK_IP})

    def tx(self, suffix: str, **overrides: Any) -> dict[str, Any]:
        payload = {
            "object_id": clean_id(f"Transaction {suffix}"),
            "account_ref": "AcctClean",
            "merchant_id": "MerchantClean",
            "product_id": "ProductGrocery",
            "beneficiary_id": "BeneficiaryOld",
            "device_id": "DevOld",
            "transaction_id": clean_id(f"Transaction {suffix}"),
            "date": iso(overrides.pop("at", BASE_TIME)),
            "amount": 500,
            "currency": "GHS",
            "country": "GH",
            "channel": "wallet",
            "ip": NORMAL_IP,
            "ip_location": "Accra",
            "ip_reputation_score": 10,
            "previous_ip_distance_km": 10,
            "previous_transaction_minutes_ago": 240,
            "required_kyc_level": 2,
            "direction": "outgoing",
        }
        payload.update(overrides)
        return payload

    def build_scenarios(self) -> list[ScenarioDef]:
        acct = filter_node(self.object_type, "account_ref", "=", field("account_ref"))
        merch = filter_node(self.object_type, "merchant_id", "=", field("merchant_id"))
        date_30d = filter_node(self.object_type, "date", ">=", time_add(field("date"), "P30D"))
        date_7d = filter_node(self.object_type, "date", ">=", time_add(field("date"), "P7D"))
        date_24h = filter_node(self.object_type, "date", ">=", time_add(field("date"), "P1D"))
        date_1h = filter_node(self.object_type, "date", ">=", time_add(field("date"), "PT1H"))
        return [
            ScenarioDef("Account Takeover Detection", [
                RuleDef("New Device High Value Transaction", "Device Fingerprinting", "device.first_seen_at for the same device_id and account_ref is within 24 hours before Now, and transaction.amount is greater than 5000.", 35, fn("and", gte(related("device", "first_seen_at"), time_add(field("date"), "P1D")), gt(field("amount"), const(5_000)))),
                RuleDef("Suspicious IP", "Transaction Geolocation and IP Monitoring", "transaction.ip is in the high-risk IP watchlist, or IP_REPUTATION_SCORE(transaction.ip) is greater than 80.", 35, fn("or", fn("in_custom_list", list=const(self.ip_list_name), value=field("ip")), gt(field("ip_reputation_score"), const(80)))),
                RuleDef("Impossible Travel", "Transaction Geolocation and IP Monitoring", "DISTANCE_KM between the current transaction.ip_location and the previous transaction.ip_location for the same account_ref is greater than 500, and DATE_DIFF from the previous transaction is less than 2 hours.", 40, fn("and", gt(field("previous_ip_distance_km"), const(500)), lt(field("previous_transaction_minutes_ago"), const(120)))),
                RuleDef("Post Credential Change Transfer", "Account Change Risk", "DATE_DIFF between Now and account.last_password_change_at is less than 24 hours, and transaction.amount is greater than 2000.", 35, fn("and", gte(related("account", "last_password_change_at"), time_add(field("date"), "P1D")), gt(field("amount"), const(2_000)))),
                RuleDef("Failed Login Before Transaction", "Authentication Risk", "COUNT of failed login attempts for the same account_ref between Now and 1 hour before Now is greater than 3.", 30, gt(aggregator(self.login_object_type, "login_id", "COUNT", filter_node(self.login_object_type, "account_ref", "=", field("account_ref")), filter_node(self.login_object_type, "status", "=", const("failed")), filter_node(self.login_object_type, "attempted_at", ">=", time_add(field("date"), "PT1H"))), const(3))),
                RuleDef("New Beneficiary After Login", "Beneficiary Risk", "beneficiary.created_at is within 24 hours before Now, and transaction.amount to that beneficiary is greater than 1000.", 30, fn("and", gte(related("beneficiary", "created_at"), time_add(field("date"), "P1D")), gt(field("amount"), const(1_000)))),
                RuleDef("Abnormal Account Spend", "Behavioral Pattern Risk", "transaction.amount is greater than 3x the AVG of transaction.amount for the same account_ref between Now and 30 days before Now.", 30, gt(field("amount"), fn("multiply", aggregator(self.object_type, "amount", "AVG", acct, date_30d), const(3)))),
                RuleDef("Rapid Post-Login Transfers", "Velocity Risk", "COUNT of transaction.transaction_id for the same account_ref between Now and 30 minutes before Now is greater than 5.", 25, gt(aggregator(self.object_type, "transaction_id", "COUNT", acct, filter_node(self.object_type, "date", ">=", time_add(field("date"), "PT30M"))), const(5))),
            ]),
            ScenarioDef("Merchant Abuse Monitoring", [
                RuleDef("High Weekly Merchant Volume", "Merchant Velocity Risk", "SUM of transaction.amount for the same merchant_id between Now and 7 days before Now is greater than 100000.", 35, gt(aggregator(self.object_type, "amount", "SUM", merch, date_7d), const(100_000))),
                RuleDef("Rapid Merchant Payment Burst", "Merchant Velocity Risk", "COUNT of transaction.transaction_id for the same merchant_id between Now and 1 hour before Now is greater than 50.", 25, gt(aggregator(self.object_type, "transaction_id", "COUNT", merch, date_1h), const(50))),
                RuleDef("Watchlisted Merchant Name Match", "Merchant Risk", "FUZZY_MATCH of merchant.merchant_name against the merchant watchlist is greater than the configured match threshold.", 45, gte(fn("FuzzyMatchAnyOf", related("merchant", "merchant_name"), list_node(const(WATCHLISTED_MERCHANT), const("Volta Crypto Exchange")), algorithm=const("bag_of_words_similarity")), const(80))),
                RuleDef("Merchant Category Mismatch", "Category Consistency Risk", "merchant.merchant_category is not consistent with product.product_category.", 30, neq(related("merchant", "merchant_category"), related("product", "product_category"))),
                RuleDef("Repeated Same Account Payments", "Transaction Pattern Risk", "COUNT of transaction.transaction_id for the same merchant_id and same account_ref between Now and 24 hours before Now is greater than 10.", 30, gt(aggregator(self.object_type, "transaction_id", "COUNT", merch, acct, date_24h), const(10))),
                RuleDef("Shared Merchant IP Cluster", "Device and IP Link Analysis", "COUNT_DISTINCT of merchant_id using the same transaction.ip between Now and 24 hours before Now is greater than 5.", 35, gt(aggregator(self.object_type, "merchant_id", "COUNT_DISTINCT", filter_node(self.object_type, "ip", "=", field("ip")), date_24h), const(5))),
                RuleDef("Abnormal Merchant Average Ticket", "Behavioral Pattern Risk", "transaction.amount is greater than 3x the AVG of transaction.amount for the same merchant_id between Now and 30 days before Now.", 30, gt(field("amount"), fn("multiply", aggregator(self.object_type, "amount", "AVG", merch, date_30d), const(3)))),
                RuleDef("Settlement Account Recently Changed", "Settlement Risk", "DATE_DIFF between Now and merchant.settlement_account_updated_at is less than 48 hours, and SUM of transaction.amount for the same merchant_id between Now and 24 hours before Now is greater than 20000.", 40, fn("and", gte(related("merchant", "settlement_account_updated_at"), time_add(field("date"), "P2D")), gt(aggregator(self.object_type, "amount", "SUM", merch, date_24h), const(20_000)))),
            ]),
            ScenarioDef("High Value Transaction Review", [
                RuleDef("High Value Transaction", "Transaction Value Risk", "transaction.amount is greater than 10000.", 30, gt(field("amount"), const(10_000))),
                RuleDef("Product Limit Breach", "Product Limit Risk", "transaction.amount is greater than product.max_amount for the matching product_id.", 40, gt(field("amount"), related("product", "max_amount"))),
                RuleDef("Low KYC High Value Transaction", "KYC and Account Profile Risk", "account.kyc_level is less than the required KYC level for transaction.amount.", 35, lt(related("account", "kyc_level"), field("required_kyc_level"))),
                RuleDef("New Device High Value Transaction", "Device Fingerprinting", "device.first_seen_at for the same device_id and account_ref is within 24 hours before Now, and transaction.amount is greater than 5000.", 35, fn("and", gte(related("device", "first_seen_at"), time_add(field("date"), "P1D")), gt(field("amount"), const(5_000)))),
                RuleDef("Unusual IP High Value Transaction", "Geolocation and IP Monitoring", "transaction.ip is in the high-risk IP watchlist, or IP_REPUTATION_SCORE(transaction.ip) is greater than 80.", 35, fn("or", fn("in_custom_list", list=const(self.ip_list_name), value=field("ip")), gt(field("ip_reputation_score"), const(80)))),
                RuleDef("Abnormal High Value Spend", "Behavioral Pattern Risk", "transaction.amount is greater than 3x the AVG of transaction.amount for the same account_ref between Now and 30 days before Now.", 30, gt(field("amount"), fn("multiply", aggregator(self.object_type, "amount", "AVG", acct, date_30d), const(3)))),
                RuleDef("Fast Outflow After Funding", "Source of Funds Risk", "SUM of incoming transaction.amount for the same account_ref between Now and 24 hours before Now is greater than 10000, and the current outgoing transaction.amount is greater than 80% of that sum.", 35, fn("and", gt(aggregator(self.object_type, "amount", "SUM", acct, date_24h, filter_node(self.object_type, "direction", "=", const("incoming"))), const(10_000)), gt(field("amount"), fn("multiply", aggregator(self.object_type, "amount", "SUM", acct, date_24h, filter_node(self.object_type, "direction", "=", const("incoming"))), const(0.8))))),
                RuleDef("Recent Account Change High Value Transaction", "Account Change Risk", "DATE_DIFF between Now and account.last_profile_change_at is less than 24 hours, and transaction.amount is greater than 5000.", 35, fn("and", gte(related("account", "last_profile_change_at"), time_add(field("date"), "P1D")), gt(field("amount"), const(5_000)))),
            ]),
        ]

    async def bootstrap_scenario(self, scenario_def: ScenarioDef) -> None:
        scenario = (await self.request(self.decision_engine, "POST", f"/v1/tenants/{self.tenant_id}/scenarios", 201, json={"name": scenario_def.name, "trigger_object_type": self.object_type}))["scenario"]
        self.scenario_ids[scenario_def.name] = scenario["id"]
        self.rule_defs_by_scenario[scenario_def.name] = scenario_def.rules
        iteration = (await self.request(self.decision_engine, "POST", f"/v1/tenants/{self.tenant_id}/scenarios/{scenario['id']}/iterations", 201))["iteration"]
        await self.request(self.decision_engine, "PUT", f"/v1/tenants/{self.tenant_id}/scenarios/{scenario['id']}/iterations/{iteration['id']}", 200, json={"trigger_formula": true_node(), "score_review_threshold": 30, "score_block_and_review_threshold": 60, "score_decline_threshold": 90, "schedule": ""})
        for index, rule in enumerate(scenario_def.rules, start=1):
            await self.request(self.decision_engine, "POST", f"/v1/tenants/{self.tenant_id}/scenarios/{scenario['id']}/iterations/{iteration['id']}/rules", 201, json={"display_order": index, "name": rule.name, "description": rule.description, "formula": rule.formula, "score_modifier": rule.score, "rule_group": rule.group, "stable_rule_id": clean_id(rule.name)})
        validation = await self.request(self.decision_engine, "POST", f"/v1/tenants/{self.tenant_id}/scenarios/{scenario['id']}/iterations/{iteration['id']}/validate", 200)
        if validation.get("validation", {}).get("valid") is not True:
            raise RuntimeError(f"iteration validation failed for {scenario_def.name}: {json.dumps(validation, default=str)}")
        await self.request(self.decision_engine, "POST", f"/v1/tenants/{self.tenant_id}/scenarios/{scenario['id']}/iterations/{iteration['id']}/commit", 200)
        await self.request(self.decision_engine, "POST", f"/v1/tenants/{self.tenant_id}/scenarios/{scenario['id']}/publications", 200, json={"action": "publish", "iteration_id": iteration["id"]})

    def build_cases(self) -> list[DemoCase]:
        cases: list[DemoCase] = []
        def add(scenario: str, name: str, suffix: str, expected: set[str], **overrides: Any) -> None:
            cases.append(DemoCase(scenario, name, clean_id(f"Case {suffix}"), self.tx(f"Case {suffix}", **overrides), expected))

        add("Account Takeover Detection", "Clean Account Takeover Transfer", "Ato Clean", set())
        add("Account Takeover Detection", "New Device Only", "Ato New Device", {"New Device High Value Transaction"}, device_id="DevNew", amount=6_000)
        add("Account Takeover Detection", "Suspicious IP Only", "Ato Ip", {"Suspicious IP"}, ip=HIGH_RISK_IP)
        add("Account Takeover Detection", "Impossible Travel Only", "Ato Travel", {"Impossible Travel"}, previous_ip_distance_km=800, previous_transaction_minutes_ago=60)
        add("Account Takeover Detection", "Post Credential Change Only", "Ato Password", {"Post Credential Change Transfer"}, account_ref="AcctRecentPassword", device_id="DevRecentPassword", amount=2_500)
        add("Account Takeover Detection", "Failed Login Only", "Ato Failed Login", {"Failed Login Before Transaction"}, account_ref="AcctFailedLogin", device_id="DevFailedLogin")
        add("Account Takeover Detection", "New Beneficiary Only", "Ato New Beneficiary", {"New Beneficiary After Login"}, beneficiary_id="BeneficiaryNew", amount=1_500)
        add("Account Takeover Detection", "Abnormal Spend Only", "Ato Abnormal", {"Abnormal Account Spend"}, account_ref="AcctAbnormal", device_id="DevAbnormal", amount=400)
        add("Account Takeover Detection", "Rapid Post-Login Only", "Ato Rapid", {"Rapid Post-Login Transfers"}, account_ref="AcctRapid", device_id="DevRapid")
        add("Account Takeover Detection", "Full Account Takeover Risk", "Ato Full", {r.name for r in self.rule_defs_by_scenario["Account Takeover Detection"]}, account_ref="AcctAtoFull", device_id="DevAtoFull", beneficiary_id="BeneficiaryNew", amount=6_000, ip=HIGH_RISK_IP, previous_ip_distance_km=800, previous_transaction_minutes_ago=60)

        add("Merchant Abuse Monitoring", "Clean Merchant Payment", "Merchant Clean", set())
        add("Merchant Abuse Monitoring", "High Weekly Merchant Volume Only", "Merchant Weekly", {"High Weekly Merchant Volume"}, merchant_id="MerchantHighWeekly")
        add("Merchant Abuse Monitoring", "Rapid Merchant Burst Only", "Merchant Burst", {"Rapid Merchant Payment Burst"}, merchant_id="MerchantBurst", amount=100)
        add("Merchant Abuse Monitoring", "Watchlisted Merchant Only", "Merchant Watch", {"Watchlisted Merchant Name Match"}, merchant_id="MerchantWatch", product_id="ProductGaming")
        add("Merchant Abuse Monitoring", "Merchant Category Mismatch Only", "Merchant Mismatch", {"Merchant Category Mismatch"}, merchant_id="MerchantMismatch", product_id="ProductGrocery")
        add("Merchant Abuse Monitoring", "Repeated Same Account Payments Only", "Merchant Repeat", {"Repeated Same Account Payments"}, merchant_id="MerchantRepeat")
        add("Merchant Abuse Monitoring", "Shared Merchant IP Cluster Only", "Merchant Ip Cluster", {"Shared Merchant IP Cluster"}, merchant_id="MerchantShared", ip="102.88.1.77")
        add("Merchant Abuse Monitoring", "Abnormal Merchant Ticket Only", "Merchant Abnormal", {"Abnormal Merchant Average Ticket"}, merchant_id="MerchantAbnormal", amount=400)
        add("Merchant Abuse Monitoring", "Recent Settlement Change Only", "Merchant Settlement", {"Settlement Account Recently Changed"}, merchant_id="MerchantSettlement")
        add("Merchant Abuse Monitoring", "Full Merchant Abuse Risk", "Merchant Full", {r.name for r in self.rule_defs_by_scenario["Merchant Abuse Monitoring"]}, merchant_id="MerchantFull", product_id="ProductGrocery", account_ref="AcctMerchantFull", amount=120_000, ip="102.88.1.77")

        add("High Value Transaction Review", "Clean High Value Review Transfer", "Hvt Clean", set())
        add("High Value Transaction Review", "High Value Only", "Hvt High Value", {"High Value Transaction"}, account_ref="AcctHighValueOnly", device_id="DevHighValueOnly", amount=11_000)
        add("High Value Transaction Review", "Product Limit Only", "Hvt Product Limit", {"Product Limit Breach"}, product_id="ProductLowLimit", amount=6_000)
        add("High Value Transaction Review", "Low KYC Only", "Hvt Low Kyc", {"Low KYC High Value Transaction"}, account_ref="AcctLowKyc", device_id="DevLowKyc", required_kyc_level=3, amount=6_000)
        add("High Value Transaction Review", "New Device High Value Only", "Hvt New Device", {"New Device High Value Transaction"}, device_id="DevNew", amount=6_000)
        add("High Value Transaction Review", "Unusual IP Only", "Hvt Ip", {"Unusual IP High Value Transaction"}, ip=HIGH_RISK_IP)
        add("High Value Transaction Review", "Abnormal High Value Spend Only", "Hvt Abnormal", {"Abnormal High Value Spend"}, account_ref="AcctAbnormal", device_id="DevAbnormal", amount=400)
        add("High Value Transaction Review", "Fast Outflow After Funding Only", "Hvt Outflow", {"Fast Outflow After Funding"}, account_ref="AcctFunding", device_id="DevFunding", amount=10_000)
        add("High Value Transaction Review", "Recent Account Change Only", "Hvt Profile", {"Recent Account Change High Value Transaction"}, account_ref="AcctRecentProfile", device_id="DevRecentProfile", amount=6_000)
        add("High Value Transaction Review", "Full High Value Risk", "Hvt Full", {r.name for r in self.rule_defs_by_scenario["High Value Transaction Review"]}, account_ref="AcctHvtFull", device_id="DevHvtFull", product_id="ProductLowLimit", beneficiary_id="BeneficiaryNew", amount=12_000, ip=HIGH_RISK_IP, required_kyc_level=4)
        return cases

    async def evaluate_case(self, case: DemoCase) -> dict[str, Any]:
        response = await self.request(self.decision_engine, "POST", f"/v1/tenants/{self.tenant_id}/scenarios/{self.scenario_ids[case.scenario]}/evaluate", 200, json={"object_id": case.object_id, "object_type": self.object_type, "fields": case.fields})
        result = response.get("result", response)
        triggered_rules = {item["rule_name"] for item in result.get("rule_executions", []) if item.get("outcome") == "hit"}
        decision = result.get("decision") or {}
        return {
            "scenario": case.scenario,
            "case": case.name,
            "object_id": case.object_id,
            "expected_rules": sorted(case.expected_rules),
            "triggered_rules": sorted(triggered_rules),
            "missing_rules": sorted(case.expected_rules - triggered_rules),
            "unexpected_rules": sorted(triggered_rules - case.expected_rules),
            "score": decision.get("score"),
            "outcome": decision.get("outcome"),
            "passed": case.expected_rules == triggered_rules,
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
        raise RuntimeError(f"{last_error}\nIngestion could not find the tenant table. Rerun with --ingestion-database-url pointing at ingestion's database.") from last_error


def print_case(result: dict[str, Any]) -> None:
    expected = ", ".join(result["expected_rules"]) if result["expected_rules"] else "none"
    triggered = ", ".join(result["triggered_rules"]) if result["triggered_rules"] else "none"
    print(f"\nScenario: {result['scenario']}")
    print(f"Case: {result['case']}")
    print(f"Expected: {expected}")
    print(f"Triggered: {triggered}")
    print(f"Score: {result['score']}")
    print(f"Decision: {result['outcome']}")
    print(f"Result: {'PASS' if result['passed'] else 'FAIL'}")
    if result["missing_rules"]:
        print(f"Missing: {', '.join(result['missing_rules'])}")
    if result["unexpected_rules"]:
        print(f"Unexpected: {', '.join(result['unexpected_rules'])}")


def parse_config() -> Config:
    parser = argparse.ArgumentParser(description="Create a fresh tenant and run deterministic demos for account takeover, merchant abuse, and high-value transaction scenarios.")
    parser.add_argument("--tenant-id", help="Optional existing tenant ID. Omit to create a fresh tenant.")
    parser.add_argument("--tenant-name", default=DEFAULT_TENANT_NAME)
    parser.add_argument("--timeout", type=float, default=30.0)
    parser.add_argument("--output", help="Output JSON path. Defaults to timestamped stress-tests/demo-runs file.")
    parser.add_argument("--data-model-url", default=os.getenv("DATA_MODEL_URL", "http://127.0.0.1:8080"))
    parser.add_argument("--ingestion-url", default=os.getenv("INGESTION_URL", "http://127.0.0.1:8081"))
    parser.add_argument("--decision-engine-url", default=os.getenv("DECISION_ENGINE_URL", "http://127.0.0.1:8082"))
    parser.add_argument("--auth-token", default=os.getenv("SERVICE_AUTH_TOKEN"))
    parser.add_argument("--ingestion-database-url", default=os.getenv("INGESTION_DATABASE_URL"))
    args = parser.parse_args()
    output = args.output or str(Path("stress-tests/demo-runs") / f"additional-scenarios-{utc_now().replace(':', '').replace('.', '-')}.json")
    return Config(args.tenant_id, args.tenant_name, args.timeout, output, args.data_model_url.rstrip("/"), args.ingestion_url.rstrip("/"), args.decision_engine_url.rstrip("/"), args.auth_token, args.ingestion_database_url)


async def async_main() -> int:
    config = parse_config()
    harness = AdditionalScenarioDemoHarness(config)
    try:
        tenant_label = config.tenant_id or "a fresh tenant"
        print(f"bootstrapping additional fraud scenarios in {tenant_label}...")
        await harness.bootstrap()
        print("\nAdditional Fraud Scenario Demo")
        results = []
        for case in harness.build_cases():
            result = await harness.evaluate_case(case)
            results.append(result)
            print_case(result)
        coverage: dict[str, dict[str, dict[str, bool]]] = {}
        for scenario_name, rules in harness.rule_defs_by_scenario.items():
            coverage[scenario_name] = {}
            scenario_results = [r for r in results if r["scenario"] == scenario_name]
            for rule in rules:
                coverage[scenario_name][rule.name] = {
                    "triggered": any(rule.name in r["triggered_rules"] for r in scenario_results),
                    "not_triggered": any(rule.name not in r["triggered_rules"] for r in scenario_results),
                }
        coverage_passed = all(item["triggered"] and item["not_triggered"] for scenario in coverage.values() for item in scenario.values())
        cases_passed = all(r["passed"] for r in results)
        print("\nRule Coverage")
        for scenario_name, items in coverage.items():
            print(f"\n{scenario_name}")
            for rule_name, item in items.items():
                print(f"{rule_name}: triggered {'YES' if item['triggered'] else 'NO'}, not-triggered {'YES' if item['not_triggered'] else 'NO'}")
        print(f"\nOverall: {'PASS' if cases_passed and coverage_passed else 'FAIL'}")
        summary = {
            "summary_version": 1,
            "test": {"name": "additional_fraud_scenario_demo", "objective": "Show each requested rule triggering and not triggering in deterministic cases."},
            "run": asdict(config) | {"auth_token": "set" if config.auth_token else None, "ingestion_database_url": "set" if config.ingestion_database_url else None},
            "setup": {
                "tenant_id": harness.tenant_id,
                "transaction_object_type": harness.object_type,
                "scenario_ids": harness.scenario_ids,
                "ip_watchlist_name": harness.ip_list_name,
                "decision_table": {"approve": "0-29", "review": "30-59", "block_and_review": "60-89", "decline": "90+"},
                "rules": {name: [asdict(rule) | {"formula": rule.formula} for rule in rules] for name, rules in harness.rule_defs_by_scenario.items()},
            },
            "cases": results,
            "coverage": coverage,
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
