from __future__ import annotations

import argparse
import asyncio
import json
import os
import subprocess
import sys
import time
from dataclasses import asdict, dataclass
from pathlib import Path
from typing import Any

import httpx

from decision_throughput_limit import Config, run_trial, utc_now


DEFAULT_OUTPUT_DIR = "stress-tests/throughput-runs"


@dataclass(frozen=True)
class OrchestratorConfig:
    target_eps: int
    start_eps: int
    rate_floor: int
    trial_duration_seconds: float
    warmup_duration_seconds: float
    timeout_seconds: float
    cooldown_duration_seconds: float
    health_timeout_seconds: float
    amount: int
    scenario_threshold: int
    min_vus: int
    vus_multiplier: int
    max_vus: int
    output_dir: str
    data_model_url: str
    ingestion_url: str
    decision_engine_url: str
    auth_token: str | None
    postgres_container: str
    postgres_user: str
    postgres_db: str
    skip_db_diagnostics: bool


@dataclass
class TrialRecord:
    rate: int
    vus: int
    output: str
    sustainable: bool
    target_met: bool
    failures: int
    timeouts: int
    dropped_requests: int
    achieved_eps: float
    summary: dict[str, Any]


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="Automatically find maximum sustainable decision-engine throughput.")
    parser.add_argument("--target-eps", type=int, default=1000, help="Upper EPS target to search for.")
    parser.add_argument("--start-eps", type=int, default=100, help="Initial EPS for exponential search.")
    parser.add_argument("--rate-floor", type=int, default=25, help="Stop binary search when rate gap is <= this value.")
    parser.add_argument("--trial-duration", type=float, default=60.0, help="Measured duration for each trial.")
    parser.add_argument("--warmup-duration", type=float, default=30.0, help="Warmup duration for each trial.")
    parser.add_argument("--timeout", type=float, default=30.0, help="Per-request timeout in seconds.")
    parser.add_argument("--cooldown-duration", type=float, default=30.0, help="Cooldown duration after each trial attempt.")
    parser.add_argument("--health-timeout", type=float, default=60.0, help="Readiness wait timeout after cooldown.")
    parser.add_argument("--amount", type=int, default=1800, help="Amount value used in generated payloads.")
    parser.add_argument("--scenario-threshold", type=int, default=1000, help="Simple amount rule threshold.")
    parser.add_argument("--min-vus", type=int, default=50, help="Minimum VUs for any trial.")
    parser.add_argument("--vus-multiplier", type=int, default=2, help="VUs are rate * multiplier, unless below min-vus.")
    parser.add_argument("--max-vus", type=int, default=2500, help="Maximum VUs allowed for any trial.")
    parser.add_argument(
        "--output-dir",
        default=None,
        help="Directory for trial and final reports. Defaults to a timestamped folder under stress-tests/throughput-runs.",
    )
    parser.add_argument("--data-model-url", default=os.getenv("DATA_MODEL_URL", "http://127.0.0.1:8080"))
    parser.add_argument("--ingestion-url", default=os.getenv("INGESTION_URL", "http://127.0.0.1:8081"))
    parser.add_argument("--decision-engine-url", default=os.getenv("DECISION_ENGINE_URL", "http://127.0.0.1:8082"))
    parser.add_argument("--auth-token", default=os.getenv("SERVICE_AUTH_TOKEN"))
    parser.add_argument("--postgres-container", default=os.getenv("POSTGRES_CONTAINER", "new-fraud-system-postgres-1"))
    parser.add_argument("--postgres-user", default=os.getenv("POSTGRES_USER", "fraud"))
    parser.add_argument("--postgres-db", default=os.getenv("POSTGRES_DB", "decision_engine"))
    parser.add_argument("--skip-db-diagnostics", action="store_true", help="Skip docker/psql database contamination diagnostics.")
    return parser


