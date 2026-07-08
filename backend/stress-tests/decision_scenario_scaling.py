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

from decision_rule_complexity_scaling import (
    DEFAULT_VARIANTS,
    RuleComplexityHarness,
    VariantDefinition,
    build_variant,
)
from decision_throughput_limit import (
    Config as BaseConfig,
    Metrics,
    add_error,
    environment_metadata,
    format_optional,
    percentile,
    unique_name,
    utc_now,
)


DEFAULT_SCENARIO_COUNTS = [1, 5, 10]
DEFAULT_RULES_PER_SCENARIO = [1, 5, 10]
DEFAULT_COMPLEXITIES = ["baseline_payload", "mixed_heavy"]


@dataclass(frozen=True)
class Config:
    scenario_counts: list[int]
    rules_per_scenario: list[int]
    complexities: list[str]
    vus: list[int]
    duration_seconds: float
    amount: int
    timeout_seconds: float
    output_dir: str
    related_seed_count: int
    data_model_url: str
    ingestion_url: str
    decision_engine_url: str
    auth_token: str | None
    scenario_threshold: int
    ingestion_database_url: str | None


@dataclass
class ClosedLoopMetrics(Metrics):
    per_vu_successes: dict[int, int] | None = None

    def __post_init__(self) -> None:
        super().__post_init__()
        if self.per_vu_successes is None:
            self.per_vu_successes = {}


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


class ScenarioScalingHarness(RuleComplexityHarness):
    def __init__(
        self,
        config: BaseConfig,
        complexity: str,
        related_seed_count: int,
        ingestion_database_url: str | None,
        scenario_count: int,
        rules_per_scenario: int,
    ) -> None:
        super().__init__(config, complexity, related_seed_count, ingestion_database_url)
        self.complexity = complexity
        self.scenario_count = scenario_count
        self.rules_per_scenario = rules_per_scenario
        self.scenario_ids: list[str] = []
        self.rule_ids: list[str] = []

    async def bootstrap(self) -> None:
        await self.wait_until_ready(self.data_model, "data-model")
        await self.wait_until_ready(self.ingestion, "ingestion")
        await self.wait_until_ready(self.decision_engine, "decision-engine")

        tenant = (
            await self.request(
                self.data_model,
                "POST",
                "/v1/tenants",
                201,
                json={"name": unique_name("scenario_scaling_tenant"), "external_key": unique_name("scenario_scaling_ext")},
            )
        )["tenant"]
        self.tenant_id = tenant["id"]
        await self.request(self.data_model, "POST", f"/v1/tenants/{self.tenant_id}/provision", 200)
        await self.bootstrap_model()

        self.variant = build_variant(
            self.complexity,
            self.object_type,
            self.owner_id,
            self.related_seed_count,
            self.config.scenario_threshold,
        )
        if self.ingestion_database_url:
            await asyncio.to_thread(self.materialize_ingestion_schema, self.ingestion_database_url)
        await self.seed_variant_data(self.variant)
        if self.variant.seed_decision_history:
            await self.seed_prior_review_decision()
        await self.bootstrap_scaled_scenarios(self.variant)

    async def bootstrap_scaled_scenarios(self, variant: VariantDefinition) -> None:
        self.scenario_ids = []
        self.rule_ids = []
        for scenario_index in range(self.scenario_count):
            scenario = (
                await self.request(
                    self.decision_engine,
                    "POST",
                    f"/v1/tenants/{self.tenant_id}/scenarios",
                    201,
                    json={
                        "name": unique_name(f"scenario_scaling_{variant.name}_{scenario_index}"),
                        "trigger_object_type": self.object_type,
                    },
                )
            )["scenario"]
            scenario_id = scenario["id"]
            iteration = (
                await self.request(
                    self.decision_engine,
                    "POST",
                    f"/v1/tenants/{self.tenant_id}/scenarios/{scenario_id}/iterations",
                    201,
                )
            )["iteration"]
            await self.request(
                self.decision_engine,
                "PUT",
                f"/v1/tenants/{self.tenant_id}/scenarios/{scenario_id}/iterations/{iteration['id']}",
                200,
                json={
                    "trigger_formula": {"constant": True},
                    "score_review_threshold": 10,
                    "score_block_and_review_threshold": 30,
                    "score_decline_threshold": 60,
                    "schedule": "",
                },
            )
            for rule_index in range(self.rules_per_scenario):
                rule = (
                    await self.request(
                        self.decision_engine,
                        "POST",
                        f"/v1/tenants/{self.tenant_id}/scenarios/{scenario_id}/iterations/{iteration['id']}/rules",
                        201,
                        json={
                            "display_order": rule_index + 1,
                            "name": f"{variant.name}_{rule_index + 1}",
                            "description": variant.description,
                            "formula": variant.formula,
                            "score_modifier": 25,
                            "rule_group": "scenario_scaling",
                            "stable_rule_id": unique_name(f"scenario_scaling_{variant.name}_{scenario_index}_{rule_index}"),
                        },
                    )
                )["rule"]
                self.rule_ids.append(rule["id"])
            validation = await self.request(
                self.decision_engine,
                "POST",
                f"/v1/tenants/{self.tenant_id}/scenarios/{scenario_id}/iterations/{iteration['id']}/validate",
                200,
            )
            if validation.get("validation", {}).get("valid") is not True:
                raise RuntimeError(f"iteration validation failed for {variant.name}: {json.dumps(validation, default=str)}")
            await self.request(
                self.decision_engine,
                "POST",
                f"/v1/tenants/{self.tenant_id}/scenarios/{scenario_id}/iterations/{iteration['id']}/commit",
                200,
            )
            await self.request(
                self.decision_engine,
                "POST",
                f"/v1/tenants/{self.tenant_id}/scenarios/{scenario_id}/publications",
                200,
                json={"action": "publish", "iteration_id": iteration["id"]},
            )
            self.scenario_ids.append(scenario_id)

        if self.scenario_ids:
            self.scenario_id = self.scenario_ids[0]
        if self.rule_ids:
            self.rule_id = self.rule_ids[0]

    async def evaluate_once(self) -> None:
        assert self.variant is not None
        object_id = self.stable_object_id if self.variant.stable_object_id else self.next_object_id()
        payload = self.payload(object_id)
        await self.request(
            self.decision_engine,
            "POST",
            f"/v1/tenants/{self.tenant_id}/decisions/all",
            200,
            json={"object_id": object_id, "object_type": self.object_type, "fields": payload},
        )


