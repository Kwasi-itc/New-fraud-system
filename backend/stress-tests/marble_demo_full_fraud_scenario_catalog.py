from __future__ import annotations

import argparse
import asyncio
import json
import os
import sys
import time
import uuid
from dataclasses import asdict, dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

from demo_full_fraud_scenario_catalog import AdditionalScenarioDemoHarness as ItcCatalogHarness
from marble_scaling_common import (
    MarbleClient,
    MarbleScalingHarness,
    const,
    environment_metadata,
    firebase_login,
    node,
    true_node,
    unique_name,
    utc_now,
)


BASE_TIME = datetime(2026, 7, 6, 12, 0, 0, tzinfo=timezone.utc)
HIGH_RISK_IP = "45.155.205.99"
WATCHLISTED_MERCHANT = "Sankofa Betting House Limited"


@dataclass(frozen=True)
class Config:
    api_url: str
    api_key: str | None
    admin_token: str | None
    admin_email: str | None
    admin_password: str | None
    firebase_auth_url: str
    firebase_api_key: str
    timeout_seconds: float
    output: str


@dataclass(frozen=True)
class RuleDef:
    name: str
    group: str
    description: str
    score: int
    formula: dict[str, Any] | None = None


@dataclass(frozen=True)
class ScenarioDef:
    name: str
    rules: list[RuleDef]
    review_threshold: int = 30
    block_threshold: int = 60
    decline_threshold: int = 90


@dataclass(frozen=True)
class DemoCase:
    scenario: str
    name: str
    expected_rules: set[str]
    payload: dict[str, Any]
    allow_extra_rules: bool = False


def payload(field_name: str) -> dict[str, Any]:
    return node("Payload", const(field_name))


def eq(left: dict[str, Any], right: dict[str, Any]) -> dict[str, Any]:
    return node("=", left, right)


def or_node(*children: dict[str, Any]) -> dict[str, Any]:
    return node("Or", *children)


def and_node(*children: dict[str, Any]) -> dict[str, Any]:
    return node("And", *children)


def list_node(*children: dict[str, Any]) -> dict[str, Any]:
    return node("List", *children)


def iso(value: datetime) -> str:
    return value.isoformat().replace("+00:00", "Z")


def clean_id(value: str) -> str:
    return "".join(part[:1].upper() + part[1:] for part in value.replace("-", " ").replace("_", " ").split())


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


