"""Read-only, named River queue snapshots and sample-based recovery checks."""
from __future__ import annotations

import asyncio
import json
import math
import re
import time
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Awaitable, Callable

from production_replay.postgres_observation import PostgresObserver


STATES = ("available", "running", "retryable", "scheduled", "pending")


def queue_sql(schema: str, queues: list[str], cap: int) -> str:
    if (not re.fullmatch(r"[a-zA-Z_][a-zA-Z0-9_]{0,62}", schema)
            or not 1 <= len(queues) <= 16 or len(set(queues)) != len(queues)
            or any(not re.fullmatch(r"[a-zA-Z0-9_-]{1,128}", q) for q in queues)
            or type(cap) is not int or not 1 <= cap <= 100_000):
        raise ValueError("invalid_queue_selection")
    rows = []
    for queue in queues:
        for state in STATES:
            # scheduled_at order makes the oldest due age exact for this prefix.
            # LIMIT bounds aggregation, not the cost of an unindexed sort: validate EXPLAIN.
            rows.append(f"""SELECT '{queue}' AS queue, '{state}' AS state,
                count(*) AS observed_jobs, count(*) > {cap} AS truncated,
                CASE WHEN '{state}' IN ('available', 'retryable', 'scheduled') THEN
                    coalesce(greatest(0, extract(epoch FROM current_timestamp - min(scheduled_at))), 0)
                    ELSE 0 END AS oldest_due_seconds
                FROM (SELECT scheduled_at FROM "{schema}".river_job
                      WHERE queue = '{queue}' AND state = '{state}'
                      ORDER BY scheduled_at LIMIT {cap + 1}) jobs""")
    names = ",".join(f"('{q}')" for q in queues)
    return f"""WITH requested(name) AS (VALUES {names}), states AS ({' UNION ALL '.join(rows)})
        SELECT json_build_object(
            'database', current_database(),
            'registry', (SELECT json_agg(json_build_object('queue', r.name,
                'registered', q.name IS NOT NULL, 'paused', q.paused_at IS NOT NULL))
                FROM requested r LEFT JOIN "{schema}".river_queue q ON q.name = r.name),
            'states', (SELECT json_agg(states) FROM states));"""


class QueueCollector:
    def __init__(self, database: PostgresObserver, expected_database: str,
                 schema: str, queues: list[str], cap: int = 1000) -> None:
        if not expected_database:
            raise ValueError("expected_database_required")
        self.database = database
        self.expected_database = expected_database
        self.queues = queues
        self.cap = cap
        self.sql = queue_sql(schema, queues, cap)

    async def __call__(self) -> dict[str, Any]:
        result = await self.database.query(self.sql)
        if result["status"] != "ok":
            return result
        try:
            raw = result["metrics"]
            if raw.get("database") != self.expected_database:
                raise ValueError("database_identity_mismatch")
            registry = raw["registry"]
            states = raw["states"]
            if len(registry) != len(self.queues) or len(states) != len(self.queues) * len(STATES):
                raise ValueError("incomplete_queue_snapshot")
            output = {}
            for queue in self.queues:
                identity = [r for r in registry if r["queue"] == queue]
                if (len(identity) != 1 or identity[0]["registered"] is not True
                        or type(identity[0]["paused"]) is not bool):
                    raise ValueError("queue_not_registered")
                selected = [row for row in states if row["queue"] == queue]
                if len(selected) != len(STATES) or {r["state"] for r in selected} != set(STATES):
                    raise ValueError("missing_queue_state")
                for row in selected:
                    count, age, truncated = row["observed_jobs"], row["oldest_due_seconds"], row["truncated"]
                    if (type(count) is not int or not 0 <= count <= self.cap + 1
                            or type(truncated) is not bool or truncated != (count > self.cap)
                            or type(age) not in (int, float) or not math.isfinite(age) or age < 0):
                        raise ValueError("invalid_queue_metric")
                output[queue] = {
                    "active_jobs": sum(r["observed_jobs"] for r in selected),
                    "counts_exact": not any(r["truncated"] for r in selected),
                    "oldest_due_seconds": max(r["oldest_due_seconds"] for r in selected),
                    "paused": identity[0]["paused"],
                    "states": {r["state"]: r["observed_jobs"] for r in selected},
                }
            return {"status": "ok", "queues": output}
        except (KeyError, TypeError, ValueError):
            return {"status": "error", "error_type": "InvalidQueueSnapshot"}


@dataclass(frozen=True)
class RecoveryPolicy:
    start: float
    deadline: float
    hold: float
    max_gap: float
    max_active: int
    max_due_age: float

    def __post_init__(self) -> None:
        if (not math.isfinite(self.start) or self.start < 0
                or any(not math.isfinite(v) or v <= 0 for v in
                       (self.deadline, self.hold, self.max_gap, self.max_due_age))
                or self.hold > self.deadline or type(self.max_active) is not int or self.max_active < 0):
            raise ValueError("invalid_recovery_policy")


