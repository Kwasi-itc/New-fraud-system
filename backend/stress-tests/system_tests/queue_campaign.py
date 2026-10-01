"""Fixed-arrival ingestion/async-decision experiments against a prepared test tenant.

No database resets, direct job writes, service restarts, or fault injection.
"""
from __future__ import annotations

import argparse
import asyncio
import hashlib
import json
import math
import os
import re
import time
from collections import Counter
from collections.abc import Iterator
from dataclasses import dataclass
from pathlib import Path
from typing import Any

from production_replay.api_client import APIError, ServiceClients, ServiceConfig
from production_replay.benchmark_observation import RuntimeObserver, capture_environment
from production_replay.benchmark_reporting import write_json_atomic
from production_replay.decision_completion import DecisionCompletionError, verify_decision_completion
from production_replay.identity_ledger import IdentityLedger
from production_replay.postgres_observation import PostgresObserver
from production_replay.replay import LatencyMetric


@dataclass(frozen=True)
class Stage:
    name: str
    rate: float
    seconds: float

    @property
    def count(self) -> int:
        return math.ceil(self.rate * self.seconds)


def parse_stage(value: str) -> Stage:
    try:
        name, rate, seconds = value.split(":")
        stage = Stage(name, float(rate), float(seconds))
        if not re.fullmatch(r"[a-zA-Z0-9_-]{1,40}", name):
            raise ValueError()
        if not all(math.isfinite(v) and v > 0 for v in (stage.rate, stage.seconds)):
            raise ValueError()
        return stage
    except ValueError as exc:
        raise argparse.ArgumentTypeError("stage must be NAME:positive-rate:positive-seconds") from exc


def read_inputs(path: Path) -> Iterator[dict[str, Any]]:
    # Streaming read with a hard row bound; prepared fields must match the tenant model.
    with path.open("rb") as handle:
        while line := handle.readline(1_048_577):
            if len(line) > 1_048_576:
                raise ValueError("input_row_too_large")
            value = json.loads(line)
            if not isinstance(value, dict) or not isinstance(value.get("object_id"), str) or not value["object_id"]:
                raise ValueError("input_requires_object_id")
            yield value