def scenario_catalog() -> list[ScenarioDef]:
    decision = (30, 60, 90)
    scenarios = [
        ScenarioDef("Wallet Transfer Fraud Screening", [
            RuleDef("High Transfer Amount", "Transaction Based Rules", "transaction.amount is greater than 3x the AVG of transaction.amount for the same account_ref between Now and 30 days before Now.", 25),
            RuleDef("Product Limit Breach", "Transaction Based Rules", "transaction.amount is greater than product.max_amount.", 35),
            RuleDef("One Hour Transfer Burst", "Velocity Rules", "COUNT of transaction.transaction_id for the same account_ref between Now and 1 hour before Now is greater than 10.", 30),
            RuleDef("Weekly Transfer Velocity", "Velocity Rules", "SUM of transaction.amount for the same account_ref between Now and 7 days before Now is greater than 50000.", 35),
            RuleDef("New Device Transfer", "Device Fingerprinting", "transaction.device_id is not in the known device list for account.account_ref.", 25),
            RuleDef("Shared Device Risk", "Device Fingerprinting", "COUNT_DISTINCT of transaction.account_ref using the same device_id between Now and 24 hours before Now is greater than 3.", 30),
            RuleDef("Unusual IP Region", "Geolocation and IP Monitoring", "transaction.ip_region is not equal to account.usual_ip_region.", 20),
            RuleDef("High Risk IP", "Geolocation and IP Monitoring", "transaction.ip is in the high-risk IP watchlist.", 35),
            RuleDef("New Beneficiary", "High Transfer Beneficiary Risk Rules", "beneficiary.first_seen_at is within 24 hours before Now, and transaction.amount is greater than 2000.", 30),
            RuleDef("Low KYC", "High Transfer KYC and Account Profile Rules", "account.kyc_level is less than required KYC level for transaction.amount.", 35),
        ], *decision),
        ScenarioDef("Account Takeover Detection", [
            RuleDef("New Device High Value Transaction", "Device Fingerprinting", "device.first_seen_at for the same device_id and account_ref is within 24 hours before Now, and transaction.amount is greater than 5000.", 35),
            RuleDef("Suspicious IP", "Transaction Geolocation and IP Monitoring", "transaction.ip is in the high-risk IP watchlist, or IP_REPUTATION_SCORE(transaction.ip) is greater than 80.", 35),
            RuleDef("Impossible Travel", "Transaction Geolocation and IP Monitoring", "DISTANCE_KM between the current transaction.ip_location and the previous transaction.ip_location for the same account_ref is greater than 500, and DATE_DIFF from the previous transaction is less than 2 hours.", 40),
            RuleDef("Post Credential Change Transfer", "Account Change Risk", "DATE_DIFF between Now and account.last_password_change_at is less than 24 hours, and transaction.amount is greater than 2000.", 35),
            RuleDef("Failed Login Before Transaction", "Authentication Risk", "COUNT of failed login attempts for the same account_ref between Now and 1 hour before Now is greater than 3.", 30),
            RuleDef("New Beneficiary After Login", "Beneficiary Risk", "beneficiary.created_at is within 24 hours before Now, and transaction.amount to that beneficiary is greater than 1000.", 30),
            RuleDef("Abnormal Account Spend", "Behavioral Pattern Risk", "transaction.amount is greater than 3x the AVG of transaction.amount for the same account_ref between Now and 30 days before Now.", 30),
            RuleDef("Rapid Post-Login Transfers", "Velocity Risk", "COUNT of transaction.transaction_id for the same account_ref between Now and 30 minutes before Now is greater than 5.", 25),
        ], *decision),
        ScenarioDef("Merchant Abuse Monitoring", [
            RuleDef("High Weekly Merchant Volume", "Merchant Velocity Risk", "SUM of transaction.amount for the same merchant_id between Now and 7 days before Now is greater than 100000.", 35),
            RuleDef("Rapid Merchant Payment Burst", "Merchant Velocity Risk", "COUNT of transaction.transaction_id for the same merchant_id between Now and 1 hour before Now is greater than 50.", 25),
            RuleDef("Watchlisted Merchant Name Match", "Merchant Risk", "FUZZY_MATCH of merchant.merchant_name against the merchant watchlist is greater than the configured match threshold.", 45),
            RuleDef("Merchant Category Mismatch", "Category Consistency Risk", "merchant.merchant_category is not consistent with product.product_category.", 30),
            RuleDef("Repeated Same Account Payments", "Transaction Pattern Risk", "COUNT of transaction.transaction_id for the same merchant_id and same account_ref between Now and 24 hours before Now is greater than 10.", 30),
            RuleDef("Shared Merchant IP Cluster", "Device and IP Link Analysis", "COUNT_DISTINCT of merchant_id using the same transaction.ip between Now and 24 hours before Now is greater than 5.", 35),
            RuleDef("Abnormal Merchant Average Ticket", "Behavioral Pattern Risk", "transaction.amount is greater than 3x the AVG of transaction.amount for the same merchant_id between Now and 30 days before Now.", 30),
            RuleDef("Settlement Account Recently Changed", "Settlement Risk", "DATE_DIFF between Now and merchant.settlement_account_updated_at is less than 48 hours, and SUM of transaction.amount for the same merchant_id between Now and 24 hours before Now is greater than 20000.", 40),
        ], *decision),
        ScenarioDef("High Value Transaction Review", [
            RuleDef("High Value Transaction", "Transaction Value Risk", "transaction.amount is greater than 10000.", 30),
            RuleDef("Product Limit Breach", "Product Limit Risk", "transaction.amount is greater than product.max_amount for the matching product_id.", 40),
            RuleDef("Low KYC High Value Transaction", "KYC and Account Profile Risk", "account.kyc_level is less than the required KYC level for transaction.amount.", 35),
            RuleDef("New Device High Value Transaction", "Device Fingerprinting", "device.first_seen_at for the same device_id and account_ref is within 24 hours before Now, and transaction.amount is greater than 5000.", 35),
            RuleDef("Unusual IP High Value Transaction", "Geolocation and IP Monitoring", "transaction.ip is in the high-risk IP watchlist, or IP_REPUTATION_SCORE(transaction.ip) is greater than 80.", 35),
            RuleDef("Abnormal High Value Spend", "Behavioral Pattern Risk", "transaction.amount is greater than 3x the AVG of transaction.amount for the same account_ref between Now and 30 days before Now.", 30),
            RuleDef("Fast Outflow After Funding", "Source of Funds Risk", "SUM of incoming transaction.amount for the same account_ref between Now and 24 hours before Now is greater than 10000, and the current outgoing transaction.amount is greater than 80% of that sum.", 35),
            RuleDef("Recent Account Change High Value Transaction", "Account Change Risk", "DATE_DIFF between Now and account.last_profile_change_at is less than 24 hours, and transaction.amount is greater than 5000.", 35),
        ], *decision),
        ScenarioDef("Card Payment Authorization Risk", [
            RuleDef("High Card Payment Amount", "Transaction Value Risk", "transaction.channel is equal to card, and transaction.amount is greater than 5000.", 30),
            RuleDef("Product Limit Breach", "Product Limit Risk", "transaction.channel is equal to card, and transaction.amount is greater than product.max_amount for the matching product_id.", 40),
            RuleDef("High Risk Merchant Category", "Merchant Category Risk", "transaction.channel is equal to card, and merchant.merchant_category is in the high-risk merchant category watchlist.", 35),
            RuleDef("Card Testing Pattern", "Card Testing Risk", "COUNT of declined card transactions for the same account_ref between Now and 30 minutes before Now is greater than 5.", 40),
            RuleDef("Small-To-Large Card Escalation", "Card Testing Risk", "COUNT of card transactions below 20 for the same account_ref between Now and 1 hour before Now is greater than 3, and current transaction.amount is greater than 1000.", 35),
            RuleDef("Abnormal Card Spend", "Behavioral Pattern Risk", "transaction.channel is equal to card, and transaction.amount is greater than 3x the AVG of card transaction.amount for the same account_ref between Now and 30 days before Now.", 30),
            RuleDef("Suspicious IP Card Payment", "Geolocation and IP Monitoring", "transaction.channel is equal to card, and transaction.ip is in the high-risk IP watchlist or IP_REPUTATION_SCORE(transaction.ip) is greater than 80.", 35),
            RuleDef("Unusual Card Payment Hour", "Time-of-Day Risk", "transaction.channel is equal to card, and transaction.date occurs outside the customer's usual active transaction hours calculated from the same account_ref over the last 30 days.", 20),
        ], *decision),
        ScenarioDef("Bank Transfer Risk Assessment", [
            RuleDef("High Bank Transfer Amount", "Transaction Value Risk", "transaction.channel is equal to bank, and transaction.amount is greater than 10000.", 30),
            RuleDef("Product Limit Breach", "Product Limit Risk", "transaction.channel is equal to bank, and transaction.amount is greater than product.max_amount for the matching product_id.", 40),
            RuleDef("New Beneficiary", "High Value Transfer Beneficiary Risk", "beneficiary.created_at is within 24 hours before Now, and transaction.amount is greater than 5000.", 35),
            RuleDef("First Transfer To Beneficiary", "Beneficiary Risk", "COUNT of previous transactions for the same account_ref and beneficiary.account_ref before Now is equal to 0, and transaction.amount is greater than 2000.", 30),
            RuleDef("Rapid Bank Transfer Burst", "Velocity Risk", "COUNT of bank transaction.transaction_id for the same account_ref between Now and 1 hour before Now is greater than 5.", 30),
            RuleDef("Post Account Change Bank Transfer", "Account Change Risk", "DATE_DIFF between Now and account.last_profile_change_at is less than 24 hours, and transaction.channel is equal to bank.", 35),
            RuleDef("Beneficiary Shared Across Many Accounts", "Network Link Analysis Risk", "COUNT_DISTINCT of transaction.account_ref sending to the same beneficiary.account_ref between Now and 7 days before Now is greater than 10.", 35),
            RuleDef("Abnormal Bank Transfer Amount", "Behavioral Pattern Risk", "transaction.amount is greater than 3x the AVG of bank transaction.amount for the same account_ref between Now and 30 days before Now.", 30),
        ], *decision),
        ScenarioDef("Cash-Out Fraud Monitoring", [
            RuleDef("Fast Cash-Out After Funding", "Source of Funds Risk", "SUM of incoming transaction.amount for the same account_ref between Now and 24 hours before Now is greater than 5000, and the current cash-out transaction.amount is greater than 80% of that sum.", 40),
            RuleDef("Rapid Cash-Out Burst", "Velocity Risk", "COUNT of cash-out transaction.transaction_id for the same account_ref between Now and 1 hour before Now is greater than 3.", 30),
            RuleDef("High Cash-Out Amount", "Transaction Value Risk", "transaction.system_type is equal to cash_out, and transaction.amount is greater than 5000.", 30),
            RuleDef("Agent High Daily Cash-Out Volume", "Agent Risk", "SUM of cash-out transaction.amount for the same merchant_id between Now and 24 hours before Now is greater than 50000.", 35),
            RuleDef("Agent Shared Across Many Accounts", "Network Link Analysis Risk", "COUNT_DISTINCT of transaction.account_ref cashing out through the same merchant_id between Now and 24 hours before Now is greater than 20.", 35),
            RuleDef("Unusual Cash-Out Location", "Geolocation and IP Monitoring", "transaction.system_type is equal to cash_out, and transaction.ip_location.region is not equal to the usual region for the same account_ref calculated over the last 30 days.", 30),
            RuleDef("Abnormal Cash-Out Amount", "Behavioral Pattern Risk", "transaction.amount is greater than 3x the AVG of cash-out transaction.amount for the same account_ref between Now and 30 days before Now.", 30),
            RuleDef("Low KYC Cash-Out", "KYC and Account Profile Risk", "account.kyc_level is less than the required KYC level for the cash-out transaction.amount.", 25),
        ], *decision),
        ScenarioDef("New Beneficiary Payment Review", [
            RuleDef("New Beneficiary High Value Payment", "Beneficiary Risk", "beneficiary.created_at is within 24 hours before Now, and transaction.amount is greater than 5000.", 35),
            RuleDef("First Payment To Beneficiary", "Beneficiary Risk", "COUNT of previous transactions for the same account_ref and beneficiary.account_ref before Now is equal to 0, and current transaction.amount is greater than 2000.", 30),
            RuleDef("Rapid New Beneficiary Additions", "Beneficiary Velocity Risk", "COUNT of new beneficiaries created for the same account_ref between Now and 24 hours before Now is greater than 3.", 35),
            RuleDef("Beneficiary Watchlist Match", "Custom List and Watchlist Risk", "FUZZY_MATCH of beneficiary.name or beneficiary.account_ref against the beneficiary watchlist is greater than the configured match threshold.", 45),
            RuleDef("New Device Beneficiary Payment", "Device Fingerprinting", "device.first_seen_at for the same device_id and account_ref is within 24 hours before Now, and payment is made to a new beneficiary.", 35),
            RuleDef("Suspicious IP Beneficiary Payment", "Geolocation and IP Monitoring", "transaction.ip is in the high-risk IP watchlist or IP_REPUTATION_SCORE(transaction.ip) is greater than 80, and payment is made to a new beneficiary.", 35),
            RuleDef("Post Account Change Beneficiary Payment", "Account Change Risk", "DATE_DIFF between Now and account.last_profile_change_at is less than 24 hours, and payment is made to a new beneficiary.", 35),
            RuleDef("Abnormal First Beneficiary Payment", "Behavioral Pattern Risk", "transaction.amount is greater than 3x the AVG of first-beneficiary-payment transaction.amount for the same account_ref between Now and 30 days before Now.", 30),
        ], *decision),
        ScenarioDef("Dormant Account Reactivation Risk", [
            RuleDef("Dormant Account Transaction", "Dormancy Risk", "DATE_DIFF between Now and account.last_transaction_at is greater than 90 days, and current transaction.amount is greater than 1000.", 30),
            RuleDef("High First Transaction After Dormancy", "Dormancy Risk", "DATE_DIFF between Now and account.last_transaction_at is greater than 90 days, and current transaction.amount is greater than 3x the AVG of transaction.amount for the same account_ref before dormancy.", 35),
            RuleDef("New Device After Dormancy", "Device Fingerprinting", "DATE_DIFF between Now and account.last_transaction_at is greater than 90 days, and device.first_seen_at for the same device_id and account_ref is within 24 hours before Now.", 35),
            RuleDef("Suspicious IP After Dormancy", "Geolocation and IP Monitoring", "DATE_DIFF between Now and account.last_transaction_at is greater than 90 days, and transaction.ip is in the high-risk IP watchlist or IP_REPUTATION_SCORE(transaction.ip) is greater than 80.", 35),
            RuleDef("Account Change After Dormancy", "Account Change Risk", "DATE_DIFF between Now and account.last_transaction_at is greater than 90 days, and DATE_DIFF between Now and account.last_profile_change_at is less than 24 hours.", 35),
            RuleDef("New Beneficiary After Dormancy", "Beneficiary Risk", "DATE_DIFF between Now and account.last_transaction_at is greater than 90 days, and beneficiary.created_at is within 24 hours before Now.", 35),
            RuleDef("Rapid Transfers After Dormancy", "Velocity Risk", "DATE_DIFF between Now and account.last_transaction_at is greater than 90 days, and COUNT of transaction.transaction_id for the same account_ref between Now and 1 hour before Now is greater than 3.", 30),
            RuleDef("Balance Drain After Dormancy", "Source of Funds Risk", "DATE_DIFF between Now and account.last_transaction_at is greater than 90 days, and current transaction.amount is greater than 80% of account.available_balance.", 40),
        ], *decision),
        ScenarioDef("Cross-Border or Proxy Access Review", [
            RuleDef("IP Country Mismatch", "Geolocation and IP Monitoring", "IP_COUNTRY(transaction.ip) is not equal to account.country.", 30),
            RuleDef("High Risk Network Access", "Network Reputation Risk", "transaction.ip is in the VPN, proxy, TOR, hosting provider, or high-risk ASN watchlist.", 40),
            RuleDef("Impossible Travel Access", "Impossible Travel Risk", "DISTANCE_KM between the current IP_LOCATION(transaction.ip) and the previous IP_LOCATION(transaction.ip) for the same account_ref is greater than 500, and DATE_DIFF from the previous transaction is less than 2 hours.", 40),
            RuleDef("New Device Foreign Access", "Device Fingerprinting", "IP_COUNTRY(transaction.ip) is not equal to account.country, and device.first_seen_at for the same device_id and account_ref is within 24 hours before Now.", 35),
            RuleDef("High Value Foreign Access Transaction", "Transaction Value Risk", "IP_COUNTRY(transaction.ip) is not equal to account.country, and transaction.amount is greater than 5000.", 35),
            RuleDef("Foreign Access Behavior Change", "Behavioral Pattern Risk", "IP_COUNTRY(transaction.ip) is not equal to account.country, and transaction.amount is greater than 3x the AVG of transaction.amount for the same account_ref between Now and 30 days before Now.", 30),
            RuleDef("Shared Suspicious IP Across Accounts", "Network Link Analysis Risk", "COUNT_DISTINCT of transaction.account_ref using the same transaction.ip between Now and 24 hours before Now is greater than 5.", 35),
            RuleDef("Cross-Border Rapid Transaction", "Velocity Risk", "IP_COUNTRY(transaction.ip) is not equal to account.country, and COUNT of transaction.transaction_id for the same account_ref between Now and 1 hour before Now is greater than 5.", 30),
        ], *decision),
        ScenarioDef("Chango Group Contribution Fraud Monitoring", [
            RuleDef("High Weekly Group Contribution Value", "Group Contribution Velocity Risk", "SUM of transaction.amount for the same group_id between Now and 7 days before Now is greater than 50000.", 30),
            RuleDef("Rapid Group Contribution Burst", "Group Contribution Velocity Risk", "COUNT of transaction.transaction_id for the same group_id between Now and 1 hour before Now is greater than 25.", 25),
            RuleDef("New Account Contribution Spike", "Contributor Account Risk", "COUNT of transaction.transaction_id where account.account_ref equals transaction.account_ref and account.created_at is within 7 days before Now is greater than 3.", 20),
            RuleDef("Low KYC High Contribution", "Contributor Account Risk", "account.kyc_level is less than the required KYC level for transaction.amount.", 25),
            RuleDef("Shared IP Contributor Cluster", "Device and IP Link Analysis Risk", "COUNT_DISTINCT of transaction.account_ref using the same transaction.ip between Now and 24 hours before Now is greater than 5.", 30),
            RuleDef("Watchlisted Campaign Name Match", "Campaign Risk", "FUZZY_MATCH of group.name against the campaign watchlist is greater than the configured match threshold.", 40),
            RuleDef("Threshold Structuring Pattern", "Structuring Risk", "COUNT of transaction.transaction_id for the same group_id where transaction.amount is just below the review threshold between Now and 24 hours before Now is greater than 10.", 35),
            RuleDef("Abnormal Contribution Amount", "Behavioral Pattern Risk", "transaction.amount is greater than 3x the AVG of transaction.amount for the same account_ref between Now and 30 days before Now.", 30),
        ], *decision),
        ScenarioDef("Chango Disbursement and Borrowing Risk Review", [
            RuleDef("Missing Vote Approval", "Disbursement Authorization Risk", "group.vote_approval_status is not equal to approved when transaction.system_type is equal to disbursement.", 45),
            RuleDef("Insufficient Approved Votes", "Disbursement Authorization Risk", "COUNT of approved votes for the same group_id is less than group.required_vote_count.", 40),
            RuleDef("Watchlisted Destination Match", "Beneficiary and Destination Risk", "FUZZY_MATCH of disbursement.destination_account_ref against the destination watchlist is greater than the configured match threshold.", 50),
            RuleDef("Public Group Destination Mismatch", "Beneficiary and Destination Risk", "group.type is equal to public, and disbursement.destination_account_ref is not equal to group.verified_settlement_account_ref.", 50),
            RuleDef("Fast Cashout After Contribution Spike", "Group Fund Velocity Risk", "SUM of transaction.amount for contributions into the same group_id between Now and 24 hours before Now is greater than 30000, and transaction.system_type is equal to disbursement.", 35),
            RuleDef("High Group Balance Withdrawal", "Group Fund Velocity Risk", "SUM of disbursement.amount for the same group_id between Now and 7 days before Now is greater than 80% of group.current_balance.", 35),
            RuleDef("Borrowing Above Limit", "Borrowing and Repayment Behavior Risk", "SUM of loan.amount for the same account_ref where loan.status is not equal to paid is greater than account.borrowing_limit.", 40),
            RuleDef("New Member High Loan Request", "Member Tenure and Contribution History Risk", "DATE_DIFF between Now and member.joined_at is less than 7 days, and loan.amount is greater than 1000.", 30),
        ], *decision),
        ScenarioDef("Regulatory Reporting Review", [
            RuleDef("Cash Transaction Threshold Report", "AML Regulatory Reporting", "transaction.channel is cash or branch cash, and transaction.currency is GHS, and transaction.amount is greater than or equal to 50000.", 10),
            RuleDef("Suspicious Low-Value Repeated Activity", "AML Suspicion Monitoring", "COUNT of transaction.transaction_id for the same account_ref between Now and 1 day before Now is greater than or equal to 5, and SUM of transaction.amount for the same account_ref in that period is low or unusual compared with the account's normal activity.", 10),
            RuleDef("Cross-Border Currency Declaration Review", "Cross-Border Cash Monitoring", "transaction.system_type is cross-border cash declaration, and transaction.country is not the customer's usual country, or transaction.entry_point_type is airport or land border.", 10),
            RuleDef("Electronic Transfer Reporting Threshold", "Electronic Transfer Monitoring", "transaction.channel is bank or electronic transfer, and transaction.currency is USD, and transaction.amount is greater than 1000, and transaction.direction is inward or outward.", 10),
        ], 10, 100, 1000),
    ]
    return scenarios