async def run_one_request(harness: ScenarioScalingHarness, metrics: ClosedLoopMetrics, vu_id: int) -> None:
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
    harness: ScenarioScalingHarness,
    metrics: ClosedLoopMetrics,
    start_gate: asyncio.Event,
    deadline: float,
) -> None:
    await start_gate.wait()
    while time.perf_counter() < deadline:
        await run_one_request(harness, metrics, vu_id)


async def run_closed_loop(harness: ScenarioScalingHarness, vus: int, duration_seconds: float) -> tuple[ClosedLoopMetrics, float]:
    metrics = ClosedLoopMetrics()
    start_gate = asyncio.Event()
    started_at = time.perf_counter()
    deadline = started_at + duration_seconds
    tasks = [asyncio.create_task(worker(vu_id, harness, metrics, start_gate, deadline)) for vu_id in range(vus)]
    start_gate.set()
    await asyncio.gather(*tasks)
    return metrics, time.perf_counter() - started_at


def trial_output_path(config: Config, complexity: str, scenario_count: int, rules_per_scenario: int, vus: int) -> str:
    duration_label = f"{config.duration_seconds:g}s"
    return str(
        Path(config.output_dir)
        / f"trial-{complexity}-{scenario_count}-scenarios-{rules_per_scenario}-rules-{vus}-vus-{duration_label}.json"
    )