def parse_config() -> OrchestratorConfig:
    args = build_parser().parse_args()
    if args.target_eps <= 0:
        raise SystemExit("--target-eps must be greater than 0")
    if args.start_eps <= 0:
        raise SystemExit("--start-eps must be greater than 0")
    if args.start_eps > args.target_eps:
        raise SystemExit("--start-eps must be less than or equal to --target-eps")
    if args.rate_floor <= 0:
        raise SystemExit("--rate-floor must be greater than 0")
    if args.trial_duration <= 0:
        raise SystemExit("--trial-duration must be greater than 0")
    if args.warmup_duration < 0:
        raise SystemExit("--warmup-duration must be greater than or equal to 0")
    if args.timeout <= 0:
        raise SystemExit("--timeout must be greater than 0")
    if args.cooldown_duration < 0:
        raise SystemExit("--cooldown-duration must be greater than or equal to 0")
    if args.health_timeout <= 0:
        raise SystemExit("--health-timeout must be greater than 0")
    if args.min_vus <= 0:
        raise SystemExit("--min-vus must be greater than 0")
    if args.vus_multiplier <= 0:
        raise SystemExit("--vus-multiplier must be greater than 0")
    if args.max_vus < args.min_vus:
        raise SystemExit("--max-vus must be greater than or equal to --min-vus")
    output_dir = args.output_dir or str(Path(DEFAULT_OUTPUT_DIR) / utc_now().replace(":", "").replace(".", "-"))
    return OrchestratorConfig(
        target_eps=args.target_eps,
        start_eps=args.start_eps,
        rate_floor=args.rate_floor,
        trial_duration_seconds=args.trial_duration,
        warmup_duration_seconds=args.warmup_duration,
        timeout_seconds=args.timeout,
        cooldown_duration_seconds=args.cooldown_duration,
        health_timeout_seconds=args.health_timeout,
        amount=args.amount,
        scenario_threshold=args.scenario_threshold,
        min_vus=args.min_vus,
        vus_multiplier=args.vus_multiplier,
        max_vus=args.max_vus,
        output_dir=output_dir,
        data_model_url=args.data_model_url.rstrip("/"),
        ingestion_url=args.ingestion_url.rstrip("/"),
        decision_engine_url=args.decision_engine_url.rstrip("/"),
        auth_token=args.auth_token,
        postgres_container=args.postgres_container,
        postgres_user=args.postgres_user,
        postgres_db=args.postgres_db,
        skip_db_diagnostics=args.skip_db_diagnostics,
    )


def choose_vus(rate: int, cfg: OrchestratorConfig) -> int:
    return min(max(cfg.min_vus, rate * cfg.vus_multiplier), cfg.max_vus)


def trial_output_path(cfg: OrchestratorConfig, rate: int, vus: int, suffix: str = "") -> str:
    safe_suffix = f"-{suffix}" if suffix else ""
    return str(Path(cfg.output_dir) / f"trial-{rate}-eps-{vus}-vus{safe_suffix}.json")


def build_trial_config(cfg: OrchestratorConfig, rate: int, vus: int, output: str) -> Config:
    return Config(
        rate=rate,
        vus=vus,
        duration_seconds=cfg.trial_duration_seconds,
        warmup_duration_seconds=cfg.warmup_duration_seconds,
        amount=cfg.amount,
        timeout_seconds=cfg.timeout_seconds,
        output=output,
        data_model_url=cfg.data_model_url,
        ingestion_url=cfg.ingestion_url,
        decision_engine_url=cfg.decision_engine_url,
        auth_token=cfg.auth_token,
        scenario_threshold=cfg.scenario_threshold,
    )


def record_from_summary(rate: int, vus: int, output: str, summary: dict[str, Any]) -> TrialRecord:
    counts = summary["workload_counts"]
    rates = summary["rates"]
    result = summary["result"]
    return TrialRecord(
        rate=rate,
        vus=vus,
        output=output,
        sustainable=bool(result["sustainable"]),
        target_met=bool(result["target_met"]),
        failures=int(counts["failed_evaluations"]),
        timeouts=int(counts["timeouts"]),
        dropped_requests=int(counts["dropped_requests"]),
        achieved_eps=float(rates["achieved_successful_evaluations_per_second"]),
        summary=summary,
    )