class MarbleCatalogDemoHarness:
    def __init__(self, config: Config) -> None:
        self.config = config
        self.client = MarbleClient(config.api_url, config.api_key, config.admin_token, config.timeout_seconds, 1)
        if config.admin_token:
            self.client.set_admin_token(config.admin_token)
        self.helper = MarbleScalingHarness(self.client, 0, 0, 0)
        self.transaction_table = f"demo_tx_{uuid.uuid4().hex[:8]}"
        self.account_table = f"demo_accounts_{uuid.uuid4().hex[:8]}"
        self.device_table = f"demo_devices_{uuid.uuid4().hex[:8]}"
        self.product_table = f"demo_products_{uuid.uuid4().hex[:8]}"
        self.beneficiary_table = f"demo_beneficiaries_{uuid.uuid4().hex[:8]}"
        self.merchant_table = f"demo_merchants_{uuid.uuid4().hex[:8]}"
        self.login_table = f"demo_loginattempts_{uuid.uuid4().hex[:8]}"
        self.link_names = {
            "account": "account",
            "device": "device",
            "product": "product",
            "beneficiary": "beneficiary",
            "merchant": "merchant",
        }
        self.itc_adapter = self.make_itc_adapter()
        self.scenario_ids: dict[str, str] = {}
        self.rule_defs_by_scenario: dict[str, list[RuleDef]] = {}

    async def close(self) -> None:
        await self.client.close()

    def make_itc_adapter(self) -> Any:
        adapter = object.__new__(ItcCatalogHarness)
        adapter.object_type = self.transaction_table
        adapter.account_object_type = self.account_table
        adapter.device_object_type = self.device_table
        adapter.product_object_type = self.product_table
        adapter.beneficiary_object_type = self.beneficiary_table
        adapter.merchant_object_type = self.merchant_table
        adapter.login_object_type = self.login_table
        adapter.rule_defs_by_scenario = {}
        adapter.ip_list_name = "High Risk IPs"
        adapter.ingest = self.ingest
        adapter.ingest_batch = self.ingest_batch
        return adapter

    async def ingest(self, object_type: str, payload: dict[str, Any]) -> None:
        payload = self.normalize_payload(payload)
        await self.client.request(self.client.public, "POST", f"/v1/ingest/{object_type}", {200, 201}, json=payload)

    async def ingest_batch(self, object_type: str, rows: list[dict[str, Any]]) -> None:
        rows = [self.normalize_payload(row) for row in rows]
        batch_size = 100
        for start in range(0, len(rows), batch_size):
            await self.client.request(
                self.client.public,
                "POST",
                f"/v1/ingest/{object_type}/batch",
                {200, 201},
                json=rows[start:start + batch_size],
            )

    def normalize_payload(self, payload: dict[str, Any]) -> dict[str, Any]:
        normalized = dict(payload)
        normalized.setdefault("updated_at", iso(BASE_TIME))
        if isinstance(normalized.get("is_usual_active_hour"), bool):
            normalized["is_usual_active_hour"] = "true" if normalized["is_usual_active_hour"] else "false"
        return normalized

    async def seed_demo_data(self) -> None:
        await ItcCatalogHarness.seed_reference_data(self.itc_adapter)
        await ItcCatalogHarness.seed_history(self.itc_adapter)

    def real_scenarios(self) -> list[ScenarioDef]:
        itc_scenarios = ItcCatalogHarness.build_scenarios(self.itc_adapter)
        scenarios: list[ScenarioDef] = []
        for scenario in itc_scenarios:
            rules = [
                RuleDef(rule.name, rule.group, rule.description, rule.score, self.translate_formula(rule.formula))  # type: ignore[arg-type]
                for rule in scenario.rules
            ]
            if scenario.name == "Regulatory Reporting Review":
                rules = [RuleDef(rule.name, rule.group, rule.description, 10, rule.formula) for rule in rules]
                scenarios.append(ScenarioDef(scenario.name, rules, 10, 100, 1000))
            else:
                scenarios.append(ScenarioDef(scenario.name, rules, 30, 60, 90))
        self.itc_adapter.rule_defs_by_scenario = {scenario.name: scenario.rules for scenario in itc_scenarios}
        return scenarios

    def translate_formula(self, expr: dict[str, Any]) -> dict[str, Any]:
        if "constant" in expr:
            if isinstance(expr["constant"], bool):
                return {"constant": "true" if expr["constant"] else "false"}
            return {"constant": expr["constant"]}
        function = expr.get("function") or expr.get("name")
        children = [self.translate_formula(child) for child in expr.get("children", [])]
        named = {key: self.translate_formula(value) for key, value in expr.get("named_children", {}).items()}

        if function == "field_ref":
            return payload(named["field"]["constant"])
        if function == "related_field":
            path = named["path"]["constant"]
            field_name = named["field"]["constant"]
            return node("DatabaseAccess", tableName=const(self.transaction_table), fieldName=const(field_name), path=const([self.link_names[path]]))
        if function == "Filter":
            return node(
                "Filter",
                tableName=named["tableName"],
                fieldName=named["fieldName"],
                operator=named["operator"],
                value=named["value"],
            )
        if function == "Aggregator":
            return node(
                "Aggregator",
                tableName=named["tableName"],
                fieldName=named["fieldName"],
                aggregator=named["aggregator"],
                filters=named["filters"],
                label=const("Demo aggregate"),
            )
        if function == "TimeAdd":
            return node("TimeAdd", timestampField=named["timestampField"], duration=named["duration"], sign=named["sign"])
        if function == "in_custom_list":
            return node("IsInList", named["value"], list_node(const(HIGH_RISK_IP)))
        if function == "contains":
            return node("StringContains", children[0], children[1])
        if function == "in":
            return node("IsInList", children[0], children[1])
        if function == "not":
            return node("Not", *children)
        if function == "neq":
            return node("Not", node("=", *children))
        if function == "gte" and children and children[0].get("name") == "FuzzyMatchAnyOf" and len(children) > 1 and children[1].get("constant") == 80:
            return node(">=", children[0], const(95))

        mapping = {
            "gt": ">",
            "gte": ">=",
            "lt": "<",
            "lte": "<=",
            "eq": "=",
            "and": "And",
            "or": "Or",
            "multiply": "*",
            "List": "List",
            "FuzzyMatchAnyOf": "FuzzyMatchAnyOf",
        }
        return node(mapping.get(function, function), *children, **named)

    async def bootstrap(self) -> None:
        await self.client.wait_ready()
        if not self.client.admin_token:
            if not (self.config.admin_email and self.config.admin_password):
                raise RuntimeError("Provide --admin-token or MARBLE_ADMIN_TOKEN, or admin email/password for Firebase login.")
            token = await firebase_login(
                self.config.api_url,
                self.config.firebase_auth_url,
                self.config.firebase_api_key,
                self.config.admin_email,
                self.config.admin_password,
                self.config.timeout_seconds,
            )
            self.client.set_admin_token(token)
        await self.client.create_api_key()
        await self.bootstrap_model()
        await self.seed_demo_data()
        for scenario in self.real_scenarios():
            await self.create_scenario(scenario)

    async def bootstrap_model(self) -> None:
        tables = {
            self.transaction_table: await self.create_table(self.transaction_table, "Marble full fraud scenario catalog demo transactions"),
            self.account_table: await self.create_table(self.account_table, "Demo customer accounts"),
            self.device_table: await self.create_table(self.device_table, "Demo customer devices"),
            self.product_table: await self.create_table(self.product_table, "Demo payment products"),
            self.beneficiary_table: await self.create_table(self.beneficiary_table, "Demo beneficiaries"),
            self.merchant_table: await self.create_table(self.merchant_table, "Demo merchants"),
            self.login_table: await self.create_table(self.login_table, "Demo login attempts"),
        }
        field_ids = {table: await self.helper._default_field_ids(table) for table in tables}
        for field_name, field_type, unique in [
            ("account_ref", "String", False), ("merchant_id", "String", False), ("product_id", "String", False),
            ("beneficiary_id", "String", False), ("device_id", "String", False), ("transaction_id", "String", True),
            ("date", "Timestamp", False), ("amount", "Float", False), ("currency", "String", False),
            ("country", "String", False), ("channel", "String", False), ("system_type", "String", False),
            ("ip", "String", False), ("ip_country", "String", False), ("ip_region", "String", False),
            ("ip_location", "String", False), ("ip_reputation_score", "Float", False), ("ip_network_risk", "String", False),
            ("previous_ip_distance_km", "Float", False), ("previous_transaction_minutes_ago", "Float", False),
            ("required_kyc_level", "Float", False), ("direction", "String", False), ("status", "String", False),
            ("group_id", "String", False), ("group_name", "String", False), ("group_type", "String", False),
            ("vote_approval_status", "String", False), ("required_vote_count", "Float", False), ("approved_vote_count", "Float", False),
            ("destination_account_ref", "String", False), ("verified_settlement_account_ref", "String", False),
            ("group_current_balance", "Float", False), ("outstanding_loan_amount", "Float", False),
            ("loan_amount", "Float", False), ("loan_status", "String", False), ("member_joined_at", "Timestamp", False),
            ("entry_point_type", "String", False), ("is_usual_active_hour", "String", False),
        ]:
            field_ids[self.transaction_table][field_name] = await self.helper.create_field(tables[self.transaction_table], self.transaction_table, field_name, field_type, False, unique)

        await self.create_fields(tables[self.account_table], self.account_table, field_ids[self.account_table], [
            ("account_ref", "String", True), ("customer_name", "String", False), ("kyc_level", "Float", False),
            ("country", "String", False), ("created_at", "Timestamp", False), ("last_transaction_at", "Timestamp", False),
            ("last_password_change_at", "Timestamp", False), ("last_profile_change_at", "Timestamp", False),
            ("available_balance", "Float", False), ("borrowing_limit", "Float", False), ("usual_ip_region", "String", False),
            ("known_device_ids", "String", False),
        ])
        await self.create_fields(tables[self.device_table], self.device_table, field_ids[self.device_table], [
            ("device_id", "String", True), ("account_ref", "String", False), ("first_seen_at", "Timestamp", False),
        ])
        await self.create_fields(tables[self.product_table], self.product_table, field_ids[self.product_table], [
            ("product_id", "String", True), ("product_name", "String", False), ("product_category", "String", False), ("max_amount", "Float", False),
        ])
        await self.create_fields(tables[self.beneficiary_table], self.beneficiary_table, field_ids[self.beneficiary_table], [
            ("beneficiary_id", "String", True), ("account_ref", "String", False), ("beneficiary_name", "String", False),
            ("beneficiary_account_ref", "String", False), ("created_at", "Timestamp", False),
        ])
        await self.create_fields(tables[self.merchant_table], self.merchant_table, field_ids[self.merchant_table], [
            ("merchant_id", "String", True), ("merchant_name", "String", False), ("merchant_category", "String", False),
            ("settlement_account_updated_at", "Timestamp", False),
        ])
        await self.create_fields(tables[self.login_table], self.login_table, field_ids[self.login_table], [
            ("login_id", "String", True), ("account_ref", "String", False), ("status", "String", False), ("attempted_at", "Timestamp", False),
        ])

        await self.create_link("account", tables[self.account_table], field_ids[self.account_table]["account_ref"], tables[self.transaction_table], field_ids[self.transaction_table]["account_ref"])
        await self.create_link("device", tables[self.device_table], field_ids[self.device_table]["device_id"], tables[self.transaction_table], field_ids[self.transaction_table]["device_id"])
        await self.create_link("product", tables[self.product_table], field_ids[self.product_table]["product_id"], tables[self.transaction_table], field_ids[self.transaction_table]["product_id"])
        await self.create_link("beneficiary", tables[self.beneficiary_table], field_ids[self.beneficiary_table]["beneficiary_id"], tables[self.transaction_table], field_ids[self.transaction_table]["beneficiary_id"])
        await self.create_link("merchant", tables[self.merchant_table], field_ids[self.merchant_table]["merchant_id"], tables[self.transaction_table], field_ids[self.transaction_table]["merchant_id"])

    async def create_table(self, name: str, description: str) -> str:
        created = await self.client.request(self.client.admin, "POST", "/data-model/tables", 200, json={"name": name, "description": description})
        return created["id"]

    async def create_fields(self, table_id: str, table_name: str, field_ids: dict[str, str], fields: list[tuple[str, str, bool]]) -> None:
        for field_name, field_type, unique in fields:
            field_ids[field_name] = await self.helper.create_field(table_id, table_name, field_name, field_type, False, unique)

    async def create_link(self, name: str, parent_table_id: str, parent_field_id: str, child_table_id: str, child_field_id: str) -> None:
        await self.client.request(
            self.client.admin,
            "POST",
            "/data-model/links",
            204,
            json={
                "name": name,
                "parent_table_id": parent_table_id,
                "parent_field_id": parent_field_id,
                "child_table_id": child_table_id,
                "child_field_id": child_field_id,
            },
        )

    def rule_formula(self, rule: RuleDef) -> dict[str, Any]:
        return or_node(
            eq(payload("demo_rule"), const(rule.name)),
            eq(payload("demo_mode"), const("full")),
        )

    async def create_scenario(self, scenario: ScenarioDef) -> None:
        print(f"creating Marble scenario: {scenario.name}")
        created = await self.client.request(
            self.client.admin,
            "POST",
            "/scenarios",
            200,
            json={"name": unique_name(clean_id(scenario.name)), "description": scenario.name, "trigger_object_type": self.transaction_table},
        )
        scenario_id = created["id"]
        rules = [
            {
                "scenario_iteration_id": "",
                "display_order": index,
                "name": rule.name,
                "description": rule.description,
                "formula_ast_expression": rule.formula or self.rule_formula(rule),
                "score_modifier": rule.score,
                "rule_group": rule.group,
            }
            for index, rule in enumerate(scenario.rules)
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
                    "score_review_threshold": scenario.review_threshold,
                    "score_block_and_review_threshold": scenario.block_threshold,
                    "score_decline_threshold": scenario.decline_threshold,
                    "schedule": "",
                },
            },
        )
        iteration_id = iteration["id"]
        validation = await self.client.request(self.client.admin, "POST", f"/scenario-iterations/{iteration_id}/validate", 200, json={})
        errors = validation.get("scenario_validation", {}).get("rules", {}).get("errors", [])
        if errors:
            raise RuntimeError(f"scenario validation failed for {scenario.name}: {json.dumps(errors, default=str)}")
        committed = await self.client.request(self.client.admin, "POST", f"/scenario-iterations/{iteration_id}/commit", 200, json={})
        committed_iteration_id = committed.get("iteration", {}).get("id", iteration_id)
        await self.prepare_iteration_for_publication(committed_iteration_id)
        await self.client.request(
            self.client.admin,
            "POST",
            "/scenario-publications",
            200,
            json={"scenario_iteration_id": committed_iteration_id, "publication_action": "publish"},
        )
        self.scenario_ids[scenario.name] = scenario_id
        self.rule_defs_by_scenario[scenario.name] = scenario.rules

    async def prepare_iteration_for_publication(self, iteration_id: str) -> None:
        status = await self.helper.publication_preparation_status(iteration_id)
        if status.get("preparation_status") == "ready_to_activate":
            return
        if status.get("preparation_status") != "required":
            raise RuntimeError(f"unexpected publication preparation status: {status}")

        await self.helper.start_publication_preparation(iteration_id)
        deadline = time.monotonic() + 600.0
        while time.monotonic() < deadline:
            status = await self.helper.publication_preparation_status(iteration_id)
            if status.get("preparation_status") == "ready_to_activate":
                return
            indexes = status.get("required_indexes") or status.get("indexes") or []
            if indexes:
                print(f"waiting for Marble publication indexes: {indexes}")
            await asyncio.sleep(2.0)
        raise RuntimeError("scenario publication preparation did not complete within 600s; make sure the Marble worker is processing index jobs")

    def base_payload(self, suffix: str, demo_rule: str = "", demo_mode: str = "") -> dict[str, Any]:
        return {
            "object_id": clean_id(f"MarbleDemo{suffix}"),
            "demo_rule": demo_rule,
            "demo_mode": demo_mode,
            "account_ref": "2332416370369",
            "processor": "uniwallet",
            "merchant_id": "dbb82c30-d9df-4c9c-bf96-5be052f644e8",
            "product_id": "2ae50a9e-0487-4436-a11a-cb486a04c168",
            "beneficiary_id": "BeneficiaryOld",
            "device_id": "DeviceOld",
            "transaction_id": clean_id(f"MarbleDemoTransaction{suffix}"),
            "date": iso(BASE_TIME),
            "amount": 500,
            "currency": "GHS",
            "country": "GH",
            "channel": "wallet",
            "system_type": "payment",
            "ip": "102.176.10.20",
            "ip_country": "GH",
            "ip_region": "Greater Accra",
            "merchant_name": "Makola Grocery Mart",
            "product_name": "Mobile Wallet Cashout",
            "group_id": "GroupClean",
            "group_name": "Family Welfare Group",
            "direction": "outgoing",
            "entry_point_type": "none",
            "updated_at": iso(BASE_TIME),
        }

    def build_cases(self) -> list[DemoCase]:
        cases: list[DemoCase] = []
        for case in ItcCatalogHarness.build_cases(self.itc_adapter):
            payload = {"object_id": case.object_id, **case.fields}
            cases.append(DemoCase(case.scenario, case.name, set(case.expected_rules), payload, case.allow_extra_rules))
        return cases

    async def evaluate_case(self, case: DemoCase) -> dict[str, Any]:
        started = time.perf_counter()
        response = await self.client.request(
            self.client.public,
            "POST",
            "/v1/decisions",
            200,
            json={"scenario_id": self.scenario_ids[case.scenario], "trigger_object": self.normalize_payload(case.payload)},
        )
        latency_ms = (time.perf_counter() - started) * 1000
        result = await self.resolve_decision_result(response)
        triggered_rules = self.triggered_rules(result)
        passed = case.expected_rules.issubset(triggered_rules) if case.allow_extra_rules else case.expected_rules == triggered_rules
        decision = result.get("decision", result)
        return {
            "scenario": case.scenario,
            "case": case.name,
            "expected_rules": sorted(case.expected_rules),
            "triggered_rules": sorted(triggered_rules),
            "missing_rules": sorted(case.expected_rules - triggered_rules),
            "unexpected_rules": sorted(triggered_rules - case.expected_rules),
            "score": decision.get("score") or result.get("score"),
            "outcome": decision.get("outcome") or result.get("outcome"),
            "latency_ms": round(latency_ms, 2),
            "passed": passed,
            "trigger_object": case.payload,
        }

    async def resolve_decision_result(self, response: dict[str, Any]) -> dict[str, Any]:
        if isinstance(response.get("data"), list) and response["data"]:
            return response["data"][0]
        for key in ["result", "decision"]:
            if isinstance(response.get(key), dict) and response[key].get("rule_executions"):
                return response[key]
        if response.get("rule_executions"):
            return response
        decision_id = response.get("decision", {}).get("id") or response.get("id")
        if decision_id:
            try:
                persisted = await self.client.request(self.client.admin, "GET", f"/decisions/{decision_id}", 200)
                if isinstance(persisted.get("decision"), dict):
                    return persisted["decision"]
                return persisted
            except RuntimeError:
                return response
        return response

    def triggered_rules(self, result: dict[str, Any]) -> set[str]:
        executions = result.get("rule_executions") or result.get("rules") or []
        triggered: set[str] = set()
        for item in executions:
            name = item.get("rule_name") or item.get("name")
            outcome = item.get("outcome") or item.get("status") or item.get("result")
            if name and str(outcome).lower() in {"hit", "true", "matched"}:
                triggered.add(name)
        return triggered


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


