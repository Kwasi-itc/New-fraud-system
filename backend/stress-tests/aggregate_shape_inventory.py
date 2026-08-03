from __future__ import annotations

import argparse
import json
from collections import Counter
from pathlib import Path
from typing import Any


def load_payload(path_value: str) -> dict[str, Any]:
    path = Path(path_value)
    if not path.exists():
        raise SystemExit(f"snapshot path does not exist: {path}")
    return json.loads(path.read_text(encoding="utf-8"))


def aggregate_shapes(payloads: list[dict[str, Any]]) -> list[tuple[str, int]]:
    counts: Counter[str] = Counter()
    for payload in payloads:
        metrics = payload.get("read_metrics", payload)
        endpoints = metrics.get("endpoints", {})
        aggregate = endpoints.get("aggregate_records", {})
        for shape, count in aggregate.get("aggregate_shape_counts", {}).items():
            try:
                counts[shape] += int(count)
            except (TypeError, ValueError):
                continue
    return counts.most_common()


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        description="Summarize top aggregate query shapes from one or more ingestion read-metrics snapshots."
    )
    parser.add_argument("snapshots", nargs="+", help="read-metrics JSON files")
    parser.add_argument("--limit", type=int, default=20, help="Maximum shapes to print")
    parser.add_argument("--output", help="Optional output JSON path")
    return parser


def main() -> None:
    args = build_parser().parse_args()
    payloads = [load_payload(path) for path in args.snapshots]
    rows = [{"shape": shape, "count": count} for shape, count in aggregate_shapes(payloads)[: max(1, args.limit)]]
    report = {
        "snapshot_count": len(payloads),
        "rows": rows,
    }
    if args.output:
        output_path = Path(args.output)
        output_path.parent.mkdir(parents=True, exist_ok=True)
        output_path.write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")
    print("")
    print("Aggregate Shape Inventory")
    print(f"  snapshots: {len(payloads)}")
    if not rows:
        print("  no aggregate shapes found")
        return
    for row in rows:
        print(f"  {row['count']:>8}  {row['shape']}")


if __name__ == "__main__":
    main()
