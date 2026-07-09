from __future__ import annotations

import argparse
import asyncio
import json
import os
from dataclasses import asdict, dataclass
from pathlib import Path
from typing import Any

from decision_scenario_scaling import (
    ScenarioScalingHarness,
    run_closed_loop,
)
from decision_throughput_limit import (
    Config as BaseConfig,
    environment_metadata,
    format_optional,
    percentile,
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
    amount: int
    timeout_seconds: float
    output_dir: str
    data_model_url: str
    ingestion_url: str
    decision_engine_url: str
    auth_token: str | None
    scenario_threshold: int
    ingestion_database_url: str | None
    scenario_count: int
    rules_per_scenario: int


def parse_csv_list(value: str, name: str) -> list[str]:
    items = [item.strip() for item in value.split(",") if item.strip()]
    if not items:
        raise SystemExit(f"{name} must not be empty")
    return items


def parse_int_list(value: str, name: str) -> list[int]:
    try:
        items = [int(item) for item in parse_csv_list(value, name)]
    except ValueError as exc:
        raise SystemExit(f"{name} values must be integers") from exc
    if any(item <= 0 for item in items):
        raise SystemExit(f"{name} values must be greater than 0")
    return items


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
    baselines = {
        (trial["run"]["variant"], trial["run"]["configured_vus"]): trial
        for trial in trials
        if trial["run"]["data_size"] == min(item["run"]["data_size"] for item in trials)
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
            "baseline_data_size": baseline["run"]["data_size"] if baseline else None,
            "baseline_available": baseline is not None,
            **ratios,
        }


def summarize_trial(
    config: Config,
    harness: ScenarioScalingHarness,
    data_size: int,
    vus: int,
    output: str,
    metrics: Any,
    elapsed_seconds: float,
) -> dict[str, Any]:
    assert harness.variant is not None
    latencies = metrics.latencies_ms or []
    per_vu_counts = list((metrics.per_vu_successes or {}).values())
    achieved_rps = metrics.successes / elapsed_seconds if elapsed_seconds > 0 else 0.0
    expected_decision_writes = metrics.successes * config.scenario_count
    expected_rule_execution_writes = metrics.successes * config.scenario_count * config.rules_per_scenario
    return {
        "summary_version": 1,
        "test": {
            "name": "decision_engine_data_volume_sensitivity",
            "objective": "Measure how decision evaluation performance changes as seeded tenant data volume grows.",
            "system_under_test": "POST /v1/tenants/{tenantId}/decisions/all",
            "load_model": "Closed loop: each VU sends a new request immediately after its previous request completes.",
            "sustainability_definition": "0% errors and 0% timeouts during measured run.",
        },
        "environment": environment_metadata(to_base_config(config, output, vus)),
        "setup": {
            "variant": harness.variant.name,
            "variant_description": harness.variant.description,
            "tenant_id": harness.tenant_id,
            "object_type": harness.object_type,
            "account_object_type": harness.account_object_type,
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
            "variant": harness.variant.name,
            "data_size": data_size,
            "configured_vus": vus,
            "scenario_count": config.scenario_count,
            "rules_per_scenario": config.rules_per_scenario,
            "total_rules_per_request": config.scenario_count * config.rules_per_scenario,
            "duration_seconds": config.duration_seconds,
            "timeout_seconds": config.timeout_seconds,
            "amount": config.amount,
            "output": output,
            "auth_token": "set" if config.auth_token else None,
        },
        "workload_counts": {
            "configured_vus": vus,
            "completed_requests": metrics.completed,
            "successful_requests": metrics.successes,
            "failed_requests": metrics.failures,
            "timeouts": metrics.timeouts,
            "dropped_requests": 0,
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
        "per_vu_successes": {
            "min": min(per_vu_counts) if per_vu_counts else 0,
            "max": max(per_vu_counts) if per_vu_counts else 0,
            "avg": sum(per_vu_counts) / len(per_vu_counts) if per_vu_counts else 0,
        },
        "latency_ms": {
            "avg": sum(latencies) / len(latencies) if latencies else None,
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


def aggregate_summary(config: Config, trials: list[dict[str, Any]]) -> dict[str, Any]:
    apply_data_size_baselines(trials)
    run_config = asdict(config)
    run_config["auth_token"] = "set" if config.auth_token else None
    run_config["ingestion_database_url"] = "set" if config.ingestion_database_url else None
    return {
        "summary_version": 1,
        "test": {
            "name": "decision_engine_data_volume_sensitivity",
            "objective": "Compare decision evaluation performance across seeded data volumes.",
        },
        "environment": environment_metadata(to_base_config(config, str(Path(config.output_dir) / "summary.json"), config.vus[0])),
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
        f"failures {counts['failed_requests']}, timeouts {counts['timeouts']}"
    )


async def run_trial(config: Config, variant: str, data_size: int, vus: int) -> dict[str, Any]:
    output = trial_output_path(config, variant, data_size, vus)
    base_config = to_base_config(config, output, vus)
    harness = ScenarioScalingHarness(
        base_config,
        variant,
        data_size,
        config.ingestion_database_url,
        config.scenario_count,
        config.rules_per_scenario,
    )
    print(
        f"bootstrapping new-system data-volume {variant}: {data_size} records, "
        f"{config.scenario_count} scenarios x {config.rules_per_scenario} rules for {vus} VUs..."
    )
    await harness.bootstrap()
    print(f"running {variant} @ {data_size} records for {config.duration_seconds:.0f}s...")
    metrics, elapsed = await run_closed_loop(harness, vus, config.duration_seconds)
    summary = summarize_trial(config, harness, data_size, vus, output, metrics, elapsed)
    output_path = Path(output)
    output_path.parent.mkdir(parents=True, exist_ok=True)
    output_path.write_text(json.dumps(summary, indent=2, default=str) + "\n", encoding="utf-8")
    print_trial_summary(summary)
    return summary


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="New-system data volume sensitivity stress test.")
    parser.add_argument("--data-sizes", default=",".join(str(item) for item in DEFAULT_DATA_SIZES))
    parser.add_argument("--variants", default=",".join(DEFAULT_VARIANTS))
    parser.add_argument("--vus", default="5")
    parser.add_argument("--duration", type=float, default=60.0)
    parser.add_argument("--amount", type=int, default=1800)
    parser.add_argument("--timeout", type=float, default=30.0)
    parser.add_argument("--scenario-threshold", type=int, default=1000)
    parser.add_argument("--scenario-count", type=int, default=1)
    parser.add_argument("--rules-per-scenario", type=int, default=1)
    parser.add_argument("--output-dir")
    parser.add_argument("--data-model-url", default=os.getenv("DATA_MODEL_URL", "http://127.0.0.1:8080"))
    parser.add_argument("--ingestion-url", default=os.getenv("INGESTION_URL", "http://127.0.0.1:8081"))
    parser.add_argument("--decision-engine-url", default=os.getenv("DECISION_ENGINE_URL", "http://127.0.0.1:8082"))
    parser.add_argument("--auth-token", default=os.getenv("AUTH_TOKEN"))
    parser.add_argument("--ingestion-database-url", default=os.getenv("INGESTION_DATABASE_URL"))
    return parser


def parse_config() -> Config:
    args = build_parser().parse_args()
    if args.duration <= 0:
        raise SystemExit("--duration must be greater than 0")
    if args.timeout <= 0:
        raise SystemExit("--timeout must be greater than 0")
    if args.scenario_count <= 0:
        raise SystemExit("--scenario-count must be greater than 0")
    if args.rules_per_scenario <= 0:
        raise SystemExit("--rules-per-scenario must be greater than 0")
    output_dir = args.output_dir or str(Path("stress-tests/data-volume-runs") / utc_now().replace(":", "").replace(".", "-"))
    return Config(
        data_sizes=parse_int_list(args.data_sizes, "--data-sizes"),
        variants=parse_csv_list(args.variants, "--variants"),
        vus=parse_int_list(args.vus, "--vus"),
        duration_seconds=args.duration,
        amount=args.amount,
        timeout_seconds=args.timeout,
        output_dir=output_dir,
        data_model_url=args.data_model_url.rstrip("/"),
        ingestion_url=args.ingestion_url.rstrip("/"),
        decision_engine_url=args.decision_engine_url.rstrip("/"),
        auth_token=args.auth_token,
        scenario_threshold=args.scenario_threshold,
        ingestion_database_url=args.ingestion_database_url,
        scenario_count=args.scenario_count,
        rules_per_scenario=args.rules_per_scenario,
    )


async def async_main() -> int:
    config = parse_config()
    Path(config.output_dir).mkdir(parents=True, exist_ok=True)
    trials: list[dict[str, Any]] = []
    for vus in config.vus:
        for data_size in config.data_sizes:
            for variant in config.variants:
                trials.append(await run_trial(config, variant, data_size, vus))
    summary = aggregate_summary(config, trials)
    for trial in trials:
        Path(trial["run"]["output"]).write_text(json.dumps(trial, indent=2, default=str) + "\n", encoding="utf-8")
    summary_path = Path(config.output_dir) / "summary.json"
    summary_path.write_text(json.dumps(summary, indent=2, default=str) + "\n", encoding="utf-8")
    print("")
    print("New-System Data Volume Sensitivity Summary")
    print(f"  trials: {len(trials)}")
    print(f"  output: {summary_path}")
    return 0 if all(trial["result"]["sustainable"] for trial in trials) else 1


def main() -> None:
    raise SystemExit(asyncio.run(async_main()))


if __name__ == "__main__":
    main()
