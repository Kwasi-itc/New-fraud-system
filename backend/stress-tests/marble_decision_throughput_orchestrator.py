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

from marble_decision_throughput_limit import Config, run_trial, utc_now


DEFAULT_OUTPUT_DIR = "stress-tests/marble-throughput-runs"


@dataclass(frozen=True)
class OrchestratorConfig:
    target_dps: int
    start_dps: int
    rate_floor: int
    trial_duration_seconds: float
    warmup_duration_seconds: float
    timeout_seconds: float
    cooldown_duration_seconds: float
    health_timeout_seconds: float
    transaction_value: float
    account_past_balance: float
    min_vus: int
    vus_multiplier: int
    max_vus: int
    output_dir: str
    api_url: str
    api_key: str
    org_export: str
    scenario_name: str
    scenario_id: str | None
    scenario_threshold: float
    use_export_scenario: bool
    skip_db_diagnostics: bool
    postgres_container: str
    postgres_user: str
    postgres_db: str


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
    achieved_dps: float
    summary: dict[str, Any]


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="Automatically find maximum sustainable Marble decision throughput.")
    parser.add_argument("--target-dps", type=int, default=1000, help="Upper DPS target to search for.")
    parser.add_argument("--start-dps", type=int, default=25, help="Initial DPS for exponential search.")
    parser.add_argument("--rate-floor", type=int, default=10, help="Stop binary search when rate gap is <= this value.")
    parser.add_argument("--trial-duration", type=float, default=60.0)
    parser.add_argument("--warmup-duration", type=float, default=30.0)
    parser.add_argument("--timeout", type=float, default=30.0)
    parser.add_argument("--cooldown-duration", type=float, default=15.0)
    parser.add_argument("--health-timeout", type=float, default=60.0)
    parser.add_argument("--transaction-value", type=float, default=10000.0)
    parser.add_argument("--account-past-balance", type=float, default=200000.0)
    parser.add_argument("--min-vus", type=int, default=50)
    parser.add_argument("--vus-multiplier", type=int, default=2)
    parser.add_argument("--max-vus", type=int, default=2500)
    parser.add_argument("--output-dir", default=None)
    parser.add_argument("--api-url", default=os.getenv("MARBLE_API_URL", "http://127.0.0.1:8080"))
    parser.add_argument("--api-key", default=os.getenv("MARBLE_API_KEY"))
    parser.add_argument("--org-export", default=os.getenv("ORG_EXPORT", "org-export.json"))
    parser.add_argument("--scenario-name", default=os.getenv("SCENARIO_NAME", "Stress - Single Value Threshold"))
    parser.add_argument("--scenario-id", default=os.getenv("SCENARIO_ID"))
    parser.add_argument("--scenario-threshold", type=float, default=1000.0)
    parser.add_argument(
        "--use-export-scenario",
        action="store_true",
        help="Use --scenario-id or --org-export/--scenario-name instead of creating a dedicated one-rule stress scenario.",
    )
    parser.add_argument("--skip-db-diagnostics", action="store_true")
    parser.add_argument("--postgres-container", default=os.getenv("POSTGRES_CONTAINER", "new-fraud-system-postgres-1"))
    parser.add_argument("--postgres-user", default=os.getenv("POSTGRES_USER", "postgres"))
    parser.add_argument("--postgres-db", default=os.getenv("POSTGRES_DB", "marble"))
    return parser


