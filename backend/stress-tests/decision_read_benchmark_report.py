from __future__ import annotations

import argparse
import json
from pathlib import Path
from typing import Any


def load_json(path_value: str) -> dict[str, Any]:
    path = Path(path_value)
    if path.is_dir():
        path = path / "summary.json"
    if not path.exists():
        raise SystemExit(f"path does not exist: {path}")
    return json.loads(path.read_text(encoding="utf-8"))


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


def aggregate_shapes(payloads: list[dict[str, Any]]) -> list[dict[str, Any]]:
    counts: dict[str, int] = {}
    for payload in payloads:
        metrics = payload.get("read_metrics", payload)
        endpoints = metrics.get("endpoints", {})
        aggregate = endpoints.get("aggregate_records", {})
        for shape, count in aggregate.get("aggregate_shape_counts", {}).items():
            try:
                counts[shape] = counts.get(shape, 0) + int(count)
            except (TypeError, ValueError):
                continue
    return [
        {"shape": shape, "count": count}
        for shape, count in sorted(counts.items(), key=lambda item: (-item[1], item[0]))
    ]


def compare_rule_complexity(baseline: dict[str, Any], candidate: dict[str, Any]) -> list[dict[str, Any]]:
    base_trials = {
        (trial["run"]["variant"], trial["run"]["configured_vus"]): trial
        for trial in baseline.get("trials", [])
    }
    rows: list[dict[str, Any]] = []
    for trial in candidate.get("trials", []):
        key = (trial["run"]["variant"], trial["run"]["configured_vus"])
        base = base_trials.get(key)
        if base is None:
            continue
        base_eps = maybe_float(base["rates"].get("achieved_successful_evaluations_per_second"))
        cand_eps = maybe_float(trial["rates"].get("achieved_successful_evaluations_per_second"))
        base_p95 = maybe_float(base["latency_ms"].get("p95"))
        cand_p95 = maybe_float(trial["latency_ms"].get("p95"))
        base_p99 = maybe_float(base["latency_ms"].get("p99"))
        cand_p99 = maybe_float(trial["latency_ms"].get("p99"))
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
    return rows


def compare_scenario_scaling(baseline: dict[str, Any], candidate: dict[str, Any]) -> list[dict[str, Any]]:
    base_trials = {
        (
            trial["setup"]["complexity"],
            trial["setup"]["scenario_count"],
            trial["setup"]["rules_per_scenario"],
            trial["run"]["configured_vus"],
        ): trial
        for trial in baseline.get("trials", [])
    }
    rows: list[dict[str, Any]] = []
    for trial in candidate.get("trials", []):
        key = (
            trial["setup"]["complexity"],
            trial["setup"]["scenario_count"],
            trial["setup"]["rules_per_scenario"],
            trial["run"]["configured_vus"],
        )
        base = base_trials.get(key)
        if base is None:
            continue
        base_rps = maybe_float(base["rates"].get("achieved_successful_requests_per_second"))
        cand_rps = maybe_float(trial["rates"].get("achieved_successful_requests_per_second"))
        base_p95 = maybe_float(base["latency_ms"].get("p95"))
        cand_p95 = maybe_float(trial["latency_ms"].get("p95"))
        base_p99 = maybe_float(base["latency_ms"].get("p99"))
        cand_p99 = maybe_float(trial["latency_ms"].get("p99"))
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
                "baseline_p99_ms": base_p99,
                "candidate_p99_ms": cand_p99,
                "p99_ratio_candidate_over_baseline": ratio(cand_p99, base_p99),
                "baseline_failures": base["workload_counts"]["failed_requests"],
                "candidate_failures": trial["workload_counts"]["failed_requests"],
                "baseline_timeouts": base["workload_counts"]["timeouts"],
                "candidate_timeouts": trial["workload_counts"]["timeouts"],
            }
        )
    return rows