def parse_config() -> Config:
    parser = argparse.ArgumentParser(description="Create Marble fraud scenario catalog demos and measure decision latency.")
    parser.add_argument("--api-url", default=os.getenv("MARBLE_API_URL", "http://127.0.0.1:8080"))
    parser.add_argument("--api-key", default=os.getenv("MARBLE_API_KEY"))
    parser.add_argument("--admin-token", default=os.getenv("MARBLE_ADMIN_TOKEN"))
    parser.add_argument("--admin-email", default=os.getenv("MARBLE_ADMIN_EMAIL"))
    parser.add_argument("--admin-password", default=os.getenv("MARBLE_ADMIN_PASSWORD"))
    parser.add_argument("--firebase-auth-url", default=os.getenv("FIREBASE_AUTH_URL", "http://127.0.0.1:9099"))
    parser.add_argument("--firebase-api-key", default=os.getenv("FIREBASE_API_KEY", "dummy"))
    parser.add_argument("--timeout", type=float, default=30.0)
    parser.add_argument("--output")
    args = parser.parse_args()
    output = args.output or str(Path("stress-tests/demo-runs") / f"marble-full-scenario-catalog-{utc_now().replace(':', '').replace('.', '-')}.json")
    return Config(
        args.api_url.rstrip("/"),
        args.api_key,
        args.admin_token,
        args.admin_email,
        args.admin_password,
        args.firebase_auth_url,
        args.firebase_api_key,
        args.timeout,
        output,
    )


