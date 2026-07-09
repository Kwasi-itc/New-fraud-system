from __future__ import annotations

import argparse
import asyncio
import json
import os
from dataclasses import asdict, dataclass
from pathlib import Path
from typing import Any

from marble_scaling_common import (
    SUPPORTED_VARIANTS,
    MarbleClient,
    MarbleScalingHarness,
    environment_metadata,
    firebase_login,
    format_optional,
    latency_summary,
    parse_csv_list,
    parse_int_list,
    per_vu_summary,
    run_closed_loop,
    utc_now,
)


DEFAULT_DATA_SIZES = [100, 1000, 10000, 100000]
DEFAULT_VARIANTS = ["aggregate_count", "aggregate_velocity", "mixed_heavy"]


@dataclass(frozen=True)
class Config:
    data_sizes: list[int]
    variants: list[str]
    vus: list[int]
    duration_seconds: float
    transaction_value: float
    timeout_seconds: float
    output_dir: str
    api_url: str
    api_key: str | None
    admin_token: str | None
    admin_email: str | None
    admin_password: str | None
    firebase_auth_url: str
    firebase_api_key: str
    scenario_threshold: float
    scenario_count: int
    rules_per_scenario: int


def trial_output_path(config: Config, variant: str, data_size: int, vus: int) -> str:
    duration_label = f"{config.duration_seconds:g}s"
    return str(
        Path(config.output_dir)
        / f"trial-{variant}-{data_size}-records-{config.scenario_count}-scenarios-"
        f"{config.rules_per_scenario}-rules-{vus}-vus-{duration_label}.json"
    )


def rate_from_trial(trial: dict[str, Any]) -> float:
    return trial["rates"]["achieved_successful_requests_per_second"]


def apply_data_size_baselines(trials: list[dict[str, Any]]) -> None:
    baseline_size = min(trial["run"]["data_size"] for trial in trials)
    baselines = {
        (trial["run"]["variant"], trial["run"]["configured_vus"]): trial
        for trial in trials
        if trial["run"]["data_size"] == baseline_size
    }
    for trial in trials:
        key = (trial["run"]["variant"], trial["run"]["configured_vus"])
        baseline = baselines.get(key)
        ratios: dict[str, float | None] = {}
        if baseline is not None:
            base_rps = rate_from_trial(baseline)
            rps = rate_from_trial(trial)
            ratios["rps_slowdown_ratio"] = (base_rps / rps) if rps else None
            for metric in ["p50", "p95", "p99", "max", "avg"]:
                value = trial["latency_ms"].get(metric)
                base = baseline["latency_ms"].get(metric)
                ratios[f"{metric}_latency_ratio"] = (value / base) if value is not None and base else None
        trial["data_size_baseline_comparison"] = {
            "baseline_data_size": baseline_size,
            "baseline_available": baseline is not None,
            **ratios,
        }


def summarize_trial(
    config: Config,
    harness: MarbleScalingHarness,
    variant: str,
    data_size: int,
    vus: int,
    output: str,
    metrics: Any,
    elapsed_seconds: float,
) -> dict[str, Any]:
    latencies = metrics.latencies_ms or []
    achieved_rps = metrics.successes / elapsed_seconds if elapsed_seconds > 0 else 0.0
    expected_decision_writes = metrics.successes * config.scenario_count
    expected_rule_execution_writes = metrics.successes * config.scenario_count * config.rules_per_scenario
    return {
        "summary_version": 1,
        "test": {
            "name": "marble_decision_data_volume_sensitivity",
            "objective": "Measure how Marble decision performance changes as seeded tenant data volume grows.",
            "system_under_test": "POST /v1/decisions or POST /v1/decisions/all",
            "load_model": "Closed loop: each VU sends a new request immediately after its previous request completes.",
            "sustainability_definition": "0% errors, 0% timeouts, and 0 skipped decisions during measured run.",
        },
        "environment": environment_metadata(config.api_url),
        "setup": {
            "variant": variant,
            "transaction_table": harness.transaction_table,
            "account_table": harness.account_table,
            "merchant_table": harness.merchant_table,
            "product_table": harness.product_table,
            "account_link_name": harness.account_link_name,
            "merchant_link_name": harness.merchant_link_name,
            "product_link_name": harness.product_link_name,
            "scenario_count": config.scenario_count,
            "rules_per_scenario": config.rules_per_scenario,
            "total_rules_per_request": config.scenario_count * config.rules_per_scenario,
            "scenario_ids": harness.scenario_ids,
            "rule_count": len(harness.rule_ids),
            "seeded_counts": harness.seeded_counts,
            "related_seed_count": data_size,
            "data_size": data_size,
        },
        "run": {
            "variant": variant,
            "data_size": data_size,
            "configured_vus": vus,
            "scenario_count": config.scenario_count,
            "rules_per_scenario": config.rules_per_scenario,
            "total_rules_per_request": config.scenario_count * config.rules_per_scenario,
            "duration_seconds": config.duration_seconds,
            "timeout_seconds": config.timeout_seconds,
            "transaction_value": config.transaction_value,
            "output": output,
            "api_key": "set",
            "admin_token": "set",
        },
        "workload_counts": {
            "configured_vus": vus,
            "completed_requests": metrics.completed,
            "successful_requests": metrics.successes,
            "failed_requests": metrics.failures,
            "timeouts": metrics.timeouts,
            "dropped_requests": 0,
            "skipped_decisions": metrics.skipped_decisions,
            "expected_decision_writes": expected_decision_writes,
            "expected_rule_execution_writes": expected_rule_execution_writes,
        },
        "rates": {
            "achieved_successful_requests_per_second": achieved_rps,
            "expected_decision_writes_per_second": expected_decision_writes / elapsed_seconds if elapsed_seconds > 0 else 0.0,
            "expected_rule_execution_writes_per_second": expected_rule_execution_writes / elapsed_seconds if elapsed_seconds > 0 else 0.0,
            "success_rate": metrics.successes / metrics.completed if metrics.completed else 0.0,
            "error_rate": metrics.failures / metrics.completed if metrics.completed else 0.0,
            "timeout_rate": metrics.timeouts / metrics.completed if metrics.completed else 0.0,
        },
        "per_vu_successes": per_vu_summary(metrics),
        "latency_ms": latency_summary(latencies),
        "result": {
            "sustainable": metrics.failures == 0 and metrics.timeouts == 0 and metrics.skipped_decisions == 0,
            "elapsed_seconds": elapsed_seconds,
            "requested_duration_seconds": config.duration_seconds,
        },
        "sample_errors": metrics.errors or [],
    }


