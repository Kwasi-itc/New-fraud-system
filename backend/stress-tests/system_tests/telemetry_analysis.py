"""Streaming, reset-aware rates from existing observer snapshots."""
from __future__ import annotations

import json
import math
from pathlib import Path


def numeric(value):
    return type(value) in (int, float) and math.isfinite(value) and value >= 0


def deltas(previous, current, fields):
    if not fields or any(not numeric(previous.get(k)) or not numeric(current.get(k)) for k in fields):
        return None
    values = {k: current[k] - previous[k] for k in fields}
    return values if all(value >= 0 for value in values.values()) else None


def summarize(path: Path):
    previous = None
    samples = resets = invalid_intervals = 0
    busy = total = db_seconds = 0.0
    commits = 0
    max_rss = None
    with path.open("rb") as handle:
        while line := handle.readline(1_048_577):
            if len(line) > 1_048_576:
                raise ValueError("metric_row_too_large")
            current = json.loads(line)
            samples += 1
            local = current.get("local", {})
            measurements = local.get("measurements", {})
            rss = measurements.get("process_memory_bytes", {}).get("rss")
            if numeric(rss):
                max_rss = max(max_rss or 0, rss)
            if previous is not None:
                now, before = current.get("elapsed_seconds"), previous.get("elapsed_seconds")
                if not numeric(now) or not numeric(before) or now <= before or not current.get("complete") or not previous.get("complete"):
                    invalid_intervals += 1
                else:
                    oldlocal = previous.get("local", {})
                    boot = local.get("boot_time_epoch_seconds")
                    oldboot = oldlocal.get("boot_time_epoch_seconds")
                    cpu = measurements.get("cpu_seconds", {})
                    oldcpu = oldlocal.get("measurements", {}).get("cpu_seconds", {})
                    fields = set(cpu) - {"guest", "guest_nice"}
                    delta = deltas(oldcpu, cpu, fields)
                    if boot is not None and oldboot is not None and boot != oldboot:
                        resets += 1
                    elif boot is None or oldboot is None or "idle" not in fields or delta is None or set(oldcpu) != set(cpu):
                        invalid_intervals += 1
                    else:
                        interval_total = sum(delta.values())
                        total += interval_total
                        busy += interval_total - delta["idle"] - delta.get("iowait", 0)
                    database = current.get("database", {}).get("metrics", {}).get("database", {})
                    olddatabase = previous.get("database", {}).get("metrics", {}).get("database", {})
                    if database or olddatabase:
                        if database.get("stats_reset") != olddatabase.get("stats_reset"):
                            resets += 1
                        else:
                            delta = deltas(olddatabase, database, {"xact_commit"})
                            if delta is None:
                                invalid_intervals += 1
                            else:
                                commits += delta["xact_commit"]
                                db_seconds += now - before
            previous = current
    return {"schema_version": 1, "samples": samples, "reset_intervals": resets,
            "invalid_component_intervals": invalid_intervals,
            "observer_host_average_busy_fraction": busy / total if total > 0 else None,
            "observer_process_max_rss_bytes": max_rss,
            "database_commits_per_second": commits / db_seconds if db_seconds > 0 else None,
            "database_observed_interval_seconds": db_seconds,
            "limitations": ["observer_host_not_remote_service_hardware", "database_commits_include_collector_and_other_work",
                            "rates_only_cover_valid_intervals", "no_hardware_sizing_or_service_cpu_demand_claim"]}
