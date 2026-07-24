from __future__ import annotations

import argparse
import asyncio
import importlib.util
import json
import os
import sys
import time
from dataclasses import asdict, dataclass
from datetime import datetime, timedelta, timezone
from pathlib import Path
from typing import Any
from urllib.parse import parse_qsl, urlencode, urlsplit, urlunsplit

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
    allow_extra_rules: bool = False


def filter_node(table_name: str, field_name: str, operator: str, value: dict[str, Any]) -> dict[str, Any]:
    return fn("Filter", tableName=const(table_name), fieldName=const(field_name), operator=const(operator), value=value)


def aggregator(table_name: str, field_name: str, name: str, *filters: dict[str, Any]) -> dict[str, Any]:
    return fn("Aggregator", tableName=const(table_name), fieldName=const(field_name), aggregator=const(name), filters=list_node(*filters))


def eq(left: dict[str, Any], right: dict[str, Any]) -> dict[str, Any]:
    return fn("eq", left, right)


def lte(left: dict[str, Any], right: dict[str, Any]) -> dict[str, Any]:
    return fn("lte", left, right)


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
            ("system_type", "string", False, False),
            ("ip", "string", False, False),
            ("ip_country", "string", False, False),
            ("ip_region", "string", False, False),
            ("ip_location", "string", False, False),
            ("ip_reputation_score", "int", False, False),
            ("ip_network_risk", "string", False, False),
            ("previous_ip_distance_km", "int", False, False),
            ("previous_transaction_minutes_ago", "int", False, False),
            ("required_kyc_level", "int", False, False),
            ("direction", "string", False, False),
            ("status", "string", False, False),
            ("group_id", "string", False, False),
            ("group_name", "string", False, False),
            ("group_type", "string", False, False),
            ("vote_approval_status", "string", False, False),
            ("required_vote_count", "int", False, False),
            ("approved_vote_count", "int", False, False),
            ("destination_account_ref", "string", False, False),
            ("verified_settlement_account_ref", "string", False, False),
            ("group_current_balance", "int", False, False),
            ("outstanding_loan_amount", "int", False, False),
            ("loan_amount", "int", False, False),
            ("loan_status", "string", False, False),
            ("member_joined_at", "timestamp", False, False),
            ("entry_point_type", "string", False, False),
            ("is_usual_active_hour", "bool", False, False),
        ]:
            name, data_type, nullable, is_unique = item
            created = await self.create_field(transactions["id"], {"name": name, "data_type": data_type, "nullable": nullable, "is_unique": is_unique})
            tx_fields[name] = created

        account_key = await self.create_field(accounts["id"], {"name": "account_ref", "data_type": "string", "nullable": False, "is_unique": True})
        for name, data_type in [
            ("customer_name", "string"),
            ("kyc_level", "int"),
            ("country", "string"),
            ("created_at", "timestamp"),
            ("last_transaction_at", "timestamp"),
            ("last_password_change_at", "timestamp"),
            ("last_profile_change_at", "timestamp"),
            ("available_balance", "int"),
            ("borrowing_limit", "int"),
            ("usual_ip_region", "string"),
            ("known_device_ids", "string"),
        ]:
            await self.create_field(accounts["id"], {"name": name, "data_type": data_type, "nullable": False})

        device_key = await self.create_field(devices["id"], {"name": "device_id", "data_type": "string", "nullable": False, "is_unique": True})
        for name, data_type in [("account_ref", "string"), ("first_seen_at", "timestamp")]:
            await self.create_field(devices["id"], {"name": name, "data_type": data_type, "nullable": False})

        product_key = await self.create_field(products["id"], {"name": "product_id", "data_type": "string", "nullable": False, "is_unique": True})
        for name, data_type in [("product_name", "string"), ("product_category", "string"), ("max_amount", "int")]:
            await self.create_field(products["id"], {"name": name, "data_type": data_type, "nullable": False})

        beneficiary_key = await self.create_field(beneficiaries["id"], {"name": "beneficiary_id", "data_type": "string", "nullable": False, "is_unique": True})
        for name, data_type in [("account_ref", "string"), ("beneficiary_name", "string"), ("beneficiary_account_ref", "string"), ("created_at", "timestamp")]:
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
                ("currency", "string"), ("country", "string"), ("channel", "string"), ("system_type", "string"),
                ("ip", "string"), ("ip_country", "string"), ("ip_region", "string"), ("ip_location", "string"),
                ("ip_reputation_score", "int"), ("ip_network_risk", "string"), ("previous_ip_distance_km", "int"),
                ("previous_transaction_minutes_ago", "int"), ("required_kyc_level", "int"), ("direction", "string"),
                ("status", "string"), ("group_id", "string"), ("group_name", "string"), ("group_type", "string"),
                ("vote_approval_status", "string"), ("required_vote_count", "int"), ("approved_vote_count", "int"),
                ("destination_account_ref", "string"), ("verified_settlement_account_ref", "string"),
                ("group_current_balance", "int"), ("outstanding_loan_amount", "int"), ("loan_amount", "int"),
                ("loan_status", "string"), ("member_joined_at", "timestamp"), ("entry_point_type", "string"),
                ("is_usual_active_hour", "bool"),
            ]],
            self.account_object_type: [{"name": k, "data_type": v, "is_unique": k == "account_ref"} for k, v in [
                ("account_ref", "string"), ("customer_name", "string"), ("kyc_level", "int"),
                ("country", "string"), ("created_at", "timestamp"), ("last_transaction_at", "timestamp"),
                ("last_password_change_at", "timestamp"), ("last_profile_change_at", "timestamp"),
                ("available_balance", "int"), ("borrowing_limit", "int"), ("usual_ip_region", "string"), ("known_device_ids", "string"),
            ]],
            self.device_object_type: [{"name": k, "data_type": v, "is_unique": k == "device_id"} for k, v in [
                ("device_id", "string"), ("account_ref", "string"), ("first_seen_at", "timestamp"),
            ]],
            self.product_object_type: [{"name": k, "data_type": v, "is_unique": k == "product_id"} for k, v in [
                ("product_id", "string"), ("product_name", "string"), ("product_category", "string"), ("max_amount", "int"),
            ]],
            self.beneficiary_object_type: [{"name": k, "data_type": v, "is_unique": k == "beneficiary_id"} for k, v in [
                ("beneficiary_id", "string"), ("account_ref", "string"), ("beneficiary_name", "string"), ("beneficiary_account_ref", "string"), ("created_at", "timestamp"),
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
        last_error: Exception | None = None
        for candidate_url in self.ingestion_database_url_candidates(database_url):
            try:
                with psycopg.connect(candidate_url) as conn:
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
                if candidate_url != database_url:
                    print(
                        "materialized ingestion schema using fallback database URL "
                        f"{self.redact_database_url(candidate_url)}",
                        file=sys.stderr,
                    )
                return
            except psycopg.OperationalError as exc:
                last_error = exc
                continue
        assert last_error is not None
        raise RuntimeError(
            "failed to connect to ingestion database for schema materialization. "
            f"Tried: {', '.join(self.redact_database_url(url) for url in self.ingestion_database_url_candidates(database_url))}. "
            f"Last error: {last_error}"
        ) from last_error

    def ingestion_database_url_candidates(self, database_url: str) -> list[str]:
        candidates = [database_url]
        parsed = urlsplit(database_url)
        host = (parsed.hostname or "").lower()
        db_name = parsed.path.lstrip("/")
        if host not in {"127.0.0.1", "localhost"} or db_name != "ingestion":
            return candidates

        fallback_specs = [
            ("fraud", "fraud", 5432),
            ("ingestion", "ingestion", 5435),
        ]
        for user, password, port in fallback_specs:
            candidate = self.replace_database_url_credentials(database_url, user, password, port)
            if candidate not in candidates:
                candidates.append(candidate)
        return candidates

    @staticmethod
    def replace_database_url_credentials(database_url: str, username: str, password: str, port: int) -> str:
        parsed = urlsplit(database_url)
        host = parsed.hostname or "localhost"
        netloc = f"{username}:{password}@{host}:{port}"
        return urlunsplit((parsed.scheme, netloc, parsed.path, parsed.query, parsed.fragment))

    @staticmethod
    def redact_database_url(database_url: str) -> str:
        parsed = urlsplit(database_url)
        query = urlencode(parse_qsl(parsed.query, keep_blank_values=True))
        username = parsed.username or ""
        host = parsed.hostname or ""
        port = f":{parsed.port}" if parsed.port is not None else ""
        auth = f"{username}:***@" if username else ""
        return urlunsplit((parsed.scheme, f"{auth}{host}{port}", parsed.path, query, parsed.fragment))

    async def seed_reference_data(self) -> None:
        account_specs = {
            "AcctClean": (3, BASE_TIME - timedelta(days=30), BASE_TIME - timedelta(days=30)),
            "AcctQuiet": (3, BASE_TIME - timedelta(days=30), BASE_TIME - timedelta(days=30)),
            "AcctRecentPassword": (3, BASE_TIME - timedelta(hours=2), BASE_TIME - timedelta(days=30)),
            "AcctLowKyc": (1, BASE_TIME - timedelta(days=30), BASE_TIME - timedelta(days=30)),
            "AcctRecentProfile": (3, BASE_TIME - timedelta(days=30), BASE_TIME - timedelta(hours=2)),
            "AcctFailedLogin": (3, BASE_TIME - timedelta(days=30), BASE_TIME - timedelta(days=30)),
            "AcctRapid": (3, BASE_TIME - timedelta(days=30), BASE_TIME - timedelta(days=30)),
            "AcctFunding": (3, BASE_TIME - timedelta(days=30), BASE_TIME - timedelta(days=30)),
            "AcctAtoFull": (3, BASE_TIME - timedelta(hours=2), BASE_TIME - timedelta(days=30)),
            "AcctHighValueOnly": (3, BASE_TIME - timedelta(days=30), BASE_TIME - timedelta(days=30)),
            "AcctHvtFull": (1, BASE_TIME - timedelta(days=30), BASE_TIME - timedelta(hours=2)),
            "AcctWalletFull": (2, BASE_TIME - timedelta(days=30), BASE_TIME - timedelta(days=30)),
            "AcctCardFull": (3, BASE_TIME - timedelta(days=30), BASE_TIME - timedelta(days=30)),
            "AcctBankFull": (3, BASE_TIME - timedelta(days=30), BASE_TIME - timedelta(hours=2)),
            "AcctBankClean": (3, BASE_TIME - timedelta(days=30), BASE_TIME - timedelta(days=30)),
            "AcctCashOutFull": (1, BASE_TIME - timedelta(days=30), BASE_TIME - timedelta(days=30)),
            "AcctCashOutClean": (3, BASE_TIME - timedelta(days=30), BASE_TIME - timedelta(days=30)),
            "AcctNewBeneficiaryFull": (3, BASE_TIME - timedelta(days=30), BASE_TIME - timedelta(hours=2)),
            "AcctCrossBorderFull": (3, BASE_TIME - timedelta(days=30), BASE_TIME - timedelta(days=30)),
            "AcctRegulatoryStr": (3, BASE_TIME - timedelta(days=30), BASE_TIME - timedelta(days=30)),
        }
        for account_ref, (kyc, password_at, profile_at) in account_specs.items():
            await self.ingest(self.account_object_type, {
                "object_id": account_ref,
                "account_ref": account_ref,
                "customer_name": account_ref.replace("_", " ").title(),
                "kyc_level": kyc,
                "country": "GH",
                "created_at": iso(BASE_TIME - timedelta(days=200)),
                "last_transaction_at": iso(BASE_TIME - timedelta(days=10)),
                "last_password_change_at": iso(password_at),
                "last_profile_change_at": iso(profile_at),
                "available_balance": 20_000,
                "borrowing_limit": 1_000,
                "usual_ip_region": "Greater Accra",
                "known_device_ids": ",DevOld,DevQuiet,DevRecentPassword,DevLowKyc,DevRecentProfile,DevFailedLogin,DevRapid,DevFunding,DevHighValueOnly,DevBankClean,DevCashOutClean,",
            })
        for account_ref, payload in {
            "AcctDormantFull": {
                "kyc_level": 3,
                "created_at": BASE_TIME - timedelta(days=300),
                "last_transaction_at": BASE_TIME - timedelta(days=120),
                "last_password_change_at": BASE_TIME - timedelta(days=30),
                "last_profile_change_at": BASE_TIME - timedelta(hours=2),
                "available_balance": 10_000,
                "borrowing_limit": 1_000,
            },
            "AcctChangoContributionFull": {
                "kyc_level": 1,
                "created_at": BASE_TIME - timedelta(days=2),
                "last_transaction_at": BASE_TIME - timedelta(days=1),
                "last_password_change_at": BASE_TIME - timedelta(days=30),
                "last_profile_change_at": BASE_TIME - timedelta(days=30),
                "available_balance": 20_000,
                "borrowing_limit": 1_000,
            },
            "AcctChangoDisbursementFull": {
                "kyc_level": 3,
                "created_at": BASE_TIME - timedelta(days=60),
                "last_transaction_at": BASE_TIME - timedelta(days=1),
                "last_password_change_at": BASE_TIME - timedelta(days=30),
                "last_profile_change_at": BASE_TIME - timedelta(days=30),
                "available_balance": 20_000,
                "borrowing_limit": 1_000,
            },
        }.items():
            await self.ingest(self.account_object_type, {
                "object_id": account_ref,
                "account_ref": account_ref,
                "customer_name": account_ref,
                "kyc_level": payload["kyc_level"],
                "country": "GH",
                "created_at": iso(payload["created_at"]),
                "last_transaction_at": iso(payload["last_transaction_at"]),
                "last_password_change_at": iso(payload["last_password_change_at"]),
                "last_profile_change_at": iso(payload["last_profile_change_at"]),
                "available_balance": payload["available_balance"],
                "borrowing_limit": payload["borrowing_limit"],
                "usual_ip_region": "Greater Accra",
                "known_device_ids": ",DevOld,",
            })
        for device_id, account_ref, first_seen in [
            ("DevOld", "AcctClean", BASE_TIME - timedelta(days=20)),
            ("DevQuiet", "AcctQuiet", BASE_TIME - timedelta(days=20)),
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
            ("DevWalletShared", "AcctWalletFull", BASE_TIME - timedelta(days=20)),
            ("DevCardFull", "AcctCardFull", BASE_TIME - timedelta(days=20)),
            ("DevBankFull", "AcctBankFull", BASE_TIME - timedelta(days=20)),
            ("DevBankClean", "AcctBankClean", BASE_TIME - timedelta(days=20)),
            ("DevCashOutFull", "AcctCashOutFull", BASE_TIME - timedelta(days=20)),
            ("DevCashOutClean", "AcctCashOutClean", BASE_TIME - timedelta(days=20)),
            ("DevNewBeneficiaryFull", "AcctNewBeneficiaryFull", BASE_TIME - timedelta(hours=2)),
            ("DevDormantFull", "AcctDormantFull", BASE_TIME - timedelta(hours=2)),
            ("DevCrossBorderFull", "AcctCrossBorderFull", BASE_TIME - timedelta(hours=2)),
            ("DevChangoContributionFull", "AcctChangoContributionFull", BASE_TIME - timedelta(days=20)),
            ("DevChangoDisbursementFull", "AcctChangoDisbursementFull", BASE_TIME - timedelta(days=20)),
            ("DevRegulatoryStr", "AcctRegulatoryStr", BASE_TIME - timedelta(days=20)),
        ]:
            await self.ingest(self.device_object_type, {"object_id": device_id, "device_id": device_id, "account_ref": account_ref, "first_seen_at": iso(first_seen)})
        for product_id, category, max_amount in [
            ("ProductWallet", "wallet_transfer", 20_000),
            ("ProductLowLimit", "wallet_transfer", 5_000),
            ("ProductGrocery", "grocery", 20_000),
            ("ProductGaming", "gaming", 20_000),
        ]:
            await self.ingest(self.product_object_type, {"object_id": product_id, "product_id": product_id, "product_name": product_id, "product_category": category, "max_amount": max_amount})
        for beneficiary_id, created_at, account_ref, beneficiary_name in [
            ("BeneficiaryOld", BASE_TIME - timedelta(days=30), "AcctClean", "Trusted Beneficiary"),
            ("BeneficiaryNew", BASE_TIME - timedelta(hours=2), "AcctClean", "New Beneficiary"),
            ("BeneficiaryWatch", BASE_TIME - timedelta(hours=2), "AcctNewBeneficiaryFull", "Blocked Beneficiary"),
            ("BeneficiaryBankClean", BASE_TIME - timedelta(days=30), "AcctBankClean", "Bank Clean Beneficiary"),
        ]:
            await self.ingest(self.beneficiary_object_type, {"object_id": beneficiary_id, "beneficiary_id": beneficiary_id, "account_ref": account_ref, "beneficiary_name": beneficiary_name, "beneficiary_account_ref": f"{beneficiary_id}Account", "created_at": iso(created_at)})
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
            ("MerchantCashOutFull", "Kumasi Cash Out Agent", "agent", BASE_TIME - timedelta(days=30)),
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
        await self.ingest(self.account_object_type, {"object_id": "AcctAbnormal", "account_ref": "AcctAbnormal", "customer_name": "Abnormal Spend", "kyc_level": 3, "country": "GH", "created_at": iso(BASE_TIME - timedelta(days=200)), "last_transaction_at": iso(BASE_TIME - timedelta(days=10)), "last_password_change_at": iso(BASE_TIME - timedelta(days=30)), "last_profile_change_at": iso(BASE_TIME - timedelta(days=30)), "available_balance": 20_000, "borrowing_limit": 1_000, "usual_ip_region": "Greater Accra", "known_device_ids": ",DevAbnormal,"})
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

        rows.append(self.tx("QuietWalletAverage", account_ref="AcctQuiet", device_id="DevQuiet", merchant_id="MerchantHistory", amount=1_000, at=BASE_TIME - timedelta(days=10)))
        rows.append(self.tx("QuietCardAverage", account_ref="AcctQuiet", device_id="DevQuiet", merchant_id="MerchantHistory", channel="card", amount=1_000, at=BASE_TIME - timedelta(days=10)))
        for i in range(20):
            rows.append(self.tx(f"WalletFullAverage{i}", account_ref="AcctWalletFull", device_id="DevOld", merchant_id="MerchantHistory", amount=100, at=BASE_TIME - timedelta(days=10, minutes=i)))
        for i in range(11):
            rows.append(self.tx(f"WalletFullBurst{i}", account_ref="AcctWalletFull", device_id="DevOld", merchant_id="MerchantHistory", amount=100, at=BASE_TIME - timedelta(minutes=50 - i)))
        for i in range(6):
            rows.append(self.tx(f"WalletFullWeekly{i}", account_ref="AcctWalletFull", device_id="DevOld", merchant_id="MerchantHistory", amount=9_000, at=BASE_TIME - timedelta(days=2, minutes=i)))
        for i in range(4):
            rows.append(self.tx(f"WalletSharedDevice{i}", account_ref=f"WalletSharedAcct{i}", device_id="DevWalletShared", merchant_id="MerchantHistory", amount=100, at=BASE_TIME - timedelta(hours=2, minutes=i)))

        for i in range(20):
            rows.append(self.tx(f"CardFullAverage{i}", account_ref="AcctCardFull", device_id="DevCardFull", merchant_id="MerchantHistory", channel="card", amount=100, at=BASE_TIME - timedelta(days=10, minutes=i)))
        for i in range(6):
            rows.append(self.tx(f"CardFullDeclined{i}", account_ref="AcctCardFull", device_id="DevCardFull", merchant_id="MerchantHistory", channel="card", status="declined", amount=10, at=BASE_TIME - timedelta(minutes=25 - i)))
        for i in range(4):
            rows.append(self.tx(f"CardFullSmall{i}", account_ref="AcctCardFull", device_id="DevCardFull", merchant_id="MerchantHistory", channel="card", amount=10, at=BASE_TIME - timedelta(minutes=50 - i)))

        for i in range(20):
            rows.append(self.tx(f"BankFullAverage{i}", account_ref="AcctBankFull", device_id="DevBankFull", merchant_id="MerchantHistory", channel="bank", beneficiary_id="BeneficiaryOld", amount=100, at=BASE_TIME - timedelta(days=10, minutes=i)))
        for i in range(6):
            rows.append(self.tx(f"BankFullBurst{i}", account_ref="AcctBankFull", device_id="DevBankFull", merchant_id="MerchantHistory", channel="bank", beneficiary_id="BeneficiaryOld", amount=100, at=BASE_TIME - timedelta(minutes=50 - i)))
        for i in range(11):
            rows.append(self.tx(f"BankBeneficiaryShared{i}", account_ref=f"BankSharedAcct{i}", device_id="DevOld", merchant_id="MerchantHistory", channel="bank", beneficiary_id="BeneficiaryNew", amount=100, at=BASE_TIME - timedelta(days=2, minutes=i)))
        rows.append(self.tx("BankCleanAverage", account_ref="AcctBankClean", device_id="DevBankClean", merchant_id="MerchantHistory", channel="bank", beneficiary_id="BeneficiaryBankClean", amount=1_000, at=BASE_TIME - timedelta(days=10)))

        for i in range(20):
            rows.append(self.tx(f"CashOutFullAverage{i}", account_ref="AcctCashOutFull", device_id="DevCashOutFull", merchant_id="MerchantHistory", system_type="cash_out", amount=100, at=BASE_TIME - timedelta(days=10, minutes=i)))
        for i in range(3):
            rows.append(self.tx(f"CashOutFunding{i}", account_ref="AcctCashOutFull", device_id="DevCashOutFull", merchant_id="MerchantHistory", direction="incoming", amount=2_000, at=BASE_TIME - timedelta(hours=2, minutes=i)))
        for i in range(4):
            rows.append(self.tx(f"CashOutBurst{i}", account_ref="AcctCashOutFull", device_id="DevCashOutFull", merchant_id="MerchantCashOutFull", system_type="cash_out", amount=100, at=BASE_TIME - timedelta(minutes=40 - i)))
        for i in range(6):
            rows.append(self.tx(f"CashOutAgentVolume{i}", account_ref=f"CashOutVolumeAcct{i}", device_id="DevOld", merchant_id="MerchantCashOutFull", system_type="cash_out", amount=9_000, at=BASE_TIME - timedelta(hours=3, minutes=i)))
        for i in range(21):
            rows.append(self.tx(f"CashOutAgentShared{i}", account_ref=f"CashOutSharedAcct{i}", device_id="DevOld", merchant_id="MerchantCashOutFull", system_type="cash_out", amount=100, at=BASE_TIME - timedelta(hours=4, minutes=i)))
        rows.append(self.tx("CashOutCleanAverage", account_ref="AcctCashOutClean", device_id="DevCashOutClean", merchant_id="MerchantHistory", system_type="cash_out", amount=1_000, at=BASE_TIME - timedelta(days=10)))

        for i in range(20):
            rows.append(self.tx(f"NewBeneficiaryAverage{i}", account_ref="AcctNewBeneficiaryFull", device_id="DevNewBeneficiaryFull", beneficiary_id="BeneficiaryOld", merchant_id="MerchantHistory", amount=100, at=BASE_TIME - timedelta(days=10, minutes=i)))
        for i in range(4):
            await self.ingest(self.beneficiary_object_type, {
                "object_id": f"BeneficiaryVelocity{i}",
                "beneficiary_id": f"BeneficiaryVelocity{i}",
                "account_ref": "AcctNewBeneficiaryFull",
                "beneficiary_name": f"Velocity Beneficiary {i}",
                "beneficiary_account_ref": f"VelocityBeneficiary{i}Account",
                "created_at": iso(BASE_TIME - timedelta(hours=3, minutes=i)),
            })

        for i in range(20):
            rows.append(self.tx(f"DormantAverage{i}", account_ref="AcctDormantFull", device_id="DevOld", merchant_id="MerchantHistory", amount=100, at=BASE_TIME - timedelta(days=10, minutes=i)))
        for i in range(4):
            rows.append(self.tx(f"DormantRapid{i}", account_ref="AcctDormantFull", device_id="DevOld", merchant_id="MerchantHistory", amount=100, at=BASE_TIME - timedelta(minutes=45 - i)))

        for i in range(20):
            rows.append(self.tx(f"CrossBorderAverage{i}", account_ref="AcctCrossBorderFull", device_id="DevOld", merchant_id="MerchantHistory", amount=100, at=BASE_TIME - timedelta(days=10, minutes=i)))
        for i in range(6):
            rows.append(self.tx(f"CrossBorderRapid{i}", account_ref="AcctCrossBorderFull", device_id="DevOld", merchant_id="MerchantHistory", amount=100, at=BASE_TIME - timedelta(minutes=50 - i)))
            rows.append(self.tx(f"CrossBorderSharedIp{i}", account_ref=f"CrossBorderSharedAcct{i}", device_id="DevOld", merchant_id="MerchantHistory", ip="185.220.101.77", amount=100, at=BASE_TIME - timedelta(hours=2, minutes=i)))

        for i in range(20):
            rows.append(self.tx(f"ChangoContributionAverage{i}", account_ref="AcctChangoContributionFull", device_id="DevChangoContributionFull", merchant_id="MerchantHistory", group_id="GroupOther", amount=100, at=BASE_TIME - timedelta(days=10, minutes=i)))
        for i in range(4):
            rows.append(self.tx(f"ChangoNewAccountContribution{i}", account_ref="AcctChangoContributionFull", device_id="DevChangoContributionFull", merchant_id="MerchantHistory", group_id="GroupOther", system_type="contribution", amount=100, at=BASE_TIME - timedelta(days=2, minutes=i)))
        for i in range(26):
            rows.append(self.tx(f"ChangoGroupBurst{i}", account_ref=f"ChangoBurstAcct{i}", device_id="DevOld", merchant_id="MerchantHistory", group_id="GroupChangoFull", system_type="contribution", amount=500, at=BASE_TIME - timedelta(minutes=55 - (i % 55), seconds=i)))
        for i in range(6):
            rows.append(self.tx(f"ChangoGroupWeekly{i}", account_ref=f"ChangoWeeklyAcct{i}", device_id="DevOld", merchant_id="MerchantHistory", group_id="GroupChangoFull", system_type="contribution", amount=9_000, at=BASE_TIME - timedelta(days=2, minutes=i)))
        for i in range(6):
            rows.append(self.tx(f"ChangoSharedIp{i}", account_ref=f"ChangoSharedIpAcct{i}", device_id="DevOld", merchant_id="MerchantHistory", group_id="GroupChangoFull", system_type="contribution", ip="102.129.50.44", amount=500, at=BASE_TIME - timedelta(hours=2, minutes=i)))
        for i in range(11):
            rows.append(self.tx(f"ChangoStructuring{i}", account_ref=f"ChangoStructuringAcct{i}", device_id="DevOld", merchant_id="MerchantHistory", group_id="GroupChangoFull", system_type="contribution", amount=4_900, at=BASE_TIME - timedelta(hours=3, minutes=i)))

        for i in range(4):
            rows.append(self.tx(f"ChangoDisbursementContrib{i}", account_ref=f"ChangoDisbContribAcct{i}", device_id="DevOld", merchant_id="MerchantHistory", group_id="GroupDisbursementFull", system_type="contribution", amount=8_000, at=BASE_TIME - timedelta(hours=2, minutes=i)))
        rows.append(self.tx("ChangoDisbursementPriorWithdrawal", account_ref="AcctChangoDisbursementFull", device_id="DevChangoDisbursementFull", merchant_id="MerchantHistory", group_id="GroupDisbursementFull", system_type="disbursement", amount=9_000, at=BASE_TIME - timedelta(days=2)))

        for i in range(5):
            rows.append(self.tx(f"RegulatoryStr{i}", account_ref="AcctRegulatoryStr", device_id="DevRegulatoryStr", merchant_id="MerchantHistory", amount=100, at=BASE_TIME - timedelta(hours=3, minutes=i)))
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
            "system_type": "payment",
            "ip": NORMAL_IP,
            "ip_country": "GH",
            "ip_region": "Greater Accra",
            "ip_location": "Accra",
            "ip_reputation_score": 10,
            "ip_network_risk": "normal",
            "previous_ip_distance_km": 10,
            "previous_transaction_minutes_ago": 240,
            "required_kyc_level": 2,
            "direction": "outgoing",
            "status": "approved",
            "group_id": "GroupClean",
            "group_name": "Family Welfare Group",
            "group_type": "private",
            "vote_approval_status": "approved",
            "required_vote_count": 3,
            "approved_vote_count": 3,
            "destination_account_ref": "VerifiedDestination",
            "verified_settlement_account_ref": "VerifiedDestination",
            "group_current_balance": 100_000,
            "outstanding_loan_amount": 0,
            "loan_amount": 0,
            "loan_status": "paid",
            "member_joined_at": iso(BASE_TIME - timedelta(days=30)),
            "entry_point_type": "none",
            "is_usual_active_hour": True,
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
        wallet = filter_node(self.object_type, "channel", "=", const("wallet"))
        card = filter_node(self.object_type, "channel", "=", const("card"))
        bank = filter_node(self.object_type, "channel", "=", const("bank"))
        cash_out = filter_node(self.object_type, "system_type", "=", const("cash_out"))
        group_filter = filter_node(self.object_type, "group_id", "=", field("group_id"))
        suspicious_ip = fn("or", fn("in_custom_list", list=const(self.ip_list_name), value=field("ip")), gt(field("ip_reputation_score"), const(80)))
        foreign_access = neq(field("ip_country"), related("account", "country"))
        new_beneficiary = gte(related("beneficiary", "created_at"), time_add(field("date"), "P1D"))
        return [
            ScenarioDef("Wallet Transfer Fraud Screening", [
                RuleDef("High Transfer Amount", "Transaction Based Rules", "transaction.amount is greater than 3x the AVG of transaction.amount for the same account_ref between Now and 30 days before Now.", 25, gt(field("amount"), fn("multiply", aggregator(self.object_type, "amount", "AVG", acct, date_30d), const(3)))),
                RuleDef("Product Limit Breach", "Transaction Based Rules", "transaction.amount is greater than product.max_amount.", 35, gt(field("amount"), related("product", "max_amount"))),
                RuleDef("One Hour Transfer Burst", "Velocity Rules", "COUNT of transaction.transaction_id for the same account_ref between Now and 1 hour before Now is greater than 10.", 30, gt(aggregator(self.object_type, "transaction_id", "COUNT", acct, date_1h), const(10))),
                RuleDef("Weekly Transfer Velocity", "Velocity Rules", "SUM of transaction.amount for the same account_ref between Now and 7 days before Now is greater than 50000.", 35, gt(aggregator(self.object_type, "amount", "SUM", acct, date_7d), const(50_000))),
                RuleDef("New Device Transfer", "Device Fingerprinting", "transaction.device_id is not in the known device list for account.account_ref.", 25, fn("not", fn("contains", related("account", "known_device_ids"), field("device_id")))),
                RuleDef("Shared Device Risk", "Device Fingerprinting", "COUNT_DISTINCT of transaction.account_ref using the same device_id between Now and 24 hours before Now is greater than 3.", 30, gt(aggregator(self.object_type, "account_ref", "COUNT_DISTINCT", filter_node(self.object_type, "device_id", "=", field("device_id")), date_24h), const(3))),
                RuleDef("Unusual IP Region", "Geolocation and IP Monitoring", "transaction.ip_region is not equal to account.usual_ip_region.", 20, neq(field("ip_region"), related("account", "usual_ip_region"))),
                RuleDef("High Risk IP", "Geolocation and IP Monitoring", "transaction.ip is in the high-risk IP watchlist.", 35, fn("in_custom_list", list=const(self.ip_list_name), value=field("ip"))),
                RuleDef("New Beneficiary", "High Transfer Beneficiary Risk Rules", "beneficiary.first_seen_at is within 24 hours before Now, and transaction.amount is greater than 2000.", 30, fn("and", new_beneficiary, gt(field("amount"), const(2_000)))),
                RuleDef("Low KYC", "High Transfer KYC and Account Profile Rules", "account.kyc_level is less than required KYC level for transaction.amount.", 35, lt(related("account", "kyc_level"), field("required_kyc_level"))),
            ]),
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
            ScenarioDef("Card Payment Authorization Risk", [
                RuleDef("High Card Payment Amount", "Transaction Value Risk", "transaction.channel is equal to card, and transaction.amount is greater than 5000.", 30, fn("and", eq(field("channel"), const("card")), gt(field("amount"), const(5_000)))),
                RuleDef("Product Limit Breach", "Product Limit Risk", "transaction.channel is equal to card, and transaction.amount is greater than product.max_amount for the matching product_id.", 40, fn("and", eq(field("channel"), const("card")), gt(field("amount"), related("product", "max_amount")))),
                RuleDef("High Risk Merchant Category", "Merchant Category Risk", "transaction.channel is equal to card, and merchant.merchant_category is in the high-risk merchant category watchlist.", 35, fn("and", eq(field("channel"), const("card")), fn("in", related("merchant", "merchant_category"), list_node(const("gaming"), const("crypto"), const("adult"))))),
                RuleDef("Card Testing Pattern", "Card Testing Risk", "COUNT of declined card transactions for the same account_ref between Now and 30 minutes before Now is greater than 5.", 40, gt(aggregator(self.object_type, "transaction_id", "COUNT", acct, card, filter_node(self.object_type, "status", "=", const("declined")), filter_node(self.object_type, "date", ">=", time_add(field("date"), "PT30M"))), const(5))),
                RuleDef("Small-To-Large Card Escalation", "Card Testing Risk", "COUNT of card transactions below 20 for the same account_ref between Now and 1 hour before Now is greater than 3, and current transaction.amount is greater than 1000.", 35, fn("and", gt(aggregator(self.object_type, "transaction_id", "COUNT", acct, card, filter_node(self.object_type, "amount", "<", const(20)), date_1h), const(3)), gt(field("amount"), const(1_000)))),
                RuleDef("Abnormal Card Spend", "Behavioral Pattern Risk", "transaction.channel is equal to card, and transaction.amount is greater than 3x the AVG of card transaction.amount for the same account_ref between Now and 30 days before Now.", 30, fn("and", eq(field("channel"), const("card")), gt(field("amount"), fn("multiply", aggregator(self.object_type, "amount", "AVG", acct, card, date_30d), const(3))))),
                RuleDef("Suspicious IP Card Payment", "Geolocation and IP Monitoring", "transaction.channel is equal to card, and transaction.ip is in the high-risk IP watchlist or IP_REPUTATION_SCORE(transaction.ip) is greater than 80.", 35, fn("and", eq(field("channel"), const("card")), suspicious_ip)),
                RuleDef("Unusual Card Payment Hour", "Time-of-Day Risk", "transaction.channel is equal to card, and transaction.date occurs outside the customer's usual active transaction hours calculated from the same account_ref over the last 30 days.", 20, fn("and", eq(field("channel"), const("card")), eq(field("is_usual_active_hour"), const(False)))),
            ]),
            ScenarioDef("Bank Transfer Risk Assessment", [
                RuleDef("High Bank Transfer Amount", "Transaction Value Risk", "transaction.channel is equal to bank, and transaction.amount is greater than 10000.", 30, fn("and", eq(field("channel"), const("bank")), gt(field("amount"), const(10_000)))),
                RuleDef("Product Limit Breach", "Product Limit Risk", "transaction.channel is equal to bank, and transaction.amount is greater than product.max_amount for the matching product_id.", 40, fn("and", eq(field("channel"), const("bank")), gt(field("amount"), related("product", "max_amount")))),
                RuleDef("New Beneficiary", "High Value Transfer Beneficiary Risk", "beneficiary.created_at is within 24 hours before Now, and transaction.amount is greater than 5000.", 35, fn("and", new_beneficiary, gt(field("amount"), const(5_000)))),
                RuleDef("First Transfer To Beneficiary", "Beneficiary Risk", "COUNT of previous transactions for the same account_ref and beneficiary.account_ref before Now is equal to 0, and transaction.amount is greater than 2000.", 30, fn("and", eq(aggregator(self.object_type, "transaction_id", "COUNT", acct, filter_node(self.object_type, "beneficiary_id", "=", field("beneficiary_id")), filter_node(self.object_type, "date", "<", field("date"))), const(0)), gt(field("amount"), const(2_000)))),
                RuleDef("Rapid Bank Transfer Burst", "Velocity Risk", "COUNT of bank transaction.transaction_id for the same account_ref between Now and 1 hour before Now is greater than 5.", 30, gt(aggregator(self.object_type, "transaction_id", "COUNT", acct, bank, date_1h), const(5))),
                RuleDef("Post Account Change Bank Transfer", "Account Change Risk", "DATE_DIFF between Now and account.last_profile_change_at is less than 24 hours, and transaction.channel is equal to bank.", 35, fn("and", gte(related("account", "last_profile_change_at"), time_add(field("date"), "P1D")), eq(field("channel"), const("bank")))),
                RuleDef("Beneficiary Shared Across Many Accounts", "Network Link Analysis Risk", "COUNT_DISTINCT of transaction.account_ref sending to the same beneficiary.account_ref between Now and 7 days before Now is greater than 10.", 35, gt(aggregator(self.object_type, "account_ref", "COUNT_DISTINCT", filter_node(self.object_type, "beneficiary_id", "=", field("beneficiary_id")), date_7d), const(10))),
                RuleDef("Abnormal Bank Transfer Amount", "Behavioral Pattern Risk", "transaction.amount is greater than 3x the AVG of bank transaction.amount for the same account_ref between Now and 30 days before Now.", 30, gt(field("amount"), fn("multiply", aggregator(self.object_type, "amount", "AVG", acct, bank, date_30d), const(3)))),
            ]),
            ScenarioDef("Cash-Out Fraud Monitoring", [
                RuleDef("Fast Cash-Out After Funding", "Source of Funds Risk", "SUM of incoming transaction.amount for the same account_ref between Now and 24 hours before Now is greater than 5000, and the current cash-out transaction.amount is greater than 80% of that sum.", 40, fn("and", gt(aggregator(self.object_type, "amount", "SUM", acct, date_24h, filter_node(self.object_type, "direction", "=", const("incoming"))), const(5_000)), gt(field("amount"), fn("multiply", aggregator(self.object_type, "amount", "SUM", acct, date_24h, filter_node(self.object_type, "direction", "=", const("incoming"))), const(0.8))))),
                RuleDef("Rapid Cash-Out Burst", "Velocity Risk", "COUNT of cash-out transaction.transaction_id for the same account_ref between Now and 1 hour before Now is greater than 3.", 30, gt(aggregator(self.object_type, "transaction_id", "COUNT", acct, cash_out, date_1h), const(3))),
                RuleDef("High Cash-Out Amount", "Transaction Value Risk", "transaction.system_type is equal to cash_out, and transaction.amount is greater than 5000.", 30, fn("and", eq(field("system_type"), const("cash_out")), gt(field("amount"), const(5_000)))),
                RuleDef("Agent High Daily Cash-Out Volume", "Agent Risk", "SUM of cash-out transaction.amount for the same merchant_id between Now and 24 hours before Now is greater than 50000.", 35, gt(aggregator(self.object_type, "amount", "SUM", merch, cash_out, date_24h), const(50_000))),
                RuleDef("Agent Shared Across Many Accounts", "Network Link Analysis Risk", "COUNT_DISTINCT of transaction.account_ref cashing out through the same merchant_id between Now and 24 hours before Now is greater than 20.", 35, gt(aggregator(self.object_type, "account_ref", "COUNT_DISTINCT", merch, cash_out, date_24h), const(20))),
                RuleDef("Unusual Cash-Out Location", "Geolocation and IP Monitoring", "transaction.system_type is equal to cash_out, and transaction.ip_location.region is not equal to the usual region for the same account_ref calculated over the last 30 days.", 30, fn("and", eq(field("system_type"), const("cash_out")), neq(field("ip_region"), related("account", "usual_ip_region")))),
                RuleDef("Abnormal Cash-Out Amount", "Behavioral Pattern Risk", "transaction.amount is greater than 3x the AVG of cash-out transaction.amount for the same account_ref between Now and 30 days before Now.", 30, gt(field("amount"), fn("multiply", aggregator(self.object_type, "amount", "AVG", acct, cash_out, date_30d), const(3)))),
                RuleDef("Low KYC Cash-Out", "KYC and Account Profile Risk", "account.kyc_level is less than the required KYC level for the cash-out transaction.amount.", 25, lt(related("account", "kyc_level"), field("required_kyc_level"))),
            ]),
            ScenarioDef("New Beneficiary Payment Review", [
                RuleDef("New Beneficiary High Value Payment", "Beneficiary Risk", "beneficiary.created_at is within 24 hours before Now, and transaction.amount is greater than 5000.", 35, fn("and", new_beneficiary, gt(field("amount"), const(5_000)))),
                RuleDef("First Payment To Beneficiary", "Beneficiary Risk", "COUNT of previous transactions for the same account_ref and beneficiary.account_ref before Now is equal to 0, and current transaction.amount is greater than 2000.", 30, fn("and", eq(aggregator(self.object_type, "transaction_id", "COUNT", acct, filter_node(self.object_type, "beneficiary_id", "=", field("beneficiary_id")), filter_node(self.object_type, "date", "<", field("date"))), const(0)), gt(field("amount"), const(2_000)))),
                RuleDef("Rapid New Beneficiary Additions", "Beneficiary Velocity Risk", "COUNT of new beneficiaries created for the same account_ref between Now and 24 hours before Now is greater than 3.", 35, gt(aggregator(self.beneficiary_object_type, "beneficiary_id", "COUNT", filter_node(self.beneficiary_object_type, "account_ref", "=", field("account_ref")), filter_node(self.beneficiary_object_type, "created_at", ">=", time_add(field("date"), "P1D"))), const(3))),
                RuleDef("Beneficiary Watchlist Match", "Custom List and Watchlist Risk", "FUZZY_MATCH of beneficiary.name or beneficiary.account_ref against the beneficiary watchlist is greater than the configured match threshold.", 45, gte(fn("FuzzyMatchAnyOf", related("beneficiary", "beneficiary_name"), list_node(const("Blocked Beneficiary"), const("Fraud Mule Account")), algorithm=const("bag_of_words_similarity")), const(80))),
                RuleDef("New Device Beneficiary Payment", "Device Fingerprinting", "device.first_seen_at for the same device_id and account_ref is within 24 hours before Now, and payment is made to a new beneficiary.", 35, fn("and", gte(related("device", "first_seen_at"), time_add(field("date"), "P1D")), new_beneficiary)),
                RuleDef("Suspicious IP Beneficiary Payment", "Geolocation and IP Monitoring", "transaction.ip is in the high-risk IP watchlist or IP_REPUTATION_SCORE(transaction.ip) is greater than 80, and payment is made to a new beneficiary.", 35, fn("and", suspicious_ip, new_beneficiary)),
                RuleDef("Post Account Change Beneficiary Payment", "Account Change Risk", "DATE_DIFF between Now and account.last_profile_change_at is less than 24 hours, and payment is made to a new beneficiary.", 35, fn("and", gte(related("account", "last_profile_change_at"), time_add(field("date"), "P1D")), new_beneficiary)),
                RuleDef("Abnormal First Beneficiary Payment", "Behavioral Pattern Risk", "transaction.amount is greater than 3x the AVG of first-beneficiary-payment transaction.amount for the same account_ref between Now and 30 days before Now.", 30, gt(field("amount"), fn("multiply", aggregator(self.object_type, "amount", "AVG", acct, date_30d), const(3)))),
            ]),
            ScenarioDef("Dormant Account Reactivation Risk", [
                RuleDef("Dormant Account Transaction", "Dormancy Risk", "DATE_DIFF between Now and account.last_transaction_at is greater than 90 days, and current transaction.amount is greater than 1000.", 30, fn("and", lt(related("account", "last_transaction_at"), time_add(field("date"), "P90D")), gt(field("amount"), const(1_000)))),
                RuleDef("High First Transaction After Dormancy", "Dormancy Risk", "DATE_DIFF between Now and account.last_transaction_at is greater than 90 days, and current transaction.amount is greater than 3x the AVG of transaction.amount for the same account_ref before dormancy.", 35, fn("and", lt(related("account", "last_transaction_at"), time_add(field("date"), "P90D")), gt(field("amount"), fn("multiply", aggregator(self.object_type, "amount", "AVG", acct, date_30d), const(3))))),
                RuleDef("New Device After Dormancy", "Device Fingerprinting", "DATE_DIFF between Now and account.last_transaction_at is greater than 90 days, and device.first_seen_at for the same device_id and account_ref is within 24 hours before Now.", 35, fn("and", lt(related("account", "last_transaction_at"), time_add(field("date"), "P90D")), gte(related("device", "first_seen_at"), time_add(field("date"), "P1D")))),
                RuleDef("Suspicious IP After Dormancy", "Geolocation and IP Monitoring", "DATE_DIFF between Now and account.last_transaction_at is greater than 90 days, and transaction.ip is in the high-risk IP watchlist or IP_REPUTATION_SCORE(transaction.ip) is greater than 80.", 35, fn("and", lt(related("account", "last_transaction_at"), time_add(field("date"), "P90D")), suspicious_ip)),
                RuleDef("Account Change After Dormancy", "Account Change Risk", "DATE_DIFF between Now and account.last_transaction_at is greater than 90 days, and DATE_DIFF between Now and account.last_profile_change_at is less than 24 hours.", 35, fn("and", lt(related("account", "last_transaction_at"), time_add(field("date"), "P90D")), gte(related("account", "last_profile_change_at"), time_add(field("date"), "P1D")))),
                RuleDef("New Beneficiary After Dormancy", "Beneficiary Risk", "DATE_DIFF between Now and account.last_transaction_at is greater than 90 days, and beneficiary.created_at is within 24 hours before Now.", 35, fn("and", lt(related("account", "last_transaction_at"), time_add(field("date"), "P90D")), new_beneficiary)),
                RuleDef("Rapid Transfers After Dormancy", "Velocity Risk", "DATE_DIFF between Now and account.last_transaction_at is greater than 90 days, and COUNT of transaction.transaction_id for the same account_ref between Now and 1 hour before Now is greater than 3.", 30, fn("and", lt(related("account", "last_transaction_at"), time_add(field("date"), "P90D")), gt(aggregator(self.object_type, "transaction_id", "COUNT", acct, date_1h), const(3)))),
                RuleDef("Balance Drain After Dormancy", "Source of Funds Risk", "DATE_DIFF between Now and account.last_transaction_at is greater than 90 days, and current transaction.amount is greater than 80% of account.available_balance.", 40, fn("and", lt(related("account", "last_transaction_at"), time_add(field("date"), "P90D")), gt(field("amount"), fn("multiply", related("account", "available_balance"), const(0.8))))),
            ]),
            ScenarioDef("Cross-Border or Proxy Access Review", [
                RuleDef("IP Country Mismatch", "Geolocation and IP Monitoring", "IP_COUNTRY(transaction.ip) is not equal to account.country.", 30, foreign_access),
                RuleDef("High Risk Network Access", "Network Reputation Risk", "transaction.ip is in the VPN, proxy, TOR, hosting provider, or high-risk ASN watchlist.", 40, fn("in", field("ip_network_risk"), list_node(const("vpn"), const("proxy"), const("tor"), const("hosting"), const("high_risk_asn")))),
                RuleDef("Impossible Travel Access", "Impossible Travel Risk", "DISTANCE_KM between the current IP_LOCATION(transaction.ip) and the previous IP_LOCATION(transaction.ip) for the same account_ref is greater than 500, and DATE_DIFF from the previous transaction is less than 2 hours.", 40, fn("and", gt(field("previous_ip_distance_km"), const(500)), lt(field("previous_transaction_minutes_ago"), const(120)))),
                RuleDef("New Device Foreign Access", "Device Fingerprinting", "IP_COUNTRY(transaction.ip) is not equal to account.country, and device.first_seen_at for the same device_id and account_ref is within 24 hours before Now.", 35, fn("and", foreign_access, gte(related("device", "first_seen_at"), time_add(field("date"), "P1D")))),
                RuleDef("High Value Foreign Access Transaction", "Transaction Value Risk", "IP_COUNTRY(transaction.ip) is not equal to account.country, and transaction.amount is greater than 5000.", 35, fn("and", foreign_access, gt(field("amount"), const(5_000)))),
                RuleDef("Foreign Access Behavior Change", "Behavioral Pattern Risk", "IP_COUNTRY(transaction.ip) is not equal to account.country, and transaction.amount is greater than 3x the AVG of transaction.amount for the same account_ref between Now and 30 days before Now.", 30, fn("and", foreign_access, gt(field("amount"), fn("multiply", aggregator(self.object_type, "amount", "AVG", acct, date_30d), const(3))))),
                RuleDef("Shared Suspicious IP Across Accounts", "Network Link Analysis Risk", "COUNT_DISTINCT of transaction.account_ref using the same transaction.ip between Now and 24 hours before Now is greater than 5.", 35, gt(aggregator(self.object_type, "account_ref", "COUNT_DISTINCT", filter_node(self.object_type, "ip", "=", field("ip")), date_24h), const(5))),
                RuleDef("Cross-Border Rapid Transaction", "Velocity Risk", "IP_COUNTRY(transaction.ip) is not equal to account.country, and COUNT of transaction.transaction_id for the same account_ref between Now and 1 hour before Now is greater than 5.", 30, fn("and", foreign_access, gt(aggregator(self.object_type, "transaction_id", "COUNT", acct, date_1h), const(5)))),
            ]),
            ScenarioDef("Chango Group Contribution Fraud Monitoring", [
                RuleDef("High Weekly Group Contribution Value", "Group Contribution Velocity Risk", "SUM of transaction.amount for the same group_id between Now and 7 days before Now is greater than 50000.", 30, gt(aggregator(self.object_type, "amount", "SUM", group_filter, date_7d), const(50_000))),
                RuleDef("Rapid Group Contribution Burst", "Group Contribution Velocity Risk", "COUNT of transaction.transaction_id for the same group_id between Now and 1 hour before Now is greater than 25.", 25, gt(aggregator(self.object_type, "transaction_id", "COUNT", group_filter, date_1h), const(25))),
                RuleDef("New Account Contribution Spike", "Contributor Account Risk", "COUNT of transaction.transaction_id where account.account_ref equals transaction.account_ref and account.created_at is within 7 days before Now is greater than 3.", 20, fn("and", gte(related("account", "created_at"), time_add(field("date"), "P7D")), gt(aggregator(self.object_type, "transaction_id", "COUNT", acct, date_7d), const(3)))),
                RuleDef("Low KYC High Contribution", "Contributor Account Risk", "account.kyc_level is less than the required KYC level for transaction.amount.", 25, lt(related("account", "kyc_level"), field("required_kyc_level"))),
                RuleDef("Shared IP Contributor Cluster", "Device and IP Link Analysis Risk", "COUNT_DISTINCT of transaction.account_ref using the same transaction.ip between Now and 24 hours before Now is greater than 5.", 30, gt(aggregator(self.object_type, "account_ref", "COUNT_DISTINCT", filter_node(self.object_type, "ip", "=", field("ip")), date_24h), const(5))),
                RuleDef("Watchlisted Campaign Name Match", "Campaign Risk", "FUZZY_MATCH of group.name against the campaign watchlist is greater than the configured match threshold.", 40, gte(fn("FuzzyMatchAnyOf", field("group_name"), list_node(const("Fake Medical Emergency"), const("Blocked Relief Campaign")), algorithm=const("bag_of_words_similarity")), const(80))),
                RuleDef("Threshold Structuring Pattern", "Structuring Risk", "COUNT of transaction.transaction_id for the same group_id where transaction.amount is just below the review threshold between Now and 24 hours before Now is greater than 10.", 35, gt(aggregator(self.object_type, "transaction_id", "COUNT", group_filter, date_24h, filter_node(self.object_type, "amount", ">=", const(4_500)), filter_node(self.object_type, "amount", "<", const(5_000))), const(10))),
                RuleDef("Abnormal Contribution Amount", "Behavioral Pattern Risk", "transaction.amount is greater than 3x the AVG of transaction.amount for the same account_ref between Now and 30 days before Now.", 30, gt(field("amount"), fn("multiply", aggregator(self.object_type, "amount", "AVG", acct, date_30d), const(3)))),
            ]),
            ScenarioDef("Chango Disbursement and Borrowing Risk Review", [
                RuleDef("Missing Vote Approval", "Disbursement Authorization Risk", "group.vote_approval_status is not equal to approved when transaction.system_type is equal to disbursement.", 45, fn("and", eq(field("system_type"), const("disbursement")), neq(field("vote_approval_status"), const("approved")))),
                RuleDef("Insufficient Approved Votes", "Disbursement Authorization Risk", "COUNT of approved votes for the same group_id is less than group.required_vote_count.", 40, lt(field("approved_vote_count"), field("required_vote_count"))),
                RuleDef("Watchlisted Destination Match", "Beneficiary and Destination Risk", "FUZZY_MATCH of disbursement.destination_account_ref against the destination watchlist is greater than the configured match threshold.", 50, gte(fn("FuzzyMatchAnyOf", field("destination_account_ref"), list_node(const("BlockedDestination"), const("FraudDestination")), algorithm=const("bag_of_words_similarity")), const(80))),
                RuleDef("Public Group Destination Mismatch", "Beneficiary and Destination Risk", "group.type is equal to public, and disbursement.destination_account_ref is not equal to group.verified_settlement_account_ref.", 50, fn("and", eq(field("group_type"), const("public")), neq(field("destination_account_ref"), field("verified_settlement_account_ref")))),
                RuleDef("Fast Cashout After Contribution Spike", "Group Fund Velocity Risk", "SUM of transaction.amount for contributions into the same group_id between Now and 24 hours before Now is greater than 30000, and transaction.system_type is equal to disbursement.", 35, fn("and", eq(field("system_type"), const("disbursement")), gt(aggregator(self.object_type, "amount", "SUM", group_filter, date_24h, filter_node(self.object_type, "system_type", "=", const("contribution"))), const(30_000)))),
                RuleDef("High Group Balance Withdrawal", "Group Fund Velocity Risk", "SUM of disbursement.amount for the same group_id between Now and 7 days before Now is greater than 80% of group.current_balance.", 35, gt(aggregator(self.object_type, "amount", "SUM", group_filter, date_7d, filter_node(self.object_type, "system_type", "=", const("disbursement"))), fn("multiply", field("group_current_balance"), const(0.8)))),
                RuleDef("Borrowing Above Limit", "Borrowing and Repayment Behavior Risk", "SUM of loan.amount for the same account_ref where loan.status is not equal to paid is greater than account.borrowing_limit.", 40, gt(field("outstanding_loan_amount"), related("account", "borrowing_limit"))),
                RuleDef("New Member High Loan Request", "Member Tenure and Contribution History Risk", "DATE_DIFF between Now and member.joined_at is less than 7 days, and loan.amount is greater than 1000.", 30, fn("and", gte(field("member_joined_at"), time_add(field("date"), "P7D")), gt(field("loan_amount"), const(1_000)))),
            ]),
            ScenarioDef("Regulatory Reporting Review", [
                RuleDef("Cash Transaction Threshold Report", "AML Regulatory Reporting", "transaction.channel is cash or branch cash, and transaction.currency is GHS, and transaction.amount is greater than or equal to 50000.", 1, fn("and", fn("in", field("channel"), list_node(const("cash"), const("branch_cash"))), eq(field("currency"), const("GHS")), gte(field("amount"), const(50_000)))),
                RuleDef("Suspicious Low-Value Repeated Activity", "AML Suspicion Monitoring", "COUNT of transaction.transaction_id for the same account_ref between Now and 1 day before Now is greater than or equal to 5, and SUM of transaction.amount for the same account_ref in that period is low or unusual compared with the account's normal activity.", 1, fn("and", gte(aggregator(self.object_type, "transaction_id", "COUNT", acct, date_24h), const(5)), lte(aggregator(self.object_type, "amount", "SUM", acct, date_24h), const(1_000)))),
                RuleDef("Cross-Border Currency Declaration Review", "Cross-Border Cash Monitoring", "transaction.system_type is cross-border cash declaration, and transaction.country is not the customer's usual country, or transaction.entry_point_type is airport or land border.", 1, fn("or", fn("and", eq(field("system_type"), const("cross_border_cash_declaration")), neq(field("country"), related("account", "country"))), fn("in", field("entry_point_type"), list_node(const("airport"), const("land_border"))))),
                RuleDef("Electronic Transfer Reporting Threshold", "Electronic Transfer Monitoring", "transaction.channel is bank or electronic transfer, and transaction.currency is USD, and transaction.amount is greater than 1000, and transaction.direction is inward or outward.", 1, fn("and", fn("in", field("channel"), list_node(const("bank"), const("electronic_transfer"))), eq(field("currency"), const("USD")), gt(field("amount"), const(1_000)), fn("in", field("direction"), list_node(const("inward"), const("outward"))))),
            ]),
        ]

    async def bootstrap_scenario(self, scenario_def: ScenarioDef) -> None:
        scenario = (await self.request(self.decision_engine, "POST", f"/v1/tenants/{self.tenant_id}/scenarios", 201, json={"name": scenario_def.name, "trigger_object_type": self.object_type}))["scenario"]
        self.scenario_ids[scenario_def.name] = scenario["id"]
        self.rule_defs_by_scenario[scenario_def.name] = scenario_def.rules
        iteration = (await self.request(self.decision_engine, "POST", f"/v1/tenants/{self.tenant_id}/scenarios/{scenario['id']}/iterations", 201))["iteration"]
        thresholds = {"score_review_threshold": 30, "score_block_and_review_threshold": 60, "score_decline_threshold": 90}
        if scenario_def.name == "Regulatory Reporting Review":
            thresholds = {"score_review_threshold": 0, "score_block_and_review_threshold": 9999, "score_decline_threshold": 99999}
        await self.request(self.decision_engine, "PUT", f"/v1/tenants/{self.tenant_id}/scenarios/{scenario['id']}/iterations/{iteration['id']}", 200, json={"trigger_formula": true_node(), **thresholds, "schedule": ""})
        for index, rule in enumerate(scenario_def.rules, start=1):
            await self.request(self.decision_engine, "POST", f"/v1/tenants/{self.tenant_id}/scenarios/{scenario['id']}/iterations/{iteration['id']}/rules", 201, json={"display_order": index, "name": rule.name, "description": rule.description, "formula": rule.formula, "score_modifier": rule.score, "rule_group": rule.group, "stable_rule_id": clean_id(rule.name)})
        validation = await self.request(self.decision_engine, "POST", f"/v1/tenants/{self.tenant_id}/scenarios/{scenario['id']}/iterations/{iteration['id']}/validate", 200)
        if validation.get("validation", {}).get("valid") is not True:
            raise RuntimeError(f"iteration validation failed for {scenario_def.name}: {json.dumps(validation, default=str)}")
        await self.request(self.decision_engine, "POST", f"/v1/tenants/{self.tenant_id}/scenarios/{scenario['id']}/iterations/{iteration['id']}/commit", 200)
        await self.prepare_iteration_for_publication(scenario["id"], iteration["id"])
        await self.request(self.decision_engine, "POST", f"/v1/tenants/{self.tenant_id}/scenarios/{scenario['id']}/publications", 200, json={"action": "publish", "iteration_id": iteration["id"]})

    async def prepare_iteration_for_publication(self, scenario_id: str, iteration_id: str) -> None:
        status = await self.publication_preparation_status(scenario_id, iteration_id)
        if status.get("preparation_finished") is True and status.get("preparation_required") is not True:
            return

        await self.request(
            self.decision_engine,
            "POST",
            f"/v1/tenants/{self.tenant_id}/scenarios/{scenario_id}/publications/preparation",
            202,
            json={"iteration_id": iteration_id},
        )
        deadline = time.monotonic() + 120.0
        while time.monotonic() < deadline:
            status = await self.publication_preparation_status(scenario_id, iteration_id)
            if status.get("preparation_finished") is True and status.get("preparation_required") is not True:
                return
            print(f"waiting for ITC publication indexes: pending={status.get('pending_items')}")
            await asyncio.sleep(1.0)
        raise RuntimeError("ITC scenario publication preparation did not complete within 120s; make sure the data-model index worker is running")

    async def publication_preparation_status(self, scenario_id: str, iteration_id: str) -> dict[str, Any]:
        response = await self.request(
            self.decision_engine,
            "GET",
            f"/v1/tenants/{self.tenant_id}/scenarios/{scenario_id}/publications/preparation",
            200,
            params={"iteration_id": iteration_id},
        )
        return response.get("preparation", response)

    def build_cases(self) -> list[DemoCase]:
        cases: list[DemoCase] = []
        def add(scenario: str, name: str, suffix: str, expected: set[str], allow_extra_rules: bool = False, **overrides: Any) -> None:
            cases.append(DemoCase(scenario, name, clean_id(f"Case {suffix}"), self.tx(f"Case {suffix}", **overrides), expected, allow_extra_rules))

        add("Wallet Transfer Fraud Screening", "Clean Wallet Transfer", "Wallet Clean", set(), account_ref="AcctQuiet", device_id="DevQuiet")
        add("Wallet Transfer Fraud Screening", "High Transfer Amount Only", "Wallet High Transfer", {"High Transfer Amount"}, account_ref="AcctQuiet", device_id="DevQuiet", amount=4_000)
        add("Wallet Transfer Fraud Screening", "Product Limit Breach Only", "Wallet Product Limit", {"Product Limit Breach"}, allow_extra_rules=True, account_ref="AcctClean", device_id="DevQuiet", product_id="ProductLowLimit", amount=6_000)
        add("Wallet Transfer Fraud Screening", "One Hour Transfer Burst Only", "Wallet Burst", {"One Hour Transfer Burst"}, allow_extra_rules=True, account_ref="AcctWalletFull", device_id="DevOld", amount=500)
        add("Wallet Transfer Fraud Screening", "Weekly Transfer Velocity Only", "Wallet Weekly", {"Weekly Transfer Velocity"}, allow_extra_rules=True, account_ref="AcctWalletFull", device_id="DevOld", amount=500)
        add("Wallet Transfer Fraud Screening", "New Device Transfer Only", "Wallet New Device", {"New Device Transfer"}, account_ref="AcctQuiet", device_id="DevNew")
        add("Wallet Transfer Fraud Screening", "Shared Device Risk Only", "Wallet Shared Device", {"Shared Device Risk"}, allow_extra_rules=True, account_ref="AcctWalletFull", device_id="DevWalletShared", amount=500)
        add("Wallet Transfer Fraud Screening", "Unusual IP Region Only", "Wallet Region", {"Unusual IP Region"}, account_ref="AcctQuiet", device_id="DevQuiet", ip_region="Ashanti")
        add("Wallet Transfer Fraud Screening", "High Risk IP Only", "Wallet High Risk Ip", {"High Risk IP"}, account_ref="AcctQuiet", device_id="DevQuiet", ip=HIGH_RISK_IP)
        add("Wallet Transfer Fraud Screening", "New Beneficiary Only", "Wallet New Beneficiary", {"New Beneficiary"}, account_ref="AcctQuiet", device_id="DevQuiet", beneficiary_id="BeneficiaryNew", amount=2_500)
        add("Wallet Transfer Fraud Screening", "Low KYC Only", "Wallet Low Kyc", {"Low KYC"}, account_ref="AcctLowKyc", device_id="DevLowKyc", required_kyc_level=3)
        add("Wallet Transfer Fraud Screening", "Full Wallet Transfer Risk", "Wallet Full", {r.name for r in self.rule_defs_by_scenario["Wallet Transfer Fraud Screening"]}, account_ref="AcctWalletFull", device_id="DevWalletShared", product_id="ProductLowLimit", beneficiary_id="BeneficiaryNew", amount=6_000, ip=HIGH_RISK_IP, ip_region="Ashanti", required_kyc_level=4)

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

        add("Card Payment Authorization Risk", "Clean Card Authorization", "Card Clean", set())
        add("Card Payment Authorization Risk", "High Card Payment Amount Only", "Card High Amount", {"High Card Payment Amount"}, allow_extra_rules=True, account_ref="AcctCardFull", device_id="DevCardFull", channel="card", amount=6_000)
        add("Card Payment Authorization Risk", "Product Limit Breach Only", "Card Product Limit", {"Product Limit Breach"}, allow_extra_rules=True, account_ref="AcctCardFull", device_id="DevCardFull", product_id="ProductLowLimit", channel="card", amount=6_000)
        add("Card Payment Authorization Risk", "High Risk Merchant Category Only", "Card Merchant Category", {"High Risk Merchant Category"}, account_ref="AcctQuiet", device_id="DevQuiet", merchant_id="MerchantWatch", channel="card", amount=500)
        add("Card Payment Authorization Risk", "Card Testing Pattern Only", "Card Testing", {"Card Testing Pattern"}, allow_extra_rules=True, account_ref="AcctCardFull", device_id="DevCardFull", channel="card", amount=500)
        add("Card Payment Authorization Risk", "Small-To-Large Card Escalation Only", "Card Escalation", {"Small-To-Large Card Escalation"}, allow_extra_rules=True, account_ref="AcctCardFull", device_id="DevCardFull", channel="card", amount=1_500)
        add("Card Payment Authorization Risk", "Abnormal Card Spend Only", "Card Abnormal", {"Abnormal Card Spend"}, allow_extra_rules=True, account_ref="AcctCardFull", device_id="DevCardFull", channel="card", amount=6_000)
        add("Card Payment Authorization Risk", "Suspicious IP Card Payment Only", "Card Suspicious Ip", {"Suspicious IP Card Payment"}, account_ref="AcctQuiet", device_id="DevQuiet", channel="card", ip=HIGH_RISK_IP)
        add("Card Payment Authorization Risk", "Unusual Card Payment Hour Only", "Card Unusual Hour", {"Unusual Card Payment Hour"}, account_ref="AcctQuiet", device_id="DevQuiet", channel="card", is_usual_active_hour=False)
        add("Card Payment Authorization Risk", "Full Card Authorization Risk", "Card Full", {r.name for r in self.rule_defs_by_scenario["Card Payment Authorization Risk"]}, account_ref="AcctCardFull", device_id="DevCardFull", merchant_id="MerchantWatch", product_id="ProductLowLimit", channel="card", amount=6_000, ip=HIGH_RISK_IP, is_usual_active_hour=False)

        add("Bank Transfer Risk Assessment", "Clean Bank Transfer", "Bank Clean", set(), account_ref="AcctBankClean", device_id="DevBankClean", beneficiary_id="BeneficiaryBankClean")
        add("Bank Transfer Risk Assessment", "High Bank Transfer Amount Only", "Bank High Amount", {"High Bank Transfer Amount"}, allow_extra_rules=True, account_ref="AcctBankClean", device_id="DevBankClean", beneficiary_id="BeneficiaryBankClean", channel="bank", amount=11_000)
        add("Bank Transfer Risk Assessment", "Product Limit Breach Only", "Bank Product Limit", {"Product Limit Breach"}, allow_extra_rules=True, account_ref="AcctBankClean", device_id="DevBankClean", beneficiary_id="BeneficiaryBankClean", product_id="ProductLowLimit", channel="bank", amount=6_000)
        add("Bank Transfer Risk Assessment", "New Beneficiary Only", "Bank New Beneficiary", {"New Beneficiary", "First Transfer To Beneficiary"}, allow_extra_rules=True, account_ref="AcctBankClean", device_id="DevBankClean", beneficiary_id="BeneficiaryNew", channel="bank", amount=6_000)
        add("Bank Transfer Risk Assessment", "First Transfer To Beneficiary Only", "Bank First Beneficiary", {"First Transfer To Beneficiary"}, allow_extra_rules=True, account_ref="AcctBankClean", device_id="DevBankClean", beneficiary_id="BeneficiaryNew", channel="bank", amount=2_500)
        add("Bank Transfer Risk Assessment", "Rapid Bank Transfer Burst Only", "Bank Burst", {"Rapid Bank Transfer Burst"}, allow_extra_rules=True, account_ref="AcctBankFull", device_id="DevBankFull", beneficiary_id="BeneficiaryBankClean", channel="bank", amount=500)
        add("Bank Transfer Risk Assessment", "Post Account Change Bank Transfer Only", "Bank Profile", {"Post Account Change Bank Transfer"}, allow_extra_rules=True, account_ref="AcctBankFull", device_id="DevBankFull", beneficiary_id="BeneficiaryBankClean", channel="bank", amount=500)
        add("Bank Transfer Risk Assessment", "Beneficiary Shared Across Many Accounts Only", "Bank Shared Beneficiary", {"Beneficiary Shared Across Many Accounts"}, allow_extra_rules=True, account_ref="AcctBankClean", device_id="DevBankClean", beneficiary_id="BeneficiaryNew", channel="bank", amount=500)
        add("Bank Transfer Risk Assessment", "Abnormal Bank Transfer Amount Only", "Bank Abnormal", {"Abnormal Bank Transfer Amount"}, allow_extra_rules=True, account_ref="AcctBankFull", device_id="DevBankFull", beneficiary_id="BeneficiaryBankClean", channel="bank", amount=6_000)
        add("Bank Transfer Risk Assessment", "Full Bank Transfer Risk", "Bank Full", {r.name for r in self.rule_defs_by_scenario["Bank Transfer Risk Assessment"]}, account_ref="AcctBankFull", device_id="DevBankFull", product_id="ProductLowLimit", beneficiary_id="BeneficiaryNew", channel="bank", amount=12_000)

        add("Cash-Out Fraud Monitoring", "Clean Cash-Out", "Cash Out Clean", set(), account_ref="AcctCashOutClean", device_id="DevCashOutClean")
        add("Cash-Out Fraud Monitoring", "Fast Cash-Out After Funding Only", "Cash Out Funding", {"Fast Cash-Out After Funding"}, allow_extra_rules=True, account_ref="AcctCashOutFull", device_id="DevCashOutFull", system_type="cash_out", amount=5_000)
        add("Cash-Out Fraud Monitoring", "Rapid Cash-Out Burst Only", "Cash Out Burst", {"Rapid Cash-Out Burst"}, allow_extra_rules=True, account_ref="AcctCashOutFull", device_id="DevCashOutFull", system_type="cash_out", amount=500)
        add("Cash-Out Fraud Monitoring", "High Cash-Out Amount Only", "Cash Out High Amount", {"High Cash-Out Amount"}, allow_extra_rules=True, account_ref="AcctCashOutClean", device_id="DevCashOutClean", system_type="cash_out", amount=6_000)
        add("Cash-Out Fraud Monitoring", "Agent High Daily Cash-Out Volume Only", "Cash Out Agent Volume", {"Agent High Daily Cash-Out Volume"}, allow_extra_rules=True, account_ref="AcctCashOutClean", device_id="DevCashOutClean", merchant_id="MerchantCashOutFull", system_type="cash_out", amount=500)
        add("Cash-Out Fraud Monitoring", "Agent Shared Across Many Accounts Only", "Cash Out Shared Agent", {"Agent Shared Across Many Accounts"}, allow_extra_rules=True, account_ref="AcctCashOutClean", device_id="DevCashOutClean", merchant_id="MerchantCashOutFull", system_type="cash_out", amount=500)
        add("Cash-Out Fraud Monitoring", "Unusual Cash-Out Location Only", "Cash Out Location", {"Unusual Cash-Out Location"}, account_ref="AcctCashOutClean", device_id="DevCashOutClean", system_type="cash_out", ip_region="Ashanti")
        add("Cash-Out Fraud Monitoring", "Abnormal Cash-Out Amount Only", "Cash Out Abnormal", {"Abnormal Cash-Out Amount"}, allow_extra_rules=True, account_ref="AcctCashOutFull", device_id="DevCashOutFull", system_type="cash_out", amount=6_000)
        add("Cash-Out Fraud Monitoring", "Low KYC Cash-Out Only", "Cash Out Low Kyc", {"Low KYC Cash-Out"}, allow_extra_rules=True, account_ref="AcctCashOutFull", device_id="DevCashOutFull", system_type="cash_out", amount=500, required_kyc_level=4)
        add("Cash-Out Fraud Monitoring", "Full Cash-Out Risk", "Cash Out Full", {r.name for r in self.rule_defs_by_scenario["Cash-Out Fraud Monitoring"]}, account_ref="AcctCashOutFull", device_id="DevCashOutFull", merchant_id="MerchantCashOutFull", system_type="cash_out", amount=6_000, ip_region="Ashanti", required_kyc_level=4)

        add("New Beneficiary Payment Review", "Clean Beneficiary Payment", "Beneficiary Clean", set())
        add("New Beneficiary Payment Review", "New Beneficiary High Value Payment Only", "Beneficiary High Value", {"New Beneficiary High Value Payment", "First Payment To Beneficiary"}, beneficiary_id="BeneficiaryNew", amount=6_000)
        add("New Beneficiary Payment Review", "First Payment To Beneficiary Only", "Beneficiary First Payment", {"First Payment To Beneficiary"}, beneficiary_id="BeneficiaryNew", amount=2_500)
        add("New Beneficiary Payment Review", "Rapid New Beneficiary Additions Only", "Beneficiary Rapid Adds", {"Rapid New Beneficiary Additions"}, allow_extra_rules=True, account_ref="AcctNewBeneficiaryFull", device_id="DevQuiet", beneficiary_id="BeneficiaryOld")
        add("New Beneficiary Payment Review", "Beneficiary Watchlist Match Only", "Beneficiary Watchlist", {"Beneficiary Watchlist Match"}, beneficiary_id="BeneficiaryWatch", amount=500)
        add("New Beneficiary Payment Review", "New Device Beneficiary Payment Only", "Beneficiary New Device", {"New Device Beneficiary Payment"}, beneficiary_id="BeneficiaryNew", device_id="DevNew")
        add("New Beneficiary Payment Review", "Suspicious IP Beneficiary Payment Only", "Beneficiary Suspicious Ip", {"Suspicious IP Beneficiary Payment"}, beneficiary_id="BeneficiaryNew", ip=HIGH_RISK_IP)
        add("New Beneficiary Payment Review", "Post Account Change Beneficiary Payment Only", "Beneficiary Profile", {"Post Account Change Beneficiary Payment"}, account_ref="AcctRecentProfile", device_id="DevRecentProfile", beneficiary_id="BeneficiaryNew")
        add("New Beneficiary Payment Review", "Abnormal First Beneficiary Payment Only", "Beneficiary Abnormal", {"Abnormal First Beneficiary Payment"}, allow_extra_rules=True, account_ref="AcctNewBeneficiaryFull", device_id="DevNewBeneficiaryFull", beneficiary_id="BeneficiaryOld", amount=6_000)
        add("New Beneficiary Payment Review", "Full New Beneficiary Risk", "Beneficiary Full", {r.name for r in self.rule_defs_by_scenario["New Beneficiary Payment Review"]}, account_ref="AcctNewBeneficiaryFull", device_id="DevNewBeneficiaryFull", beneficiary_id="BeneficiaryWatch", amount=6_000, ip=HIGH_RISK_IP)

        add("Dormant Account Reactivation Risk", "Clean Dormant Review Transfer", "Dormant Clean", set())
        add("Dormant Account Reactivation Risk", "Dormant Account Transaction Only", "Dormant Transaction", {"Dormant Account Transaction"}, allow_extra_rules=True, account_ref="AcctDormantFull", device_id="DevOld", amount=1_500)
        add("Dormant Account Reactivation Risk", "High First Transaction After Dormancy Only", "Dormant High First", {"Dormant Account Transaction", "High First Transaction After Dormancy"}, allow_extra_rules=True, account_ref="AcctDormantFull", device_id="DevOld", amount=2_000)
        add("Dormant Account Reactivation Risk", "New Device After Dormancy Only", "Dormant New Device", {"New Device After Dormancy"}, allow_extra_rules=True, account_ref="AcctDormantFull", device_id="DevDormantFull")
        add("Dormant Account Reactivation Risk", "Suspicious IP After Dormancy Only", "Dormant Suspicious Ip", {"Suspicious IP After Dormancy"}, allow_extra_rules=True, account_ref="AcctDormantFull", device_id="DevOld", ip=HIGH_RISK_IP)
        add("Dormant Account Reactivation Risk", "Account Change After Dormancy Only", "Dormant Profile", {"Account Change After Dormancy"}, allow_extra_rules=True, account_ref="AcctDormantFull", device_id="DevOld")
        add("Dormant Account Reactivation Risk", "New Beneficiary After Dormancy Only", "Dormant Beneficiary", {"New Beneficiary After Dormancy"}, allow_extra_rules=True, account_ref="AcctDormantFull", device_id="DevOld", beneficiary_id="BeneficiaryNew")
        add("Dormant Account Reactivation Risk", "Rapid Transfers After Dormancy Only", "Dormant Rapid", {"Rapid Transfers After Dormancy"}, allow_extra_rules=True, account_ref="AcctDormantFull", device_id="DevOld")
        add("Dormant Account Reactivation Risk", "Balance Drain After Dormancy Only", "Dormant Balance Drain", {"Dormant Account Transaction", "High First Transaction After Dormancy", "Balance Drain After Dormancy"}, allow_extra_rules=True, account_ref="AcctDormantFull", device_id="DevOld", amount=9_000)
        add("Dormant Account Reactivation Risk", "Full Dormant Reactivation Risk", "Dormant Full", {r.name for r in self.rule_defs_by_scenario["Dormant Account Reactivation Risk"]}, account_ref="AcctDormantFull", device_id="DevDormantFull", beneficiary_id="BeneficiaryNew", amount=9_000, ip=HIGH_RISK_IP)

        add("Cross-Border or Proxy Access Review", "Clean Cross-Border Review Transfer", "Cross Border Clean", set(), ip="102.176.10.200")
        add("Cross-Border or Proxy Access Review", "IP Country Mismatch Only", "Cross Border Country", {"IP Country Mismatch"}, allow_extra_rules=True, ip_country="NG")
        add("Cross-Border or Proxy Access Review", "High Risk Network Access Only", "Cross Border Network", {"High Risk Network Access"}, ip_network_risk="proxy", ip="102.176.10.200")
        add("Cross-Border or Proxy Access Review", "Impossible Travel Access Only", "Cross Border Travel", {"Impossible Travel Access"}, ip="102.176.10.200", previous_ip_distance_km=800, previous_transaction_minutes_ago=60)
        add("Cross-Border or Proxy Access Review", "New Device Foreign Access Only", "Cross Border New Device", {"IP Country Mismatch", "New Device Foreign Access"}, allow_extra_rules=True, ip_country="NG", device_id="DevNew")
        add("Cross-Border or Proxy Access Review", "High Value Foreign Access Transaction Only", "Cross Border High Value", {"IP Country Mismatch", "High Value Foreign Access Transaction"}, allow_extra_rules=True, ip_country="NG", amount=6_000)
        add("Cross-Border or Proxy Access Review", "Foreign Access Behavior Change Only", "Cross Border Behavior", {"IP Country Mismatch", "Foreign Access Behavior Change"}, allow_extra_rules=True, account_ref="AcctCrossBorderFull", device_id="DevOld", ip_country="NG", amount=6_000, ip="102.176.10.200")
        add("Cross-Border or Proxy Access Review", "Shared Suspicious IP Across Accounts Only", "Cross Border Shared Ip", {"Shared Suspicious IP Across Accounts"}, ip="185.220.101.77")
        add("Cross-Border or Proxy Access Review", "Cross-Border Rapid Transaction Only", "Cross Border Rapid", {"IP Country Mismatch", "Cross-Border Rapid Transaction"}, allow_extra_rules=True, account_ref="AcctCrossBorderFull", device_id="DevOld", ip_country="NG", ip="102.176.10.200")
        add("Cross-Border or Proxy Access Review", "Full Cross-Border Proxy Risk", "Cross Border Full", {r.name for r in self.rule_defs_by_scenario["Cross-Border or Proxy Access Review"]}, account_ref="AcctCrossBorderFull", device_id="DevCrossBorderFull", amount=6_000, ip="185.220.101.77", ip_country="NG", ip_network_risk="proxy", previous_ip_distance_km=800, previous_transaction_minutes_ago=60)

        add("Chango Group Contribution Fraud Monitoring", "Clean Chango Contribution", "Chango Contribution Clean", set(), account_ref="AcctQuiet", device_id="DevQuiet", group_id="GroupQuiet", ip="102.176.10.201")
        add("Chango Group Contribution Fraud Monitoring", "High Weekly Group Contribution Value Only", "Chango Contribution Weekly", {"High Weekly Group Contribution Value"}, allow_extra_rules=True, group_id="GroupChangoFull", ip="102.176.10.201")
        add("Chango Group Contribution Fraud Monitoring", "Rapid Group Contribution Burst Only", "Chango Contribution Burst", {"Rapid Group Contribution Burst"}, allow_extra_rules=True, group_id="GroupChangoFull", ip="102.176.10.201")
        add("Chango Group Contribution Fraud Monitoring", "New Account Contribution Spike Only", "Chango Contribution New Account", {"New Account Contribution Spike"}, allow_extra_rules=True, account_ref="AcctChangoContributionFull", device_id="DevChangoContributionFull", group_id="GroupQuiet", ip="102.176.10.201")
        add("Chango Group Contribution Fraud Monitoring", "Low KYC High Contribution Only", "Chango Contribution Low Kyc", {"Low KYC High Contribution"}, allow_extra_rules=True, account_ref="AcctChangoContributionFull", device_id="DevChangoContributionFull", group_id="GroupQuiet", ip="102.176.10.201", required_kyc_level=4)
        add("Chango Group Contribution Fraud Monitoring", "Shared IP Contributor Cluster Only", "Chango Contribution Shared Ip", {"Shared IP Contributor Cluster"}, ip="102.129.50.44", group_id="GroupQuiet")
        add("Chango Group Contribution Fraud Monitoring", "Watchlisted Campaign Name Match Only", "Chango Contribution Watchlist", {"Watchlisted Campaign Name Match"}, group_id="GroupQuiet", group_name="Fake Medical Emergency", ip="102.176.10.201")
        add("Chango Group Contribution Fraud Monitoring", "Threshold Structuring Pattern Only", "Chango Contribution Structuring", {"Threshold Structuring Pattern"}, allow_extra_rules=True, group_id="GroupChangoFull", ip="102.176.10.201")
        add("Chango Group Contribution Fraud Monitoring", "Abnormal Contribution Amount Only", "Chango Contribution Abnormal", {"Abnormal Contribution Amount"}, allow_extra_rules=True, account_ref="AcctChangoContributionFull", device_id="DevChangoContributionFull", group_id="GroupQuiet", amount=6_000, ip="102.176.10.201")
        add("Chango Group Contribution Fraud Monitoring", "Full Chango Contribution Risk", "Chango Contribution Full", {r.name for r in self.rule_defs_by_scenario["Chango Group Contribution Fraud Monitoring"]}, account_ref="AcctChangoContributionFull", device_id="DevChangoContributionFull", group_id="GroupChangoFull", group_name="Fake Medical Emergency", system_type="contribution", amount=6_000, ip="102.129.50.44", required_kyc_level=4)

        add("Chango Disbursement and Borrowing Risk Review", "Clean Chango Disbursement", "Chango Disbursement Clean", set())
        add("Chango Disbursement and Borrowing Risk Review", "Missing Vote Approval Only", "Chango Disbursement Vote", {"Missing Vote Approval"}, system_type="disbursement", vote_approval_status="pending")
        add("Chango Disbursement and Borrowing Risk Review", "Insufficient Approved Votes Only", "Chango Disbursement Votes Count", {"Insufficient Approved Votes"}, approved_vote_count=1, required_vote_count=3)
        add("Chango Disbursement and Borrowing Risk Review", "Watchlisted Destination Match Only", "Chango Disbursement Destination", {"Watchlisted Destination Match"}, destination_account_ref="BlockedDestination")
        add("Chango Disbursement and Borrowing Risk Review", "Public Group Destination Mismatch Only", "Chango Disbursement Public Mismatch", {"Public Group Destination Mismatch"}, group_type="public", destination_account_ref="OtherDestination", verified_settlement_account_ref="VerifiedDestination")
        add("Chango Disbursement and Borrowing Risk Review", "Fast Cashout After Contribution Spike Only", "Chango Disbursement Spike", {"Fast Cashout After Contribution Spike"}, group_id="GroupDisbursementFull", system_type="disbursement")
        add("Chango Disbursement and Borrowing Risk Review", "High Group Balance Withdrawal Only", "Chango Disbursement Balance", {"High Group Balance Withdrawal"}, group_id="GroupDisbursementFull", group_current_balance=10_000)
        add("Chango Disbursement and Borrowing Risk Review", "Borrowing Above Limit Only", "Chango Disbursement Borrowing", {"Borrowing Above Limit"}, account_ref="AcctChangoDisbursementFull", device_id="DevChangoDisbursementFull", outstanding_loan_amount=2_000)
        add("Chango Disbursement and Borrowing Risk Review", "New Member High Loan Request Only", "Chango Disbursement New Member", {"New Member High Loan Request"}, member_joined_at=iso(BASE_TIME - timedelta(days=2)), loan_amount=1_500)
        add("Chango Disbursement and Borrowing Risk Review", "Full Chango Disbursement Risk", "Chango Disbursement Full", {r.name for r in self.rule_defs_by_scenario["Chango Disbursement and Borrowing Risk Review"]}, account_ref="AcctChangoDisbursementFull", device_id="DevChangoDisbursementFull", group_id="GroupDisbursementFull", system_type="disbursement", amount=1_500, vote_approval_status="pending", approved_vote_count=1, required_vote_count=3, destination_account_ref="BlockedDestination", verified_settlement_account_ref="VerifiedDestination", group_type="public", group_current_balance=10_000, outstanding_loan_amount=2_000, loan_amount=1_500, loan_status="active", member_joined_at=iso(BASE_TIME - timedelta(days=2)))

        add("Regulatory Reporting Review", "Clean Regulatory Review", "Regulatory Clean", set())
        add("Regulatory Reporting Review", "Cash Transaction Threshold Report", "Regulatory Cash", {"Cash Transaction Threshold Report"}, channel="branch_cash", currency="GHS", amount=50_000)
        add("Regulatory Reporting Review", "Suspicious Low-Value Repeated Activity", "Regulatory Str", {"Suspicious Low-Value Repeated Activity"}, account_ref="AcctRegulatoryStr", device_id="DevRegulatoryStr", amount=100)
        add("Regulatory Reporting Review", "Cross-Border Currency Declaration Review", "Regulatory Cdr", {"Cross-Border Currency Declaration Review"}, system_type="cross_border_cash_declaration", country="US", entry_point_type="airport")
        add("Regulatory Reporting Review", "Electronic Transfer Reporting Threshold", "Regulatory Ectr", {"Electronic Transfer Reporting Threshold"}, channel="bank", currency="USD", amount=2_000, direction="outward")
        return cases

    async def evaluate_case(self, case: DemoCase) -> dict[str, Any]:
        started = time.perf_counter()
        response = await self.request(self.decision_engine, "POST", f"/v1/tenants/{self.tenant_id}/scenarios/{self.scenario_ids[case.scenario]}/evaluate", 200, json={"object_id": case.object_id, "object_type": self.object_type, "fields": case.fields})
        latency_ms = (time.perf_counter() - started) * 1000
        result = response.get("result", response)
        triggered_rules = {item["rule_name"] for item in result.get("rule_executions", []) if item.get("outcome") == "hit"}
        decision = result.get("decision") or {}
        passed = case.expected_rules.issubset(triggered_rules) if case.allow_extra_rules else case.expected_rules == triggered_rules
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
            "latency_ms": round(latency_ms, 2),
            "allow_extra_rules": case.allow_extra_rules,
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
    print(f"Latency: {result['latency_ms']:.2f} ms")
    print(f"Result: {'PASS' if result['passed'] else 'FAIL'}")
    if result["missing_rules"]:
        print(f"Missing: {', '.join(result['missing_rules'])}")
    if result["unexpected_rules"]:
        print(f"Unexpected: {', '.join(result['unexpected_rules'])}")


def percentile(values: list[float], pct: float) -> float | None:
    if not values:
        return None
    ordered = sorted(values)
    position = (pct / 100.0) * (len(ordered) - 1)
    lower = int(position)
    upper = min(lower + 1, len(ordered) - 1)
    if lower == upper:
        return round(ordered[lower], 2)
    weight = position - lower
    return round(ordered[lower] + ((ordered[upper] - ordered[lower]) * weight), 2)


def latency_stats(values: list[float]) -> dict[str, float | int | None]:
    if not values:
        return {"count": 0, "min_ms": None, "avg_ms": None, "p50_ms": None, "p95_ms": None, "p99_ms": None, "max_ms": None}
    return {
        "count": len(values),
        "min_ms": round(min(values), 2),
        "avg_ms": round(sum(values) / len(values), 2),
        "p50_ms": percentile(values, 50),
        "p95_ms": percentile(values, 95),
        "p99_ms": percentile(values, 99),
        "max_ms": round(max(values), 2),
    }


def parse_config() -> Config:
    parser = argparse.ArgumentParser(description="Create a fresh tenant with the full fraud scenario catalog and run deterministic smoke demos.")
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
    output = args.output or str(Path("stress-tests/demo-runs") / f"full-scenario-catalog-{utc_now().replace(':', '').replace('.', '-')}.json")
    return Config(args.tenant_id, args.tenant_name, args.timeout, output, args.data_model_url.rstrip("/"), args.ingestion_url.rstrip("/"), args.decision_engine_url.rstrip("/"), args.auth_token, args.ingestion_database_url)


async def async_main() -> int:
    config = parse_config()
    harness = AdditionalScenarioDemoHarness(config)
    try:
        tenant_label = config.tenant_id or "a fresh tenant"
        print(f"bootstrapping full fraud scenario catalog in {tenant_label}...")
        await harness.bootstrap()
        print("\nFull Fraud Scenario Catalog Demo")
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
        coverage_passed = all(
            item["triggered"] and item["not_triggered"]
            for scenario in coverage.values()
            for item in scenario.values()
        )
        cases_passed = all(r["passed"] for r in results)
        scenario_latencies = {
            scenario_name: latency_stats([r["latency_ms"] for r in results if r["scenario"] == scenario_name])
            for scenario_name in harness.rule_defs_by_scenario
        }
        overall_latency = latency_stats([r["latency_ms"] for r in results])
        print("\nRule Coverage")
        for scenario_name, items in coverage.items():
            print(f"\n{scenario_name}")
            for rule_name, item in items.items():
                print(f"{rule_name}: triggered {'YES' if item['triggered'] else 'NO'}, not-triggered {'YES' if item['not_triggered'] else 'NO'}")
        print("\nDecision Latency")
        for scenario_name, stats in scenario_latencies.items():
            print(f"{scenario_name}: avg {stats['avg_ms']} ms, p50 {stats['p50_ms']} ms, p95 {stats['p95_ms']} ms, p99 {stats['p99_ms']} ms, min {stats['min_ms']} ms, max {stats['max_ms']} ms, cases {stats['count']}")
        print(f"Overall latency: avg {overall_latency['avg_ms']} ms, p50 {overall_latency['p50_ms']} ms, p95 {overall_latency['p95_ms']} ms, p99 {overall_latency['p99_ms']} ms, min {overall_latency['min_ms']} ms, max {overall_latency['max_ms']} ms, cases {overall_latency['count']}")
        print(f"\nOverall: {'PASS' if cases_passed and coverage_passed else 'FAIL'}")
        summary = {
            "summary_version": 1,
            "test": {"name": "full_fraud_scenario_catalog", "objective": "Create a fresh tenant with all requested fraud scenarios and run deterministic smoke cases."},
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
            "latency": {
                "overall": overall_latency,
                "by_scenario": scenario_latencies,
            },
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
