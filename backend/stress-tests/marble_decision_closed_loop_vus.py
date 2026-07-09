from __future__ import annotations

import argparse
import asyncio
import json
import os
import statistics
import time
from dataclasses import asdict, dataclass
from pathlib import Path
from typing import Any

from marble_scaling_common import (
    CHANNELS,
    DOMAIN_ACCOUNTS,
    DOMAIN_MERCHANTS,
    DOMAIN_PRODUCTS,
    MarbleClient,
    MarbleScalingHarness,
    PROCESSORS,
    Metrics,
    environment_metadata,
    firebase_login,
    format_optional,
    latency_summary,
    per_vu_summary,
    percentile,
    run_closed_loop,
)


@dataclass(frozen=True)
class Config:
    vus: int
    duration_seconds: float
    transaction_value: float
    account_past_balance: float
    timeout_seconds: float
    output: str
    api_url: str
    api_key: str | None
    admin_token: str | None
    admin_email: str | None
    admin_password: str | None
    firebase_auth_url: str
    firebase_api_key: str
    scenario_threshold: float


@dataclass
class ClosedLoopMetrics(Metrics):
    per_vu_successes: dict[int, int] | None = None

    def __post_init__(self) -> None:
        super().__post_init__()
        if self.per_vu_successes is None:
            self.per_vu_successes = {}


def summarize_metrics(
    config: Config,
    harness: MarbleScalingHarness,
    metrics: Metrics,
    elapsed_seconds: float,
) -> dict[str, Any]:
    latencies = metrics.latencies_ms or []
    per_vu_counts = list((metrics.per_vu_successes or {}).values())
    achieved_dps = metrics.successes / elapsed_seconds if elapsed_seconds > 0 else 0.0
    sustainable = metrics.failures == 0 and metrics.timeouts == 0
    return {
        "summary_version": 1,
        "test": {
            "name": "marble_decision_closed_loop_vus",
            "objective": "Measure achieved public API decision creations per second with fixed always-active VUs.",
            "system_under_test": "POST /v1/decisions",
            "load_model": "Closed loop: each VU sends a new request immediately after its previous request completes.",
            "sustainability_definition": "0% errors, 0% timeouts, and 0 skipped decisions during measured run.",
        },
        "environment": environment_metadata(config.api_url),
        "setup": {
            "scenario_ids": harness.scenario_ids,
            "trigger_object_type": harness.transaction_table,
            "transaction_table": harness.transaction_table,
            "account_table": harness.account_table,
            "merchant_table": harness.merchant_table,
            "product_table": harness.product_table,
            "account_count": len(DOMAIN_ACCOUNTS),
            "merchant_count": len(DOMAIN_MERCHANTS),
            "product_count": len(DOMAIN_PRODUCTS),
            "processors": PROCESSORS,
            "channels": CHANNELS,
            "seeded_counts": harness.seeded_counts,
        },
        "run": asdict(config) | {"api_key": "set", "admin_token": "set"},
        "workload_counts": {
            "configured_vus": config.vus,
            "completed_decisions": metrics.completed,
            "successful_decisions": metrics.successes,
            "failed_decisions": metrics.failures,
            "timeouts": metrics.timeouts,
            "dropped_requests": 0,
            "skipped_decisions": metrics.skipped_decisions,
        },
        "rates": {
            "achieved_successful_decisions_per_second": achieved_dps,
            "success_rate": metrics.successes / metrics.completed if metrics.completed else 0.0,
            "error_rate": metrics.failures / metrics.completed if metrics.completed else 0.0,
            "timeout_rate": metrics.timeouts / metrics.completed if metrics.completed else 0.0,
        },
        "per_vu_successes": per_vu_summary(metrics),
        "latency_ms": latency_summary(latencies),
        "result": {
            "sustainable": sustainable,
            "elapsed_seconds": elapsed_seconds,
            "requested_duration_seconds": config.duration_seconds,
        },
        "sample_errors": metrics.errors or [],
    }