def failed_only_from_drops(record: TrialRecord) -> bool:
    return record.failures == 0 and record.timeouts == 0 and record.dropped_requests > 0


def run_command(args: list[str], timeout_seconds: float = 10.0) -> str | None:
    try:
        result = subprocess.run(args, capture_output=True, text=True, timeout=timeout_seconds, check=False)
    except (OSError, subprocess.SubprocessError):
        return None
    output = (result.stdout or result.stderr).strip()
    return output or None


def collect_db_diagnostics(cfg: OrchestratorConfig) -> dict[str, Any]:
    if cfg.skip_db_diagnostics:
        return {"available": False, "reason": "skipped"}
    sql = (
        "select 'decisions', count(*) from core.decisions "
        "union all select 'rule_executions', count(*) from core.rule_executions "
        "union all select 'outbox_pending', count(*) from core.outbox_events where status = 'pending' "
        "union all select 'outbox_failed', count(*) from core.outbox_events where status = 'failed' "
        "union all select 'scenarios', count(*) from core.scenarios "
        "union all select 'scenario_iterations', count(*) from core.scenario_iterations"
    )
    output = run_command(
        [
            "docker",
            "exec",
            cfg.postgres_container,
            "psql",
            "-U",
            cfg.postgres_user,
            "-d",
            cfg.postgres_db,
            "-t",
            "-A",
            "-F",
            ",",
            "-c",
            sql,
        ]
    )
    if output is None:
        return {"available": False, "reason": "docker_psql_unavailable"}

    values: dict[str, Any] = {"available": True, "captured_at": utc_now()}
    for line in output.splitlines():
        if "," not in line:
            continue
        key, value = line.split(",", 1)
        try:
            values[key] = int(value)
        except ValueError:
            values[key] = value
    return values


def diagnostic_warnings(diagnostics: dict[str, Any]) -> list[str]:
    if not diagnostics.get("available"):
        return [f"db_diagnostics_unavailable:{diagnostics.get('reason', 'unknown')}"]

    warnings: list[str] = []
    if int(diagnostics.get("decisions", 0)) > 0 or int(diagnostics.get("rule_executions", 0)) > 0:
        warnings.append("database_contains_previous_decision_engine_rows")
    if int(diagnostics.get("outbox_pending", 0)) > 0:
        warnings.append("outbox_backlog_present_worker_may_compete_with_api")
    if int(diagnostics.get("outbox_failed", 0)) > 0:
        warnings.append("failed_outbox_events_present")
    return warnings


async def wait_ready_url(cfg: OrchestratorConfig, name: str, base_url: str) -> None:
    headers: dict[str, str] = {}
    if cfg.auth_token:
        headers["Authorization"] = f"Bearer {cfg.auth_token}"

    deadline = time.monotonic() + cfg.health_timeout_seconds
    last_error: Exception | str | None = None
    async with httpx.AsyncClient(base_url=base_url, headers=headers, timeout=httpx.Timeout(10.0)) as client:
        while time.monotonic() < deadline:
            try:
                response = await client.get("/readyz")
                if response.status_code == 200:
                    return
                last_error = f"status {response.status_code}"
            except (httpx.HTTPError, OSError) as exc:
                last_error = exc
            await asyncio.sleep(0.5)
    detail = f": {last_error}" if last_error else ""
    raise RuntimeError(f"{name} was not ready after cooldown{detail}")


async def cooldown_and_health_check(cfg: OrchestratorConfig, reason: str) -> None:
    if cfg.cooldown_duration_seconds > 0:
        print(f"cooling down for {cfg.cooldown_duration_seconds:.0f}s after {reason}...")
        await asyncio.sleep(cfg.cooldown_duration_seconds)
    print("checking service readiness before next trial...")
    await asyncio.gather(
        wait_ready_url(cfg, "data-model", cfg.data_model_url),
        wait_ready_url(cfg, "ingestion", cfg.ingestion_url),
        wait_ready_url(cfg, "decision-engine", cfg.decision_engine_url),
    )