def summarize_trial(
    config: Config,
    harness: ScenarioScalingHarness,
    output: str,
    metrics: ClosedLoopMetrics,
    elapsed_seconds: float,
    vus: int,
) -> dict[str, Any]:
    assert harness.variant is not None
    latencies = metrics.latencies_ms or []
    per_vu_counts = list((metrics.per_vu_successes or {}).values())
    achieved_eps = metrics.successes / elapsed_seconds if elapsed_seconds > 0 else 0.0
    expected_decision_writes = metrics.successes * harness.scenario_count
    expected_rule_execution_writes = metrics.successes * harness.scenario_count * harness.rules_per_scenario
    return {
        "summary_version": 1,
        "test": {
            "name": "decision_engine_scenario_scaling",
            "objective": "Measure decision evaluation performance as live scenarios, rules per scenario, and rule complexity scale.",
            "system_under_test": "POST /v1/tenants/{tenantId}/decisions/all",
            "load_model": "Closed loop: each VU sends a new request immediately after its previous request completes.",
            "sustainability_definition": "0% errors and 0% timeouts during measured run.",
        },
        "environment": environment_metadata(to_base_config(config, output, vus)),
        "setup": {
            "complexity": harness.variant.name,
            "variant_description": harness.variant.description,
            "tenant_id": harness.tenant_id,
            "object_type": harness.object_type,
            "account_object_type": harness.account_object_type,
            "scenario_count": harness.scenario_count,
            "rules_per_scenario": harness.rules_per_scenario,
            "total_rules_per_request": harness.scenario_count * harness.rules_per_scenario,
            "scenario_ids": harness.scenario_ids,
            "rule_count": len(harness.rule_ids),
            "seeded_counts": harness.seeded_counts,
            "related_seed_count": config.related_seed_count,
            "stable_object_id": harness.variant.stable_object_id,
        },
        "run": {
            "complexity": harness.variant.name,
            "configured_vus": vus,
            "scenario_count": harness.scenario_count,
            "rules_per_scenario": harness.rules_per_scenario,
            "total_rules_per_request": harness.scenario_count * harness.rules_per_scenario,
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
            "achieved_successful_requests_per_second": achieved_eps,
            "expected_decision_writes_per_second": expected_decision_writes / elapsed_seconds if elapsed_seconds > 0 else 0.0,
            "expected_rule_execution_writes_per_second": expected_rule_execution_writes / elapsed_seconds if elapsed_seconds > 0 else 0.0,
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
            "sustainable": metrics.failures == 0 and metrics.timeouts == 0,
            "elapsed_seconds": elapsed_seconds,
            "requested_duration_seconds": config.duration_seconds,
        },
        "sample_errors": metrics.errors or [],
    }


def baseline_key(trial: dict[str, Any]) -> tuple[int, int, int]:
    run = trial["run"]
    return run["scenario_count"], run["rules_per_scenario"], run["configured_vus"]


def apply_baseline_ratios(trials: list[dict[str, Any]]) -> None:
    baseline_by_key = {
        baseline_key(trial): trial
        for trial in trials
        if trial["run"]["complexity"] == "baseline_payload"
    }
    for trial in trials:
        baseline = baseline_by_key.get(baseline_key(trial))
        ratios: dict[str, float | None] = {}
        if baseline is not None:
            for key in ["p50", "p95", "p99", "max", "avg"]:
                value = trial["latency_ms"].get(key)
                base = baseline["latency_ms"].get(key)
                ratios[f"latency_{key}_ratio"] = (value / base) if value is not None and base else None
            eps = trial["rates"]["achieved_successful_requests_per_second"]
            base_eps = baseline["rates"]["achieved_successful_requests_per_second"]
            ratios["achieved_eps_ratio"] = (eps / base_eps) if base_eps else None
        trial["baseline_comparison"] = {
            "baseline_complexity": "baseline_payload",
            "baseline_available": baseline is not None,
            "baseline_key": {
                "scenario_count": trial["run"]["scenario_count"],
                "rules_per_scenario": trial["run"]["rules_per_scenario"],
                "configured_vus": trial["run"]["configured_vus"],
            },
            **ratios,
        }


def compact_ranking_item(trial: dict[str, Any], ratio_key: str) -> dict[str, Any]:
    run = trial["run"]
    return {
        "complexity": run["complexity"],
        "vus": run["configured_vus"],
        "scenario_count": run["scenario_count"],
        "rules_per_scenario": run["rules_per_scenario"],
        "total_rules_per_request": run["total_rules_per_request"],
        ratio_key: trial.get("baseline_comparison", {}).get(ratio_key),
        "achieved_rps": trial["rates"]["achieved_successful_requests_per_second"],
        "p95_ms": trial["latency_ms"]["p95"],
        "p99_ms": trial["latency_ms"]["p99"],
        "max_ms": trial["latency_ms"]["max"],
        "failures": trial["workload_counts"]["failed_requests"],
        "timeouts": trial["workload_counts"]["timeouts"],
    }


