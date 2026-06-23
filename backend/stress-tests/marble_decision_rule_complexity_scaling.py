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
    VariantDefinition,
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


@dataclass(frozen=True)
class Config:
    variants: list[str]
    vus: list[int]
    duration_seconds: float
    transaction_value: float
    timeout_seconds: float
    output_dir: str
    related_seed_count: int
    api_url: str
    api_key: str
    admin_token: str | None
    admin_email: str | None
    admin_password: str | None
    firebase_auth_url: str
    firebase_api_key: str
    scenario_threshold: float


def trial_output_path(config: Config, variant: str, vus: int) -> str:
    duration_label = f"{config.duration_seconds:g}s"
    return str(Path(config.output_dir) / f"trial-{variant}-{vus}-vus-{duration_label}.json")


def summarize_trial(
    config: Config,
    variant: VariantDefinition,
    harness: MarbleScalingHarness,
    vus: int,
    output: str,
    metrics: Any,
    elapsed_seconds: float,
) -> dict[str, Any]:
    latencies = metrics.latencies_ms or []
    achieved_dps = metrics.successes / elapsed_seconds if elapsed_seconds > 0 else 0.0
    return {
        "summary_version": 1,
        "test": {
            "name": "marble_decision_rule_complexity_scaling",
            "objective": "Measure how Marble decision performance changes as rule complexity changes.",
            "system_under_test": "POST /v1/decisions",
            "load_model": "Closed loop: each VU sends a new request immediately after its previous request completes.",
            "sustainability_definition": "0% errors, 0% timeouts, and 0 skipped decisions during measured run.",
        },
        "environment": environment_metadata(config.api_url),
        "setup": {
            "variant": variant.name,
            "variant_description": variant.description,
            "transaction_table": harness.transaction_table,
            "account_table": harness.account_table,
            "link_name": harness.link_name,
            "scenario_ids": harness.scenario_ids,
            "scenario_count": 1,
            "rules_per_scenario": 1,
            "seeded_counts": harness.seeded_counts,
            "related_seed_count": config.related_seed_count,
        },
        "run": {
            "variant": variant.name,
            "configured_vus": vus,
            "duration_seconds": config.duration_seconds,
            "timeout_seconds": config.timeout_seconds,
            "transaction_value": config.transaction_value,
            "output": output,
            "api_key": "set",
            "admin_token": "set",
        },
        "workload_counts": {
            "configured_vus": vus,
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
            "sustainable": metrics.failures == 0 and metrics.timeouts == 0 and metrics.skipped_decisions == 0,
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
            dps = trial["rates"]["achieved_successful_decisions_per_second"]
            base_dps = baseline["rates"]["achieved_successful_decisions_per_second"]
            ratios["achieved_dps_ratio"] = (dps / base_dps) if base_dps else None
        trial["baseline_comparison"] = {"baseline_variant": "baseline_payload", "baseline_available": baseline is not None, **ratios}


def compact_ranking_item(trial: dict[str, Any], ratio_key: str) -> dict[str, Any]:
    return {
        "variant": trial["run"]["variant"],
        "vus": trial["run"]["configured_vus"],
        ratio_key: trial.get("baseline_comparison", {}).get(ratio_key),
        "achieved_dps": trial["rates"]["achieved_successful_decisions_per_second"],
        "p95_ms": trial["latency_ms"]["p95"],
        "p99_ms": trial["latency_ms"]["p99"],
        "failures": trial["workload_counts"]["failed_decisions"],
        "timeouts": trial["workload_counts"]["timeouts"],
    }


def aggregate_summary(config: Config, trials: list[dict[str, Any]]) -> dict[str, Any]:
    apply_baseline_ratios(trials)
    run_config = asdict(config)
    run_config["api_key"] = "set"
    run_config["admin_token"] = "set" if config.admin_token else "generated"
    run_config["admin_password"] = "set" if config.admin_password else None
    ranked_by_p95 = sorted(trials, key=lambda item: item.get("baseline_comparison", {}).get("latency_p95_ratio") or 0, reverse=True)
    ranked_by_dps = sorted(trials, key=lambda item: item.get("baseline_comparison", {}).get("achieved_dps_ratio") or 1)
    return {
        "summary_version": 1,
        "test": {
            "name": "marble_decision_rule_complexity_scaling",
            "objective": "Compare Marble decision performance across rule complexity variants.",
        },
        "environment": environment_metadata(config.api_url),
        "run": run_config,
        "trial_count": len(trials),
        "trials": trials,
        "rankings": {
            "highest_p95_latency_ratio": [
                compact_ranking_item(item, "latency_p95_ratio")
                for item in ranked_by_p95
                if item.get("baseline_comparison", {}).get("latency_p95_ratio") is not None
            ],
            "lowest_achieved_dps_ratio": [
                compact_ranking_item(item, "achieved_dps_ratio")
                for item in ranked_by_dps
                if item.get("baseline_comparison", {}).get("achieved_dps_ratio") is not None
            ],
        },
    }


def print_trial_summary(summary: dict[str, Any]) -> None:
    latency = summary["latency_ms"]
    rates = summary["rates"]
    counts = summary["workload_counts"]
    print(
        f"  {summary['run']['variant']} @ {summary['run']['configured_vus']} VUs: "
        f"{rates['achieved_successful_decisions_per_second']:.2f} DPS, "
        f"p95 {format_optional(latency['p95'])} ms, "
        f"p99 {format_optional(latency['p99'])} ms, "
        f"failures {counts['failed_decisions']}, timeouts {counts['timeouts']}"
    )


async def run_trial(config: Config, variant_name: str, vus: int, admin_token: str) -> dict[str, Any]:
    output = trial_output_path(config, variant_name, vus)
    client = MarbleClient(config.api_url, config.api_key, admin_token, config.timeout_seconds, vus)
    client.set_admin_token(admin_token)
    harness = MarbleScalingHarness(client, config.transaction_value, config.scenario_threshold, config.related_seed_count)
    try:
        print(f"bootstrapping Marble variant {variant_name} for {vus} VUs...")
        await client.wait_ready()
        await harness.bootstrap_model()
        await harness.seed_for_variant(variant_name)
        variant = harness.variant(variant_name)
        scenario_id = await harness.create_scenario(variant)
        print(f"running {variant_name} for {config.duration_seconds:.0f}s with {vus} VUs; scenario_id={scenario_id}")

        async def action() -> bool:
            response = await client.request(
                client.public,
                "POST",
                "/v1/decisions",
                200,
                json={"scenario_id": scenario_id, "trigger_object": harness.next_transaction_payload()},
            )
            metadata = response.get("metadata", {})
            return int(metadata.get("skipped", 0)) == 0

        metrics, elapsed = await run_closed_loop(action, vus, config.duration_seconds)
        summary = summarize_trial(config, variant, harness, vus, output, metrics, elapsed)
        output_path = Path(output)
        output_path.parent.mkdir(parents=True, exist_ok=True)
        output_path.write_text(json.dumps(summary, indent=2, default=str) + "\n", encoding="utf-8")
        print_trial_summary(summary)
        return summary
    finally:
        await client.close()


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="Marble rule complexity scaling stress test.")
    parser.add_argument("--variants", default=",".join(SUPPORTED_VARIANTS), help="Comma-separated variant names.")
    parser.add_argument("--vus", default="5,10,30", help="Comma-separated closed-loop VU levels.")
    parser.add_argument("--duration", type=float, default=60.0)
    parser.add_argument("--transaction-value", type=float, default=10000.0)
    parser.add_argument("--timeout", type=float, default=30.0)
    parser.add_argument("--scenario-threshold", type=float, default=1000.0)
    parser.add_argument("--related-seed-count", type=int, default=100)
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
    if "baseline_payload" not in variants:
        variants = ["baseline_payload", *variants]
    vus = parse_int_list(args.vus, "--vus")
    if args.duration <= 0:
        raise SystemExit("--duration must be greater than 0")
    if args.timeout <= 0:
        raise SystemExit("--timeout must be greater than 0")
    if args.related_seed_count <= 0:
        raise SystemExit("--related-seed-count must be greater than 0")
    if not args.api_key:
        raise SystemExit("set --api-key or MARBLE_API_KEY")
    if not args.admin_token and (not args.admin_email or not args.admin_password):
        raise SystemExit("set --admin-token/MARBLE_ADMIN_TOKEN or --admin-email and --admin-password")
    output_dir = args.output_dir or str(Path("stress-tests/marble-rule-complexity-runs") / utc_now().replace(":", "").replace(".", "-"))
    return Config(
        variants=variants,
        vus=vus,
        duration_seconds=args.duration,
        transaction_value=args.transaction_value,
        timeout_seconds=args.timeout,
        output_dir=output_dir,
        related_seed_count=args.related_seed_count,
        api_url=args.api_url.rstrip("/"),
        api_key=args.api_key,
        admin_token=args.admin_token,
        admin_email=args.admin_email,
        admin_password=args.admin_password,
        firebase_auth_url=args.firebase_auth_url.rstrip("/"),
        firebase_api_key=args.firebase_api_key,
        scenario_threshold=args.scenario_threshold,
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
    trials = []
    for vus in config.vus:
        for variant in config.variants:
            trials.append(await run_trial(config, variant, vus, admin_token))
    summary = aggregate_summary(config, trials)
    for trial in trials:
        Path(trial["run"]["output"]).write_text(json.dumps(trial, indent=2, default=str) + "\n", encoding="utf-8")
    summary_path = Path(config.output_dir) / "summary.json"
    summary_path.write_text(json.dumps(summary, indent=2, default=str) + "\n", encoding="utf-8")
    print("")
    print("Marble Rule Complexity Scaling Summary")
    print(f"  trials: {len(trials)}")
    print(f"  output: {summary_path}")
    return 0 if all(trial["result"]["sustainable"] for trial in trials) else 1


def main() -> None:
    raise SystemExit(asyncio.run(async_main()))


if __name__ == "__main__":
    main()

