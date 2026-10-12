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
import shutil
import time
from collections import Counter
from collections.abc import Callable, Iterator
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
from .queue_recovery import QueueCollector, QueueRecoveryMonitor, RecoveryGate, RecoveryPolicy
from .input_source import InputSource, inspect_source, read_inputs
from .worker_outage import DockerWorker, bounded_control, run_outage


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
        if not all(math.isfinite(v) and v > 0 for v in (stage.rate, stage.seconds, stage.rate * stage.seconds)):
            raise ValueError()
        return stage
    except ValueError as exc:
        raise argparse.ArgumentTypeError("stage must be NAME:positive-rate:positive-seconds") from exc


async def run_campaign(
    clients: Any, tenant: str, inputs: Iterator[dict[str, Any]], stages: list[Stage],
    output: Path, *, max_inflight: int, completion_timeout: float, poll_interval: float,
    expected_scenarios: int, deadline: float, max_lag: float, ledger: IdentityLedger,
    path: str = "ingest-async",
    minimum_free_disk_bytes: int = 0,
    on_stage: Callable[[str], None] | None = None,
) -> dict[str, Any]:
    """One bounded set of logical operations, including completion polling.

    Slots retain their original monotonic schedule. Saturated/late arrivals are
    recorded as generator drops, never silently delayed into a closed-loop run.
    """
    if (path not in {"ingestion", "ingest-sync", "ingest-async"}
            or not stages or len({s.name for s in stages}) != len(stages)
            or max_inflight <= 0 or expected_scenarios <= 0
            or minimum_free_disk_bytes < 0
            or any(not math.isfinite(v) or v <= 0 for v in
                   (completion_timeout, poll_interval, deadline, max_lag))):
        raise ValueError("invalid_campaign_configuration")
    for stage in stages:
        parse_stage(f"{stage.name}:{stage.rate}:{stage.seconds}")
    if shutil.disk_usage(output).free < minimum_free_disk_bytes:
        raise RuntimeError("minimum_free_disk_reserve_reached")
    started = time.monotonic()
    totals: Counter[str] = Counter()
    errors: Counter[str] = Counter()
    cohorts = {s.name: Counter() for s in stages}
    latencies = {s.name: {key: LatencyMetric() for key in
                        ("submission_delay", "ingestion", "decision_acceptance", "successful_completion")}
                 for s in stages}
    tasks: set[asyncio.Task[None]] = set()
    failure: BaseException | None = None
    failed = asyncio.Event()
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
        return {"schema_version": 2, "status": status, "path": path,
                "scope": "ingestion_response" if path == "ingestion" else "ingestion_to_decision_response",
                "elapsed_seconds": elapsed, "schedule_seconds": schedule_seconds,
                "planned": planned, "counts": counts, "peak_inflight": peak,
                "errors": dict(errors),
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
                if path == "ingestion":
                    ledger.transition(tenant, identity, "ingested", "completed")
                    increment(stage, "completed")
                    latency = time.monotonic() - scheduled
                    last_completion[stage] = time.monotonic() - started
                    latencies[stage]["successful_completion"].add(latency * 1000)
                    if latency <= deadline:
                        increment(stage, "deadline_met")
                    return
                ledger.transition(tenant, identity, "ingested", "evaluating")
                state = "evaluating"
                request_start = time.monotonic()
                response, code, _ = await clients.record_ingested(
                    tenant, identity, fields, mode="async" if path == "ingest-async" else "sync", source="queue_campaign")
                latencies[stage]["decision_acceptance"].add((time.monotonic() - request_start) * 1000)
                if path == "ingest-async" and code != 202 and response.get("deferred") is not True:
                    raise DecisionCompletionError("async_path_not_exercised")
                if code == 202 or response.get("deferred") is True:
                    increment(stage, "async_accepted")
                decisions = await verify_decision_completion(
                    clients, tenant, identity, response, code, allow_deferred=path == "ingest-async",
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
            category = (exc.category if isinstance(exc, DecisionCompletionError) else
                        f"http_{exc.status_code}" if isinstance(exc, APIError) and exc.status_code is not None
                        else type(exc).__name__)
            errors[f"{state}.{category}"] += 1
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
            failed.set()

    stop = asyncio.Event()
    with (output / "timeline.ndjson").open("x", encoding="utf-8") as timeline:
        def save_timeline(state: dict[str, Any]) -> None:
            timeline.write(json.dumps({"elapsed_seconds": state["elapsed_seconds"],
                "stage": current, "counts": state["counts"]}) + "\n")
            timeline.flush()

        async def report_progress() -> None:
            while True:
                if shutil.disk_usage(output).free < minimum_free_disk_bytes:
                    raise RuntimeError("minimum_free_disk_reserve_reached")
                state = snapshot()
                save_timeline(state)
                write_json_atomic(output / "summary.json", state)
                try:
                    await asyncio.wait_for(stop.wait(), timeout=1)
                    return
                except TimeoutError:
                    pass

        progress = asyncio.create_task(report_progress())
        def progress_finished(task):
            nonlocal failure
            if not task.cancelled() and task.exception() is not None:
                failure = failure or task.exception()
                failed.set()
        progress.add_done_callback(progress_finished)

        async def wait_until(when: float) -> None:
            if failure:
                raise failure
            remaining = when - time.monotonic()
            if remaining <= 0:
                await asyncio.sleep(0)
                return
            try:
                await asyncio.wait_for(failed.wait(), timeout=remaining)
            except TimeoutError:
                return
            raise failure or RuntimeError("campaign_task_failed")

        try:
            for stage in stages:
                current = stage.name
                if on_stage is not None:
                    on_stage(stage.name)
                stage_start = started + stage_offsets[stage.name]
                for slot in range(stage.count):
                    if failure:
                        raise failure
                    if progress.done():
                        progress.result()
                    scheduled = stage_start + slot / stage.rate
                    await wait_until(scheduled)
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
                await wait_until(stage_start + stage.seconds)
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
            progress_error = None
            try:
                await progress
            except Exception as exc:
                progress_error = exc
                status = "interrupted_or_failed"
            result = snapshot()
            result["identity_reconciliation"] = ledger.summary(planned)
            result["acceptance"]["passed"] &= result["identity_reconciliation"]["valid"]
            save_timeline(result)
            write_json_atomic(output / "summary.json", result)
            if progress_error is not None:
                raise progress_error
    return result


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--input", required=True, type=Path, help="NDJSON of unique transaction field objects")
    parser.add_argument("--output", required=True, type=Path, help="New artifact directory")
    parser.add_argument("--tenant-id", required=True, help="Existing prepared test tenant")
    parser.add_argument("--path", choices=("ingestion", "ingest-sync", "ingest-async"), default="ingest-async")
    parser.add_argument("--stage", action="append", type=parse_stage, required=True)
    parser.add_argument("--expected-scenarios", type=int, required=True)
    parser.add_argument("--deadline", type=float, required=True, help="Scheduled arrival to completion, seconds")
    parser.add_argument("--max-inflight", type=int, default=100)
    parser.add_argument("--completion-timeout", type=float, default=60)
    parser.add_argument("--poll-interval", type=float, default=0.5)
    parser.add_argument("--max-scheduling-lag", type=float, default=0.1)
    parser.add_argument("--max-operations", type=int, default=1_000_000)
    parser.add_argument("--max-duration", type=float, default=86400, help="Maximum scheduled traffic duration, seconds")
    parser.add_argument("--minimum-free-disk-bytes", type=int, default=104857600)
    parser.add_argument("--data-model-url", default="http://127.0.0.1:8080")
    parser.add_argument("--ingestion-url", default="http://127.0.0.1:8081")
    parser.add_argument("--decision-engine-url", default="http://127.0.0.1:8082")
    parser.add_argument("--auth-token-env", default="SERVICE_AUTH_TOKEN")
    parser.add_argument("--capture-metrics", action="store_true")
    parser.add_argument("--capture-database-metrics", action="store_true", help="psql uses PGHOST/PGDATABASE/etc.")
    parser.add_argument("--metrics-interval", type=float, default=5)
    parser.add_argument("--queue", action="append", help="Exact River queue name; repeat for up to 16 queues")
    parser.add_argument("--queue-schema", default="public")
    parser.add_argument("--queue-database", help="Expected shared test database name; psql uses PG* environment")
    parser.add_argument("--queue-recovery-stage", help="Stage whose start begins the recovery deadline")
    parser.add_argument("--queue-recovery-deadline", type=float, help="Time to confirm recovery from stage start")
    parser.add_argument("--queue-healthy-seconds", type=float, help="Required consecutive healthy observation window")
    parser.add_argument("--queue-max-active", type=int, help="Maximum active jobs per selected queue after recovery")
    parser.add_argument("--queue-max-due-age", type=float, help="Maximum due age in seconds after recovery")
    parser.add_argument("--queue-interval", type=float, default=1)
    parser.add_argument("--queue-timeout", type=float, default=5)
    parser.add_argument("--queue-max-sample-gap", type=float, default=7)
    parser.add_argument("--queue-sample-cap", type=int, default=1000)
    parser.add_argument("--worker-container-id", help="Full Docker ID of a labeled disposable test worker")
    parser.add_argument("--worker-project", help="Owning system-tests-* disposable Compose project")
    parser.add_argument("--worker-outage-stage", help="Start an outage when this traffic stage begins")
    parser.add_argument("--worker-outage-seconds", type=float)
    parser.add_argument("--dry-run", action="store_true", help="Validate schedule and source; no requests or artifacts")
    return parser


async def async_main(argv: list[str] | None = None) -> int:
    parser = build_parser()
    args = parser.parse_args(argv)
    if (args.max_inflight <= 0 or args.expected_scenarios <= 0 or args.max_operations <= 0
            or args.minimum_free_disk_bytes < 0
            or len({s.name for s in args.stage}) != len(args.stage)
            or any(not math.isfinite(v) or v <= 0 for v in (args.deadline, args.completion_timeout,
                args.poll_interval, args.max_scheduling_lag, args.metrics_interval, args.max_duration))):
        parser.error("counts must be positive, stage names unique, and durations finite and positive")
    planned = sum(s.count for s in args.stage)
    if planned > args.max_operations:
        parser.error("schedule exceeds --max-operations")
    if sum(s.seconds for s in args.stage) > args.max_duration:
        parser.error("schedule exceeds --max-duration")
    if args.capture_database_metrics and not args.capture_metrics:
        parser.error("--capture-database-metrics requires --capture-metrics")
    queue_monitor = None
    if args.queue:
        if any(v is None for v in (args.queue_database, args.queue_recovery_stage,
                args.queue_recovery_deadline, args.queue_healthy_seconds,
                args.queue_max_active, args.queue_max_due_age)):
            parser.error("--queue requires database, recovery stage/deadline, healthy seconds, max active and max due age")
        selected = next((s for s in args.stage if s.name == args.queue_recovery_stage), None)
        if selected is None or args.queue_recovery_deadline > selected.seconds:
            parser.error("queue recovery deadline must fit within the named recovery stage")
        offset = 0.0
        for stage in args.stage:
            if stage.name == selected.name:
                break
            offset += stage.seconds
        try:
            collector = QueueCollector(PostgresObserver(["psql"], dict(os.environ), args.queue_timeout),
                args.queue_database, args.queue_schema, args.queue, args.queue_sample_cap)
            policy = RecoveryPolicy(offset, args.queue_recovery_deadline, args.queue_healthy_seconds,
                args.queue_max_sample_gap, args.queue_max_active, args.queue_max_due_age)
            queue_monitor = QueueRecoveryMonitor(collector, RecoveryGate(args.queue, policy),
                args.output / "queues.ndjson", args.queue_interval)
        except ValueError:
            parser.error("invalid queue selection, timeouts or recovery thresholds")
    elif any(v is not None for v in (args.queue_database, args.queue_recovery_stage,
            args.queue_recovery_deadline, args.queue_healthy_seconds, args.queue_max_active, args.queue_max_due_age)):
        parser.error("queue recovery settings require --queue")
    worker = None
    fault_options = (args.worker_container_id, args.worker_project, args.worker_outage_stage, args.worker_outage_seconds)
    if any(v is not None for v in fault_options):
        if any(v is None for v in fault_options) or args.path != "ingest-async":
            parser.error("worker outage requires all four worker options and the ingest-async path")
        selected = next((s for s in args.stage if s.name == args.worker_outage_stage), None)
        if (selected is None or not math.isfinite(args.worker_outage_seconds)
                or not 0 < args.worker_outage_seconds <= min(300, selected.seconds - 2)):
            parser.error("worker outage must be positive, at most 300 seconds, and fit its stage with 2 seconds for stop")
        try:
            worker = DockerWorker(args.worker_container_id, args.worker_project)
        except ValueError:
            parser.error("worker identity must be a full container ID in a system-tests-* project")
    input_evidence = await asyncio.to_thread(inspect_source, args.input, planned)
    if args.dry_run:
        print(json.dumps({"planned": planned, "schedule_seconds": sum(s.seconds for s in args.stage),
                          "max_inflight": args.max_inflight, "requests_sent": 0,
                          "input": input_evidence}))
        return 0
    args.output.mkdir(parents=True, exist_ok=False)
    endpoints = {"decision": args.decision_engine_url, "ingestion": args.ingestion_url}
    config = {key: value for key, value in vars(args).items()
              if key not in {"auth_token_env", "tenant_id", "input"} and not key.endswith("_url")}
    config["stage"] = [vars(s) for s in args.stage]
    write_json_atomic(args.output / "config.json", config)
    environment = capture_environment(Path(__file__).resolve().parents[3], args.output, endpoints)
    environment["system_tests_sha256"] = {p.name: hashlib.sha256(p.read_bytes()).hexdigest()
        for p in sorted(Path(__file__).parent.glob("*.py"))}
    write_json_atomic(args.output / "environment.json", environment)
    token = os.getenv(args.auth_token_env)
    write_json_atomic(args.output / "input.json", input_evidence)
    inputs = InputSource(args.input, planned)
    trigger = asyncio.Event()
    journal = args.output / "worker-fault.json"
    try:
        if worker is not None and not (await bounded_control(worker.inspect))["Running"]:
            raise ValueError("worker_not_running_before_campaign")
        async with ServiceClients(ServiceConfig(args.data_model_url, args.ingestion_url,
                args.decision_engine_url, token, max_connections=args.max_inflight)) as clients:
            with IdentityLedger(args.output / "identities.sqlite3") as ledger:
                async def workload() -> dict[str, Any]:
                    return await run_campaign(clients, args.tenant_id, inputs, args.stage, args.output,
                        max_inflight=args.max_inflight, completion_timeout=args.completion_timeout,
                        poll_interval=args.poll_interval, expected_scenarios=args.expected_scenarios,
                        deadline=args.deadline, max_lag=args.max_scheduling_lag, ledger=ledger, path=args.path,
                        minimum_free_disk_bytes=args.minimum_free_disk_bytes,
                        on_stage=lambda name: trigger.set() if name == args.worker_outage_stage else None)

                async def faulted_workload() -> dict[str, Any]:
                    if worker is None:
                        return await workload()
                    work = asyncio.create_task(workload())
                    fault = asyncio.create_task(run_outage(worker, journal, trigger, duration=args.worker_outage_seconds,
                        trigger_timeout=sum(s.seconds for s in args.stage) + args.completion_timeout))
                    try:
                        result, evidence = await asyncio.gather(work, fault)
                        result["worker_fault"] = evidence
                        result["acceptance"]["passed"] &= evidence["passed"]
                        return result
                    finally:
                        work.cancel()
                        fault.cancel()
                        await asyncio.gather(work, fault, return_exceptions=True)

                async def monitored_workload() -> dict[str, Any]:
                    if queue_monitor is None:
                        return await faulted_workload()
                    result = await queue_monitor.run_during(faulted_workload)
                    result["queue_recovery"] = queue_monitor.summary()
                    result["acceptance"]["passed"] &= result["queue_recovery"]["passed"]
                    return result

                if args.capture_metrics:
                    database = PostgresObserver(["psql"], dict(os.environ), 5) if args.capture_database_metrics else None
                    async with RuntimeObserver(args.output / "metrics.ndjson", endpoints, auth_token=token,
                            interval=args.metrics_interval, database=database) as observer:
                        result = await observer.run_during(monitored_workload)
                        result["telemetry"] = observer.summary()
                        result["acceptance"]["passed"] &= result["telemetry"]["valid"]
                else:
                    result = await monitored_workload()
                    result["telemetry"] = {"enabled": False}
                result["input"] = {**inputs.evidence(), "matches_preflight": inputs.evidence() == input_evidence}
                result["acceptance"]["passed"] &= result["input"]["matches_preflight"]
                write_json_atomic(args.output / "summary.json", result)
                return 0 if result["acceptance"]["passed"] else 2
    except BaseException as exc:
        path = args.output / "summary.json"
        state = json.loads(path.read_text()) if path.exists() else {}
        state.update(status="interrupted_or_failed", error_type=type(exc).__name__,
                     acceptance={"passed": False, "evaluated": False})
        if queue_monitor is not None:
            state["queue_recovery"] = queue_monitor.summary()
        if journal.exists() and journal.stat().st_size:
            state["worker_fault"] = json.loads(journal.read_text())
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