async def run_campaign(
    clients: Any, tenant: str, inputs: Iterator[dict[str, Any]], stages: list[Stage],
    output: Path, *, max_inflight: int, completion_timeout: float, poll_interval: float,
    expected_scenarios: int, deadline: float, max_lag: float, ledger: IdentityLedger,
) -> dict[str, Any]:
    """One bounded set of logical operations, including completion polling.

    Slots retain their original monotonic schedule. Saturated/late arrivals are
    recorded as generator drops, never silently delayed into a closed-loop run.
    """
    if (not stages or len({s.name for s in stages}) != len(stages)
            or max_inflight <= 0 or expected_scenarios <= 0
            or any(not math.isfinite(v) or v <= 0 for v in
                   (completion_timeout, poll_interval, deadline, max_lag))):
        raise ValueError("invalid_campaign_configuration")
    for stage in stages:
        parse_stage(f"{stage.name}:{stage.rate}:{stage.seconds}")
    started = time.monotonic()
    totals: Counter[str] = Counter()
    cohorts = {s.name: Counter() for s in stages}
    latencies = {s.name: {key: LatencyMetric() for key in
                        ("submission_delay", "ingestion", "decision_acceptance", "successful_completion")}
                 for s in stages}
    tasks: set[asyncio.Task[None]] = set()
    failure: BaseException | None = None
    peak = 0
    current = stages[0].name
    status = "running"
    planned = sum(s.count for s in stages)
    schedule_seconds = sum(s.seconds for s in stages)
    stage_offsets: dict[str, float] = {}
    offset = 0.0
    for stage in stages:
        stage_offsets[stage.name] = offset
        offset += stage.seconds
    last_completion: dict[str, float] = {}

    def increment(stage: str, key: str, amount: int = 1) -> None:
        totals[key] += amount
        cohorts[stage][key] += amount

    def snapshot() -> dict[str, Any]:
        elapsed = time.monotonic() - started
        counts = {key: totals[key] for key in (
            "offered", "submitted", "generator_dropped", "ingested", "ingestion_attempts",
            "async_accepted", "completed", "deadline_met", "failed", "unresolved")}
        counts["outstanding"] = counts["submitted"] - counts["completed"] - counts["failed"] - counts["unresolved"]
        return {"schema_version": 1, "status": status, "scope": "ingestion_to_async_decision_response",
                "elapsed_seconds": elapsed, "schedule_seconds": schedule_seconds,
                "planned": planned, "counts": counts, "peak_inflight": peak,
                "completion_rate_including_final_drain": totals["completed"] / elapsed if elapsed else 0,
                "cohorts": {s.name: {
                    "rate": s.rate, "seconds": s.seconds, "planned": s.count,
                    "counts": dict(cohorts[s.name]),
                    "last_success_seconds_after_stage_end": max(0, last_completion.get(s.name, 0)
                        - stage_offsets[s.name] - s.seconds) if cohorts[s.name]["completed"] else None,
                    "latencies": {key: metric.summary() for key, metric in latencies[s.name].items()},
                } for s in stages},
                "acceptance": {"evaluated": status == "completed", "passed": status == "completed"
                    and totals["completed"] == planned and totals["deadline_met"] == planned
                    and totals["generator_dropped"] == 0},
                "limitations": ["response_evidence_not_independent_durable_effect_reconciliation",
                    "outstanding_is_client_cohort_not_database_queue_depth",
                    "does_not_verify_callbacks_workflows_screening_or_cases",
                    "last_success_is_not_proof_of_whole_queue_recovery",
                    "successful_latency_excludes_failed_and_unresolved_inputs",
                    "polling_and_local_ledger_cost_are_included",
                    "no_production_capacity_or_hardware_qualification"]}

    async def operation(stage: str, fields: dict[str, Any], scheduled: float) -> None:
        identity = fields["object_id"]
        state = "ingesting"
        operation_start = time.monotonic()
        latencies[stage]["submission_delay"].add((operation_start - scheduled) * 1000)
        try:
            async with asyncio.timeout(completion_timeout):
                ingest_start = time.monotonic()
                key = hashlib.sha256(f"{tenant}:{identity}".encode()).hexdigest()
                try:
                    _, attempts = await clients.ingest_one(tenant, "transactions", fields, key, max_attempts=1)
                except APIError as exc:
                    increment(stage, "ingestion_attempts", exc.attempts)
                    raise
                increment(stage, "ingestion_attempts", attempts)
                increment(stage, "ingested")
                latencies[stage]["ingestion"].add((time.monotonic() - ingest_start) * 1000)
                ledger.transition(tenant, identity, "ingesting", "ingested", attempts=attempts)
                ledger.transition(tenant, identity, "ingested", "evaluating")
                state = "evaluating"
                request_start = time.monotonic()
                response, code, _ = await clients.record_ingested(
                    tenant, identity, fields, mode="async", source="queue_campaign")
                latencies[stage]["decision_acceptance"].add((time.monotonic() - request_start) * 1000)
                if code != 202 and response.get("deferred") is not True:
                    raise DecisionCompletionError("async_path_not_exercised")
                increment(stage, "async_accepted")
                decisions = await verify_decision_completion(
                    clients, tenant, identity, response, code, allow_deferred=True,
                    timeout_seconds=completion_timeout, poll_interval_seconds=poll_interval,
                    expected_scenarios=expected_scenarios)
                ledger.transition(tenant, identity, "evaluating", "completed", decisions=decisions)
                increment(stage, "completed")
                now = time.monotonic()
                last_completion[stage] = now - started
                latency = now - scheduled
                latencies[stage]["successful_completion"].add(latency * 1000)
                if latency <= deadline:
                    increment(stage, "deadline_met")
        except (APIError, TimeoutError, asyncio.CancelledError) as exc:
            # An HTTP error can follow a committed mutation. Do not resubmit it.
            terminal = isinstance(exc, DecisionCompletionError) and not exc.unresolved
            increment(stage, "failed" if terminal else "unresolved")
            if state == "evaluating":
                ledger.transition(tenant, identity, state, "decision_failed" if terminal else "unresolved")
            # Ingesting entries intentionally remain unfinished: no commit evidence.
            if isinstance(exc, asyncio.CancelledError):
                raise

    def finished(task: asyncio.Task[None]) -> None:
        nonlocal failure
        tasks.discard(task)
        if not task.cancelled() and task.exception() is not None and failure is None:
            failure = task.exception()

    stop = asyncio.Event()
    with (output / "timeline.ndjson").open("x", encoding="utf-8") as timeline:
        async def report_progress() -> None:
            while True:
                state = snapshot()
                timeline.write(json.dumps({"elapsed_seconds": state["elapsed_seconds"],
                    "stage": current, "counts": state["counts"]}) + "\n")
                timeline.flush()
                write_json_atomic(output / "summary.json", state)
                try:
                    await asyncio.wait_for(stop.wait(), timeout=1)
                    return
                except TimeoutError:
                    pass

        progress = asyncio.create_task(report_progress())
        try:
            for stage in stages:
                current = stage.name
                stage_start = started + stage_offsets[stage.name]
                for slot in range(stage.count):
                    if failure:
                        raise failure
                    if progress.done():
                        progress.result()
                    scheduled = stage_start + slot / stage.rate
                    await asyncio.sleep(max(0, scheduled - time.monotonic()))
                    try:
                        fields = next(inputs)
                    except StopIteration as exc:
                        raise ValueError("input_exhausted_before_schedule_end") from exc
                    increment(stage.name, "offered")
                    if time.monotonic() - scheduled > max_lag or len(tasks) >= max_inflight:
                        increment(stage.name, "generator_dropped")
                        continue
                    ledger.register(tenant, fields["object_id"])
                    increment(stage.name, "submitted")
                    task = asyncio.create_task(operation(stage.name, fields, scheduled))
                    tasks.add(task)
                    task.add_done_callback(finished)
                    peak = max(peak, len(tasks))
                await asyncio.sleep(max(0, stage_start + stage.seconds - time.monotonic()))
            current = "final_drain"
            if tasks:
                await asyncio.gather(*tasks)
            if failure:
                raise failure
            progress.result() if progress.done() else None
            status = "completed"
        except BaseException:
            status = "interrupted_or_failed"
            raise
        finally:
            for task in tasks:
                task.cancel()
            await asyncio.gather(*tasks, return_exceptions=True)
            stop.set()
            await progress
            result = snapshot()
            result["identity_reconciliation"] = ledger.summary(planned)
            result["acceptance"]["passed"] &= result["identity_reconciliation"]["valid"]
            write_json_atomic(output / "summary.json", result)
    return result


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--input", required=True, type=Path, help="NDJSON of unique transaction field objects")
    parser.add_argument("--output", required=True, type=Path, help="New artifact directory")
    parser.add_argument("--tenant-id", required=True, help="Existing prepared test tenant")
    parser.add_argument("--stage", action="append", type=parse_stage, required=True)
    parser.add_argument("--expected-scenarios", type=int, required=True)
    parser.add_argument("--deadline", type=float, required=True, help="Scheduled arrival to completion, seconds")
    parser.add_argument("--max-inflight", type=int, default=100)
    parser.add_argument("--completion-timeout", type=float, default=60)
    parser.add_argument("--poll-interval", type=float, default=0.5)
    parser.add_argument("--max-scheduling-lag", type=float, default=0.1)
    parser.add_argument("--max-operations", type=int, default=1_000_000)
    parser.add_argument("--data-model-url", default="http://127.0.0.1:8080")
    parser.add_argument("--ingestion-url", default="http://127.0.0.1:8081")
    parser.add_argument("--decision-engine-url", default="http://127.0.0.1:8082")
    parser.add_argument("--auth-token-env", default="SERVICE_AUTH_TOKEN")
    parser.add_argument("--capture-metrics", action="store_true")
    parser.add_argument("--capture-database-metrics", action="store_true", help="psql uses PGHOST/PGDATABASE/etc.")
    parser.add_argument("--metrics-interval", type=float, default=5)
    parser.add_argument("--dry-run", action="store_true", help="Validate schedule and source; no requests or artifacts")
    return parser


