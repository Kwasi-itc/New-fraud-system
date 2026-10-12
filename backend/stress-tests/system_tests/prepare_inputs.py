"""Prepare finite NDJSON cohorts from replay sources or synthetic internal-model records."""
from __future__ import annotations

import argparse
import json
import re
from datetime import datetime, timedelta, timezone
from itertools import islice
from pathlib import Path

from production_replay.adapters import get_adapter
from production_replay.manifest import load_manifest
from .input_source import fingerprint_record, inspect_source


def manifest_fields(path: Path):
    manifest = load_manifest(path)
    # Preserve declared stream order, file order, and row order. No hidden global sort.
    for stream in manifest.transaction_streams:
        adapter = get_adapter(stream.adapter)
        for event in adapter.iter_events(stream, manifest.stream_files(stream)):
            yield event.fields


def synthetic_fields(prefix: str, count: int):
    if not re.fullmatch(r"[a-zA-Z0-9_-]{1,80}", prefix):
        raise ValueError("invalid_synthetic_prefix")
    start = datetime(2026, 1, 1, tzinfo=timezone.utc)
    for index in range(count):
        identity = f"{prefix}-{index:09d}"
        yield {"object_id": identity, "transaction_id": identity,
            "date": (start + timedelta(seconds=index)).isoformat(), "amount": 10,
            "channel": "wallet", "direction": "incoming", "system_type": "wallet_transfer",
            "stream_id": "synthetic", "merchant_id": "synthetic-merchant", "account_ref": "synthetic-account"}


def prepare(output: Path, records, count: int, skip: int = 0):
    if count <= 0 or skip < 0:
        raise ValueError("invalid_cohort_bounds")
    output.parent.mkdir(parents=True, exist_ok=True)
    with output.open("xb") as handle:
        try:
            written = 0
            for value in islice(records, skip, skip + count):
                handle.write(fingerprint_record(value))
                written += 1
            if written != count:
                raise ValueError("insufficient_source_records")
        except BaseException:
            handle.close()
            output.unlink()  # Only this invocation's exclusively created file.
            raise
    try:
        return inspect_source(output, count)
    except BaseException:
        output.unlink()
        raise


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    mode = parser.add_mutually_exclusive_group(required=True)
    mode.add_argument("--manifest", type=Path)
    mode.add_argument("--synthetic-prefix", help="Unique prefix for this run's internal-model smoke records")
    parser.add_argument("--count", required=True, type=int)
    parser.add_argument("--skip", type=int, default=0)
    parser.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()
    if args.count <= 0 or args.skip < 0:
        parser.error("count must be positive and skip nonnegative")
    records = manifest_fields(args.manifest) if args.manifest else synthetic_fields(args.synthetic_prefix, args.count + args.skip)
    try:
        result = prepare(args.output, records, args.count, args.skip)
        print(json.dumps({**result, "source_order": "manifest_stream_file_row" if args.manifest else "synthetic",
                          "synthetic_is_not_production_representative": args.manifest is None}))
    finally:
        records.close()


if __name__ == "__main__":
    main()