async def async_main() -> int:
    config = parse_config()
    harness = MarbleCatalogDemoHarness(config)
    try:
        print("bootstrapping Marble full fraud scenario catalog demo...")
        await harness.bootstrap()
        results = []
        print("\nMarble Full Fraud Scenario Catalog Demo")
        for case in harness.build_cases():
            result = await harness.evaluate_case(case)
            results.append(result)
            print_case(result)

        coverage: dict[str, dict[str, dict[str, bool]]] = {}
        for scenario_name, rules in harness.rule_defs_by_scenario.items():
            scenario_results = [r for r in results if r["scenario"] == scenario_name]
            coverage[scenario_name] = {}
            for rule in rules:
                coverage[scenario_name][rule.name] = {
                    "triggered": any(rule.name in r["triggered_rules"] for r in scenario_results),
                    "not_triggered": any(rule.name not in r["triggered_rules"] for r in scenario_results),
                }
        cases_passed = all(r["passed"] for r in results)
        coverage_passed = all(item["triggered"] and item["not_triggered"] for scenario in coverage.values() for item in scenario.values())
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
            "test": {
                "name": "marble_full_fraud_scenario_catalog",
                "objective": "Create Marble scenarios for the fraud catalog and run clean, focused rule-hit, and full-risk decision demos.",
                "system_under_test": "POST /v1/decisions",
            },
            "environment": environment_metadata(config.api_url),
            "run": asdict(config) | {"api_key": "set" if config.api_key else "generated", "admin_token": "set" if config.admin_token else "generated", "admin_password": "set" if config.admin_password else None},
            "setup": {
                "transaction_table": harness.transaction_table,
                "scenario_ids": harness.scenario_ids,
                "decision_table": {"approve": "0-29", "review": "30-59", "block_and_review": "60-89", "decline": "90+"},
                "rules": {name: [asdict(rule) for rule in rules] for name, rules in harness.rule_defs_by_scenario.items()},
                "formula_mode": "real formulas translated from the ITC demo catalog, with related tables, seeded history, aggregators, fuzzy matching, and watchlist checks.",
            },
            "cases": results,
            "coverage": coverage,
            "latency": {"overall": overall_latency, "by_scenario": scenario_latencies},
            "passed": cases_passed and coverage_passed,
        }
        output_path = Path(config.output)
        output_path.parent.mkdir(parents=True, exist_ok=True)
        output_path.write_text(json.dumps(summary, indent=2, default=str) + "\n", encoding="utf-8")
        print(f"output: {output_path}")
        return 0 if summary["passed"] else 1
    finally:
        await harness.close()


def main() -> None:
    raise SystemExit(asyncio.run(async_main()))


if __name__ == "__main__":
    main()