class RecoveryGate:
    def __init__(self, queues: list[str], policy: RecoveryPolicy) -> None:
        if not queues or len(set(queues)) != len(queues):
            raise ValueError("unique_queues_required")
        self.queues = queues
        self.policy = policy
        self.previous: float | None = None
        self.healthy_since: float | None = None
        self.confirmed_at: float | None = None
        self.samples = 0
        self.invalid = 0
        self.peak_active: dict[str, int] = {}

    def observe(self, elapsed: float, snapshot: dict[str, Any]) -> None:
        if not math.isfinite(elapsed) or elapsed < 0:
            raise ValueError("invalid_sample_time")
        self.samples += 1
        # Multiple samples may share a clock tick on Windows; only backwards time is invalid.
        gap = self.previous is not None and (elapsed < self.previous or elapsed - self.previous > self.policy.max_gap)
        self.previous = elapsed
        queues = snapshot.get("queues", {})
        complete = snapshot.get("status") == "ok" and set(queues) == set(self.queues)
        if gap or not complete:
            self.invalid += 1
            self.healthy_since = self.confirmed_at = None
            return
        healthy = True
        for queue in self.queues:
            row = queues[queue]
            self.peak_active[queue] = max(self.peak_active.get(queue, 0), row["active_jobs"])
            healthy &= (row["counts_exact"] and not row["paused"]
                        and row["active_jobs"] <= self.policy.max_active
                        and row["oldest_due_seconds"] <= self.policy.max_due_age)
        if elapsed < self.policy.start:
            return
        if not healthy:
            self.healthy_since = self.confirmed_at = None
            return
        if self.healthy_since is None:
            self.healthy_since = elapsed
        if (self.confirmed_at is None and elapsed - self.healthy_since >= self.policy.hold
                and elapsed <= self.policy.start + self.policy.deadline):
            self.confirmed_at = elapsed

    def summary(self, *, finished: bool) -> dict[str, Any]:
        return {"enabled": True, "evaluated": finished, "passed": finished and self.invalid == 0
                    and self.confirmed_at is not None,
                "samples": self.samples, "invalid_samples_or_gaps": self.invalid,
                "confirmed_seconds_after_recovery_start": None if self.confirmed_at is None
                    else self.confirmed_at - self.policy.start,
                "peak_observed_active_jobs": self.peak_active,
                "policy": vars(self.policy), "queues": self.queues,
                "scope": "selected_river_queues_all_tenants_sampled_recovery",
                "limitations": ["between_sample_behavior_unobserved", "active_includes_future_and_pending_jobs",
                    "oldest_due_excludes_running_and_pending", "no_business_delivery_reconciliation",
                    "queue_registration_does_not_prove_worker_health", "query_plan_and_overhead_need_live_validation"]}


class QueueRecoveryMonitor:
    def __init__(self, collector: Callable[[], Awaitable[dict[str, Any]]], gate: RecoveryGate,
                 output: Path, interval: float) -> None:
        if not math.isfinite(interval) or interval <= 0 or interval > gate.policy.max_gap:
            raise ValueError("invalid_queue_interval")
        self.collector, self.gate, self.output, self.interval = collector, gate, output, interval
        self.finished = False

    async def run_during(self, workload: Callable[[], Awaitable[Any]]) -> Any:
        # Verify target database and queue registrations before generating mutations.
        preflight = await self.collector()
        if preflight["status"] != "ok":
            self.gate.observe(0, preflight)
            raise ValueError("queue_preflight_failed")
        started = time.monotonic()
        with self.output.open("x", encoding="utf-8") as handle:
            def save(snapshot: dict[str, Any]) -> None:
                elapsed = time.monotonic() - started
                self.gate.observe(elapsed, snapshot)
                handle.write(json.dumps({"elapsed_seconds": elapsed, **snapshot}, allow_nan=False) + "\n")
                handle.flush()

            save(preflight)
            work = asyncio.create_task(workload())
            async def poll() -> None:
                while True:
                    await asyncio.sleep(self.interval)
                    save(await self.collector())
            observer = asyncio.create_task(poll())
            try:
                done, _ = await asyncio.wait((work, observer), return_when=asyncio.FIRST_COMPLETED)
                if observer in done:
                    observer.result()
                result = await work
                observer.cancel()
                await asyncio.gather(observer, return_exceptions=True)
                save(await self.collector())
                self.finished = True
                return result
            finally:
                work.cancel()
                observer.cancel()
                await asyncio.gather(work, observer, return_exceptions=True)

    def summary(self) -> dict[str, Any]:
        return self.gate.summary(finished=self.finished)
