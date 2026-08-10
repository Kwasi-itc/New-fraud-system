from __future__ import annotations

import asyncio
import hashlib
import json
import re
import time
from collections import Counter
from collections.abc import Callable, Iterable
from dataclasses import dataclass, field
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Awaitable

from .api_client import APIError, ServiceClients
from .domain import TransactionEvent


_SUCCESS_LOG_BATCH_SIZE = 100


def _append_text(path: Path, value: str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("a", encoding="utf-8") as handle:
        handle.write(value)


@dataclass
class LatencyMetric:
    count: int = 0
    total_ms: float = 0.0
    min_ms: float | None = None
    max_ms: float | None = None
    buckets: Counter[int] = field(default_factory=Counter)

    def add(self, value: float) -> None:
        value = max(value, 0.0)
        self.count += 1
        self.total_ms += value
        self.min_ms = value if self.min_ms is None else min(self.min_ms, value)
        self.max_ms = value if self.max_ms is None else max(self.max_ms, value)
        self.buckets[_latency_bucket(value)] += 1

    def summary(self) -> dict[str, float | int | bool | None]:
        if self.count == 0:
            return {
                "count": 0,
                "min_ms": None,
                "avg_ms": None,
                "p50_ms": None,
                "p95_ms": None,
                "p99_ms": None,
                "max_ms": None,
                "percentiles_approximate": True,
            }
        return {
            "count": self.count,
            "min_ms": round(self.min_ms or 0.0, 2),
            "avg_ms": round(self.total_ms / self.count, 2),
            "p50_ms": self._percentile(50),
            "p95_ms": self._percentile(95),
            "p99_ms": self._percentile(99),
            "max_ms": round(self.max_ms or 0.0, 2),
            "percentiles_approximate": True,
        }

    def to_state(self) -> dict[str, Any]:
        return {
            "count": self.count,
            "total_ms": self.total_ms,
            "min_ms": self.min_ms,
            "max_ms": self.max_ms,
            "buckets": {str(key): value for key, value in self.buckets.items()},
        }

    @classmethod
    def from_state(cls, value: dict[str, Any]) -> "LatencyMetric":
        return cls(
            count=int(value.get("count", 0)),
            total_ms=float(value.get("total_ms", 0.0)),
            min_ms=_optional_float(value.get("min_ms")),
            max_ms=_optional_float(value.get("max_ms")),
            buckets=Counter({int(key): int(count) for key, count in value.get("buckets", {}).items()}),
        )

    def _percentile(self, percentile: int) -> float:
        target = max(1, ((percentile * self.count) + 99) // 100)
        cumulative = 0
        for bucket, count in sorted(self.buckets.items()):
            cumulative += count
            if cumulative >= target:
                return float(bucket)
        return float(max(self.buckets, default=0))


@dataclass(frozen=True)
class ReplayCursor:
    occurred_at: str
    sequence: int
    stream_id: str
    object_id: str

    @classmethod
    def from_event(cls, event: TransactionEvent) -> "ReplayCursor":
        if event.sequence is None:
            raise ValueError("sorted replay event has no sequence")
        return cls(_iso(event.occurred_at) or "", event.sequence, event.stream_id, event.object_id)

    @classmethod
    def from_dict(cls, value: dict[str, Any]) -> "ReplayCursor":
        return cls(
            occurred_at=str(value["occurred_at"]),
            sequence=int(value["sequence"]),
            stream_id=str(value["stream_id"]),
            object_id=str(value["object_id"]),
        )


@dataclass
class ReplayMetrics:
    scheduled: int = 0
    completed: int = 0
    ingestion_successes: int = 0
    ingestion_failures: int = 0
    decision_successes: int = 0
    decision_failures: int = 0
    ingestion_retries: int = 0
    max_observed_concurrency: int = 0
    ingestion_latencies_ms: LatencyMetric = field(default_factory=LatencyMetric)
    decision_latencies_ms: LatencyMetric = field(default_factory=LatencyMetric)
    end_to_end_latencies_ms: LatencyMetric = field(default_factory=LatencyMetric)
    schedule_lag_ms: LatencyMetric = field(default_factory=LatencyMetric)
    streams: dict[str, dict[str, int]] = field(default_factory=dict)
    errors: list[dict[str, Any]] = field(default_factory=list)

    def summary(self, *, multiplier: float, source_start: datetime | None, source_end: datetime | None) -> dict[str, Any]:
        has_errors = self.ingestion_failures > 0 or self.decision_failures > 0
        return {
            "summary_version": 1,
            "status": "completed_with_errors" if has_errors else "completed",
            "completed_at": datetime.now(timezone.utc).isoformat().replace("+00:00", "Z"),
            "multiplier": multiplier,
            "source_start": _iso(source_start),
            "source_end": _iso(source_end),
            "scheduled": self.scheduled,
            "completed": self.completed,
            "ingestion": {
                "successes": self.ingestion_successes,
                "failures": self.ingestion_failures,
                "retries": self.ingestion_retries,
                "latency": self.ingestion_latencies_ms.summary(),
            },
            "decision": {
                "successes": self.decision_successes,
                "failures": self.decision_failures,
                "latency": self.decision_latencies_ms.summary(),
            },
            "end_to_end_latency": self.end_to_end_latencies_ms.summary(),
            "schedule_lag": self.schedule_lag_ms.summary(),
            "max_observed_concurrency": self.max_observed_concurrency,
            "streams": self.streams,
            "sampled_errors": self.errors,
            "sampled_error_breakdown": build_error_breakdown(self.errors),
            "acceptance_thresholds": None,
        }

    def stream(self, stream_id: str) -> dict[str, int]:
        return self.streams.setdefault(
            stream_id,
            {
                "scheduled": 0,
                "completed": 0,
                "ingestion_successes": 0,
                "ingestion_failures": 0,
                "decision_successes": 0,
                "decision_failures": 0,
            },
        )

    def to_state(self) -> dict[str, Any]:
        return {
            "scheduled": self.scheduled,
            "completed": self.completed,
            "ingestion_successes": self.ingestion_successes,
            "ingestion_failures": self.ingestion_failures,
            "decision_successes": self.decision_successes,
            "decision_failures": self.decision_failures,
            "ingestion_retries": self.ingestion_retries,
            "max_observed_concurrency": self.max_observed_concurrency,
            "ingestion_latencies_ms": self.ingestion_latencies_ms.to_state(),
            "decision_latencies_ms": self.decision_latencies_ms.to_state(),
            "end_to_end_latencies_ms": self.end_to_end_latencies_ms.to_state(),
            "schedule_lag_ms": self.schedule_lag_ms.to_state(),
            "streams": self.streams,
            "errors": self.errors,
        }

    @classmethod
    def from_state(cls, value: dict[str, Any]) -> "ReplayMetrics":
        return cls(
            scheduled=int(value.get("scheduled", 0)),
            completed=int(value.get("completed", 0)),
            ingestion_successes=int(value.get("ingestion_successes", 0)),
            ingestion_failures=int(value.get("ingestion_failures", 0)),
            decision_successes=int(value.get("decision_successes", 0)),
            decision_failures=int(value.get("decision_failures", 0)),
            ingestion_retries=int(value.get("ingestion_retries", 0)),
            max_observed_concurrency=int(value.get("max_observed_concurrency", 0)),
            ingestion_latencies_ms=LatencyMetric.from_state(value.get("ingestion_latencies_ms", {})),
            decision_latencies_ms=LatencyMetric.from_state(value.get("decision_latencies_ms", {})),
            end_to_end_latencies_ms=LatencyMetric.from_state(value.get("end_to_end_latencies_ms", {})),
            schedule_lag_ms=LatencyMetric.from_state(value.get("schedule_lag_ms", {})),
            streams={str(key): {str(name): int(count) for name, count in stats.items()} for key, stats in value.get("streams", {}).items()},
            errors=list(value.get("errors", []))[:100],
        )


class TransactionChain:
    def __init__(
        self,
        clients: ServiceClients,
        tenant_id: str,
        metrics: ReplayMetrics,
        max_in_flight: int,
        decision_mode: str = "sync",
        async_wait_timeout_ms: int = 0,
        async_callback_url: str = "",
        async_tracking_path: Path | None = None,
        error_log_path: Path | None = None,
        success_log_path: Path | None = None,
    ) -> None:
        if decision_mode not in {"sync", "async"}:
            raise ValueError("decision_mode must be 'sync' or 'async'")
        self.clients = clients
        self.tenant_id = tenant_id
        self.metrics = metrics
        self.semaphore = asyncio.Semaphore(max_in_flight)
        self.decision_mode = decision_mode
        self.async_wait_timeout_ms = async_wait_timeout_ms
        self.async_callback_url = async_callback_url
        self.async_tracking_path = async_tracking_path
        self.error_log_path = error_log_path
        self.success_log_path = success_log_path
        self._active = 0
        self._lock = asyncio.Lock()
        self._tracking_lock = asyncio.Lock()
        self._error_log_lock = asyncio.Lock()
        self._success_log_lock = asyncio.Lock()
        self._success_log_buffer: list[str] = []

    async def __call__(self, event: TransactionEvent, schedule_lag_ms: float) -> None:
        async with self.semaphore:
            async with self._lock:
                self._active += 1
                self.metrics.max_observed_concurrency = max(self.metrics.max_observed_concurrency, self._active)
            started = time.perf_counter()
            try:
                stream_metrics = self.metrics.stream(event.stream_id)
                self.metrics.schedule_lag_ms.add(schedule_lag_ms)
                ingestion_request_started_at = _now_iso()
                ingestion_started = time.perf_counter()
                idempotency_key = _event_idempotency_key(self.tenant_id, event.object_id)
                try:
                    response, attempts = await self.clients.ingest_one(
                        self.tenant_id,
                        "transactions",
                        event.fields,
                        idempotency_key,
                        max_attempts=3,
                    )
                    ingestion_response_received_at = _now_iso()
                    ingestion_latency_ms = (time.perf_counter() - ingestion_started) * 1_000
                    self.metrics.ingestion_successes += 1
                    stream_metrics["ingestion_successes"] += 1
                    self.metrics.ingestion_retries += attempts - 1
                    self.metrics.ingestion_latencies_ms.add(ingestion_latency_ms)
                    await self._record_success(
                        event,
                        stage="ingestion",
                        request_started_at=ingestion_request_started_at,
                        response_received_at=ingestion_response_received_at,
                        latency_ms=ingestion_latency_ms,
                        attempts=attempts,
                        request={
                            "method": "POST",
                            "path": f"/v1/tenants/{self.tenant_id}/ingest/transactions",
                            "headers": {"Idempotency-Key": idempotency_key},
                            "body": event.fields,
                        },
                        response={"status_code": 200, "body": response},
                    )
                except APIError as exc:
                    self.metrics.ingestion_failures += 1
                    stream_metrics["ingestion_failures"] += 1
                    await self._record_error(event, "ingestion", exc)
                    return

                decision_request_started_at = _now_iso()
                decision_started = time.perf_counter()
                try:
                    if self.decision_mode == "async":
                        async_idempotency_key = _async_decision_idempotency_key(self.tenant_id, event.object_id)
                        request_body: dict[str, Any] = {
                            "object_type": "transactions",
                            "idempotency_key": async_idempotency_key,
                            "wait_timeout_ms": self.async_wait_timeout_ms,
                            "items": [
                                {
                                    "object_id": event.object_id,
                                    "object_type": "transactions",
                                    "fields": event.fields,
                                }
                            ],
                        }
                        if self.async_callback_url:
                            request_body["callback_url"] = self.async_callback_url
                        response = await self.clients.create_async_decision_execution(
                            self.tenant_id,
                            event.object_id,
                            event.fields,
                            async_idempotency_key,
                            wait_timeout_ms=self.async_wait_timeout_ms,
                            callback_url=self.async_callback_url,
                        )
                        decision_response_received_at = _now_iso()
                        decision_latency_ms = (time.perf_counter() - decision_started) * 1_000
                        decision_status_code = 201
                        decision_path = f"/v1/tenants/{self.tenant_id}/async-decision-executions"
                        await self._record_async_submission(
                            event,
                            response,
                            decision_request_started_at,
                            decision_started,
                        )
                    else:
                        request_body = {
                            "object_id": event.object_id,
                            "object_type": "transactions",
                            "fields": event.fields,
                            "source": "production_replay",
                        }
                        response = await self.clients.decide_once(self.tenant_id, event.object_id, event.fields)
                        decision_response_received_at = _now_iso()
                        decision_latency_ms = (time.perf_counter() - decision_started) * 1_000
                        decision_status_code = 200
                        decision_path = f"/v1/tenants/{self.tenant_id}/ingestion-events/record-ingested"
                    self.metrics.decision_successes += 1
                    stream_metrics["decision_successes"] += 1
                    self.metrics.decision_latencies_ms.add(decision_latency_ms)
                    await self._record_success(
                        event,
                        stage="decision",
                        request_started_at=decision_request_started_at,
                        response_received_at=decision_response_received_at,
                        latency_ms=decision_latency_ms,
                        attempts=1,
                        request={
                            "method": "POST",
                            "path": decision_path,
                            "headers": {},
                            "body": request_body,
                        },
                        response={"status_code": decision_status_code, "body": response},
                    )
                except APIError as exc:
                    self.metrics.decision_failures += 1
                    stream_metrics["decision_failures"] += 1
                    await self._record_error(event, "decision", exc)
            finally:
                self.metrics.completed += 1
                self.metrics.stream(event.stream_id)["completed"] += 1
                self.metrics.end_to_end_latencies_ms.add((time.perf_counter() - started) * 1_000)
                async with self._lock:
                    self._active -= 1

    async def _record_success(
        self,
        event: TransactionEvent,
        *,
        stage: str,
        request_started_at: str,
        response_received_at: str,
        latency_ms: float,
        attempts: int,
        request: dict[str, Any],
        response: dict[str, Any],
    ) -> None:
        if self.success_log_path is None:
            return
        record = {
            "stage": stage,
            "request_started_at": request_started_at,
            "response_received_at": response_received_at,
            "latency_ms": round(latency_ms, 2),
            "attempts": attempts,
            "tenant_id": self.tenant_id,
            "stream_id": event.stream_id,
            "object_id": event.object_id,
            "source_file": event.source_file.name,
            "row_number": event.row_number,
            "request": request,
            "response": response,
        }
        line = json.dumps(record, sort_keys=True, default=str) + "\n"
        async with self._success_log_lock:
            self._success_log_buffer.append(line)
            if len(self._success_log_buffer) >= _SUCCESS_LOG_BATCH_SIZE:
                await self._flush_success_log_locked()

    async def flush_success_log(self) -> None:
        async with self._success_log_lock:
            await self._flush_success_log_locked()

    async def _flush_success_log_locked(self) -> None:
        if self.success_log_path is None or not self._success_log_buffer:
            return
        payload = "".join(self._success_log_buffer)
        await asyncio.to_thread(_append_text, self.success_log_path, payload)
        self._success_log_buffer.clear()

    async def _record_async_submission(
        self,
        event: TransactionEvent,
        response: dict[str, Any],
        request_started_at: str,
        decision_started: float,
    ) -> None:
        if self.async_tracking_path is None:
            return
        execution = response.get("async_decision_execution")
        if not isinstance(execution, dict):
            execution = {}
        record = {
            "request_started_at": request_started_at,
            "response_received_at": _now_iso(),
            "decision_request_latency_ms": round((time.perf_counter() - decision_started) * 1_000, 2),
            "tenant_id": self.tenant_id,
            "object_id": event.object_id,
            "stream_id": event.stream_id,
            "source_file": event.source_file.name,
            "row_number": event.row_number,
            "execution_id": str(execution.get("id") or ""),
            "completed_inline": bool(response.get("completed_inline")),
            "status": str(execution.get("status") or ""),
            "callback_url": self.async_callback_url or None,
        }
        line = json.dumps(record, sort_keys=True) + "\n"
        async with self._tracking_lock:
            self.async_tracking_path.parent.mkdir(parents=True, exist_ok=True)
            with self.async_tracking_path.open("a", encoding="utf-8") as handle:
                handle.write(line)

    async def _record_error(self, event: TransactionEvent, stage: str, error: Exception) -> None:
        record = {
            "stage": stage,
            "stream_id": event.stream_id,
            "object_id": event.object_id,
            "source_file": event.source_file.name,
            "row_number": event.row_number,
            "error": str(error)[:1_000],
        }
        if len(self.metrics.errors) < 100:
            self.metrics.errors.append(record)
        if self.error_log_path is None:
            return
        line = json.dumps(record, sort_keys=True) + "\n"
        async with self._error_log_lock:
            self.error_log_path.parent.mkdir(parents=True, exist_ok=True)
            with self.error_log_path.open("a", encoding="utf-8") as handle:
                handle.write(line)


_STATUS_CODE_RE = re.compile(r"returned\s+(\d{3})")


def build_error_breakdown(errors: list[dict[str, Any]]) -> dict[str, Any]:
    stage_counts: Counter[str] = Counter()
    status_counts: Counter[str] = Counter()
    classification_counts: Counter[str] = Counter()
    route_counts: Counter[str] = Counter()
    classification_examples: dict[str, str] = {}

    for item in errors:
        stage = str(item.get("stage") or "unknown")
        message = str(item.get("error") or "")
        classification = classify_error(stage, message)
        route = classify_route_hint(message)
        status_code = extract_status_code(message)

        stage_counts[stage] += 1
        classification_counts[classification] += 1
        route_counts[route] += 1
        if status_code is not None:
            status_counts[status_code] += 1
        classification_examples.setdefault(classification, message[:240])

    top_classifications = []
    for name, count in classification_counts.most_common(10):
        top_classifications.append(
            {
                "classification": name,
                "count": count,
                "example": classification_examples.get(name),
            }
        )

    return {
        "total_errors": sum(stage_counts.values()),
        "by_stage": dict(sorted(stage_counts.items())),
        "by_status_code": dict(sorted(status_counts.items())),
        "by_route_hint": dict(sorted(route_counts.items())),
        "by_classification": dict(sorted(classification_counts.items())),
        "top_classifications": top_classifications,
    }


def extract_status_code(message: str) -> str | None:
    match = _STATUS_CODE_RE.search(message)
    if match is None:
        return None
    return match.group(1)


def classify_route_hint(message: str) -> str:
    if "/ingestion-events/record-ingested" in message:
        return "decision_callback"
    if "/records/" in message and "?limit=" in message:
        return "tenant_data_list_read"
    if "/records/" in message:
        return "record_write"
    if "/async-decision-executions" in message:
        return "async_decision_execution"
    return "other"


def classify_error(stage: str, message: str) -> str:
    lower = message.lower()
    status_code = extract_status_code(message)
    if stage == "decision":
        if "record_ingested_processing_failed" in lower:
            if "context deadline exceeded" in lower or "client.timeout exceeded" in lower:
                return "decision.record_ingested_processing_failed.timeout"
            return "decision.record_ingested_processing_failed"
        if status_code == "429":
            return "decision.overload"
        if "context deadline exceeded" in lower or "client.timeout exceeded" in lower:
            return "decision.timeout"
        if status_code is not None and status_code.startswith("5"):
            return "decision.server_error"
        return "decision.other"

    if stage == "ingestion":
        if status_code == "429":
            return "ingestion.overload"
        if "context deadline exceeded" in lower or "client.timeout exceeded" in lower:
            return "ingestion.timeout"
        if status_code == "400":
            if any(keyword in lower for keyword in ("validation", "invalid", "required", "must be", "malformed")):
                return "ingestion.validation"
            return "ingestion.bad_request"
        if status_code == "409":
            return "ingestion.conflict"
        if status_code is not None and status_code.startswith("5"):
            return "ingestion.server_error"
        return "ingestion.other"

    return f"{stage}.other"


async def schedule_events(
    events: Iterable[TransactionEvent],
    *,
    multiplier: float,
    max_in_flight: int,
    processor: Callable[[TransactionEvent, float], Awaitable[None]],
    metrics: ReplayMetrics | None = None,
    resume_after: ReplayCursor | None = None,
    checkpoint_every: int = 10_000,
    checkpoint: Callable[[ReplayCursor, datetime, datetime], Awaitable[None]] | None = None,
) -> tuple[datetime | None, datetime | None]:
    if multiplier <= 0:
        raise ValueError("multiplier must be positive")
    if max_in_flight <= 0:
        raise ValueError("max_in_flight must be positive")
    if checkpoint_every <= 0:
        raise ValueError("checkpoint_every must be positive")
    iterator = iter(_events_after_cursor(events, resume_after))
    try:
        current = next(iterator)
    except StopIteration:
        return None, None

    source_start = current.occurred_at
    source_end = current.occurred_at
    wall_start = time.monotonic()
    pending: set[asyncio.Task[None]] = set()
    lookahead: TransactionEvent | None = None
    since_checkpoint = 0
    last_event: TransactionEvent | None = None

    while current is not None:
        timestamp = current.occurred_at
        group = [current]
        while True:
            try:
                lookahead = next(iterator)
            except StopIteration:
                lookahead = None
                break
            if lookahead.occurred_at != timestamp:
                break
            group.append(lookahead)

        due = wall_start + ((timestamp - source_start).total_seconds() / multiplier)
        delay = due - time.monotonic()
        if delay > 0:
            await asyncio.sleep(delay)
        lag_ms = max((time.monotonic() - due) * 1_000, 0.0)
        source_end = timestamp
        for event in group:
            if metrics is not None:
                metrics.scheduled += 1
                metrics.stream(event.stream_id)["scheduled"] += 1
            task = asyncio.create_task(processor(event, lag_ms))
            pending.add(task)
            since_checkpoint += 1
            last_event = event

        # Bound scheduler-owned tasks while preserving simultaneous launch for one timestamp group.
        if len(pending) >= max(max_in_flight * 4, max_in_flight + 1):
            done, pending = await asyncio.wait(pending, return_when=asyncio.FIRST_COMPLETED)
            await asyncio.gather(*done)
        if checkpoint is not None and since_checkpoint >= checkpoint_every and last_event is not None:
            if pending:
                await asyncio.gather(*pending)
                pending.clear()
            await checkpoint(ReplayCursor.from_event(last_event), source_start, source_end)
            since_checkpoint = 0
        current = lookahead

    if pending:
        await asyncio.gather(*pending)
    if checkpoint is not None and since_checkpoint and last_event is not None:
        await checkpoint(ReplayCursor.from_event(last_event), source_start, source_end)
    return source_start, source_end


def _event_idempotency_key(tenant_id: str, object_id: str) -> str:
    digest = hashlib.sha256(f"{tenant_id}:{object_id}".encode()).hexdigest()
    return f"production-replay:{digest}"


def _async_decision_idempotency_key(tenant_id: str, object_id: str) -> str:
    digest = hashlib.sha256(f"async:{tenant_id}:{object_id}".encode()).hexdigest()
    return f"production-replay-async:{digest}"


def _events_after_cursor(
    events: Iterable[TransactionEvent], cursor: ReplayCursor | None
) -> Iterable[TransactionEvent]:
    if cursor is None:
        yield from events
        return
    found = False
    for event in events:
        if not found:
            if (
                event.sequence == cursor.sequence
                and _iso(event.occurred_at) == cursor.occurred_at
                and event.stream_id == cursor.stream_id
                and event.object_id == cursor.object_id
            ):
                found = True
            continue
        yield event
    if not found:
        raise ValueError("resume cursor was not found in the sorted source data")


def _latency_bucket(value: float) -> int:
    if value < 1_000:
        return int(round(value))
    if value < 10_000:
        return int(round(value / 10.0) * 10)
    if value < 60_000:
        return int(round(value / 100.0) * 100)
    return int(round(value / 1_000.0) * 1_000)


def _optional_float(value: Any) -> float | None:
    return None if value is None else float(value)


def _iso(value: datetime | None) -> str | None:
    return value.isoformat().replace("+00:00", "Z") if value else None


def _now_iso() -> str:
    return datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")
