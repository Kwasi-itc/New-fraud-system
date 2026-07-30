from __future__ import annotations

import argparse
import csv
import heapq
import json
import re
from dataclasses import dataclass
from datetime import datetime, timedelta
from glob import glob
from pathlib import Path
from typing import Any, Iterator


DEFAULT_STREAM_COUNTS = {
    "genpay-inflow": 50,
    "genpay-v2-inflow": 100,
    "uniwallet-inflow": 300,
    "uniwallet-outflow": 100,
    "uniwallet-v2-inflow": 200,
    "uniwallet-v2-outflow": 250,
}

STREAM_SOURCE_PATHS = {
    "genpay-inflow": "transactions/genpay/inflow/2026-06-01.csv",
    "genpay-v2-inflow": "transactions/genpayv2/inflow/2026-06-01.csv",
    "uniwallet-inflow": "transactions/uniwallet/inflow/2026-06-01.csv",
    "uniwallet-outflow": "transactions/uniwallet/outflow/2026-06-01.csv",
    "uniwallet-v2-inflow": "transactions/uniwalletv2/inflow/2026-06-01.csv",
    "uniwallet-v2-outflow": "transactions/uniwalletv2/outflow/2026-06-01.csv",
}


def create_local_sample(
    base_manifest_path: Path,
    data_root: Path,
    output_dir: Path,
    output_manifest_path: Path,
    stream_counts: dict[str, int] | None = None,
    total_transactions: int | None = None,
) -> int:
    if stream_counts is not None and total_transactions is not None:
        raise ValueError("define either stream counts or total transactions, not both")
    if total_transactions is not None and total_transactions <= 0:
        raise ValueError("total transactions must be positive")

    counts = stream_counts if stream_counts is not None else None
    if total_transactions is None:
        counts = counts or DEFAULT_STREAM_COUNTS
        if set(counts) != set(STREAM_SOURCE_PATHS):
            raise ValueError("local sample counts must define every configured replay stream")
        if any(count <= 0 for count in counts.values()):
            raise ValueError("local sample counts must be positive")

    manifest = json.loads(base_manifest_path.read_text(encoding="utf-8"))
    if not isinstance(manifest, dict):
        raise ValueError("base replay manifest must contain a JSON object")

    output_dir.mkdir(parents=True, exist_ok=True)
    configured_streams = {stream["id"]: stream for stream in manifest["transaction_streams"]}
    sample_streams: list[dict[str, Any]] = []
    total = 0
    stream_files = _stream_files(data_root)

    if total_transactions is not None:
        total, selected_counts = _copy_global_rows(stream_files, output_dir, total_transactions)
        if total != total_transactions:
            available = sum(selected_counts.values())
            raise ValueError(f"configured transaction streams contain {available} rows; {total_transactions} are required")
        for stream_id in STREAM_SOURCE_PATHS:
            if selected_counts.get(stream_id, 0) == 0:
                continue
            stream = dict(configured_streams[stream_id])
            stream["globs"] = [str(output_dir / f"{stream_id}.csv")]
            sample_streams.append(stream)
    elif counts is not None:
        for stream_id, requested_count in counts.items():
            output_path = output_dir / f"{stream_id}.csv"
            written = _copy_rows_from_files(stream_files[stream_id], output_path, requested_count)
            if written != requested_count:
                stream_root = data_root / Path(STREAM_SOURCE_PATHS[stream_id]).parent
                raise ValueError(f"{stream_root} contains {written} rows; {requested_count} are required")
            stream = dict(configured_streams[stream_id])
            stream["globs"] = [str(output_path)]
            sample_streams.append(stream)
            total += written

    reference_data = manifest["reference_data"]
    reference_data["merchant_globs"] = [str(data_root / "data/dumps/merchant-info-dump/batch_*.json")]
    reference_data["merchant_product_globs"] = [
        str(data_root / "data/dumps/merchant-product-dump/batch_*.json")
    ]
    reference_data["staff_csv"] = str(data_root / "data/lists/fraud-staff.csv")
    reference_data["merchant_watchlist_xlsx"] = str(data_root / "data/lists/merchants.xlsx")
    manifest["transaction_streams"] = sample_streams
    output_manifest_path.write_text(json.dumps(manifest, indent=2) + "\n", encoding="utf-8")
    return total


