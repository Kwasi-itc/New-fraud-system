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

from decision_throughput_limit import (
    Config as BaseConfig,
    Metrics,
    ThroughputHarness,
    add_error,
    environment_metadata,
    format_optional,
    percentile,
)


@dataclass(frozen=True)
class Config:
    vus: int
    duration_seconds: float
    amount: int
    timeout_seconds: float
    output: str
    data_model_url: str
    ingestion_url: str
    decision_engine_url: str
    auth_token: str | None
    scenario_threshold: int


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
        amount=config.amount,
        timeout_seconds=config.timeout_seconds,
        output=config.output,
        data_model_url=config.data_model_url,
        ingestion_url=config.ingestion_url,
        decision_engine_url=config.decision_engine_url,
        auth_token=config.auth_token,
        scenario_threshold=config.scenario_threshold,
    )


async def run_one_request(harness: ThroughputHarness, metrics: ClosedLoopMetrics, vu_id: int) -> None:
    started_at = time.perf_counter()
    metrics.attempted += 1
    try:
        await harness.evaluate_once()
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
        metrics.successes += 1
        assert metrics.latencies_ms is not None
        metrics.latencies_ms.append((time.perf_counter() - started_at) * 1000.0)
        assert metrics.per_vu_successes is not None
        metrics.per_vu_successes[vu_id] = metrics.per_vu_successes.get(vu_id, 0) + 1


async def worker(
    vu_id: int,
    harness: ThroughputHarness,
    metrics: ClosedLoopMetrics,
    start_gate: asyncio.Event,
    deadline: float,
) -> None:
    await start_gate.wait()
    while time.perf_counter() < deadline:
        await run_one_request(harness, metrics, vu_id)


async def run_closed_loop(harness: ThroughputHarness, vus: int, duration_seconds: float) -> tuple[ClosedLoopMetrics, float]:
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
    harness: ThroughputHarness,
    metrics: ClosedLoopMetrics,
    elapsed_seconds: float,
) -> dict[str, Any]:
    latencies = metrics.latencies_ms or []
    per_vu_counts = list((metrics.per_vu_successes or {}).values())
    achieved_eps = metrics.successes / elapsed_seconds if elapsed_seconds > 0 else 0.0
    sustainable = metrics.failures == 0 and metrics.timeouts == 0
    return {
        "summary_version": 1,
        "test": {
            "name": "decision_engine_closed_loop_vus",
            "objective": "Measure achieved direct decision evaluations per second with fixed always-active VUs.",
            "system_under_test": "POST /v1/tenants/{tenantId}/scenarios/{scenarioId}/evaluate",
            "load_model": "Closed loop: each VU sends a new request immediately after its previous request completes.",
            "sustainability_definition": "0% errors and 0% timeouts during measured run.",
        },
        "environment": environment_metadata(to_base_config(config)),
        "setup": {
            "tenant_id": harness.tenant_id,
            "object_type": harness.object_type,
            "scenario_id": harness.scenario_id,
            "rule_id": harness.rule_id,
            "scenario_count": 1,
            "rules_per_scenario": 1,
            "rule_shape": "amount > scenario_threshold",
            "scenario_threshold": config.scenario_threshold,
        },
        "run": asdict(config) | {"auth_token": "set" if config.auth_token else None},
        "workload_counts": {
            "configured_vus": config.vus,
            "completed_evaluations": metrics.completed,
            "successful_evaluations": metrics.successes,
            "failed_evaluations": metrics.failures,
            "timeouts": metrics.timeouts,
            "dropped_requests": 0,
        },
        "rates": {
            "achieved_successful_evaluations_per_second": achieved_eps,
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
    print("Decision Engine Closed-Loop VU Summary")
    print(f"  VUs: {counts['configured_vus']}")
    print(f"  achieved EPS: {rates['achieved_successful_evaluations_per_second']:.2f}")
    print(f"  successful: {counts['successful_evaluations']}")
    print(f"  failed: {counts['failed_evaluations']}")
    print(f"  timeouts: {counts['timeouts']}")
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
    parser = argparse.ArgumentParser(description="Closed-loop VU decision-engine stress test.")
    parser.add_argument("--vus", type=int, required=True, help="Constant number of active VUs.")
    parser.add_argument("--duration", type=float, required=True, help="Measured run duration in seconds.")
    parser.add_argument("--amount", type=int, default=1800, help="Amount value used in generated payloads.")
    parser.add_argument("--timeout", type=float, default=30.0, help="Per-request timeout in seconds.")
    parser.add_argument("--scenario-threshold", type=int, default=1000, help="Simple amount rule threshold.")
    parser.add_argument("--output", default="stress-tests/closed-loop-vus-summary.json", help="Summary JSON output path.")
    parser.add_argument("--data-model-url", default=os.getenv("DATA_MODEL_URL", "http://127.0.0.1:8080"))
    parser.add_argument("--ingestion-url", default=os.getenv("INGESTION_URL", "http://127.0.0.1:8081"))
    parser.add_argument("--decision-engine-url", default=os.getenv("DECISION_ENGINE_URL", "http://127.0.0.1:8082"))
    parser.add_argument("--auth-token", default=os.getenv("SERVICE_AUTH_TOKEN"))
    return parser


def parse_config() -> Config:
    args = build_parser().parse_args()
    if args.vus <= 0:
        raise SystemExit("--vus must be greater than 0")
    if args.duration <= 0:
        raise SystemExit("--duration must be greater than 0")
    if args.timeout <= 0:
        raise SystemExit("--timeout must be greater than 0")
    return Config(
        vus=args.vus,
        duration_seconds=args.duration,
        amount=args.amount,
        timeout_seconds=args.timeout,
        output=args.output,
        data_model_url=args.data_model_url.rstrip("/"),
        ingestion_url=args.ingestion_url.rstrip("/"),
        decision_engine_url=args.decision_engine_url.rstrip("/"),
        auth_token=args.auth_token,
        scenario_threshold=args.scenario_threshold,
    )


async def run_trial(config: Config) -> dict[str, Any]:
    harness = ThroughputHarness(to_base_config(config))
    try:
        print("bootstrapping tenant/model/scenario...")
        await harness.bootstrap()
        print("bootstrap complete")
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
