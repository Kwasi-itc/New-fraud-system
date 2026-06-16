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

import httpx

from marble_decision_throughput_limit import (
    Config as BaseConfig,
    MarbleThroughputHarness,
    Metrics,
    add_error,
    environment_metadata,
    format_optional,
    percentile,
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
    api_key: str
    scenario_id: str
    seed_parent_records: bool


@dataclass
class ClosedLoopMetrics(Metrics):
    per_vu_successes: dict[int, int] | None = None

    def __post_init__(self) -> None:
        super().__post_init__()
        if self.per_vu_successes is None:
            self.per_vu_successes = {}


def to_base_config(config: Config) -> BaseConfig:
    return BaseConfig(
        rate=0,
        vus=config.vus,
        duration_seconds=config.duration_seconds,
        warmup_duration_seconds=0,
        transaction_value=config.transaction_value,
        account_past_balance=config.account_past_balance,
        timeout_seconds=config.timeout_seconds,
        output=config.output,
        api_url=config.api_url,
        api_key=config.api_key,
        org_export="org-export.json",
        scenario_name="",
        scenario_id=config.scenario_id,
        scenario_threshold=0,
        seed_parent_records=config.seed_parent_records,
        create_stress_scenario=False,
    )


async def run_one_request(harness: MarbleThroughputHarness, metrics: ClosedLoopMetrics, vu_id: int) -> None:
    started_at = time.perf_counter()
    metrics.attempted += 1
    try:
        decision_created = await harness.decide_once()
    except httpx.TimeoutException as exc:
        metrics.completed += 1
        metrics.failures += 1
        metrics.timeouts += 1
        add_error(metrics, f"timeout: {exc}")
    except Exception as exc:
        metrics.completed += 1
        metrics.failures += 1
        add_error(metrics, str(exc))
    else:
        metrics.completed += 1
        if decision_created:
            metrics.successes += 1
            assert metrics.latencies_ms is not None
            metrics.latencies_ms.append((time.perf_counter() - started_at) * 1000.0)
            assert metrics.per_vu_successes is not None
            metrics.per_vu_successes[vu_id] = metrics.per_vu_successes.get(vu_id, 0) + 1
        else:
            metrics.failures += 1
            metrics.skipped_decisions += 1
            add_error(metrics, "scenario trigger condition did not match")


async def worker(
    vu_id: int,
    harness: MarbleThroughputHarness,
    metrics: ClosedLoopMetrics,
    start_gate: asyncio.Event,
    deadline: float,
) -> None:
    await start_gate.wait()
    while time.perf_counter() < deadline:
        await run_one_request(harness, metrics, vu_id)


async def run_closed_loop(harness: MarbleThroughputHarness, vus: int, duration_seconds: float) -> tuple[ClosedLoopMetrics, float]:
    metrics = ClosedLoopMetrics()
    start_gate = asyncio.Event()
    started_at = time.perf_counter()
    deadline = started_at + duration_seconds
    tasks = [
        asyncio.create_task(worker(vu_id, harness, metrics, start_gate, deadline))
        for vu_id in range(vus)
    ]
    start_gate.set()
    await asyncio.gather(*tasks)
    return metrics, time.perf_counter() - started_at


def summarize_metrics(
    config: Config,
    harness: MarbleThroughputHarness,
    metrics: ClosedLoopMetrics,
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
        "environment": environment_metadata(to_base_config(config)),
        "setup": {
            "scenario_id": harness.scenario_id,
            "trigger_object_type": "transactions",
            "seeded_company_id": harness.company_id if config.seed_parent_records else None,
            "seeded_account_id": harness.account_id if config.seed_parent_records else None,
        },
        "run": asdict(config) | {"api_key": "set"},
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
        "per_vu_successes": {
            "min": min(per_vu_counts) if per_vu_counts else 0,
            "max": max(per_vu_counts) if per_vu_counts else 0,
            "avg": statistics.fmean(per_vu_counts) if per_vu_counts else 0,
        },
        "latency_ms": {
            "avg": statistics.fmean(latencies) if latencies else None,
            "p50": percentile(latencies, 50),
            "p95": percentile(latencies, 95),
            "p99": percentile(latencies, 99),
            "max": max(latencies) if latencies else None,
        },
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
    parser.add_argument("--output", default="stress-tests/marble-closed-loop-vus-summary.json")
    parser.add_argument("--api-url", default=os.getenv("MARBLE_API_URL", "http://127.0.0.1:8080"))
    parser.add_argument("--api-key", default=os.getenv("MARBLE_API_KEY"))
    parser.add_argument("--scenario-id", default=os.getenv("SCENARIO_ID"))
    parser.add_argument(
        "--seed-parent-records",
        action="store_true",
        help="Ingest linked company/account records before the run.",
    )
    return parser


def parse_config() -> Config:
    args = build_parser().parse_args()
    if args.vus <= 0:
        raise SystemExit("--vus must be greater than 0")
    if args.duration <= 0:
        raise SystemExit("--duration must be greater than 0")
    if args.timeout <= 0:
        raise SystemExit("--timeout must be greater than 0")
    if not args.api_key:
        raise SystemExit("set --api-key or MARBLE_API_KEY")
    if not args.scenario_id:
        raise SystemExit("set --scenario-id or SCENARIO_ID")
    return Config(
        vus=args.vus,
        duration_seconds=args.duration,
        transaction_value=args.transaction_value,
        account_past_balance=args.account_past_balance,
        timeout_seconds=args.timeout,
        output=args.output,
        api_url=args.api_url.rstrip("/"),
        api_key=args.api_key,
        scenario_id=args.scenario_id,
        seed_parent_records=args.seed_parent_records,
    )


async def run_trial(config: Config) -> dict[str, Any]:
    harness = MarbleThroughputHarness(to_base_config(config))
    try:
        print("bootstrapping API readiness...")
        await harness.bootstrap()
        print(f"bootstrap complete; scenario_id={harness.scenario_id}")
        print(f"running closed-loop load for {config.duration_seconds:.0f}s with {config.vus} VUs...")
        metrics, elapsed = await run_closed_loop(harness, config.vus, config.duration_seconds)
        summary = summarize_metrics(config, harness, metrics, elapsed)
        output_path = Path(config.output)
        output_path.parent.mkdir(parents=True, exist_ok=True)
        output_path.write_text(json.dumps(summary, indent=2, default=str) + "\n", encoding="utf-8")
        return summary
    finally:
        await harness.close()


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
