from __future__ import annotations

import heapq
import json
from dataclasses import dataclass
from pathlib import Path
from typing import IO, Iterator

from .adapters import get_adapter
from .domain import TransactionEvent
from .manifest import ReplayManifest


@dataclass(frozen=True)
class SortResult:
    chunk_paths: tuple[Path, ...]
    event_count: int


def build_sorted_chunks(manifest: ReplayManifest, work_dir: Path, chunk_size: int = 100_000) -> SortResult:
    if chunk_size <= 0:
        raise ValueError("chunk_size must be positive")
    work_dir.mkdir(parents=True, exist_ok=True)
    chunk: list[dict[str, object]] = []
    chunk_paths: list[Path] = []
    sequence = 0

    def flush() -> None:
        if not chunk:
            return
        chunk.sort(key=lambda item: (str(item["occurred_at"]), int(item["sequence"])))
        path = work_dir / f"chunk-{len(chunk_paths):06d}.ndjson"
        with path.open("w", encoding="utf-8") as handle:
            for item in chunk:
                handle.write(json.dumps(item, separators=(",", ":"), ensure_ascii=True) + "\n")
        chunk_paths.append(path)
        chunk.clear()

    for stream in manifest.transaction_streams:
        adapter = get_adapter(stream.adapter)
        for event in adapter.iter_events(stream, manifest.stream_files(stream)):
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
