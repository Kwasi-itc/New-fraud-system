from __future__ import annotations

import asyncio
import hashlib
import time
from collections.abc import Callable, Iterator
from dataclasses import dataclass
from typing import Any

from .adapters import get_adapter
from .api_client import APIError, ServiceClients
from .manifest import ReplayManifest


@dataclass(frozen=True)
class SeedBatch:
    number: int
    records: list[dict[str, Any]]
    object_ids: tuple[str, ...]


@dataclass(frozen=True)
class SeedBatchResult:
    record_count: int
    replayed: bool


def iter_seed_batches(manifest: ReplayManifest, batch_size: int) -> Iterator[SeedBatch]:
    if batch_size <= 0 or batch_size > 500:
        raise ValueError("seed batch size must be between 1 and 500")

    stream_sources = [(stream, manifest.stream_files(stream)) for stream in manifest.transaction_streams]
    records: list[dict[str, Any]] = []
    object_ids: list[str] = []
    batch_number = 1
    for stream, files in stream_sources:
        adapter = get_adapter(stream.adapter)
        for event in adapter.iter_events(stream, files):
            records.append(event.fields)
            object_ids.append(event.object_id)
            if len(records) == batch_size:
                yield SeedBatch(batch_number, records, tuple(object_ids))
                batch_number += 1
                records = []
                object_ids = []
    if records:
        yield SeedBatch(batch_number, records, tuple(object_ids))


async def seed_transactions(
    clients: ServiceClients,
    manifest: ReplayManifest,
    tenant_id: str,
    *,
    batch_size: int = 500,
    max_in_flight: int = 10,
    progress: Callable[[int, int, int, int], None] | None = None,
) -> dict[str, Any]:
    if not tenant_id.strip():
        raise ValueError("seed tenant ID must not be empty")
    if max_in_flight <= 0:
        raise ValueError("seed max in flight must be positive")

    started = time.perf_counter()
    completed_batches = 0
    completed_records = 0
    inserted_batches = 0
    inserted_records = 0
    replayed_batches = 0
    replayed_records = 0
    pending: set[asyncio.Task[SeedBatchResult]] = set()

    async def submit(batch: SeedBatch) -> SeedBatchResult:
        response = await clients.ingest_batch(
            tenant_id,
            "transactions",
            batch.records,
            _batch_idempotency_key(tenant_id, batch.object_ids),
        )
        results = response.get("results")
        if not isinstance(results, list) or len(results) != len(batch.records):
            actual = len(results) if isinstance(results, list) else "missing"
            raise APIError(
                f"seed batch {batch.number} returned {actual} results for {len(batch.records)} records"
            )
        replay_flags = [isinstance(result, dict) and result.get("replayed") is True for result in results]
        if any(replay_flags) and not all(replay_flags):
            raise APIError(f"seed batch {batch.number} returned a mixture of replayed and new results")
        return SeedBatchResult(record_count=len(batch.records), replayed=all(replay_flags))

    async def collect(tasks: set[asyncio.Task[SeedBatchResult]]) -> None:
        nonlocal completed_batches, completed_records
        nonlocal inserted_batches, inserted_records
        nonlocal replayed_batches, replayed_records
        for batch_result in await asyncio.gather(*tasks):
            completed_records += batch_result.record_count
            completed_batches += 1
            if batch_result.replayed:
                replayed_records += batch_result.record_count
                replayed_batches += 1
            else:
                inserted_records += batch_result.record_count
                inserted_batches += 1
            if progress is not None:
                progress(completed_records, completed_batches, inserted_records, replayed_records)

    try:
        for batch in iter_seed_batches(manifest, batch_size):
            pending.add(asyncio.create_task(submit(batch)))
            if len(pending) >= max_in_flight:
                done, pending = await asyncio.wait(pending, return_when=asyncio.FIRST_COMPLETED)
                await collect(done)
        if pending:
            await collect(pending)
    except BaseException:
        for task in pending:
            task.cancel()
        if pending:
            await asyncio.gather(*pending, return_exceptions=True)
        raise

    if completed_records == 0:
        raise ValueError("seed manifest contains no transaction records")

    elapsed_seconds = time.perf_counter() - started
    return {
        "records": completed_records,
        "batches": completed_batches,
        "inserted_records": inserted_records,
        "inserted_batches": inserted_batches,
        "replayed_records": replayed_records,
        "replayed_batches": replayed_batches,
        "batch_size": batch_size,
        "max_in_flight": max_in_flight,
        "elapsed_seconds": round(elapsed_seconds, 3),
        "records_per_second": round(completed_records / elapsed_seconds, 2) if elapsed_seconds > 0 else None,
    }


def _batch_idempotency_key(tenant_id: str, object_ids: tuple[str, ...]) -> str:
    digest = hashlib.sha256()
    digest.update(tenant_id.encode())
    for object_id in object_ids:
        digest.update(b"\0")
        digest.update(object_id.encode())
    return f"production-replay-seed:{digest.hexdigest()}"
