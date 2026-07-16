from __future__ import annotations

import argparse
import asyncio
import json
import os
import sys
from dataclasses import asdict
from datetime import timedelta
from pathlib import Path
from typing import Any

from decision_throughput_limit import utc_now
from demo_full_fraud_scenario_catalog import (
    BASE_TIME,
    DEFAULT_TENANT_NAME,
    AdditionalScenarioDemoHarness,
    Config,
    DemoCase,
    ScenarioDef,
    clean_id,
    iso,
    latency_stats,
    print_case,
)


CHANGO_SCENARIOS = {
    "Chango Group Contribution Fraud Monitoring",
    "Chango Disbursement and Borrowing Risk Review",
}


class ChangoScenarioDemoHarness(AdditionalScenarioDemoHarness):
    def build_scenarios(self) -> list[ScenarioDef]:
        return [scenario for scenario in super().build_scenarios() if scenario.name in CHANGO_SCENARIOS]

    def build_cases(self) -> list[DemoCase]:
        cases: list[DemoCase] = []

        def add(
            scenario: str,
            name: str,
            suffix: str,
            expected: set[str],
            allow_extra_rules: bool = False,
            **overrides: Any,
        ) -> None:
            cases.append(
                DemoCase(
                    scenario,
                    name,
                    clean_id(f"Case {suffix}"),
                    self.tx(f"Case {suffix}", **overrides),
                    expected,
                    allow_extra_rules,
                )
            )

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

        return cases


def parse_config() -> Config:
    parser = argparse.ArgumentParser(description="Create a fresh tenant with only Chango fraud scenarios and run deterministic smoke demos.")
    parser.add_argument("--tenant-id", help="Optional existing tenant ID. Omit to create a fresh tenant.")
    parser.add_argument("--tenant-name", default=f"{DEFAULT_TENANT_NAME} - Chango")
    parser.add_argument("--timeout", type=float, default=30.0)
    parser.add_argument("--output", help="Output JSON path. Defaults to timestamped stress-tests/demo-runs file.")
    parser.add_argument("--data-model-url", default=os.getenv("DATA_MODEL_URL", "http://127.0.0.1:8080"))
    parser.add_argument("--ingestion-url", default=os.getenv("INGESTION_URL", "http://127.0.0.1:8081"))
    parser.add_argument("--decision-engine-url", default=os.getenv("DECISION_ENGINE_URL", "http://127.0.0.1:8082"))
    parser.add_argument("--auth-token", default=os.getenv("SERVICE_AUTH_TOKEN"))
    parser.add_argument("--ingestion-database-url", default=os.getenv("INGESTION_DATABASE_URL"))
    args = parser.parse_args()
    output = args.output or str(Path("stress-tests/demo-runs") / f"chango-scenario-catalog-{utc_now().replace(':', '').replace('.', '-')}.json")
    return Config(args.tenant_id, args.tenant_name, args.timeout, output, args.data_model_url.rstrip("/"), args.ingestion_url.rstrip("/"), args.decision_engine_url.rstrip("/"), args.auth_token, args.ingestion_database_url)


async def async_main() -> int:
    config = parse_config()
    harness = ChangoScenarioDemoHarness(config)
    try:
        tenant_label = config.tenant_id or "a fresh tenant"
        print(f"bootstrapping Chango fraud scenario catalog in {tenant_label}...")
        await harness.bootstrap()
        print("\nChango Fraud Scenario Catalog Demo")
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
            "test": {"name": "chango_scenario_catalog", "objective": "Create a fresh tenant with Chango fraud scenarios and run deterministic smoke cases."},
            "run": asdict(config) | {"auth_token": "set" if config.auth_token else None, "ingestion_database_url": "set" if config.ingestion_database_url else None},
            "setup": {
                "tenant_id": harness.tenant_id,
                "transaction_object_type": harness.object_type,
                "scenario_ids": harness.scenario_ids,
                "ip_watchlist_name": harness.ip_list_name,
                "decision_table": {"approve": "0-29", "review": "30-59", "block_and_review": "60-89", "decline": "90+"},
                "rules": {
                    name: [asdict(rule) | {"formula": rule.formula} for rule in rules]
                    for name, rules in harness.rule_defs_by_scenario.items()
                },
            },
            "cases": results,
            "coverage": coverage,
            "latency": {"overall": overall_latency, "by_scenario": scenario_latencies},
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
