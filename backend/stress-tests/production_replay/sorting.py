from __future__ import annotations

import heapq
import json
import asyncio
from collections import deque
from concurrent.futures import ThreadPoolExecutor
from dataclasses import dataclass
from pathlib import Path
from typing import IO, Callable, Iterable, Iterator

from .adapters import get_adapter
from .domain import TransactionEvent
from .manifest import ReplayManifest


@dataclass(frozen=True)
class SortResult:
    chunk_paths: tuple[Path, ...]
    event_count: int


def build_sorted_chunks(
    manifest: ReplayManifest,
    work_dir: Path,
    chunk_size: int = 100_000,
    transform: Callable[[TransactionEvent], TransactionEvent] | None = None,
    storage_check: Callable[[], None] | None = None,
) -> SortResult:
    if chunk_size <= 0:
        raise ValueError("chunk_size must be positive")
    work_dir.mkdir(parents=True, exist_ok=True)
    chunk: list[dict[str, object]] = []
    chunk_paths: list[Path] = []
    sequence = 0

    def flush() -> None:
        if not chunk:
            return
        if storage_check is not None:
            storage_check()
        chunk.sort(key=lambda item: (str(item["occurred_at"]), int(item["sequence"])))
        path = work_dir / f"chunk-{len(chunk_paths):06d}.ndjson"
        with path.open("w", encoding="utf-8") as handle:
            for index, item in enumerate(chunk):
                if storage_check is not None and index % 256 == 0:
                    storage_check()
                handle.write(json.dumps(item, separators=(",", ":"), ensure_ascii=True) + "\n")
        chunk_paths.append(path)
        chunk.clear()

    for stream in manifest.transaction_streams:
        adapter = get_adapter(stream.adapter)
        for event in adapter.iter_events(stream, manifest.stream_files(stream)):
            if transform is not None:
                event = transform(event)
            chunk.append(event.to_sort_record(sequence))
            sequence += 1
            if len(chunk) >= chunk_size:
                flush()
    flush()
    return SortResult(tuple(chunk_paths), sequence)


def iter_merged_events(chunk_paths: tuple[Path, ...] | list[Path]) -> Iterator[TransactionEvent]:
    handles: list[IO[str]] = []
    heap: list[tuple[str, int, int, dict[str, object]]] = []
    try:
        for index, path in enumerate(chunk_paths):
            handle = path.open("r", encoding="utf-8")
            handles.append(handle)
            item = _read_record(handle)
            if item is not None:
                heapq.heappush(heap, (str(item["occurred_at"]), int(item["sequence"]), index, item))
        while heap:
            _occurred_at, _sequence, index, item = heapq.heappop(heap)
            yield TransactionEvent.from_sort_record(item)
            next_item = _read_record(handles[index])
            if next_item is not None:
                heapq.heappush(
                    heap,
                    (str(next_item["occurred_at"]), int(next_item["sequence"]), index, next_item),
                )
    finally:
        for handle in handles:
            handle.close()


def _read_record(handle: IO[str]) -> dict[str, object] | None:
    line = handle.readline()
    if not line:
        return None
    value = json.loads(line)
    if not isinstance(value, dict):
        raise ValueError("sort chunk line must be a JSON object")
    return value


class AsyncEventSource:
    """Read a bounded batch on one thread; close the iterator after cancellation.

    Iterator creation, advancement and closure all have the same thread owner.
    Input must have bounded read latency: cancellation joins the outstanding read
    before closing its files, rather than abandoning a live reader thread.
    """

    def __init__(self, events: Iterable[TransactionEvent], batch_size: int = 128) -> None:
        if batch_size <= 0:
            raise ValueError("source batch size must be positive")
        self.events = events
        self.batch_size = batch_size
        self.buffer: deque[TransactionEvent] = deque()
        self.iterator: Iterator[TransactionEvent] | None = None
        self.exhausted = False
        self.executor = ThreadPoolExecutor(max_workers=1, thread_name_prefix="scale-source")
        self.pending: asyncio.Future[list[TransactionEvent]] | None = None

    def _read(self) -> list[TransactionEvent]:
        if self.iterator is None:
            self.iterator = iter(self.events)
        batch = []
        for _ in range(self.batch_size):
            try:
                batch.append(next(self.iterator))
            except StopIteration:
                break
        return batch

    async def next(self) -> TransactionEvent | None:
        # The caller serializes claims so buffering never reorders source records.
        if not self.buffer and not self.exhausted:
            self.pending = asyncio.get_running_loop().run_in_executor(self.executor, self._read)
            batch = await asyncio.shield(self.pending)
            self.pending = None
            self.buffer.extend(batch)
            self.exhausted = len(batch) < self.batch_size
        return self.buffer.popleft() if self.buffer else None

    async def close(self) -> None:
        if self.pending is not None:
            await asyncio.gather(self.pending, return_exceptions=True)
        def close_iterator() -> None:
            if self.iterator is not None and hasattr(self.iterator, "close"):
                self.iterator.close()
        try:
            await asyncio.get_running_loop().run_in_executor(self.executor, close_iterator)
        finally:
            self.executor.shutdown(wait=True)