def aggregate_summary(config: Config, trials: list[dict[str, Any]]) -> dict[str, Any]:
    apply_data_size_baselines(trials)
    run_config = asdict(config)
    run_config["api_key"] = "set"
    run_config["admin_token"] = "set" if config.admin_token else "generated"
    run_config["admin_password"] = "set" if config.admin_password else None
    return {
        "summary_version": 1,
        "test": {
            "name": "marble_decision_data_volume_sensitivity",
            "objective": "Compare Marble decision performance across seeded data volumes.",
        },
        "environment": environment_metadata(config.api_url),
        "run": run_config,
        "trial_count": len(trials),
        "trials": trials,
    }


def print_trial_summary(summary: dict[str, Any]) -> None:
    run = summary["run"]
    rates = summary["rates"]
    latency = summary["latency_ms"]
    counts = summary["workload_counts"]
    print(
        f"  {run['variant']} @ {run['data_size']} records, "
        f"{run['scenario_count']} scenarios x {run['rules_per_scenario']} rules, "
        f"{run['configured_vus']} VUs: {rates['achieved_successful_requests_per_second']:.2f} RPS, "
        f"p95 {format_optional(latency['p95'])} ms, "
        f"p99 {format_optional(latency['p99'])} ms, "
        f"failures {counts['failed_requests']}, timeouts {counts['timeouts']}, skipped {counts['skipped_decisions']}"
    )


async def run_trial(config: Config, variant_name: str, data_size: int, vus: int, admin_token: str) -> dict[str, Any]:
    output = trial_output_path(config, variant_name, data_size, vus)
    client = MarbleClient(config.api_url, config.api_key, admin_token, config.timeout_seconds, vus)
    client.set_admin_token(admin_token)
    harness = MarbleScalingHarness(client, config.transaction_value, config.scenario_threshold, data_size)
    try:
        print(
            f"bootstrapping Marble data-volume {variant_name}: {data_size} records, "
            f"{config.scenario_count} scenarios x {config.rules_per_scenario} rules for {vus} VUs..."
        )
        await client.wait_ready()
        await client.create_api_key()
        await harness.bootstrap_model()
        await harness.seed_for_variant(variant_name)
        variant = harness.variant(variant_name)
        for _ in range(config.scenario_count):
            await harness.create_scenario(variant, config.rules_per_scenario)
        print(f"running {variant_name} @ {data_size} records for {config.duration_seconds:.0f}s...")

        async def action() -> bool:
            if config.scenario_count == 1:
                response = await client.request(
                    client.public,
                    "POST",
                    "/v1/decisions",
                    200,
                    json={"scenario_id": harness.scenario_ids[0], "trigger_object": harness.next_transaction_payload()},
                )
            else:
                response = await client.request(
                    client.public,
                    "POST",
                    "/v1/decisions/all",
                    200,
                    json={"trigger_object_type": harness.transaction_table, "trigger_object": harness.next_transaction_payload()},
                )
            metadata = response.get("metadata", {})
            return int(metadata.get("skipped", 0)) == 0

        metrics, elapsed = await run_closed_loop(action, vus, config.duration_seconds)
        summary = summarize_trial(config, harness, variant_name, data_size, vus, output, metrics, elapsed)
        output_path = Path(output)
        output_path.parent.mkdir(parents=True, exist_ok=True)
        output_path.write_text(json.dumps(summary, indent=2, default=str) + "\n", encoding="utf-8")
        print_trial_summary(summary)
        return summary
    finally:
        await client.close()


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="Marble data volume sensitivity stress test.")
    parser.add_argument("--data-sizes", default=",".join(str(item) for item in DEFAULT_DATA_SIZES))
    parser.add_argument("--variants", default=",".join(DEFAULT_VARIANTS), help="Comma-separated variant names.")
    parser.add_argument("--vus", default="5", help="Comma-separated closed-loop VU levels.")
    parser.add_argument("--duration", type=float, default=60.0)
    parser.add_argument("--transaction-value", type=float, default=10000.0)
    parser.add_argument("--timeout", type=float, default=30.0)
    parser.add_argument("--scenario-threshold", type=float, default=1000.0)
    parser.add_argument("--scenario-count", type=int, default=1)
    parser.add_argument("--rules-per-scenario", type=int, default=1)
    parser.add_argument("--output-dir")
    parser.add_argument("--api-url", default=os.getenv("MARBLE_API_URL", "http://127.0.0.1:8080"))
    parser.add_argument("--api-key", default=os.getenv("MARBLE_API_KEY"))
    parser.add_argument("--admin-token", default=os.getenv("MARBLE_ADMIN_TOKEN"))
    parser.add_argument("--admin-email", default=os.getenv("MARBLE_ADMIN_EMAIL"))
    parser.add_argument("--admin-password", default=os.getenv("MARBLE_ADMIN_PASSWORD"))
    parser.add_argument("--firebase-auth-url", default=os.getenv("FIREBASE_AUTH_URL", "http://127.0.0.1:9099"))
    parser.add_argument("--firebase-api-key", default=os.getenv("FIREBASE_API_KEY", "dummy"))
    return parser