async def async_main(argv: list[str] | None = None) -> int:
    parser = build_parser()
    args = parser.parse_args(argv)
    if (args.max_inflight <= 0 or args.expected_scenarios <= 0 or args.max_operations <= 0
            or len({s.name for s in args.stage}) != len(args.stage)
            or any(not math.isfinite(v) or v <= 0 for v in (args.deadline, args.completion_timeout,
                args.poll_interval, args.max_scheduling_lag, args.metrics_interval))):
        parser.error("counts must be positive, stage names unique, and durations finite and positive")
    planned = sum(s.count for s in args.stage)
    if planned > args.max_operations:
        parser.error("schedule exceeds --max-operations")
    if args.capture_database_metrics and not args.capture_metrics:
        parser.error("--capture-database-metrics requires --capture-metrics")
    if args.dry_run:
        # Validate only the scheduled prefix. Runtime ledger checks duplicate identities.
        inputs = read_inputs(args.input)
        try:
            for _ in range(planned):
                next(inputs)
        except StopIteration:
            parser.error("input has fewer records than the schedule requires")
        finally:
            inputs.close()
        print(json.dumps({"planned": planned, "schedule_seconds": sum(s.seconds for s in args.stage),
                          "max_inflight": args.max_inflight, "requests_sent": 0}))
        return 0
    args.output.mkdir(parents=True, exist_ok=False)
    endpoints = {"decision": args.decision_engine_url, "ingestion": args.ingestion_url}
    config = {key: value for key, value in vars(args).items()
              if key not in {"auth_token_env", "tenant_id", "input"} and not key.endswith("_url")}
    config["stage"] = [vars(s) for s in args.stage]
    write_json_atomic(args.output / "config.json", config)
    write_json_atomic(args.output / "environment.json", capture_environment(
        Path(__file__).resolve().parents[3], args.output, endpoints))
    token = os.getenv(args.auth_token_env)
    inputs = read_inputs(args.input)
    try:
        async with ServiceClients(ServiceConfig(args.data_model_url, args.ingestion_url,
                args.decision_engine_url, token, max_connections=args.max_inflight)) as clients:
            with IdentityLedger(args.output / "identities.sqlite3") as ledger:
                async def workload() -> dict[str, Any]:
                    return await run_campaign(clients, args.tenant_id, inputs, args.stage, args.output,
                        max_inflight=args.max_inflight, completion_timeout=args.completion_timeout,
                        poll_interval=args.poll_interval, expected_scenarios=args.expected_scenarios,
                        deadline=args.deadline, max_lag=args.max_scheduling_lag, ledger=ledger)

                if args.capture_metrics:
                    database = PostgresObserver(["psql"], dict(os.environ), 5) if args.capture_database_metrics else None
                    async with RuntimeObserver(args.output / "metrics.ndjson", endpoints, auth_token=token,
                            interval=args.metrics_interval, database=database) as observer:
                        result = await observer.run_during(workload)
                        result["telemetry"] = observer.summary()
                        result["acceptance"]["passed"] &= result["telemetry"]["valid"]
                else:
                    result = await workload()
                    result["telemetry"] = {"enabled": False}
                write_json_atomic(args.output / "summary.json", result)
                return 0 if result["acceptance"]["passed"] else 2
    except BaseException as exc:
        path = args.output / "summary.json"
        state = json.loads(path.read_text()) if path.exists() else {}
        state.update(status="interrupted_or_failed", error_type=type(exc).__name__,
                     acceptance={"passed": False, "evaluated": False})
        write_json_atomic(path, state)
        raise
    finally:
        inputs.close()


def main() -> None:
    try:
        raise SystemExit(asyncio.run(async_main()))
    except KeyboardInterrupt:
        raise SystemExit(130) from None
    except Exception as exc:
        print(f"Campaign failed: {type(exc).__name__}; inspect output artifacts")
        raise SystemExit(1) from None


if __name__ == "__main__":
    main()