def aggregate_summary(config: Config, trials: list[dict[str, Any]]) -> dict[str, Any]:
    apply_baseline_ratios(trials)
    run_config = asdict(config)
    run_config["auth_token"] = "set" if config.auth_token else None
    run_config["ingestion_database_url"] = "set" if config.ingestion_database_url else None
    ranked_by_p95_ratio = sorted(
        trials,
        key=lambda item: item.get("baseline_comparison", {}).get("latency_p95_ratio") or 0,
        reverse=True,
    )
    ranked_by_eps_ratio = sorted(
        trials,
        key=lambda item: item.get("baseline_comparison", {}).get("achieved_eps_ratio") or 1,
    )
    ranked_by_p95 = sorted(trials, key=lambda item: item["latency_ms"].get("p95") or 0, reverse=True)
    ranked_by_eps = sorted(trials, key=lambda item: item["rates"]["achieved_successful_requests_per_second"])
    return {
        "summary_version": 1,
        "test": {
            "name": "decision_engine_scenario_scaling",
            "objective": "Compare live-scenario fanout, rules-per-scenario, and rule-complexity scaling.",
        },
        "environment": environment_metadata(to_base_config(config, str(Path(config.output_dir) / "summary.json"), max(config.vus))),
        "run": run_config,
        "trial_count": len(trials),
        "trials": trials,
        "rankings": {
            "highest_p95_latency_ratio": [
                compact_ranking_item(item, "latency_p95_ratio")
                for item in ranked_by_p95_ratio
                if item.get("baseline_comparison", {}).get("latency_p95_ratio") is not None
            ],
            "lowest_achieved_eps_ratio": [
                compact_ranking_item(item, "achieved_eps_ratio")
                for item in ranked_by_eps_ratio
                if item.get("baseline_comparison", {}).get("achieved_eps_ratio") is not None
            ],
            "highest_p95_latency": [compact_ranking_item(item, "latency_p95_ratio") for item in ranked_by_p95],
            "lowest_achieved_rps": [compact_ranking_item(item, "achieved_eps_ratio") for item in ranked_by_eps],
        },
    }


def print_trial_summary(summary: dict[str, Any]) -> None:
    latency = summary["latency_ms"]
    rates = summary["rates"]
    counts = summary["workload_counts"]
    run = summary["run"]
    print(
        f"  {run['complexity']} @ {run['scenario_count']} scenarios x {run['rules_per_scenario']} rules, "
        f"{run['configured_vus']} VUs: {rates['achieved_successful_requests_per_second']:.2f} RPS, "
        f"p95 {format_optional(latency['p95'])} ms, "
        f"p99 {format_optional(latency['p99'])} ms, "
        f"failures {counts['failed_requests']}, timeouts {counts['timeouts']}"
    )


def parse_csv_list(value: str, valid: set[str] | None = None) -> list[str]:
    items = [item.strip() for item in value.split(",") if item.strip()]
    if valid is not None:
        unknown = [item for item in items if item not in valid]
        if unknown:
            raise SystemExit(f"unknown values: {', '.join(unknown)}; expected one of {', '.join(sorted(valid))}")
    if not items:
        raise SystemExit("list must not be empty")
    return items


def parse_int_list(value: str, name: str) -> list[int]:
    try:
        items = [int(item) for item in parse_csv_list(value)]
    except ValueError as exc:
        raise SystemExit(f"{name} values must be integers") from exc
    if any(item <= 0 for item in items):
        raise SystemExit(f"{name} values must be greater than 0")
    return items


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="Decision-engine scenario and rules-per-scenario scaling stress test.")
    parser.add_argument("--scenario-counts", default=",".join(str(item) for item in DEFAULT_SCENARIO_COUNTS))
    parser.add_argument("--rules-per-scenario", default=",".join(str(item) for item in DEFAULT_RULES_PER_SCENARIO))
    parser.add_argument("--complexities", default=",".join(DEFAULT_COMPLEXITIES), help="Comma-separated complexity names.")
    parser.add_argument("--vus", default="5", help="Comma-separated closed-loop VU levels.")
    parser.add_argument("--duration", type=float, default=60.0, help="Measured run duration in seconds.")
    parser.add_argument("--amount", type=int, default=1800, help="Amount value used in generated payloads.")
    parser.add_argument("--timeout", type=float, default=30.0, help="Per-request timeout in seconds.")
    parser.add_argument("--scenario-threshold", type=int, default=1000, help="Simple amount rule threshold.")
    parser.add_argument("--related-seed-count", type=int, default=100, help="Seeded related transaction count.")
    parser.add_argument("--output-dir", help="Output directory. Defaults to timestamped stress-tests/scenario-scaling-runs folder.")
    parser.add_argument("--data-model-url", default=os.getenv("DATA_MODEL_URL", "http://127.0.0.1:8080"))
    parser.add_argument("--ingestion-url", default=os.getenv("INGESTION_URL", "http://127.0.0.1:8081"))
    parser.add_argument("--decision-engine-url", default=os.getenv("DECISION_ENGINE_URL", "http://127.0.0.1:8082"))
    parser.add_argument("--auth-token", default=os.getenv("SERVICE_AUTH_TOKEN"))
    parser.add_argument(
        "--ingestion-database-url",
        default=os.getenv("INGESTION_DATABASE_URL"),
        help="Optional Postgres URL used to materialize tenant tables when ingestion uses a separate DB.",
    )
    return parser