@dataclass(frozen=True)
class _StreamRow:
    stream_id: str
    source_path: Path
    row_number: int
    row: dict[str, str]
    occurred_at: datetime


def create_full_manifest(
    base_manifest_path: Path,
    data_root: Path,
    output_manifest_path: Path,
) -> None:
    manifest = json.loads(base_manifest_path.read_text(encoding="utf-8"))
    if not isinstance(manifest, dict):
        raise ValueError("base replay manifest must contain a JSON object")

    reference_data = manifest["reference_data"]
    reference_data["merchant_globs"] = [str(data_root / "data/dumps/merchant-info-dump/batch_*.json")]
    reference_data["merchant_product_globs"] = [
        str(data_root / "data/dumps/merchant-product-dump/batch_*.json")
    ]
    reference_data["staff_csv"] = str(data_root / "data/lists/fraud-staff.csv")
    reference_data["merchant_watchlist_xlsx"] = str(data_root / "data/lists/merchants.xlsx")

    for stream_id, relative_path in STREAM_SOURCE_PATHS.items():
        stream_root = data_root / Path(relative_path).parent
        for stream in manifest["transaction_streams"]:
            if stream["id"] == stream_id:
                stream["globs"] = [str(stream_root / "*.csv")]
                break
        else:
            raise ValueError(f"base manifest is missing transaction stream {stream_id!r}")

    output_manifest_path.write_text(json.dumps(manifest, indent=2) + "\n", encoding="utf-8")


def create_duration_sample(
    base_manifest_path: Path,
    data_root: Path,
    output_dir: Path,
    output_manifest_path: Path,
    duration: timedelta,
) -> int:
    if duration <= timedelta(0):
        raise ValueError("duration must be positive")

    manifest = json.loads(base_manifest_path.read_text(encoding="utf-8"))
    if not isinstance(manifest, dict):
        raise ValueError("base replay manifest must contain a JSON object")

    stream_files = _stream_files(data_root)
    source_start = _earliest_source_timestamp(stream_files)
    cutoff = source_start + duration

    output_dir.mkdir(parents=True, exist_ok=True)
    configured_streams = {stream["id"]: stream for stream in manifest["transaction_streams"]}
    sample_streams: list[dict[str, Any]] = []
    total = 0

    for stream_id, files in stream_files.items():
        output_path = output_dir / f"{stream_id}.csv"
        written = _copy_rows_until(files, output_path, cutoff)
        if written == 0:
            continue
        stream = dict(configured_streams[stream_id])
        stream["globs"] = [str(output_path)]
        sample_streams.append(stream)
        total += written

    if total == 0:
        raise ValueError(f"duration sample selected no rows before {cutoff.isoformat()}")

    reference_data = manifest["reference_data"]
    reference_data["merchant_globs"] = [str(data_root / "data/dumps/merchant-info-dump/batch_*.json")]
    reference_data["merchant_product_globs"] = [
        str(data_root / "data/dumps/merchant-product-dump/batch_*.json")
    ]
    reference_data["staff_csv"] = str(data_root / "data/lists/fraud-staff.csv")
    reference_data["merchant_watchlist_xlsx"] = str(data_root / "data/lists/merchants.xlsx")
    manifest["transaction_streams"] = sample_streams
    output_manifest_path.write_text(json.dumps(manifest, indent=2) + "\n", encoding="utf-8")
    return total


def parse_duration(value: str) -> timedelta:
    match = re.fullmatch(r"\s*([0-9]+(?:\.[0-9]+)?)\s*([hHdDwW])\s*", value)
    if not match:
        raise ValueError("duration must look like 6h, 2d, or 1w")
    amount = float(match.group(1))
    unit = match.group(2).lower()
    if amount <= 0:
        raise ValueError("duration must be positive")
    if unit == "h":
        return timedelta(hours=amount)
    if unit == "d":
        return timedelta(days=amount)
    return timedelta(weeks=amount)