def add_orchestration_metadata(
    cfg: OrchestratorConfig,
    output: str,
    summary: dict[str, Any],
    db_before: dict[str, Any],
    db_after: dict[str, Any],
) -> dict[str, Any]:
    summary["orchestration"] = {
        "db_diagnostics_before": db_before,
        "db_diagnostics_after": db_after,
        "diagnostic_warnings_before": diagnostic_warnings(db_before),
        "diagnostic_warnings_after": diagnostic_warnings(db_after),
        "cooldown_duration_seconds": cfg.cooldown_duration_seconds,
        "health_timeout_seconds": cfg.health_timeout_seconds,
    }
    Path(output).write_text(json.dumps(summary, indent=2, default=str) + "\n")
    return summary


async def run_rate(cfg: OrchestratorConfig, rate: int, suffix: str = "") -> TrialRecord:
    vus = choose_vus(rate, cfg)
    output = trial_output_path(cfg, rate, vus, suffix)
    print(f"\n=== trial rate={rate} EPS vus={vus} ===")
    db_before = collect_db_diagnostics(cfg)
    summary = await run_trial(build_trial_config(cfg, rate, vus, output))
    db_after = collect_db_diagnostics(cfg)
    summary = add_orchestration_metadata(cfg, output, summary, db_before, db_after)
    record = record_from_summary(rate, vus, output, summary)
    await cooldown_and_health_check(cfg, f"rate={rate} EPS vus={vus}")

    if failed_only_from_drops(record) and vus < cfg.max_vus:
        retry_vus = min(vus * 2, cfg.max_vus)
        retry_output = trial_output_path(cfg, rate, retry_vus, f"{suffix}retry-vus".strip("-"))
        print(f"\n=== retry rate={rate} EPS vus={retry_vus} after dropped requests ===")
        retry_db_before = collect_db_diagnostics(cfg)
        retry_summary = await run_trial(build_trial_config(cfg, rate, retry_vus, retry_output))
        retry_db_after = collect_db_diagnostics(cfg)
        retry_summary = add_orchestration_metadata(cfg, retry_output, retry_summary, retry_db_before, retry_db_after)
        retry_record = record_from_summary(rate, retry_vus, retry_output, retry_summary)
        await cooldown_and_health_check(cfg, f"rate={rate} EPS vus={retry_vus} retry")
        return retry_record

    return record


def next_exponential_rate(current: int, target: int) -> int:
    if current >= target:
        return target
    return min(current * 2, target)


async def find_limit(
    cfg: OrchestratorConfig,
) -> tuple[TrialRecord | None, TrialRecord | None, list[TrialRecord], TrialRecord | None, list[TrialRecord]]:
    trials: list[TrialRecord] = []
    sustainable_candidates: list[TrialRecord] = []
    last_sustainable: TrialRecord | None = None
    first_failing: TrialRecord | None = None

    rate = cfg.start_eps
    while True:
        record = await run_rate(cfg, rate)
        trials.append(record)
        if record.sustainable:
            last_sustainable = record
            sustainable_candidates.append(record)
            if rate >= cfg.target_eps:
                break
            rate = next_exponential_rate(rate, cfg.target_eps)
            continue

        first_failing = record
        break

    if first_failing is not None and last_sustainable is not None:
        low = last_sustainable.rate
        high = first_failing.rate
        while high - low > cfg.rate_floor:
            mid = (low + high) // 2
            record = await run_rate(cfg, mid, "binary")
            trials.append(record)
            if record.sustainable:
                last_sustainable = record
                sustainable_candidates.append(record)
                low = mid
            else:
                first_failing = record
                high = mid

    confirmation: TrialRecord | None = None
    confirmation_attempts: list[TrialRecord] = []
    for candidate in sorted(sustainable_candidates, key=lambda item: item.rate, reverse=True):
        attempt = await run_rate(cfg, candidate.rate, "confirmation")
        trials.append(attempt)
        confirmation_attempts.append(attempt)
        if attempt.sustainable:
            confirmation = attempt
            break
        first_failing = attempt

    return last_sustainable, first_failing, trials, confirmation, confirmation_attempts


