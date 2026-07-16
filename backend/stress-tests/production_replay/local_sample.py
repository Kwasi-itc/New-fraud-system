from __future__ import annotations

import argparse
import csv
import json
from pathlib import Path
from typing import Any


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
) -> int:
    counts = stream_counts or DEFAULT_STREAM_COUNTS
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

    for stream_id, requested_count in counts.items():
        source_path = data_root / STREAM_SOURCE_PATHS[stream_id]
        if not source_path.is_file():
            raise ValueError(f"local sample source does not exist: {source_path}")
        output_path = output_dir / f"{stream_id}.csv"
        written = _copy_rows(source_path, output_path, requested_count)
        if written != requested_count:
            raise ValueError(f"{source_path} contains {written} rows; {requested_count} are required")
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


def main() -> None:
    parser = argparse.ArgumentParser(description="Create the exact 1,000-event local replay sample")
    parser.add_argument("--base-manifest", required=True, type=Path)
    parser.add_argument("--data-root", required=True, type=Path)
    parser.add_argument("--output-dir", required=True, type=Path)
    parser.add_argument("--output-manifest", required=True, type=Path)
    args = parser.parse_args()
    total = create_local_sample(
        args.base_manifest.resolve(),
        args.data_root.resolve(),
        args.output_dir.resolve(),
        args.output_manifest.resolve(),
    )
    print(f"created local replay sample with {total} transactions")


if __name__ == "__main__":
    main()
