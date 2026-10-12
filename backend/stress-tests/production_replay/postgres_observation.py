"""Bounded, read-only PostgreSQL/River observation through the installed psql."""
from __future__ import annotations

import asyncio
import json
import math
from typing import Any
from datetime import datetime, timezone


def observation_sql(*, storage: bool = False) -> str:
    # A separate ordered prefix per active state avoids scanning completed history.
    # The cap bounds returned rows; statement_timeout also bounds a missing-index plan.
    prefixes = " UNION ALL ".join(
        f"(SELECT queue, state::text, scheduled_at FROM public.river_job "
        f"WHERE state = '{state}' ORDER BY scheduled_at LIMIT 1001)"
        for state in ("available", "running", "retryable", "scheduled", "pending")
    )
    storage_sql = """
        (SELECT coalesce(json_agg(s), '[]'::json) FROM (
            SELECT n.nspname AS schema_name, c.relname AS table_name,
                   pg_total_relation_size(c.oid) AS total_bytes,
                   pg_table_size(c.oid) AS table_bytes,
                   pg_indexes_size(c.oid) AS index_bytes,
                   st.n_live_tup AS estimated_live_rows, st.n_dead_tup AS estimated_dead_rows,
                   st.last_autovacuum, st.last_autoanalyze
            FROM pg_catalog.pg_class c
            JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
            LEFT JOIN pg_catalog.pg_stat_user_tables st ON st.relid = c.oid
            WHERE c.relkind IN ('r', 'p') AND n.nspname !~ '^pg_' AND n.nspname <> 'information_schema'
            ORDER BY n.nspname, c.relname LIMIT 1001
        ) s)
    """ if storage else "NULL"
    return f"""
        WITH sampled AS ({prefixes}),
        state_totals AS (SELECT state, count(*) AS sampled_count FROM sampled GROUP BY state),
        queues AS (
            SELECT queue, state, count(*) AS observed_jobs,
                   greatest(0, extract(epoch FROM current_timestamp - min(scheduled_at))) AS oldest_due_seconds
            FROM sampled GROUP BY queue, state
        )
        SELECT json_build_object(
            'captured_at', clock_timestamp(),
            'database_bytes', pg_database_size(current_database()),
            'database', (SELECT row_to_json(d) FROM (
                SELECT numbackends, xact_commit, xact_rollback, blks_read, blks_hit,
                       tup_returned, tup_fetched, temp_files, temp_bytes, deadlocks,
                       blk_read_time, blk_write_time, tup_inserted, tup_updated, tup_deleted, stats_reset
                FROM pg_catalog.pg_stat_database WHERE datname = current_database()
            ) d),
            'activity', (SELECT coalesce(json_agg(a), '[]'::json) FROM (
                SELECT state, wait_event_type, wait_event, count(*) AS connections,
                       count(*) FILTER (WHERE cardinality(pg_blocking_pids(pid)) > 0) AS blocked_connections,
                       max(extract(epoch FROM current_timestamp - xact_start)) AS oldest_transaction_seconds
                FROM pg_catalog.pg_stat_activity WHERE datname = current_database()
                GROUP BY state, wait_event_type, wait_event
            ) a),
            'configuration', json_build_object(
                'server_version', current_setting('server_version'),
                'max_connections', current_setting('max_connections'),
                'shared_buffers', current_setting('shared_buffers'),
                'work_mem', current_setting('work_mem'),
                'track_io_timing', current_setting('track_io_timing')),
            'storage', {storage_sql},
            'river', (SELECT coalesce(json_agg(q), '[]'::json) FROM (
                SELECT queues.*, state_totals.sampled_count >= 1001 AS counts_are_lower_bounds
                FROM queues JOIN state_totals USING (state)
            ) q)
        );
    """