def parse_config() -> OrchestratorConfig:
    args = build_parser().parse_args()
    if args.target_dps <= 0:
        raise SystemExit("--target-dps must be greater than 0")
    if args.start_dps <= 0 or args.start_dps > args.target_dps:
        raise SystemExit("--start-dps must be greater than 0 and <= --target-dps")
    if args.rate_floor <= 0:
        raise SystemExit("--rate-floor must be greater than 0")
    if args.min_vus <= 0 or args.vus_multiplier <= 0 or args.max_vus < args.min_vus:
        raise SystemExit("invalid VU settings")
    if not args.api_key:
        raise SystemExit("set --api-key or MARBLE_API_KEY")
    output_dir = args.output_dir or str(Path(DEFAULT_OUTPUT_DIR) / utc_now().replace(":", "").replace(".", "-"))
    return OrchestratorConfig(
        target_dps=args.target_dps,
        start_dps=args.start_dps,
        rate_floor=args.rate_floor,
        trial_duration_seconds=args.trial_duration,
        warmup_duration_seconds=args.warmup_duration,
        timeout_seconds=args.timeout,
        cooldown_duration_seconds=args.cooldown_duration,
        health_timeout_seconds=args.health_timeout,
        transaction_value=args.transaction_value,
        account_past_balance=args.account_past_balance,
        min_vus=args.min_vus,
        vus_multiplier=args.vus_multiplier,
        max_vus=args.max_vus,
        output_dir=output_dir,
        api_url=args.api_url.rstrip("/"),
        api_key=args.api_key,
        org_export=args.org_export,
        scenario_name=args.scenario_name,
        scenario_id=args.scenario_id,
        scenario_threshold=args.scenario_threshold,
        use_export_scenario=args.use_export_scenario,
        skip_db_diagnostics=args.skip_db_diagnostics,
        postgres_container=args.postgres_container,
        postgres_user=args.postgres_user,
        postgres_db=args.postgres_db,
    )


def choose_vus(rate: int, cfg: OrchestratorConfig) -> int:
    return min(max(cfg.min_vus, rate * cfg.vus_multiplier), cfg.max_vus)


def trial_output_path(cfg: OrchestratorConfig, rate: int, vus: int, suffix: str = "") -> str:
    safe_suffix = f"-{suffix}" if suffix else ""
    return str(Path(cfg.output_dir) / f"trial-{rate}-dps-{vus}-vus{safe_suffix}.json")


def build_trial_config(cfg: OrchestratorConfig, rate: int, vus: int, output: str) -> Config:
    return Config(
        rate=rate,
        vus=vus,
        duration_seconds=cfg.trial_duration_seconds,
        warmup_duration_seconds=cfg.warmup_duration_seconds,
        transaction_value=cfg.transaction_value,
        account_past_balance=cfg.account_past_balance,
        timeout_seconds=cfg.timeout_seconds,
        output=output,
        api_url=cfg.api_url,
        api_key=cfg.api_key,
        org_export=cfg.org_export,
        scenario_name=cfg.scenario_name,
        scenario_id=cfg.scenario_id,
        scenario_threshold=cfg.scenario_threshold,
        seed_parent_records=True,
        create_stress_scenario=not cfg.use_export_scenario and not cfg.scenario_id,
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
        failures=int(counts["failed_decisions"]),
        timeouts=int(counts["timeouts"]),
        dropped_requests=int(counts["dropped_requests"]),
        achieved_dps=float(rates["achieved_successful_decisions_per_second"]),
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
        "select 'decisions', count(*) from decisions "
        "union all select 'rule_executions', count(*) from rule_executions "
        "union all select 'cases', count(*) from cases "
        "union all select 'inbox_cases', count(*) from inbox_cases"
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


async def wait_ready(cfg: OrchestratorConfig) -> None:
    deadline = time.monotonic() + cfg.health_timeout_seconds
    last_error: Exception | str | None = None
    async with httpx.AsyncClient(base_url=cfg.api_url, timeout=httpx.Timeout(10.0)) as client:
        while time.monotonic() < deadline:
            try:
                response = await client.get("/v1/-/version")
                if response.status_code == 200:
                    return
                last_error = f"status {response.status_code}"
            except (httpx.HTTPError, OSError) as exc:
                last_error = exc
            await asyncio.sleep(0.5)
    raise RuntimeError(f"API was not ready after cooldown: {last_error}")


async def cooldown_and_health_check(cfg: OrchestratorConfig, reason: str) -> None:
    if cfg.cooldown_duration_seconds > 0:
        print(f"cooling down for {cfg.cooldown_duration_seconds:.0f}s after {reason}...")
        await asyncio.sleep(cfg.cooldown_duration_seconds)
    print("checking API readiness before next trial...")
    await wait_ready(cfg)


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
        "cooldown_duration_seconds": cfg.cooldown_duration_seconds,
        "health_timeout_seconds": cfg.health_timeout_seconds,
    }
    Path(output).write_text(json.dumps(summary, indent=2, default=str) + "\n", encoding="utf-8")
    return summary