def _stream_files(data_root: Path) -> dict[str, list[Path]]:
    result: dict[str, list[Path]] = {}
    for stream_id, relative_path in STREAM_SOURCE_PATHS.items():
        stream_root = data_root / Path(relative_path).parent
        files = [Path(path) for path in sorted(glob(str(stream_root / "*.csv")))]
        if not files:
            raise ValueError(f"transaction stream {stream_id!r} did not match any files below {stream_root}")
        result[stream_id] = files
    return result


def _copy_global_rows(
    stream_files: dict[str, list[Path]],
    output_dir: Path,
    limit: int,
) -> tuple[int, dict[str, int]]:
    iterators = [_iter_stream_file(stream_id, path) for stream_id, paths in stream_files.items() for path in paths]
    heap: list[tuple[datetime, int, _StreamRow, Iterator[_StreamRow]]] = []
    sequence = 0
    for iterator in iterators:
        try:
            item = next(iterator)
        except StopIteration:
            continue
        heapq.heappush(heap, (item.occurred_at, sequence, item, iterator))
        sequence += 1

    writers: dict[str, csv.DictWriter[str]] = {}
    handles: dict[str, Any] = {}
    headers: dict[str, list[str]] = {}
    selected_counts = {stream_id: 0 for stream_id in stream_files}
    total = 0
    try:
        while heap and total < limit:
            _, _, item, iterator = heapq.heappop(heap)
            writer = writers.get(item.stream_id)
            if writer is None:
                fieldnames = list(item.row)
                headers[item.stream_id] = fieldnames
                handle = (output_dir / f"{item.stream_id}.csv").open("w", encoding="utf-8", newline="")
                handles[item.stream_id] = handle
                writer = csv.DictWriter(handle, fieldnames=fieldnames)
                writer.writeheader()
                writers[item.stream_id] = writer
            elif headers[item.stream_id] != list(item.row):
                raise ValueError(f"{item.source_path} headers do not match earlier local sample files")

            writer.writerow(item.row)
            selected_counts[item.stream_id] += 1
            total += 1

            try:
                next_item = next(iterator)
            except StopIteration:
                continue
            heapq.heappush(heap, (next_item.occurred_at, sequence, next_item, iterator))
            sequence += 1
        return total, selected_counts
    finally:
        for handle in handles.values():
            handle.close()


def _iter_stream_file(stream_id: str, source_path: Path) -> Iterator[_StreamRow]:
    with source_path.open("r", encoding="utf-8-sig", newline="") as source:
        reader = csv.DictReader(source)
        if not reader.fieldnames:
            raise ValueError(f"local sample source has no CSV header: {source_path}")
        for row_number, row in enumerate(reader, start=2):
            yield _StreamRow(
                stream_id=stream_id,
                source_path=source_path,
                row_number=row_number,
                row=row,
                occurred_at=_parse_source_timestamp(row, source_path, row_number),
            )


def _earliest_source_timestamp(stream_files: dict[str, list[Path]]) -> datetime:
    earliest: datetime | None = None
    for files in stream_files.values():
        for path in files:
            with path.open("r", encoding="utf-8-sig", newline="") as source:
                reader = csv.DictReader(source)
                if "source_date_created" not in (reader.fieldnames or []):
                    raise ValueError(f"{path} is missing required column: source_date_created")
                for row_number, row in enumerate(reader, start=2):
                    occurred_at = _parse_source_timestamp(row, path, row_number)
                    earliest = occurred_at if earliest is None else min(earliest, occurred_at)
    if earliest is None:
        raise ValueError("transaction sources contain no rows")
    return earliest


def _copy_rows(source_path: Path, output_path: Path, limit: int) -> int:
    with source_path.open("r", encoding="utf-8-sig", newline="") as source:
        reader = csv.DictReader(source)
        if not reader.fieldnames:
            raise ValueError(f"local sample source has no CSV header: {source_path}")
        with output_path.open("w", encoding="utf-8", newline="") as target:
            writer = csv.DictWriter(target, fieldnames=reader.fieldnames)
            writer.writeheader()
            count = 0
            for row in reader:
                writer.writerow(row)
                count += 1
                if count == limit:
                    break
    return count