class PostgresObserver:
    def __init__(self, command: list[str], env: dict[str, str], timeout: float) -> None:
        if not math.isfinite(timeout) or timeout <= 0:
            raise ValueError("database observation timeout must be finite and positive")
        self.command = command
        self.env = {**env, "PGAPPNAME": "fraud_scale_observer", "PGOPTIONS": "-c default_transaction_read_only=on "
                    f"-c statement_timeout={max(1, int(timeout * 1000))} "
                    "-c lock_timeout=1000", "PGCONNECT_TIMEOUT": str(max(1, math.ceil(timeout)))}
        self.timeout = timeout

    async def __call__(self, *, storage: bool = False) -> dict[str, Any]:
        process = None
        try:
            process = await asyncio.create_subprocess_exec(
                *self.command, "-X", "-w", "-qAt", "-v", "ON_ERROR_STOP=1", "-c", observation_sql(storage=storage),
                env=self.env, stdout=asyncio.subprocess.PIPE, stderr=asyncio.subprocess.PIPE,
            )
            stdout, _stderr = await asyncio.wait_for(process.communicate(), self.timeout)
            if process.returncode:
                return {"status": "error", "error_type": "PsqlFailure", "exit_code": process.returncode}
            metrics = json.loads(stdout)
            if not isinstance(metrics, dict) or not isinstance(metrics.get("database"), dict):
                raise ValueError("invalid database metrics")
            if storage and len(metrics.get("storage") or []) >= 1001:
                return {"status": "error", "error_type": "StorageCatalogTruncated", "metrics": metrics}
            return {"status": "ok", "captured_at": datetime.now(timezone.utc).isoformat(), "metrics": metrics,
                    "scope": "current_database_and_public_river_active_jobs",
                    "limitations": ["case_queue_and_non_river_queues_not_collected",
                                    "queue_prefix_cap_1001_per_state",
                                    "oldest_due_is_scheduled_age_not_enqueue_or_running_age",
                                    "activity_visibility_depends_on_database_role",
                                    "raw_counters_require_stats_reset_aware_deltas",
                                    "query_plans_require_validation_on_target_database"]}
        except (OSError, ValueError, asyncio.TimeoutError, TimeoutError) as exc:
            return {"status": "error", "error_type": type(exc).__name__}
        finally:
            if process is not None and process.returncode is None:
                process.kill()
                await process.communicate()


COUNTERS = ("xact_commit", "xact_rollback", "blks_read", "blks_hit", "tup_returned",
            "tup_fetched", "tup_inserted", "tup_updated", "tup_deleted", "temp_files",
            "temp_bytes", "deadlocks", "blk_read_time", "blk_write_time")


def counter_deltas(before: dict[str, Any], after: dict[str, Any]) -> dict[str, Any]:
    """Never interpret a reset or a missing counter as zero workload."""
    if before.get("status") != "ok" or after.get("status") != "ok":
        return {"valid": False, "reason": "incomplete_boundary_snapshot"}
    start, end = before["metrics"], after["metrics"]
    a, b = start["database"], end["database"]
    if "stats_reset" not in a or "stats_reset" not in b or a["stats_reset"] != b["stats_reset"]:
        return {"valid": False, "reason": "statistics_reset_or_unknown"}
    try:
        elapsed = (datetime.fromisoformat(end["captured_at"].replace("Z", "+00:00")) -
                   datetime.fromisoformat(start["captured_at"].replace("Z", "+00:00"))).total_seconds()
        values = {name: b[name] - a[name] for name in COUNTERS}
    except (KeyError, TypeError, ValueError):
        return {"valid": False, "reason": "missing_counter_or_timestamp"}
    if elapsed <= 0 or any(value < 0 for value in values.values()):
        return {"valid": False, "reason": "counter_reset_or_invalid_window"}
    reads = values["blks_read"] + values["blks_hit"]
    return {"valid": True, "elapsed_seconds": elapsed, "counters": values,
            "per_second": {name: value / elapsed for name, value in values.items()},
            "buffer_hit_ratio": values["blks_hit"] / reads if reads else None,
            "io_timing_enabled": start.get("configuration", {}).get("track_io_timing") == "on"
                                 and end.get("configuration", {}).get("track_io_timing") == "on",
            "scope": "current_database_including_monitoring_and_background_work"}
