from __future__ import annotations

import argparse
import json
from dataclasses import dataclass
from pathlib import Path
from typing import Any


@dataclass(frozen=True)
class RunSummary:
    label: str
    path: Path
    payload: dict[str, Any]

    @property
    def test_name(self) -> str:
        return str(self.payload.get("test", {}).get("name", "unknown"))


def load_summary(label: str, path_value: str) -> RunSummary:
    path = Path(path_value)
    if path.is_dir():
        path = path / "summary.json"
    if not path.exists():
        raise SystemExit(f"summary path does not exist: {path}")
    payload = json.loads(path.read_text(encoding="utf-8"))
    return RunSummary(label=label, path=path, payload=payload)


def maybe_number(value: Any) -> float | None:
    if value is None:
        return None
    try:
        return float(value)
    except (TypeError, ValueError):
        return None


def format_number(value: float | None) -> str:
    if value is None:
        return "n/a"
    return f"{value:.2f}"


def ratio(left: float | None, right: float | None) -> float | None:
    if left is None or right in (None, 0):
        return None
    return left / right


def compare_rule_complexity(baseline: RunSummary, candidate: RunSummary) -> dict[str, Any]:
    base_trials = {(trial["run"]["variant"], trial["run"]["configured_vus"]): trial for trial in baseline.payload.get("trials", [])}
    rows: list[dict[str, Any]] = []
    for trial in candidate.payload.get("trials", []):
        key = (trial["run"]["variant"], trial["run"]["configured_vus"])
        base = base_trials.get(key)
        if base is None:
            continue
        base_eps = maybe_number(base["rates"].get("achieved_successful_evaluations_per_second"))
        cand_eps = maybe_number(trial["rates"].get("achieved_successful_evaluations_per_second"))
        base_p95 = maybe_number(base["latency_ms"].get("p95"))
        cand_p95 = maybe_number(trial["latency_ms"].get("p95"))
        base_p99 = maybe_number(base["latency_ms"].get("p99"))
        cand_p99 = maybe_number(trial["latency_ms"].get("p99"))
        rows.append(
            {
                "variant": key[0],
                "vus": key[1],
                "baseline_eps": base_eps,
                "candidate_eps": cand_eps,
                "eps_ratio_candidate_over_baseline": ratio(cand_eps, base_eps),
                "baseline_p95_ms": base_p95,
                "candidate_p95_ms": cand_p95,
                "p95_ratio_candidate_over_baseline": ratio(cand_p95, base_p95),
                "baseline_p99_ms": base_p99,
                "candidate_p99_ms": cand_p99,
                "p99_ratio_candidate_over_baseline": ratio(cand_p99, base_p99),
                "baseline_failures": base["workload_counts"]["failed_evaluations"],
                "candidate_failures": trial["workload_counts"]["failed_evaluations"],
                "baseline_timeouts": base["workload_counts"]["timeouts"],
                "candidate_timeouts": trial["workload_counts"]["timeouts"],
            }
        )
    return {
        "comparison_type": "rule_complexity_scaling",
        "baseline_label": baseline.label,
        "candidate_label": candidate.label,
        "baseline_summary": str(baseline.path),
        "candidate_summary": str(candidate.path),
        "rows": rows,
    }


