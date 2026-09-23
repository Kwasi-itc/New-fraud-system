from __future__ import annotations

import argparse
import csv
import json
import os
import shutil
from concurrent.futures import ProcessPoolExecutor, as_completed
from dataclasses import asdict, dataclass
from pathlib import Path
from typing import Any

from .privacy import EXPLICITLY_DROPPED_PII_FIELDS, InternalPrivacyTransformer


OUTPUT_COLUMNS = (
    "transtype",
    "source_date_created",
    "source_account_no",
    "source_trans_id",
    "merchant_id",
    "product_id",
    "amount",
    "currency",
)
REQUIRED_INPUT_COLUMNS = frozenset(
    {"source_date_created", "source_trans_id", "merchant_id", "product_id", "amount", "currency"}
)


@dataclass(frozen=True)
class FileResult:
    relative_path: str
    rows: int
    input_bytes: int
    output_bytes: int


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        description="Create a minimal, HMAC-pseudonymized copy of a database-scale CSV source tree"
    )
    parser.add_argument("--source-root", required=True)
    parser.add_argument("--output-root", required=True)
    parser.add_argument("--pii-key-file", required=True)
    parser.add_argument("--workers", type=int, default=max(1, min(8, os.cpu_count() or 1)))
    parser.add_argument(
        "--resume",
        action="store_true",
        help="Keep completed output CSVs and process only missing files",
    )
    return parser


def _clean(value: Any) -> str | None:
    if value is None:
        return None
    result = str(value).strip()
    return result or None


def _json_value(value: str | None, key: str) -> str | None:
    if not value or not value.startswith("{"):
        return None
    try:
        parsed = json.loads(value)
    except json.JSONDecodeError:
        return None
    item = parsed.get(key) if isinstance(parsed, dict) else None
    return _clean(item)


def _sanitize_file(
    source_path: str,
    output_path: str,
    source_root: str,
    key: bytes,
) -> FileResult:
    source = Path(source_path)
    destination = Path(output_path)
    destination.parent.mkdir(parents=True, exist_ok=True)
    temporary = destination.with_name(f".{destination.name}.tmp")
    transformer = InternalPrivacyTransformer(key)
    rows = 0
    try:
        with source.open("r", encoding="utf-8-sig", newline="") as input_handle, temporary.open(
            "w", encoding="utf-8", newline=""
        ) as output_handle:
            reader = csv.DictReader(input_handle)
            headers = set(reader.fieldnames or [])
            missing = REQUIRED_INPUT_COLUMNS - headers
            if missing:
                raise ValueError(f"{source} is missing required columns: {sorted(missing)}")
            writer = csv.DictWriter(output_handle, fieldnames=OUTPUT_COLUMNS, extrasaction="ignore")
            writer.writeheader()
            for row_number, row in enumerate(reader, start=2):
                raw_account_ref = _clean(row.get("accountref"))
                account_ref = (
                    _clean(row.get("source_account_no"))
                    or _clean(row.get("paymentmsisdn"))
                    or _json_value(raw_account_ref, "idNumber")
                    or raw_account_ref
                )
                writer.writerow(
                    {
                        "transtype": _clean(row.get("transtype")),
                        "source_date_created": _clean(row.get("source_date_created")),
                        "source_account_no": (
                            transformer.token(account_ref, namespace="account_ref")
                            if account_ref
                            else None
                        ),
                        # The adapter requires this header, but the internal scenarios do not
                        # use its value. Leave it blank so identity falls back to file + row.
                        "source_trans_id": "",
                        "merchant_id": _clean(row.get("merchant_id")),
                        "product_id": _clean(row.get("product_id")),
                        "amount": _clean(row.get("amount")),
                        "currency": _clean(row.get("currency")),
                    }
                )
                rows += 1
        temporary.replace(destination)
    except BaseException:
        temporary.unlink(missing_ok=True)
        raise
    return FileResult(
        relative_path=str(source.relative_to(Path(source_root))),
        rows=rows,
        input_bytes=source.stat().st_size,
        output_bytes=destination.stat().st_size,
    )


def main(argv: list[str] | None = None) -> None:
    args = build_parser().parse_args(argv)
    if args.workers <= 0:
        raise SystemExit("error: --workers must be positive")
    source_root = Path(args.source_root).expanduser().resolve()
    output_root = Path(args.output_root).expanduser().resolve()
    key_file = Path(args.pii_key_file).expanduser().resolve()
    if not source_root.is_dir():
        raise SystemExit(f"error: source root does not exist: {source_root}")
    if source_root == output_root or source_root in output_root.parents:
        raise SystemExit("error: output root must not be the source root or a child of it")
    if output_root.exists() and not args.resume:
        raise SystemExit(f"error: output root already exists: {output_root}; use --resume")
    if not key_file.is_file():
        raise SystemExit(f"error: PII key file does not exist: {key_file}")
    key = key_file.read_bytes().strip()
    InternalPrivacyTransformer(key)

    source_transactions = source_root / "transactions"
    files = sorted(source_transactions.rglob("*.csv"))
    if not files:
        raise SystemExit(f"error: no transaction CSVs found under {source_transactions}")
    output_root.mkdir(parents=True, exist_ok=True)
    source_reference = source_root / "data"
    if source_reference.is_dir():
        shutil.copytree(source_reference, output_root / "data", dirs_exist_ok=True)

    completed: list[FileResult] = []
    pending: list[tuple[Path, Path]] = []
    for source in files:
        destination = output_root / source.relative_to(source_root)
        if args.resume and destination.is_file():
            completed.append(
                FileResult(
                    relative_path=str(source.relative_to(source_root)),
                    rows=-1,
                    input_bytes=source.stat().st_size,
                    output_bytes=destination.stat().st_size,
                )
            )
        else:
            pending.append((source, destination))

    print(f"Sanitizing {len(pending)} CSV files with {args.workers} workers...")
    with ProcessPoolExecutor(max_workers=args.workers) as executor:
        futures = {
            executor.submit(
                _sanitize_file,
                str(source),
                str(destination),
                str(source_root),
                key,
            ): source
            for source, destination in pending
        }
        for index, future in enumerate(as_completed(futures), start=1):
            result = future.result()
            completed.append(result)
            print(f"[{index}/{len(pending)}] {result.relative_path}: {result.rows} rows", flush=True)

    measured = [item for item in completed if item.rows >= 0]
    report = {
        "status": "complete",
        "source_root": str(source_root),
        "output_root": str(output_root),
        "files": len(completed),
        "rows_processed_this_run": sum(item.rows for item in measured),
        "input_bytes_processed_this_run": sum(item.input_bytes for item in measured),
        "output_bytes_processed_this_run": sum(item.output_bytes for item in measured),
        "output_columns": list(OUTPUT_COLUMNS),
        "tokenized_source_columns": ["source_account_no"],
        "blank_compatibility_columns": ["source_trans_id"],
        "dropped_pii_columns": sorted(EXPLICITLY_DROPPED_PII_FIELDS),
        "key_recorded": False,
        "file_results": [asdict(item) for item in sorted(completed, key=lambda value: value.relative_path)],
    }
    report_path = output_root / "sanitization-report.json"
    report_path.write_text(json.dumps(report, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    print(f"Sanitized data: {output_root}")
    print(f"Report: {report_path}")


if __name__ == "__main__":
    main()