def parse_config() -> Config:
    args = build_parser().parse_args()
    variants = parse_csv_list(args.variants, set(SUPPORTED_VARIANTS))
    data_sizes = parse_int_list(args.data_sizes, "--data-sizes")
    vus = parse_int_list(args.vus, "--vus")
    if args.duration <= 0:
        raise SystemExit("--duration must be greater than 0")
    if args.timeout <= 0:
        raise SystemExit("--timeout must be greater than 0")
    if args.scenario_count <= 0:
        raise SystemExit("--scenario-count must be greater than 0")
    if args.rules_per_scenario <= 0:
        raise SystemExit("--rules-per-scenario must be greater than 0")
    if not args.admin_token and (not args.admin_email or not args.admin_password):
        raise SystemExit("set --admin-token/MARBLE_ADMIN_TOKEN or --admin-email and --admin-password")
    output_dir = args.output_dir or str(Path("stress-tests/marble-data-volume-runs") / utc_now().replace(":", "").replace(".", "-"))
    return Config(
        data_sizes=data_sizes,
        variants=variants,
        vus=vus,
        duration_seconds=args.duration,
        transaction_value=args.transaction_value,
        timeout_seconds=args.timeout,
        output_dir=output_dir,
        api_url=args.api_url.rstrip("/"),
        api_key=args.api_key,
        admin_token=args.admin_token,
        admin_email=args.admin_email,
        admin_password=args.admin_password,
        firebase_auth_url=args.firebase_auth_url.rstrip("/"),
        firebase_api_key=args.firebase_api_key,
        scenario_threshold=args.scenario_threshold,
        scenario_count=args.scenario_count,
        rules_per_scenario=args.rules_per_scenario,
    )


async def resolve_admin_token(config: Config) -> str:
    if config.admin_token:
        return config.admin_token
    assert config.admin_email is not None and config.admin_password is not None
    return await firebase_login(
        config.api_url,
        config.firebase_auth_url,
        config.firebase_api_key,
        config.admin_email,
        config.admin_password,
        config.timeout_seconds,
    )


async def async_main() -> int:
    config = parse_config()
    Path(config.output_dir).mkdir(parents=True, exist_ok=True)
    admin_token = await resolve_admin_token(config)
    trials: list[dict[str, Any]] = []
    for vus in config.vus:
        for data_size in config.data_sizes:
            for variant in config.variants:
                trials.append(await run_trial(config, variant, data_size, vus, admin_token))
    summary = aggregate_summary(config, trials)
    for trial in trials:
        Path(trial["run"]["output"]).write_text(json.dumps(trial, indent=2, default=str) + "\n", encoding="utf-8")
    summary_path = Path(config.output_dir) / "summary.json"
    summary_path.write_text(json.dumps(summary, indent=2, default=str) + "\n", encoding="utf-8")
    print("")
    print("Marble Data Volume Sensitivity Summary")
    print(f"  trials: {len(trials)}")
    print(f"  output: {summary_path}")
    return 0 if all(trial["result"]["sustainable"] for trial in trials) else 1


def main() -> None:
    raise SystemExit(asyncio.run(async_main()))


if __name__ == "__main__":
    main()
