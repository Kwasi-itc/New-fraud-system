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


def aggregate_shapes(payloads: list[dict[str, Any]], sort_by: str) -> list[dict[str, Any]]:
    counts: Counter[str] = Counter()
    merged: dict[str, dict[str, int]] = {}
    for payload in payloads:
        metrics = payload.get("read_metrics", payload)
        endpoints = metrics.get("endpoints", {})
        aggregate = endpoints.get("aggregate_records", {})
        for shape, count in aggregate.get("aggregate_shape_counts", {}).items():
            try:
                counts[shape] += int(count)
            except (TypeError, ValueError):
                continue
        for shape, row in aggregate.get("aggregate_shape_metrics", {}).items():
            if not isinstance(row, dict):
                continue
            item = merged.setdefault(
                shape,
                {
                    "shape": shape,
                    "count": 0,
                    "overloads": 0,
                    "timeouts": 0,
                    "cancellations": 0,
                    "p95_latency_micros": 0,
                    "max_latency_micros": 0,
                    "total_latency_micros": 0,
                },
            )
            for key in ("requests", "overloads", "timeouts", "cancellations", "total_latency_micros"):
                try:
                    item_key = "count" if key == "requests" else key
                    item[item_key] += int(row.get(key, 0))
                except (TypeError, ValueError):
                    continue
            for key in ("p95_latency_micros", "max_latency_micros"):
                try:
                    item[key] = max(item[key], int(row.get(key, 0)))
                except (TypeError, ValueError):
                    continue
    if not merged:
        return [{"shape": shape, "count": count} for shape, count in counts.most_common()]
    for shape, count in counts.items():
        merged.setdefault(shape, {"shape": shape, "count": 0})
        merged[shape]["count"] = max(merged[shape].get("count", 0), count)
    rows = list(merged.values())
    sort_key = {
        "count": lambda item: (item.get("count", 0), item.get("p95_latency_micros", 0), item.get("max_latency_micros", 0)),
        "p95_latency": lambda item: (item.get("p95_latency_micros", 0), item.get("count", 0), item.get("max_latency_micros", 0)),
        "max_latency": lambda item: (item.get("max_latency_micros", 0), item.get("p95_latency_micros", 0), item.get("count", 0)),
        "overloads": lambda item: (item.get("overloads", 0), item.get("count", 0), item.get("p95_latency_micros", 0)),
        "timeouts": lambda item: (item.get("timeouts", 0) + item.get("cancellations", 0), item.get("count", 0), item.get("p95_latency_micros", 0)),
    }[sort_by]
    rows.sort(key=sort_key, reverse=True)
    return rows


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        description="Summarize top aggregate query shapes from one or more ingestion read-metrics snapshots."
    )
    parser.add_argument("snapshots", nargs="+", help="read-metrics JSON files")
    parser.add_argument("--limit", type=int, default=20, help="Maximum shapes to print")
    parser.add_argument(
        "--sort-by",
        choices=("count", "p95_latency", "max_latency", "overloads", "timeouts"),
        default="count",
        help="Sort criterion for the report.",
    )
    parser.add_argument("--output", help="Optional output JSON path")
    return parser


def main() -> None:
    args = build_parser().parse_args()
    payloads = [load_payload(path) for path in args.snapshots]
    rows = aggregate_shapes(payloads, args.sort_by)[: max(1, args.limit)]
    report = {
        "snapshot_count": len(payloads),
        "sort_by": args.sort_by,
        "rows": rows,
    }
    if args.output:
        output_path = Path(args.output)
        output_path.parent.mkdir(parents=True, exist_ok=True)
        output_path.write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")
    print("")
    print("Aggregate Shape Inventory")
    print(f"  snapshots: {len(payloads)}")
    print(f"  sort_by:   {args.sort_by}")
    if not rows:
        print("  no aggregate shapes found")
        return
    for row in rows:
        print(
            f"  {row.get('count', 0):>8}  p95={row.get('p95_latency_micros', 0):>8}us"
            f"  max={row.get('max_latency_micros', 0):>8}us"
            f"  overloads={row.get('overloads', 0):>6}"
            f"  {row['shape']}"
        )


if __name__ == "__main__":
    main()