def build_summary_comparison(baseline: dict[str, Any], candidate: dict[str, Any]) -> dict[str, Any]:
    baseline_name = str(baseline.get("test", {}).get("name", "unknown"))
    candidate_name = str(candidate.get("test", {}).get("name", "unknown"))
    if baseline_name != candidate_name:
        raise SystemExit(
            f"summary test names do not match: baseline={baseline_name!r}, candidate={candidate_name!r}"
        )
    if baseline_name == "decision_engine_rule_complexity_scaling":
        return {"comparison_type": "rule_complexity_scaling", "rows": compare_rule_complexity(baseline, candidate)}
    if baseline_name == "decision_engine_scenario_scaling":
        return {"comparison_type": "scenario_scaling", "rows": compare_scenario_scaling(baseline, candidate)}
    raise SystemExit(f"unsupported summary type for benchmark report: {baseline_name}")


def digest_runtime_metrics(payload: dict[str, Any]) -> dict[str, Any]:
    metrics = payload.get("runtime_metrics", payload)
    evaluation = metrics.get("evaluation", {})
    aggregate = metrics.get("aggregate_pushdown", {})
    db_pool = metrics.get("db_pool", {})
    return {
        "evaluation_single_p95_us": evaluation.get("single_p95_micros"),
        "evaluation_single_p99_us": evaluation.get("single_p99_micros"),
        "evaluation_multi_p95_us": evaluation.get("multi_p95_micros"),
        "evaluation_multi_p99_us": evaluation.get("multi_p99_micros"),
        "aggregate_remote_call_count": aggregate.get("remote_call_count"),
        "aggregate_remote_error_count": aggregate.get("remote_error_count"),
        "aggregate_fallback_count": aggregate.get("fallback_count"),
        "aggregate_latency_p95_us": aggregate.get("aggregate_latency_p95_micros"),
        "aggregate_latency_p99_us": aggregate.get("aggregate_latency_p99_micros"),
        "db_pool_acquired_conns": db_pool.get("acquired_conns"),
        "db_pool_idle_conns": db_pool.get("idle_conns"),
        "db_pool_empty_acquire_count": db_pool.get("empty_acquire_count"),
        "db_pool_canceled_acquire_count": db_pool.get("canceled_acquire_count"),
    }


def digest_read_metrics(payload: dict[str, Any]) -> dict[str, Any]:
    metrics = payload.get("read_metrics", payload)
    endpoints = metrics.get("endpoints", {})
    aggregate_endpoint = endpoints.get("aggregate_records", {})
    list_endpoint = endpoints.get("list_records", {})
    get_endpoint = endpoints.get("get_record", {})
    pressure = metrics.get("pressure", {})
    db_pool = metrics.get("db_pool", {})
    return {
        "aggregate_p95_us": aggregate_endpoint.get("p95_latency_micros"),
        "aggregate_p99_us": aggregate_endpoint.get("p99_latency_micros"),
        "aggregate_timeouts": aggregate_endpoint.get("timeouts"),
        "aggregate_cancellations": aggregate_endpoint.get("cancellations"),
        "aggregate_overloads": aggregate_endpoint.get("overloads"),
        "list_p95_us": list_endpoint.get("p95_latency_micros"),
        "get_p95_us": get_endpoint.get("p95_latency_micros"),
        "pressure_status": pressure.get("status"),
        "pressure_reasons": pressure.get("reasons", []),
        "pressure_db_pool_saturation_pct": pressure.get("db_pool_saturation_pct"),
        "pressure_aggregate_timeout_rate_pct": pressure.get("aggregate_timeout_rate_pct"),
        "pressure_read_overload_count": pressure.get("read_overload_count"),
        "pressure_aggregate_overload_count": pressure.get("aggregate_overload_count"),
        "db_pool_acquired_conns": db_pool.get("acquired_conns"),
        "db_pool_idle_conns": db_pool.get("idle_conns"),
        "db_pool_empty_acquire_count": db_pool.get("empty_acquire_count"),
        "db_pool_canceled_acquire_count": db_pool.get("canceled_acquire_count"),
    }