def _copy_rows_from_files(source_paths: list[Path], output_path: Path, limit: int) -> int:
    count = 0
    writer: csv.DictWriter[str] | None = None
    target_handle = None
    try:
        for source_path in source_paths:
            with source_path.open("r", encoding="utf-8-sig", newline="") as source:
                reader = csv.DictReader(source)
                if not reader.fieldnames:
                    raise ValueError(f"local sample source has no CSV header: {source_path}")
                if writer is None:
                    target_handle = output_path.open("w", encoding="utf-8", newline="")
                    writer = csv.DictWriter(target_handle, fieldnames=reader.fieldnames)
                    writer.writeheader()
                elif list(writer.fieldnames or []) != list(reader.fieldnames):
                    raise ValueError(f"{source_path} headers do not match earlier local sample files")

                for row in reader:
                    writer.writerow(row)
                    count += 1
                    if count == limit:
                        return count
        return count
    finally:
        if target_handle is not None:
            target_handle.close()


def _copy_rows_until(source_paths: list[Path], output_path: Path, cutoff: datetime) -> int:
    count = 0
    writer: csv.DictWriter[str] | None = None
    target_handle = None
    try:
        for source_path in source_paths:
            with source_path.open("r", encoding="utf-8-sig", newline="") as source:
                reader = csv.DictReader(source)
                if not reader.fieldnames:
                    raise ValueError(f"duration sample source has no CSV header: {source_path}")
                if writer is None:
                    target_handle = output_path.open("w", encoding="utf-8", newline="")
                    writer = csv.DictWriter(target_handle, fieldnames=reader.fieldnames)
                    writer.writeheader()
                elif list(writer.fieldnames or []) != list(reader.fieldnames):
                    raise ValueError(f"{source_path} headers do not match earlier duration sample files")

                for row_number, row in enumerate(reader, start=2):
                    if _parse_source_timestamp(row, source_path, row_number) < cutoff:
                        writer.writerow(row)
                        count += 1
        return count
    finally:
        if target_handle is not None:
            target_handle.close()


def _parse_source_timestamp(row: dict[str, str], path: Path, row_number: int) -> datetime:
    try:
        return datetime.strptime(row["source_date_created"].strip(), "%Y-%m-%d %H:%M:%S")
    except (KeyError, ValueError) as exc:
        raise ValueError(f"invalid source_date_created at {path}:{row_number}: {exc}") from exc


def main() -> None:
    parser = argparse.ArgumentParser(description="Create a local replay manifest for sampled or full production data")
    parser.add_argument("--base-manifest", required=True, type=Path)
    parser.add_argument("--data-root", required=True, type=Path)
    parser.add_argument("--output-dir", required=True, type=Path)
    parser.add_argument("--output-manifest", required=True, type=Path)
    parser.add_argument(
        "--transactions",
        default="1000",
        help="Total sampled transaction count, or 'all' to use every configured source file",
    )
    parser.add_argument(
        "--duration",
        help="Source-time replay window, for example 6h, 2d, or 1w. Overrides --transactions.",
    )
    args = parser.parse_args()

    if args.duration:
        total = create_duration_sample(
            args.base_manifest.resolve(),
            args.data_root.resolve(),
            args.output_dir.resolve(),
            args.output_manifest.resolve(),
            parse_duration(args.duration),
        )
        print(f"created local replay duration sample with {total} transactions")
        return

    if args.transactions.strip().lower() == "all":
        create_full_manifest(
            args.base_manifest.resolve(),
            args.data_root.resolve(),
            args.output_manifest.resolve(),
        )
        print("created local replay manifest for all transactions")
        return

    total_transactions = int(args.transactions)
    total = create_local_sample(
        args.base_manifest.resolve(),
        args.data_root.resolve(),
        args.output_dir.resolve(),
        args.output_manifest.resolve(),
        total_transactions=total_transactions,
    )
    print(f"created local replay sample with {total} transactions")


if __name__ == "__main__":
    main()