def recommendation(cfg: OrchestratorConfig, confirmed: TrialRecord | None) -> str:
    if confirmed is None:
        return "inconclusive"
    if confirmed.rate >= cfg.target_eps:
        return "target_met"
    return "bottleneck_investigation_needed"


def write_final_summary(
    cfg: OrchestratorConfig,
    candidate_highest: TrialRecord | None,
    first_failing: TrialRecord | None,
    trials: list[TrialRecord],
    confirmed_highest: TrialRecord | None,
    confirmation_attempts: list[TrialRecord],
    db_before: dict[str, Any],
    db_after: dict[str, Any],
) -> dict[str, Any]:
    Path(cfg.output_dir).mkdir(parents=True, exist_ok=True)
    final = {
        "summary_version": 1,
        "test": "decision_engine_throughput_limit_orchestration",
        "created_at": utc_now(),
        "command": sys.argv,
        "config": asdict(cfg) | {"auth_token": "set" if cfg.auth_token else None},
        "environment_diagnostics": {
            "db_before": db_before,
            "db_after": db_after,
            "warnings_before": diagnostic_warnings(db_before),
            "warnings_after": diagnostic_warnings(db_after),
        },
        "result": {
            "highest_sustainable_eps": confirmed_highest.rate if confirmed_highest else None,
            "highest_sustainable_vus": confirmed_highest.vus if confirmed_highest else None,
            "confirmed_highest_sustainable_eps": confirmed_highest.rate if confirmed_highest else None,
            "confirmed_highest_sustainable_vus": confirmed_highest.vus if confirmed_highest else None,
            "candidate_highest_sustainable_eps": candidate_highest.rate if candidate_highest else None,
            "candidate_highest_sustainable_vus": candidate_highest.vus if candidate_highest else None,
            "first_failing_eps": first_failing.rate if first_failing else None,
            "target_eps": cfg.target_eps,
            "target_met": bool(confirmed_highest and confirmed_highest.rate >= cfg.target_eps),
            "recommendation": recommendation(cfg, confirmed_highest),
        },
        "confirmation": asdict(confirmed_highest) if confirmed_highest else None,
        "confirmation_attempts": [asdict(attempt) for attempt in confirmation_attempts],
        "trials": [asdict(trial) for trial in trials],
    }
    output = Path(cfg.output_dir) / "orchestration-summary.json"
    output.write_text(json.dumps(final, indent=2, default=str) + "\n")
    return final


def print_final_summary(final: dict[str, Any], output_dir: str) -> None:
    result = final["result"]
    print("\nDecision Engine Throughput Orchestration Summary")
    print(f"  confirmed highest sustainable EPS: {result['confirmed_highest_sustainable_eps']}")
    print(f"  candidate highest sustainable EPS: {result['candidate_highest_sustainable_eps']}")
    print(f"  first failing EPS: {result['first_failing_eps']}")
    print(f"  target EPS: {result['target_eps']}")
    print(f"  target met: {result['target_met']}")
    print(f"  recommendation: {result['recommendation']}")
    print(f"  summary: {Path(output_dir) / 'orchestration-summary.json'}")
    print("")


async def async_main() -> int:
    cfg = parse_config()
    Path(cfg.output_dir).mkdir(parents=True, exist_ok=True)
    db_before = collect_db_diagnostics(cfg)
    candidate_highest, first_failing, trials, confirmed_highest, confirmation_attempts = await find_limit(cfg)
    db_after = collect_db_diagnostics(cfg)
    final = write_final_summary(
        cfg,
        candidate_highest,
        first_failing,
        trials,
        confirmed_highest,
        confirmation_attempts,
        db_before,
        db_after,
    )
    print_final_summary(final, cfg.output_dir)
    return 0 if final["result"]["recommendation"] == "target_met" else 1


def main() -> None:
    raise SystemExit(asyncio.run(async_main()))


if __name__ == "__main__":
    main()