def print_text_summary(summary: dict[str, Any]) -> None:
    counts = summary["workload_counts"]
    rates = summary["rates"]
    latency = summary["latency_ms"]
    result = summary["result"]
    per_vu = summary["per_vu_successes"]
    print("")
    print("Marble Decision Closed-Loop VU Summary")
    print(f"  VUs: {counts['configured_vus']}")
    print(f"  achieved DPS: {rates['achieved_successful_decisions_per_second']:.2f}")
    print(f"  successful: {counts['successful_decisions']}")
    print(f"  failed: {counts['failed_decisions']}")
    print(f"  timeouts: {counts['timeouts']}")
    print(f"  skipped: {counts['skipped_decisions']}")
    print(f"  per-VU successes min / avg / max: {per_vu['min']} / {per_vu['avg']:.2f} / {per_vu['max']}")
    print("  latency p50 / p95 / p99 / max, ms:")
    print(
        "  "
        f"{format_optional(latency['p50'])} / "
        f"{format_optional(latency['p95'])} / "
        f"{format_optional(latency['p99'])} / "
        f"{format_optional(latency['max'])}"
    )
    print(f"  sustainable: {result['sustainable']}")
    print("")


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="Closed-loop VU Marble public API decision stress test.")
    parser.add_argument("--vus", type=int, required=True, help="Constant number of active VUs.")
    parser.add_argument("--duration", type=float, required=True, help="Measured run duration in seconds.")
    parser.add_argument("--transaction-value", type=float, default=10000.0, help="Value used in generated transactions.")
    parser.add_argument("--account-past-balance", type=float, default=200000.0, help="Seeded account past balance.")
    parser.add_argument("--timeout", type=float, default=30.0, help="Per-request timeout in seconds.")
    parser.add_argument("--output", help="Summary JSON output path.")
    parser.add_argument("--api-url", default=os.getenv("MARBLE_API_URL", "http://127.0.0.1:8080"))
    parser.add_argument("--api-key", default=os.getenv("MARBLE_API_KEY"))
    parser.add_argument("--admin-token", default=os.getenv("MARBLE_ADMIN_TOKEN"))
    parser.add_argument("--admin-email", default=os.getenv("MARBLE_ADMIN_EMAIL"))
    parser.add_argument("--admin-password", default=os.getenv("MARBLE_ADMIN_PASSWORD"))
    parser.add_argument("--firebase-auth-url", default=os.getenv("FIREBASE_AUTH_URL", "http://127.0.0.1:9099"))
    parser.add_argument("--firebase-api-key", default=os.getenv("FIREBASE_API_KEY", "dummy"))
    parser.add_argument("--scenario-threshold", type=float, default=1000.0, help="Simple amount rule threshold.")
    return parser


def default_output_path(vus: int, duration_seconds: float) -> str:
    duration_label = f"{duration_seconds:g}s"
    return f"stress-tests/marble-closed-loop-vus-summary-{vus}-vus-{duration_label}.json"


def parse_config() -> Config:
    args = build_parser().parse_args()
    if args.vus <= 0:
        raise SystemExit("--vus must be greater than 0")
    if args.duration <= 0:
        raise SystemExit("--duration must be greater than 0")
    if args.timeout <= 0:
        raise SystemExit("--timeout must be greater than 0")
    if not args.admin_token and not (args.admin_email and args.admin_password):
        raise SystemExit("set --admin-token or MARBLE_ADMIN_TOKEN, or set admin email/password")
    return Config(
        vus=args.vus,
        duration_seconds=args.duration,
        transaction_value=args.transaction_value,
        account_past_balance=args.account_past_balance,
        timeout_seconds=args.timeout,
        output=args.output or default_output_path(args.vus, args.duration),
        api_url=args.api_url.rstrip("/"),
        api_key=args.api_key,
        admin_token=args.admin_token,
        admin_email=args.admin_email,
        admin_password=args.admin_password,
        firebase_auth_url=args.firebase_auth_url.rstrip("/"),
        firebase_api_key=args.firebase_api_key,
        scenario_threshold=args.scenario_threshold,
    )


async def run_trial(config: Config) -> dict[str, Any]:
    client = MarbleClient(config.api_url, config.api_key, config.admin_token, config.timeout_seconds, config.vus)
    if config.admin_token:
        client.set_admin_token(config.admin_token)
    else:
        assert config.admin_email is not None
        assert config.admin_password is not None
        token = await firebase_login(
            config.api_url,
            config.firebase_auth_url,
            config.firebase_api_key,
            config.admin_email,
            config.admin_password,
            config.timeout_seconds,
        )
        client.set_admin_token(token)
    harness = MarbleScalingHarness(client, config.transaction_value, config.scenario_threshold, related_seed_count=100)
    try:
        print("bootstrapping tenant model and baseline scenario...")
        await client.wait_ready()
        await client.create_api_key()
        await harness.bootstrap_model()
        await harness.seed_for_variant("baseline_payload")
        variant = harness.variant("baseline_payload")
        await harness.create_scenario(variant)
        print(f"bootstrap complete; scenario_id={harness.scenario_ids[0]}")
        print(f"running closed-loop load for {config.duration_seconds:.0f}s with {config.vus} VUs...")
        async def action() -> bool:
            response = await client.request(
                client.public,
                "POST",
                "/v1/decisions",
                200,
                json={"scenario_id": harness.scenario_ids[0], "trigger_object": harness.next_transaction_payload()},
            )
            metadata = response.get("metadata", {})
            return int(metadata.get("skipped", 0)) == 0

        metrics, elapsed = await run_closed_loop(action, config.vus, config.duration_seconds)
        summary = summarize_metrics(config, harness, metrics, elapsed)
        output_path = Path(config.output)
        output_path.parent.mkdir(parents=True, exist_ok=True)
        output_path.write_text(json.dumps(summary, indent=2, default=str) + "\n", encoding="utf-8")
        return summary
    finally:
        await client.close()


async def async_main() -> int:
    config = parse_config()
    summary = await run_trial(config)
    print_text_summary(summary)
    print(f"summary written to {config.output}")
    return 0 if summary["result"]["sustainable"] else 1


def main() -> None:
    raise SystemExit(asyncio.run(async_main()))


if __name__ == "__main__":
    main()
