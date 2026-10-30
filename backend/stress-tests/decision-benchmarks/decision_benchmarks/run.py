"""Bounded decision-only execution, semantic gating and raw evidence collection."""
from __future__ import annotations

import asyncio
import json
import shutil
import time
import uuid
from collections import Counter
from pathlib import Path

import httpx

from production_replay.benchmark_observation import RuntimeObserver
from production_replay.benchmark_reporting import write_json_atomic
from production_replay.replay import LatencyMetric
from system_tests.arrival_schedule import arrivals
from system_tests.queue_campaign import Stage
from .api import CorrectnessError, evaluate, reconcile, require, setup, verify
from .model import Experiment, VERSION, digest, history_oracle, payload, reference_cases


def latency_summary(metric: LatencyMetric) -> dict:
    result = metric.summary()
    target, cumulative = max(1, (90 * metric.count + 99) // 100), 0
    result["p90_ms"] = None
    for bound, count in sorted(metric.buckets.items()):
        cumulative += count
        if cumulative >= target:
            result["p90_ms"] = float(bound)
            break
    return {**result, "histogram": metric.to_state(), "method": "replay_latency_buckets_nearest_rank"}


def decision_signatures(response: dict) -> list[dict]:
    result = response["result"]
    items = result.get("results", [result])
    return [{"id": item["decision"]["id"], "sha256": digest({
        "decision": item["decision"], "rule_executions": sorted(item["rule_executions"], key=lambda x: x["rule_id"])})}
        for item in items if item["triggered"]]


async def qualify(clients, exp, fixtures, aggregates, output):
    checked = 0
    with (output / "correctness.ndjson").open("x", encoding="utf-8") as records:
        for fixture in fixtures:
            for index, (case, fields, status) in enumerate(reference_cases(exp)):
                fields["object_id"] = f"qualify-{index}"
                row = {"case": case, "tenant_id": fixture["tenant_id"], "expected_status": status}
                try:
                    body, code, _ = await evaluate(clients, exp, fixture, fields)
                    ids = verify(exp, fixture, fields, body, code, aggregates, status)
                    await reconcile(clients, exp, fixture, fields, body, ids, aggregates)
                    # Rejection and false-trigger cases must not write decisions.
                    if not ids:
                        counts = await clients.request(clients.decision_engine, "GET",
                            f"/v1/tenants/{fixture['tenant_id']}/decisions/count", 200,
                            params={"object_id": fields["object_id"], "object_type": "transactions"})
                        require(counts.get("count") == 0, "unexpected_persisted_decision")
                    if ids:
                        foreign = fixtures[(fixtures.index(fixture)+1) % len(fixtures)]["tenant_id"] if len(fixtures)>1 else str(uuid.uuid4())
                        denied = await clients.decision_engine.get(f"/v1/tenants/{foreign}/decisions/{ids[0]}")
                        require(denied.status_code in (403, 404), "cross_tenant_read")
                    row.update(passed=True, status=code)
                    checked += 1
                except Exception as exc:
                    row.update(passed=False, error=str(exc), error_type=type(exc).__name__)
                    raise
                finally:
                    records.write(json.dumps(row, allow_nan=False)+"\n")
                    records.flush()
        # Wrong and absent credentials must fail before evaluating an object.
        base = f"/v1/tenants/{fixtures[0]['tenant_id']}/decisions/count"
        for value in ("", "Bearer invalid-benchmark-credential"):
            response = await clients.decision_engine.get(base, headers={"Authorization": value})
            require(response.status_code == 401, "authentication_not_enforced")
    return checked


async def phase(clients, exp: Experiment, fixtures, aggregates, output: Path, name: str, duration: float) -> dict:
    if shutil.disk_usage(output).free < 512 * 1024 * 1024:
        raise RuntimeError("minimum_disk_reserve_512MiB")
    counters = Counter()
    histograms = {x: LatencyMetric() for x in ("all_attempts", "successful", "failed", "scheduled_completion", "dispatch_lag", "verification")}
    started = time.monotonic()
    tasks = set()
    next_index, peak = 0, 0
    failure = None
    prefix = f"{name}-{uuid.uuid4().hex[:12]}"
    file = (output / f"{name}-requests.ndjson").open("x", encoding="utf-8")
    background = asyncio.create_task(ingestion_interference(clients, exp, fixtures, output, name, duration, started)) if exp.ingestion_rate and duration else None

    async def operation(index, scheduled):
        if index % 1000 == 0 and shutil.disk_usage(output).free < 512 * 1024 * 1024:
            raise RuntimeError("minimum_disk_reserve_512MiB")
        fixture = fixtures[index % len(fixtures)]
        fields = payload(exp, index, prefix)
        dispatched = time.monotonic()
        lag = (dispatched-scheduled)*1000
        histograms["dispatch_lag"].add(lag)
        row = {"index": index, "tenant_index": index % len(fixtures), "object_id": fields["object_id"],
               "scheduled_s": scheduled-started, "dispatched_s": dispatched-started,
               "payload_sha256": digest(fields), "payload_bytes": len(json.dumps(fields).encode()), "decisions": []}
        response_at = None
        try:
            async with asyncio.timeout(exp.timeout):
                body, code, size = await evaluate(clients, exp, fixture, fields)
            response_at = time.monotonic()
            row.update(status=code, response_bytes=size)
            verify_started = time.monotonic()
            ids = verify(exp, fixture, fields, body, code, aggregates)
            row["decisions"] = decision_signatures(body)
            histograms["verification"].add((time.monotonic()-verify_started)*1000)
            counters["correct"] += 1
            counters["scenario_decisions"] += len(ids)
            counters["rule_executions"] += len(ids)*exp.rules
            counters["trigger_misses"] += exp.scenarios-len(ids)
            histograms["successful"].add((response_at-dispatched)*1000)
            row["correct"] = True
        except (httpx.HTTPError, TimeoutError, ValueError, CorrectnessError) as exc:
            counters["failed"] += 1
            counters["timeouts"] += int(isinstance(exc, (TimeoutError, httpx.TimeoutException)))
            counters["correctness_failures"] += int(isinstance(exc, CorrectnessError))
            counters["unresolved"] += 1  # Errors may follow committed scenario writes.
            row.update(correct=False, error_type=type(exc).__name__, error=str(exc), commit_state="unresolved")
            histograms["failed"].add(((response_at or time.monotonic())-dispatched)*1000)
        finally:
            ended = time.monotonic()
            response_at = response_at or ended
            histograms["all_attempts"].add((response_at-dispatched)*1000)
            histograms["scheduled_completion"].add((ended-scheduled)*1000)
            counters["completed"] += 1
            row.update(response_s=response_at-started, completed_s=ended-started)
            file.write(json.dumps(row, allow_nan=False)+"\n")

    def finished(task):
        nonlocal failure
        tasks.discard(task)
        if not task.cancelled() and task.exception():
            failure = task.exception()

    async def closed_worker():
        nonlocal next_index
        while time.monotonic() < started+duration:
            if failure:
                raise failure
            index = next_index
            next_index += 1
            if index >= 1_000_000:
                raise RuntimeError("operation_limit")
            counters["offered"] += 1
            counters["submitted"] += 1
            await operation(index, time.monotonic())

    try:
        if duration:
            if exp.mode == "closed":
                async with asyncio.TaskGroup() as group:
                    for _ in range(exp.concurrency):
                        group.create_task(closed_worker())
                peak = exp.concurrency
            else:
                async for _, slot, scheduled in arrivals([Stage(name, exp.rate, duration)], started):
                    if failure:
                        raise failure
                    counters["offered"] += 1
                    if len(tasks) >= exp.concurrency or time.monotonic()-scheduled > exp.max_lag:
                        counters["generator_dropped"] += 1
                        file.write(json.dumps({"index": slot, "scheduled_s": scheduled-started, "generator_dropped": True})+"\n")
                        continue
                    counters["submitted"] += 1
                    task = asyncio.create_task(operation(slot, scheduled))
                    tasks.add(task)
                    task.add_done_callback(finished)
                    peak = max(peak, len(tasks))
                if tasks:
                    await asyncio.gather(*tasks)
                if failure:
                    raise failure
    finally:
        for task in tasks:
            task.cancel()
        await asyncio.gather(*tasks, return_exceptions=True)
        file.close()
        if background and not background.done():
            # On normal completion its schedule should have elapsed; bound final drain.
            try:
                await asyncio.wait_for(asyncio.shield(background), timeout=exp.timeout+1)
            except BaseException:
                background.cancel()
                await asyncio.gather(background, return_exceptions=True)
                raise
    elapsed = time.monotonic()-started
    counts = {key: counters[key] for key in ("offered", "submitted", "completed", "correct", "failed", "timeouts",
        "correctness_failures", "unresolved", "generator_dropped", "scenario_decisions", "rule_executions", "trigger_misses")}
    result = {"counts": counts, "elapsed_seconds": elapsed, "schedule_seconds": duration,
              "drain_seconds": max(0, elapsed-duration), "peak_inflight": peak,
              "prefix": prefix, "latencies": {k: latency_summary(v) for k, v in histograms.items()},
              "rates": {"correct_inputs_per_second": counters["correct"]/elapsed,
                        "scenario_decisions_per_second": counters["scenario_decisions"]/elapsed,
                        "submitted_per_schedule_second": counters["submitted"]/duration if duration else None,
                        "error_rate": counters["failed"]/counters["submitted"] if counters["submitted"] else None,
                        "timeout_rate": counters["timeouts"]/counters["submitted"] if counters["submitted"] else None,
                        "retry_rate": 0}}
    result["concurrent_ingestion"] = background.result() if background else {"enabled": False}
    write_json_atomic(output / f"{name}-summary.json", result)
    return result


async def ingestion_interference(clients, exp, fixtures, output, name, duration, started):
    """Use a disjoint merchant key: changing table volume, fixed selected-history oracle."""
    counters = Counter()
    tasks = set()
    errors = []
    prefix = uuid.uuid4().hex
    with (output / f"{name}-ingestion.ndjson").open("x", encoding="utf-8") as file:
        async def ingest(index, scheduled):
            fields = {"object_id": f"interference-{prefix}-{index}", "sequence": index,
                      "amount": 1.0, "merchant_id": "background-unselected", "destination": "background",
                      "timestamp": "2026-01-15T11:59:00Z"}
            start = time.monotonic()
            row = {"index": index, "scheduled_s": scheduled-started}
            try:
                async with asyncio.timeout(exp.timeout):
                    response, attempts = await clients.ingest_one(fixtures[index % len(fixtures)]["tenant_id"], "history",
                        fields, digest(fields), max_attempts=1)
                if response.get("deferred") is True:
                    raise CorrectnessError("deferred_ingestion")
                result = response.get("result", {})
                require(result.get("object_id") == fields["object_id"] and bool(result.get("revision_id")),
                        "ingestion_identity_or_revision")
                require(attempts == 1 and result.get("replayed") is False, "unexpected_ingestion_replay")
                counters["completed"] += 1
                row.update(passed=True, attempts=attempts)
            except Exception as exc:
                counters["failed"] += 1
                row.update(passed=False, error_type=type(exc).__name__)
            finally:
                row["latency_ms"] = (time.monotonic()-start)*1000
                file.write(json.dumps(row)+"\n")
        def finished(task):
            tasks.discard(task)
            if not task.cancelled() and task.exception():
                errors.append(task.exception())
        try:
            async for _, index, scheduled in arrivals([Stage("ingestion", exp.ingestion_rate, duration)], started):
                if errors:
                    raise errors[0]
                counters["offered"] += 1
                if len(tasks) >= exp.ingestion_concurrency or time.monotonic()-scheduled > exp.max_lag:
                    counters["dropped"] += 1
                    continue
                counters["submitted"] += 1
                task = asyncio.create_task(ingest(index, scheduled))
                tasks.add(task)
                task.add_done_callback(finished)
            if tasks:
                await asyncio.gather(*tasks)
            if errors:
                raise errors[0]
        finally:
            for task in tasks:
                task.cancel()
            await asyncio.gather(*tasks, return_exceptions=True)
    return {"enabled": True, "counts": dict(counters), "target_rate": exp.ingestion_rate,
            "valid": counters["failed"] == 0 and counters["dropped"] == 0,
            "oracle": "background merchant excluded by all generated aggregate filters"}


async def reconcile_measurements(clients, fixtures, output: Path) -> dict:
    checked, failed, unresolved = 0, 0, 0
    with (output / "measured-requests.ndjson").open(encoding="utf-8") as source, \
         (output / "reconciliation.ndjson").open("x", encoding="utf-8") as target:
        for line in source:
            row = json.loads(line)
            if row.get("generator_dropped"):
                continue
            fixture = fixtures[row["tenant_index"]]
            tid = fixture["tenant_id"]
            if not row.get("correct"):
                unresolved += 1
                target.write(json.dumps({"object_id": row["object_id"], "state": "unresolved_no_retry"})+"\n")
                continue
            count = await clients.request(clients.decision_engine, "GET", f"/v1/tenants/{tid}/decisions/count", 200,
                                          params={"object_id": row["object_id"], "object_type": "transactions"})
            count_matches = count.get("count") == len(row["decisions"])
            failed += not count_matches
            target.write(json.dumps({"object_id": row["object_id"], "decision_count_matches": count_matches})+"\n")
            for signature in row["decisions"]:
                stored = await clients.request(clients.decision_engine, "GET",
                    f"/v1/tenants/{tid}/decisions/{signature['id']}", 200)
                stored["decision"].pop("request_body", None)
                actual = digest({"decision": stored["decision"],
                                 "rule_executions": sorted(stored["rule_executions"], key=lambda x: x["rule_id"])})
                good = signature["sha256"] == actual
                checked += 1
                failed += not good
                target.write(json.dumps({"decision_id": signature["id"], "passed": good})+"\n")
    return {"decisions_checked": checked, "mismatches": failed, "unresolved_inputs": unresolved}


async def run_experiment(clients, exp, output, endpoints, auth_token, database=None, database_check=None):
    output.mkdir(exist_ok=False)
    write_json_atomic(output / "manifest.json", {"schema_version": VERSION, "experiment": exp.manifest(), "sha256": digest(exp.manifest())})
    summary = {"schema_version": VERSION, "benchmark_id": exp.name, "status": "preparing", "production_qualified": False,
               "configuration": exp.manifest(), "valid": False,
               "limitations": ["disposable_local_environment", "no_SLO_qualification", "per_rule_latency_unavailable",
                               "immutable_selected_history", "synchronous_decisions_only", "no_automatic_decision_retries",
                               "HTTP_errors_can_have_partial_commits", "DB_OS_cache_state_unknown"]}
    try:
        fixtures = [await setup(clients, exp, output, i) for i in range(exp.tenants)]
        aggregates = history_oracle(exp)
        write_json_atomic(output / "aggregate-oracle.json", aggregates)
        if database_check:
            summary["dataset_verification"] = await database_check(fixtures, output)
        summary["qualification_cases"] = await qualify(clients, exp, fixtures, aggregates, output)
        summary["warmup"] = await phase(clients, exp, fixtures, aggregates, output, "warmup", exp.warmup)
        require(summary["warmup"]["counts"]["failed"] == 0, "warmup_correctness_gate")
        async with RuntimeObserver(output / "telemetry.ndjson", endpoints, auth_token=auth_token,
                                   interval=1, timeout=3, database=database) as observer:
            summary["measurement"] = await observer.run_during(
                lambda: phase(clients, exp, fixtures, aggregates, output, "measured", exp.duration))
            summary["observation"] = observer.summary()
        summary["reconciliation"] = await reconcile_measurements(clients, fixtures, output)
        counts = summary["measurement"]["counts"]
        summary["valid"] = (counts["submitted"] > 0 and counts["failed"] == 0 and counts["generator_dropped"] == 0
                            and summary["reconciliation"]["mismatches"] == 0
                            and summary["measurement"]["concurrent_ingestion"].get("valid", True))
        summary["status"] = "completed" if summary["valid"] else "invalid"
        return summary
    except BaseException as exc:
        summary.update(status="failed", error_type=type(exc).__name__, error=str(exc))
        raise
    finally:
        write_json_atomic(output / "summary.json", summary)