async def run_rate(cfg: OrchestratorConfig, rate: int, suffix: str = "") -> TrialRecord:
    vus = choose_vus(rate, cfg)
    output = trial_output_path(cfg, rate, vus, suffix)
    print(f"\n=== trial rate={rate} DPS vus={vus} ===")
    db_before = collect_db_diagnostics(cfg)
    summary = await run_trial(build_trial_config(cfg, rate, vus, output))
    db_after = collect_db_diagnostics(cfg)
    summary = add_orchestration_metadata(cfg, output, summary, db_before, db_after)
    record = record_from_summary(rate, vus, output, summary)
    await cooldown_and_health_check(cfg, f"rate={rate} DPS vus={vus}")

    if failed_only_from_drops(record) and vus < cfg.max_vus:
        retry_vus = min(vus * 2, cfg.max_vus)
        retry_output = trial_output_path(cfg, rate, retry_vus, f"{suffix}retry-vus".strip("-"))
        print(f"\n=== retry rate={rate} DPS vus={retry_vus} after dropped requests ===")
        retry_db_before = collect_db_diagnostics(cfg)
        retry_summary = await run_trial(build_trial_config(cfg, rate, retry_vus, retry_output))
        retry_db_after = collect_db_diagnostics(cfg)
        retry_summary = add_orchestration_metadata(cfg, retry_output, retry_summary, retry_db_before, retry_db_after)
        retry_record = record_from_summary(rate, retry_vus, retry_output, retry_summary)
        await cooldown_and_health_check(cfg, f"rate={rate} DPS vus={retry_vus} retry")
        return retry_record

    return record


def next_exponential_rate(current: int, target: int) -> int:
    return target if current >= target else min(current * 2, target)


async def find_limit(
    cfg: OrchestratorConfig,
) -> tuple[TrialRecord | None, TrialRecord | None, list[TrialRecord], TrialRecord | None, list[TrialRecord]]:
    trials: list[TrialRecord] = []
    sustainable_candidates: list[TrialRecord] = []
    last_sustainable: TrialRecord | None = None
    first_failing: TrialRecord | None = None

    rate = cfg.start_dps
    while True:
        record = await run_rate(cfg, rate)
        trials.append(record)
        if record.sustainable:
            last_sustainable = record
            sustainable_candidates.append(record)
            if rate >= cfg.target_dps:
                break
            rate = next_exponential_rate(rate, cfg.target_dps)
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
    if confirmed.rate >= cfg.target_dps:
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
        "test": "marble_decision_throughput_limit_orchestration",
        "created_at": utc_now(),
        "command": sys.argv,
        "config": asdict(cfg) | {"api_key": "set"},
        "environment_diagnostics": {"db_before": db_before, "db_after": db_after},
        "result": {
            "highest_sustainable_dps": confirmed_highest.rate if confirmed_highest else None,
            "highest_sustainable_vus": confirmed_highest.vus if confirmed_highest else None,
            "candidate_highest_sustainable_dps": candidate_highest.rate if candidate_highest else None,
            "candidate_highest_sustainable_vus": candidate_highest.vus if candidate_highest else None,
            "first_failing_dps": first_failing.rate if first_failing else None,
            "target_dps": cfg.target_dps,
            "target_met": bool(confirmed_highest and confirmed_highest.rate >= cfg.target_dps),
            "recommendation": recommendation(cfg, confirmed_highest),
        },
        "confirmation": asdict(confirmed_highest) if confirmed_highest else None,
        "confirmation_attempts": [asdict(attempt) for attempt in confirmation_attempts],
        "trials": [asdict(trial) for trial in trials],
    }
    output = Path(cfg.output_dir) / "orchestration-summary.json"
    output.write_text(json.dumps(final, indent=2, default=str) + "\n", encoding="utf-8")
    return final


def print_final_summary(final: dict[str, Any], output_dir: str) -> None:
    result = final["result"]
    print("\nMarble Decision Throughput Orchestration Summary")
    print(f"  confirmed highest sustainable DPS: {result['highest_sustainable_dps']}")
    print(f"  candidate highest sustainable DPS: {result['candidate_highest_sustainable_dps']}")
    print(f"  first failing DPS: {result['first_failing_dps']}")
    print(f"  target DPS: {result['target_dps']}")
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
