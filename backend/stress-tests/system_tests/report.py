"""Standalone HTML evidence report; no percentile averaging or capacity extrapolation."""
from __future__ import annotations

import html
import json
from pathlib import Path
from typing import Any
from production_replay.benchmark_reporting import write_json_atomic
from .telemetry_analysis import summarize


def write_report(output: Path, campaign: dict[str, Any]) -> Path:
    rows = []
    telemetry = {}
    for run in campaign["runs"]:
        metrics_path = output / run["name"] / "metrics.ndjson"
        if metrics_path.exists():
            telemetry[run["name"]] = summarize(metrics_path)
        path = output / run["name"] / "summary.json"
        if not path.exists():
            rows.append([run["name"], run.get("profile", ""), "unknown", "no report", "—", "—", "—", "—", "—", "—"])
            continue
        result = json.loads(path.read_text())
        for name, cohort in result.get("cohorts", {}).items():
            latency = cohort["latencies"]["successful_completion"]
            counts = cohort["counts"]
            rows.append([run["name"], run.get("profile", ""), result.get("path", "ingest-async"), name,
                cohort["rate"], cohort["planned"], counts.get("completed", 0), counts.get("deadline_met", 0),
                latency.get("p95_ms"), latency.get("p99_ms")])
    def cell(value):
        return html.escape("unknown" if value is None else str(value))
    table = "".join("<tr>" + "".join(f"<td>{cell(c)}</td>" for c in row) + "</tr>" for row in rows)
    outcomes = "".join(f"<li>{cell(r['name'])}: {cell(r['status'])}; checks passed: "
        f"{cell(r.get('acceptance', {}).get('passed', False))}</li>" for r in campaign["runs"])
    resources = "".join(f"<li>{cell(name)}: observer host busy fraction {cell(value['observer_host_average_busy_fraction'])}; "
        f"observer process peak RSS {cell(value['observer_process_max_rss_bytes'])} bytes; database commits/s "
        f"{cell(value['database_commits_per_second'])}; reset intervals {cell(value['reset_intervals'])}; "
        f"invalid component intervals {cell(value['invalid_component_intervals'])}.</li>" for name, value in telemetry.items())
    document = f"""<!doctype html><html lang="en"><meta charset="utf-8">
<title>System performance campaign</title><style>
body {{font:16px system-ui;max-width:1200px;margin:3rem auto;padding:0 1rem;color:#17202a}}
table {{border-collapse:collapse;width:100%}}td,th {{padding:.6rem;border:1px solid #ccd1d1;text-align:left}}
th {{background:#eef2f7}} .note {{padding:1rem;background:#fff4d6}} </style>
<h1>System performance campaign</h1>
<p>Status: {cell(campaign['status'])}. Planned runs: {cell(campaign['planned_runs'])}.</p>
<p class="note">Discovery evidence only. This report does not qualify production capacity or recommend hardware.
Profiles are operator labels, not independently verified resource allocations.</p>
<ul>{outcomes}</ul><table><thead><tr>
<th>Run</th><th>Profile</th><th>Path</th><th>Input cohort</th><th>Offered/s</th><th>Planned</th>
<th>Completed</th><th>By deadline</th><th>p95 ms</th><th>p99 ms</th>
</tr></thead><tbody>{table}</tbody></table>
<p>Latency percentiles are approximate, per input cohort, and include successful operations only.
Failures, unresolved inputs and generator drops remain failures of the completion gate. Percentiles are never averaged.
Input history grows during a run; different histories, tenants, source cohorts and profiles may prevent direct comparisons.</p>
<h2>Resource observations</h2><ul>{resources or '<li>No resource measurements collected.</li>'}</ul>
<p>Host measurements describe the observer machine. Database commit counts include collector and other traffic.
Rates exclude counter-reset or invalid intervals; missing values are unknown.</p>
<p>Inspect each run's summary.json, timeline.ndjson and optional queues.ndjson / metrics.ndjson for evidence.
No database mutation or service request is made when generating this report.</p></html>"""
    destination = output / "report.html"
    temporary = output / "report.html.tmp"
    temporary.write_text(document, encoding="utf-8")
    temporary.replace(destination)
    write_json_atomic(output / "telemetry-summary.json", telemetry)
    return destination
