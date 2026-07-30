from __future__ import annotations

import re
from dataclasses import dataclass
from typing import Any

from .manifest import ReplayManifest


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
    trigger_formula: dict[str, Any]
    rules: tuple[RuleDef, ...]
    regulatory: bool = False


def field(name: str) -> dict[str, Any]:
    return {"function": "field_ref", "named_children": {"field": {"constant": name}}}


def const(value: Any) -> dict[str, Any]:
    return {"constant": value}


def fn(name: str, *children: dict[str, Any], **named: dict[str, Any]) -> dict[str, Any]:
    node: dict[str, Any] = {"function": name}
    if children:
        node["children"] = list(children)
    if named:
        node["named_children"] = named
    return node


def list_node(*children: dict[str, Any]) -> dict[str, Any]:
    return fn("List", *children)


def eq(left: dict[str, Any], right: dict[str, Any]) -> dict[str, Any]:
    return fn("eq", left, right)


def neq(left: dict[str, Any], right: dict[str, Any]) -> dict[str, Any]:
    return fn("neq", left, right)


def gt(left: dict[str, Any], right: dict[str, Any]) -> dict[str, Any]:
    return fn("gt", left, right)


def gte(left: dict[str, Any], right: dict[str, Any]) -> dict[str, Any]:
    return fn("gte", left, right)


def lte(left: dict[str, Any], right: dict[str, Any]) -> dict[str, Any]:
    return fn("lte", left, right)


def always_true() -> dict[str, Any]:
    return eq(const(1), const(1))


def filter_node(field_name: str, operator: str, value: dict[str, Any]) -> dict[str, Any]:
    return fn(
        "Filter",
        tableName=const("transactions"),
        fieldName=const(field_name),
        operator=const(operator),
        value=value,
    )


def time_add(duration: str) -> dict[str, Any]:
    return fn("TimeAdd", timestampField=field("date"), duration=const(duration), sign=const("-"))


def aggregate(field_name: str, operation: str, *filters: dict[str, Any]) -> dict[str, Any]:
    return fn(
        "Aggregator",
        tableName=const("transactions"),
        fieldName=const(field_name),
        aggregator=const(operation),
        filters=list_node(*filters),
    )


def stable_rule_id(value: str) -> str:
    parts = re.findall(r"[A-Za-z0-9]+", value)
    return "".join(part[:1].upper() + part[1:] for part in parts)


