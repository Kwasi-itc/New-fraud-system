"""Comparison reports retain invalid runs and never combine percentile values."""
from __future__ import annotations

import html
import json
import statistics
from collections import defaultdict
from pathlib import Path

from production_replay.benchmark_reporting import write_json_atomic
from .model import digest


def report(output: Path) -> dict:
    records = []
    for path in sorted(output.glob("*/summary.json")):
        value = json.loads(path.read_text(encoding="utf-8"))
        if value.get("schema_version") == "decision-benchmark/v1":
            records.append({**value, "artifact": str(path.relative_to(output))})
    groups = defaultdict(list)
    for item in records:
        # Exact configuration equality only. Other-axis comparisons are descriptive rows.
        if item.get("valid"):
            groups[digest(item["configuration"])].append(item)
    comparisons = []
    for key, items in groups.items():
        rates = [x["measurement"]["rates"]["correct_inputs_per_second"] for x in items]
        comparisons.append({"configuration_sha256": key, "benchmark_id": items[0]["benchmark_id"],
            "valid_repeats": len(items), "correct_inputs_per_second_median": statistics.median(rates),
            "range": [min(rates), max(rates)], "variability_qualified": len(items)>=3})
    result = {"schema_version": "decision-benchmark-report/v1", "runs": records,
              "comparisons": comparisons, "production_qualified": False}
    write_json_atomic(output / "report.json", result)
    lines = ["# Decision benchmark results", "", "Discovery only; not production capacity qualification.", "",
             "| Benchmark | Valid | S / R | Correct inputs/s | p50 / p90 / p95 / p99 ms | Failures | Drops |",
             "|---|---|---|---|---|---|---|"]
    for item in records:
        m = item.get("measurement", {})
        latency = m.get("latencies", {}).get("all_attempts", {})
        config = item["configuration"]
        percentiles = " / ".join(str(latency.get(f"p{x}_ms", "unavailable")) for x in (50,90,95,99))
        lines.append(f"| {item['benchmark_id']} | {item.get('valid', False)} | {config['scenarios']} / {config['rules']} | "
                     f"{m.get('rates', {}).get('correct_inputs_per_second', 'unavailable')} | {percentiles} | "
                     f"{m.get('counts', {}).get('failed', 'unavailable')} | {m.get('counts', {}).get('generator_dropped', 'unavailable')} |")
    lines += ["", "Percentiles are per-run approximate histogram estimates, not averaged across repetitions.",
              "See report.json for repeat ranges, correctness, reconciliation, telemetry and failure details."]
    if not records:
        lines += ["", "No completed experiment records. Inspect lifecycle.json and docker.log for setup failures."]
    content = "\n".join(lines)+"\n"
    (output / "report.md").write_text(content, encoding="utf-8")
    (output / "report.html").write_text("<!doctype html><meta charset='utf-8'><title>Decision benchmark</title><pre>"+
                                       html.escape(content)+"</pre>", encoding="utf-8")
    return result
