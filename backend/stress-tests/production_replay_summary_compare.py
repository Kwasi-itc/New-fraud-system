from __future__ import annotations

import argparse
import json
from pathlib import Path
from typing import Any


def load_summary(label: str, path_value: str) -> tuple[str, Path, dict[str, Any]]:
    path = Path(path_value)
    if path.is_dir():
        path = path / "summary.json"
    if not path.exists():
        raise SystemExit(f"summary path does not exist: {path}")
    payload = json.loads(path.read_text(encoding="utf-8"))
    if not isinstance(payload, dict):
        raise SystemExit(f"summary payload must be an object: {path}")
    return label, path, payload


def maybe_float(value: Any) -> float | None:
    if value is None:
        return None
    try:
        return float(value)
    except (TypeError, ValueError):
        return None


def ratio(left: float | None, right: float | None) -> float | None:
    if left is None or right in (None, 0):
        return None
    return left / right


def metric(payload: dict[str, Any], *path: str) -> Any:
    current: Any = payload
    for key in path:
        if not isinstance(current, dict):
            return None
        current = current.get(key)
    return current


def compare(baseline: dict[str, Any], candidate: dict[str, Any]) -> dict[str, Any]:
    rows = {
        "decision_failures": {
            "baseline": metric(baseline, "decision", "failures"),
            "candidate": metric(candidate, "decision", "failures"),
        },
        "ingestion_failures": {
            "baseline": metric(baseline, "ingestion", "failures"),
            "candidate": metric(candidate, "ingestion", "failures"),
        },
        "decision_p95_ms": {
            "baseline": metric(baseline, "decision", "latency", "p95_ms"),
            "candidate": metric(candidate, "decision", "latency", "p95_ms"),
        },
        "decision_p99_ms": {
            "baseline": metric(baseline, "decision", "latency", "p99_ms"),
            "candidate": metric(candidate, "decision", "latency", "p99_ms"),
        },
        "end_to_end_p95_ms": {
            "baseline": metric(baseline, "end_to_end_latency", "p95_ms"),
            "candidate": metric(candidate, "end_to_end_latency", "p95_ms"),
        },
        "end_to_end_p99_ms": {
            "baseline": metric(baseline, "end_to_end_latency", "p99_ms"),
            "candidate": metric(candidate, "end_to_end_latency", "p99_ms"),
        },
        "schedule_lag_p50_ms": {
            "baseline": metric(baseline, "schedule_lag", "p50_ms"),
            "candidate": metric(candidate, "schedule_lag", "p50_ms"),
        },
        "schedule_lag_p95_ms": {
            "baseline": metric(baseline, "schedule_lag", "p95_ms"),
            "candidate": metric(candidate, "schedule_lag", "p95_ms"),
        },
    }
    for row in rows.values():
        base = maybe_float(row["baseline"])
        cand = maybe_float(row["candidate"])
        row["delta"] = None if base is None or cand is None else cand - base
        row["ratio_candidate_over_baseline"] = ratio(cand, base)
    return rows


def build_report(
    baseline_label: str,
    baseline_path: Path,
    baseline_payload: dict[str, Any],
    candidate_label: str,
    candidate_path: Path,
    candidate_payload: dict[str, Any],
) -> dict[str, Any]:
    return {
        "comparison_type": "production_replay_summary",
        "baseline_label": baseline_label,
        "candidate_label": candidate_label,
        "baseline_summary_path": str(baseline_path),
        "candidate_summary_path": str(candidate_path),
        "baseline_status": baseline_payload.get("status"),
        "candidate_status": candidate_payload.get("status"),
        "metrics": compare(baseline_payload, candidate_payload),
    }


def format_number(value: Any) -> str:
    parsed = maybe_float(value)
    if parsed is None:
        return "n/a"
    return f"{parsed:.2f}"


def print_report(report: dict[str, Any]) -> None:
    print("")
    print("Production Replay Summary Comparison")
    print(f"  baseline:  {report['baseline_label']}")
    print(f"  candidate: {report['candidate_label']}")
    print("")
    for name, row in report["metrics"].items():
        print(
            f"  {name}: "
            f"{format_number(row['candidate'])} vs {format_number(row['baseline'])} "
            f"(delta {format_number(row['delta'])}, ratio {format_number(row['ratio_candidate_over_baseline'])})"
        )


def main() -> None:
    parser = argparse.ArgumentParser(description="Compare two production replay summary.json artifacts.")
    parser.add_argument("--baseline-label", default="baseline")
    parser.add_argument("--baseline-summary", required=True)
    parser.add_argument("--candidate-label", default="candidate")
    parser.add_argument("--candidate-summary", required=True)
    parser.add_argument("--output", help="Optional output JSON path")
    args = parser.parse_args()

    baseline_label, baseline_path, baseline_payload = load_summary(args.baseline_label, args.baseline_summary)
    candidate_label, candidate_path, candidate_payload = load_summary(args.candidate_label, args.candidate_summary)
    report = build_report(
        baseline_label,
        baseline_path,
        baseline_payload,
        candidate_label,
        candidate_path,
        candidate_payload,
    )
    if args.output:
        output_path = Path(args.output)
        output_path.parent.mkdir(parents=True, exist_ok=True)
        output_path.write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")
    print_report(report)


if __name__ == "__main__":
    main()