def build_portable_scenarios(manifest: ReplayManifest) -> tuple[ScenarioDef, ...]:
    account = filter_node("account_ref", "=", field("account_ref"))
    merchant = filter_node("merchant_id", "=", field("merchant_id"))
    product = filter_node("product_id", "=", field("product_id"))
    source = filter_node("source_id", "=", field("source_id"))
    thirdparty = filter_node("thirdparty_id", "=", field("thirdparty_id"))
    terminal = filter_node("terminal_id", "=", field("terminal_id"))
    payment_number = filter_node("payment_msisdn", "=", field("payment_msisdn"))
    one_hour = filter_node("date", ">=", time_add("PT1H"))
    one_day = filter_node("date", ">=", time_add("P1D"))
    one_week = filter_node("date", ">=", time_add("P7D"))
    thirty_days = filter_node("date", ">=", time_add("P30D"))
    incoming = filter_node("direction", "=", const("incoming"))
    wallet = filter_node("channel", "=", const("wallet"))
    card = filter_node("channel", "=", const("card"))
    bank = filter_node("channel", "=", const("bank"))
    cash_out = filter_node("system_type", "=", const("cash_out"))
    incoming_24h = aggregate("amount", "SUM", account, one_day, incoming)

    channels = {stream.channel for stream in manifest.transaction_streams}
    system_types = {stream.system_type for stream in manifest.transaction_streams}
    scenarios: list[ScenarioDef] = []
    merchant_rules = [
        RuleDef("High Weekly Merchant Volume", "Merchant Velocity Risk", "Merchant transaction value exceeds 100000 in seven days.", 35, gt(aggregate("amount", "SUM", merchant, one_week), const(100_000))),
        RuleDef("Rapid Merchant Payment Burst", "Merchant Velocity Risk", "More than fifty merchant payments occur in one hour.", 25, gt(aggregate("transaction_id", "COUNT", merchant, one_hour), const(50))),
        RuleDef("Repeated Same Account Payments", "Transaction Pattern Risk", "The same account pays a merchant more than ten times in one day.", 30, gt(aggregate("transaction_id", "COUNT", merchant, account, one_day), const(10))),
        RuleDef("Abnormal Merchant Average Ticket", "Behavioral Pattern Risk", "Amount exceeds three times the merchant's 30-day average.", 30, gt(field("amount"), fn("multiply", aggregate("amount", "AVG", merchant, thirty_days), const(3)))),
        RuleDef("High Account Merchant Exposure", "Merchant Exposure Risk", "One account sends more than 25000 to the same merchant in one day.", 35, gt(aggregate("amount", "SUM", account, merchant, one_day), const(25_000))),
        RuleDef("Merchant Product Abuse Burst", "Product Abuse Risk", "The same account and product combination appears more than six times in one hour.", 30, fn("and", fn("is_not_empty", field("product_id")), gt(aggregate("transaction_id", "COUNT", account, product, one_hour), const(6)))),
    ]

    if "wallet" in channels:
        scenarios.append(
            ScenarioDef(
                "Wallet Transfer Fraud Screening",
                eq(field("channel"), const("wallet")),
                (
                    RuleDef("High Transfer Amount", "Transaction Based Rules", "Amount exceeds three times the account's 30-day average.", 25, gt(field("amount"), fn("multiply", aggregate("amount", "AVG", account, thirty_days), const(3)))),
                    RuleDef("One Hour Transfer Burst", "Velocity Rules", "More than ten account transactions occur in one hour.", 30, gt(aggregate("transaction_id", "COUNT", account, one_hour), const(10))),
                    RuleDef("Weekly Transfer Velocity", "Velocity Rules", "Account transaction value exceeds 50000 in seven days.", 35, gt(aggregate("amount", "SUM", account, one_week), const(50_000))),
                    RuleDef("Repeated Wallet Number Activity", "Wallet Velocity Risk", "The same wallet or payment number appears more than eight times in one hour.", 30, fn("and", fn("is_not_empty", field("payment_msisdn")), gt(aggregate("transaction_id", "COUNT", payment_number, wallet, one_hour), const(8)))),
                ),
            )
        )

    scenarios.extend(
        (
            ScenarioDef(
                "Merchant Abuse Monitoring",
                always_true(),
                tuple(merchant_rules),
            ),
            ScenarioDef(
                "High Value Transaction Review",
                always_true(),
                (
                    RuleDef("High Value Transaction", "Transaction Value Risk", "Transaction amount exceeds 10000.", 30, gt(field("amount"), const(10_000))),
                    RuleDef("High Fee Ratio", "Pricing and Fee Risk", "Transaction fees exceed ten percent of the transaction amount.", 20, fn("and", gt(field("amount"), const(0)), gt(field("fees"), fn("multiply", field("amount"), const(0.1))))),
                    RuleDef("Round Amount Structuring", "Structuring Risk", "Account has repeated near-threshold round-value transactions in one day.", 30, fn("and", gte(field("amount"), const(9_000)), lte(field("amount"), const(10_000)), gte(aggregate("transaction_id", "COUNT", account, filter_node("amount", ">=", const(9_000)), filter_node("amount", "<=", const(10_000)), one_day), const(3)))),
                    RuleDef(
                        "Fast Outflow After Funding",
                        "Source of Funds Risk",
                        "An outgoing transaction exceeds 80 percent of more than 10000 received by the account in one day.",
                        35,
                        fn(
                            "and",
                            eq(field("direction"), const("outgoing")),
                            gt(incoming_24h, const(10_000)),
                            gt(field("amount"), fn("multiply", incoming_24h, const(0.8))),
                        ),
                    ),
                ),
            ),
            ScenarioDef(
                "Staff Transaction Monitoring",
                always_true(),
                (
                    RuleDef(
                        "Staff-Linked Transaction",
                        "Internal Staff Monitoring",
                        "A transaction account or payment number matches the internal staff list.",
                        40,
                        fn(
                            "or",
                            fn("in_custom_list", list=const("fraud_staff_msisdns"), value=field("payment_msisdn")),
                            fn("in_custom_list", list=const("fraud_staff_msisdns"), value=field("source_account_no")),
                            fn("in_custom_list", list=const("fraud_staff_msisdns"), value=field("account_ref")),
                            fn("in_custom_list", list=const("fraud_staff_numbers"), value=field("account_ref")),
                        ),
                    ),
                ),
            ),
            ScenarioDef(
                "Source And Processor Abuse Monitoring",
                always_true(),
                (
                    RuleDef("Third-Party Identifier Burst", "Third-Party Risk", "The same third-party identifier appears on more than twenty transactions in one hour.", 25, fn("and", fn("is_not_empty", field("thirdparty_id")), gt(aggregate("transaction_id", "COUNT", thirdparty, one_hour), const(20)))),
                    RuleDef("Source Identifier Daily Volume Spike", "Source System Risk", "A source identifier produces more than 100000 in transaction value in one day.", 30, fn("and", fn("is_not_empty", field("source_id")), gt(aggregate("amount", "SUM", source, one_day), const(100_000)))),
                    RuleDef("Terminal High Value Burst", "Terminal Risk", "A terminal processes more than five high-value transactions in one hour.", 30, fn("and", fn("is_not_empty", field("terminal_id")), gt(aggregate("transaction_id", "COUNT", terminal, filter_node("amount", ">=", const(5_000)), one_hour), const(5)))),
                    RuleDef("Raw Account Reference Mismatch", "Data Consistency Risk", "The normalized account reference differs from the raw account reference on a high-value transaction.", 20, fn("and", gt(field("amount"), const(5_000)), fn("is_not_empty", field("raw_account_ref")), neq(field("account_ref"), field("raw_account_ref")))),
                ),
            ),
        )
    )

    if "card" in channels:
        scenarios.append(
            ScenarioDef(
                "Card Payment Authorization Risk",
                eq(field("channel"), const("card")),
                (
                    RuleDef("High Card Payment Amount", "Transaction Value Risk", "Card payment amount exceeds 5000.", 30, gt(field("amount"), const(5_000))),
                    RuleDef("Small-To-Large Card Escalation", "Card Testing Risk", "More than three small card payments precede a payment over 1000 within one hour.", 35, fn("and", gt(aggregate("transaction_id", "COUNT", account, card, filter_node("amount", "<", const(20)), one_hour), const(3)), gt(field("amount"), const(1_000)))),
                    RuleDef("Abnormal Card Spend", "Behavioral Pattern Risk", "Card amount exceeds three times the account's 30-day card average.", 30, gt(field("amount"), fn("multiply", aggregate("amount", "AVG", account, card, thirty_days), const(3)))),
                ),
            )
        )

    if "bank" in channels:
        scenarios.append(
            ScenarioDef(
                "Bank Transfer Risk Assessment",
                eq(field("channel"), const("bank")),
                (
                    RuleDef("High Bank Transfer Amount", "Transaction Value Risk", "Bank transfer amount exceeds 10000.", 30, gt(field("amount"), const(10_000))),
                    RuleDef("Rapid Bank Transfer Burst", "Velocity Risk", "More than five bank transfers occur for an account in one hour.", 30, gt(aggregate("transaction_id", "COUNT", account, bank, one_hour), const(5))),
                    RuleDef("Abnormal Bank Transfer Amount", "Behavioral Pattern Risk", "Bank amount exceeds three times the account's 30-day bank average.", 30, gt(field("amount"), fn("multiply", aggregate("amount", "AVG", account, bank, thirty_days), const(3)))),
                ),
            )
        )

    if "cash_out" in system_types:
        scenarios.append(
            ScenarioDef(
                "Cash-Out Fraud Monitoring",
                eq(field("system_type"), const("cash_out")),
                (
                    RuleDef("Fast Cash-Out After Funding", "Source of Funds Risk", "Cash-out exceeds 80 percent of more than 5000 received in one day.", 40, fn("and", gt(incoming_24h, const(5_000)), gt(field("amount"), fn("multiply", incoming_24h, const(0.8))))),
                    RuleDef("Rapid Cash-Out Burst", "Velocity Risk", "More than three cash-outs occur for an account in one hour.", 30, gt(aggregate("transaction_id", "COUNT", account, cash_out, one_hour), const(3))),
                    RuleDef("High Cash-Out Amount", "Transaction Value Risk", "Cash-out amount exceeds 5000.", 30, gt(field("amount"), const(5_000))),
                    RuleDef("Agent High Daily Cash-Out Volume", "Agent Risk", "Agent cash-out volume exceeds 50000 in one day.", 35, gt(aggregate("amount", "SUM", merchant, cash_out, one_day), const(50_000))),
                    RuleDef("Agent Shared Across Many Accounts", "Network Link Analysis Risk", "More than twenty accounts cash out through one agent in one day.", 35, gt(aggregate("account_ref", "COUNT_DISTINCT", merchant, cash_out, one_day), const(20))),
                    RuleDef("Abnormal Cash-Out Amount", "Behavioral Pattern Risk", "Cash-out exceeds three times the account's 30-day cash-out average.", 30, gt(field("amount"), fn("multiply", aggregate("amount", "AVG", account, cash_out, thirty_days), const(3)))),
                ),
            )
        )

    regulatory_rules = [
        RuleDef("Suspicious Low-Value Repeated Activity", "AML Suspicion Monitoring", "At least five account transactions totaling no more than 1000 occur in one day.", 1, fn("and", gte(aggregate("transaction_id", "COUNT", account, one_day), const(5)), lte(aggregate("amount", "SUM", account, one_day), const(1_000))))
    ]
    if channels & {"cash", "branch_cash"}:
        regulatory_rules.append(RuleDef("Cash Transaction Threshold Report", "AML Regulatory Reporting", "GHS cash transaction amount is at least 50000.", 1, fn("and", fn("in", field("channel"), list_node(const("cash"), const("branch_cash"))), eq(field("currency"), const("GHS")), gte(field("amount"), const(50_000)))))
    if channels & {"bank", "electronic_transfer"}:
        regulatory_rules.append(RuleDef("Electronic Transfer Reporting Threshold", "Electronic Transfer Monitoring", "USD electronic transfer amount exceeds 1000.", 1, fn("and", fn("in", field("channel"), list_node(const("bank"), const("electronic_transfer"))), eq(field("currency"), const("USD")), gt(field("amount"), const(1_000)), fn("in", field("direction"), list_node(const("incoming"), const("outgoing"), const("inward"), const("outward"))))))
    scenarios.append(ScenarioDef("Regulatory Reporting Review", always_true(), tuple(regulatory_rules), regulatory=True))
    return tuple(scenarios)