def compare_scenario_scaling(baseline: RunSummary, candidate: RunSummary) -> dict[str, Any]:
    base_trials = {
        (
            trial["setup"]["complexity"],
            trial["setup"]["scenario_count"],
            trial["setup"]["rules_per_scenario"],
            trial["run"]["configured_vus"],
        ): trial
        for trial in baseline.payload.get("trials", [])
    }
    rows: list[dict[str, Any]] = []
    for trial in candidate.payload.get("trials", []):
        key = (
            trial["setup"]["complexity"],
            trial["setup"]["scenario_count"],
            trial["setup"]["rules_per_scenario"],
            trial["run"]["configured_vus"],
        )
        base = base_trials.get(key)
        if base is None:
            continue
        base_rps = maybe_number(base["rates"].get("achieved_successful_requests_per_second"))
        cand_rps = maybe_number(trial["rates"].get("achieved_successful_requests_per_second"))
        base_p95 = maybe_number(base["latency_ms"].get("p95"))
        cand_p95 = maybe_number(trial["latency_ms"].get("p95"))
        rows.append(
            {
                "complexity": key[0],
                "scenario_count": key[1],
                "rules_per_scenario": key[2],
                "vus": key[3],
                "baseline_rps": base_rps,
                "candidate_rps": cand_rps,
                "rps_ratio_candidate_over_baseline": ratio(cand_rps, base_rps),
                "baseline_p95_ms": base_p95,
                "candidate_p95_ms": cand_p95,
                "p95_ratio_candidate_over_baseline": ratio(cand_p95, base_p95),
                "baseline_failures": base["workload_counts"]["failed_requests"],
                "candidate_failures": trial["workload_counts"]["failed_requests"],
                "baseline_timeouts": base["workload_counts"]["timeouts"],
                "candidate_timeouts": trial["workload_counts"]["timeouts"],
            }
        )
    return {
        "comparison_type": "scenario_scaling",
        "baseline_label": baseline.label,
        "candidate_label": candidate.label,
        "baseline_summary": str(baseline.path),
        "candidate_summary": str(candidate.path),
        "rows": rows,
    }


def detect_and_compare(baseline: RunSummary, candidate: RunSummary) -> dict[str, Any]:
    baseline_name = baseline.test_name
    candidate_name = candidate.test_name
    if baseline_name != candidate_name:
        raise SystemExit(
            f"summary test names do not match: baseline={baseline_name!r}, candidate={candidate_name!r}"
        )
    if baseline_name == "decision_engine_rule_complexity_scaling":
        return compare_rule_complexity(baseline, candidate)
    if baseline_name == "decision_engine_scenario_scaling":
        return compare_scenario_scaling(baseline, candidate)
    raise SystemExit(f"unsupported summary type for comparison: {baseline_name}")


def print_report(report: dict[str, Any]) -> None:
    print("")
    print("Read Mode Comparison")
    print(f"  baseline:  {report['baseline_label']}")
    print(f"  candidate: {report['candidate_label']}")
    print(f"  type:      {report['comparison_type']}")
    print("")
    rows = report.get("rows", [])
    if not rows:
        print("  no comparable trial rows found")
        return

    if report["comparison_type"] == "rule_complexity_scaling":
        for row in rows:
            print(
                f"  {row['variant']} @ {row['vus']} VUs: "
                f"EPS {format_number(row['candidate_eps'])} vs {format_number(row['baseline_eps'])} "
                f"({format_number(row['eps_ratio_candidate_over_baseline'])}x), "
                f"p95 {format_number(row['candidate_p95_ms'])} vs {format_number(row['baseline_p95_ms'])} "
                f"({format_number(row['p95_ratio_candidate_over_baseline'])}x)"
            )
        return

    for row in rows:
        print(
            f"  {row['complexity']} / {row['scenario_count']} scenarios / {row['rules_per_scenario']} rules / {row['vus']} VUs: "
            f"RPS {format_number(row['candidate_rps'])} vs {format_number(row['baseline_rps'])} "
            f"({format_number(row['rps_ratio_candidate_over_baseline'])}x), "
            f"p95 {format_number(row['candidate_p95_ms'])} vs {format_number(row['baseline_p95_ms'])} "
            f"({format_number(row['p95_ratio_candidate_over_baseline'])}x)"
        )


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        description="Compare two stress-test summary runs, typically ingestion_http vs direct_db."
    )
    parser.add_argument("--baseline-label", default="ingestion_http")
    parser.add_argument("--baseline-summary", required=True)
    parser.add_argument("--candidate-label", default="direct_db")
    parser.add_argument("--candidate-summary", required=True)
    parser.add_argument("--output", help="Optional path to write comparison JSON.")
    return parser


def main() -> None:
    args = build_parser().parse_args()
    baseline = load_summary(args.baseline_label, args.baseline_summary)
    candidate = load_summary(args.candidate_label, args.candidate_summary)
    report = detect_and_compare(baseline, candidate)
    if args.output:
        output_path = Path(args.output)
        output_path.parent.mkdir(parents=True, exist_ok=True)
        output_path.write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")
    print_report(report)


if __name__ == "__main__":
    main()