def parse_config() -> Config:
    args = build_parser().parse_args()
    complexities = parse_csv_list(args.complexities, set(DEFAULT_VARIANTS))
    if "baseline_payload" not in complexities:
        complexities = ["baseline_payload", *complexities]
    if args.duration <= 0:
        raise SystemExit("--duration must be greater than 0")
    if args.timeout <= 0:
        raise SystemExit("--timeout must be greater than 0")
    if args.related_seed_count <= 0:
        raise SystemExit("--related-seed-count must be greater than 0")
    output_dir = args.output_dir or str(Path("stress-tests/scenario-scaling-runs") / utc_now().replace(":", "").replace(".", "-"))
    return Config(
        scenario_counts=parse_int_list(args.scenario_counts, "--scenario-counts"),
        rules_per_scenario=parse_int_list(args.rules_per_scenario, "--rules-per-scenario"),
        complexities=complexities,
        vus=parse_int_list(args.vus, "--vus"),
        duration_seconds=args.duration,
        amount=args.amount,
        timeout_seconds=args.timeout,
        output_dir=output_dir,
        related_seed_count=args.related_seed_count,
        data_model_url=args.data_model_url.rstrip("/"),
        ingestion_url=args.ingestion_url.rstrip("/"),
        decision_engine_url=args.decision_engine_url.rstrip("/"),
        auth_token=args.auth_token,
        scenario_threshold=args.scenario_threshold,
        ingestion_database_url=args.ingestion_database_url,
    )


async def run_trial(config: Config, complexity: str, scenario_count: int, rules_per_scenario: int, vus: int) -> dict[str, Any]:
    output = trial_output_path(config, complexity, scenario_count, rules_per_scenario, vus)
    harness = ScenarioScalingHarness(
        to_base_config(config, output, vus),
        complexity,
        config.related_seed_count,
        config.ingestion_database_url,
        scenario_count,
        rules_per_scenario,
    )
    try:
        print(f"bootstrapping {complexity}: {scenario_count} scenarios x {rules_per_scenario} rules for {vus} VUs...")
        await harness.bootstrap()
        print(f"running {complexity}: {scenario_count} scenarios x {rules_per_scenario} rules for {config.duration_seconds:.0f}s...")
        metrics, elapsed = await run_closed_loop(harness, vus, config.duration_seconds)
        summary = summarize_trial(config, harness, output, metrics, elapsed, vus)
        output_path = Path(output)
        output_path.parent.mkdir(parents=True, exist_ok=True)
        output_path.write_text(json.dumps(summary, indent=2, default=str) + "\n")
        print_trial_summary(summary)
        return summary
    finally:
        await harness.close()


async def async_main() -> int:
    config = parse_config()
    trials = []
    Path(config.output_dir).mkdir(parents=True, exist_ok=True)
    for vus in config.vus:
        for scenario_count in config.scenario_counts:
            for rules_per_scenario in config.rules_per_scenario:
                for complexity in config.complexities:
                    trials.append(await run_trial(config, complexity, scenario_count, rules_per_scenario, vus))
    summary = aggregate_summary(config, trials)
    for trial in trials:
        Path(trial["run"]["output"]).write_text(json.dumps(trial, indent=2, default=str) + "\n")
    summary_path = Path(config.output_dir) / "summary.json"
    summary_path.write_text(json.dumps(summary, indent=2, default=str) + "\n")
    print("")
    print("Scenario Scaling Summary")
    print(f"  trials: {len(trials)}")
    print(f"  output: {summary_path}")
    print("  highest p95 latency ratios:")
    for item in summary["rankings"]["highest_p95_latency_ratio"][:10]:
        print(
            f"    {item['complexity']} @ {item['scenario_count']} scenarios x {item['rules_per_scenario']} rules, "
            f"{item['vus']} VUs: {item['latency_p95_ratio']:.2f}x, p95 {format_optional(item['p95_ms'])} ms"
        )
    return 0 if all(trial["result"]["sustainable"] for trial in trials) else 1


def main() -> None:
    raise SystemExit(asyncio.run(async_main()))


if __name__ == "__main__":
    main()