def format_number(value: Any) -> str:
    number = maybe_float(value)
    if number is None:
        return "n/a"
    return f"{number:.2f}"


def print_report(report: dict[str, Any]) -> None:
    print("")
    print("Decision Read Benchmark Report")
    print(f"  baseline:  {report['baseline_label']}")
    print(f"  candidate: {report['candidate_label']}")
    print(f"  type:      {report['summary_comparison']['comparison_type']}")
    print("")
    rows = report["summary_comparison"].get("rows", [])
    if rows:
        first = rows[0]
        if "variant" in first:
            for row in rows:
                print(
                    f"  {row['variant']} @ {row['vus']} VUs: "
                    f"EPS {format_number(row['candidate_eps'])} vs {format_number(row['baseline_eps'])}, "
                    f"p95 {format_number(row['candidate_p95_ms'])} vs {format_number(row['baseline_p95_ms'])}"
                )
        else:
            for row in rows:
                print(
                    f"  {row['complexity']} / {row['scenario_count']} scenarios / {row['rules_per_scenario']} rules / {row['vus']} VUs: "
                    f"RPS {format_number(row['candidate_rps'])} vs {format_number(row['baseline_rps'])}, "
                    f"p95 {format_number(row['candidate_p95_ms'])} vs {format_number(row['baseline_p95_ms'])}"
                )
    top_shapes = report.get("aggregate_shape_inventory", {}).get("rows", [])[:5]
    if top_shapes:
        print("")
        print("Top Aggregate Shapes")
        for row in top_shapes:
            print(f"  {row['count']:>8}  {row['shape']}")


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        description="Build one benchmark report comparing ingestion_http vs direct_db read modes."
    )
    parser.add_argument("--baseline-label", default="ingestion_http")
    parser.add_argument("--baseline-summary", required=True)
    parser.add_argument("--candidate-label", default="direct_db")
    parser.add_argument("--candidate-summary", required=True)
    parser.add_argument("--baseline-runtime-metrics")
    parser.add_argument("--candidate-runtime-metrics")
    parser.add_argument("--baseline-read-metrics")
    parser.add_argument("--candidate-read-metrics")
    parser.add_argument("--aggregate-shape-snapshots", nargs="*", default=[])
    parser.add_argument("--output", required=True, help="Output JSON path")
    return parser


def main() -> None:
    args = build_parser().parse_args()
    baseline_summary = load_json(args.baseline_summary)
    candidate_summary = load_json(args.candidate_summary)
    report: dict[str, Any] = {
        "baseline_label": args.baseline_label,
        "candidate_label": args.candidate_label,
        "summary_comparison": build_summary_comparison(baseline_summary, candidate_summary),
        "baseline_summary_path": args.baseline_summary,
        "candidate_summary_path": args.candidate_summary,
    }
    if args.baseline_runtime_metrics:
        report["baseline_runtime_metrics"] = digest_runtime_metrics(load_json(args.baseline_runtime_metrics))
    if args.candidate_runtime_metrics:
        report["candidate_runtime_metrics"] = digest_runtime_metrics(load_json(args.candidate_runtime_metrics))
    if args.baseline_read_metrics:
        report["baseline_read_metrics"] = digest_read_metrics(load_json(args.baseline_read_metrics))
    if args.candidate_read_metrics:
        report["candidate_read_metrics"] = digest_read_metrics(load_json(args.candidate_read_metrics))
    if args.aggregate_shape_snapshots:
        shape_payloads = [load_json(path) for path in args.aggregate_shape_snapshots]
        report["aggregate_shape_inventory"] = {
            "snapshot_count": len(shape_payloads),
            "rows": aggregate_shapes(shape_payloads),
        }

    output_path = Path(args.output)
    output_path.parent.mkdir(parents=True, exist_ok=True)
    output_path.write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")
    print_report(report)


if __name__ == "__main__":
    main()
