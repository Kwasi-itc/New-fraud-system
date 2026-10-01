"""Bounded, read-only PostgreSQL/River observation through the installed psql."""
from __future__ import annotations

import asyncio
import json
import math
from typing import Any


def observation_sql() -> str:
    # A separate ordered prefix per active state avoids scanning completed history.
    # The cap bounds returned rows; statement_timeout also bounds a missing-index plan.
    prefixes = " UNION ALL ".join(
        f"(SELECT queue, state::text, scheduled_at FROM public.river_job "
        f"WHERE state = '{state}' ORDER BY scheduled_at LIMIT 1001)"
        for state in ("available", "running", "retryable", "scheduled", "pending")
    )
    return f"""
        WITH sampled AS ({prefixes}),
        state_totals AS (SELECT state, count(*) AS sampled_count FROM sampled GROUP BY state),
        queues AS (
            SELECT queue, state, count(*) AS observed_jobs,
                   greatest(0, extract(epoch FROM current_timestamp - min(scheduled_at))) AS oldest_due_seconds
            FROM sampled GROUP BY queue, state
        )
        SELECT json_build_object(
            'database', (SELECT row_to_json(d) FROM (
                SELECT numbackends, xact_commit, xact_rollback, blks_read, blks_hit,
                       tup_returned, tup_fetched, temp_files, temp_bytes, deadlocks,
                       blk_read_time, blk_write_time, stats_reset
                FROM pg_catalog.pg_stat_database WHERE datname = current_database()
            ) d),
            'activity', (SELECT coalesce(json_agg(a), '[]'::json) FROM (
                SELECT state, wait_event_type, count(*) AS connections
                FROM pg_catalog.pg_stat_activity WHERE datname = current_database()
                GROUP BY state, wait_event_type
            ) a),
            'configuration', json_build_object(
                'max_connections', current_setting('max_connections'),
                'shared_buffers', current_setting('shared_buffers'),
                'work_mem', current_setting('work_mem'),
                'track_io_timing', current_setting('track_io_timing')),
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
        self.env = {**env, "PGOPTIONS": "-c default_transaction_read_only=on "
                    f"-c statement_timeout={max(1, int(timeout * 1000))} "
                    "-c lock_timeout=1000", "PGCONNECT_TIMEOUT": str(max(1, math.ceil(timeout)))}
        self.timeout = timeout

    async def __call__(self) -> dict[str, Any]:
        process = None
        try:
            process = await asyncio.create_subprocess_exec(
                *self.command, "-X", "-w", "-qAt", "-v", "ON_ERROR_STOP=1", "-c", observation_sql(),
                env=self.env, stdout=asyncio.subprocess.PIPE, stderr=asyncio.subprocess.PIPE,
            )
            stdout, _stderr = await asyncio.wait_for(process.communicate(), self.timeout)
            if process.returncode:
                return {"status": "error", "error_type": "PsqlFailure", "exit_code": process.returncode}
            metrics = json.loads(stdout)
            if not isinstance(metrics, dict) or not isinstance(metrics.get("database"), dict):
                raise ValueError("invalid database metrics")
            return {"status": "ok", "metrics": metrics,
                    "scope": "current_database_and_public_river_active_jobs",
                    "limitations": ["case_queue_and_non_river_queues_not_collected",
                                    "queue_prefix_cap_1001_per_state",
                                    "oldest_due_is_scheduled_age_not_enqueue_or_running_age",
                                    "activity_visibility_depends_on_database_role",
                                    "raw_counters_require_stats_reset_aware_deltas",
                                    "query_plans_require_validation_on_target_database"]}
        except (OSError, ValueError, TimeoutError) as exc:
            return {"status": "error", "error_type": type(exc).__name__}
        finally:
            if process is not None and process.returncode is None:
                process.kill()
                await process.communicate()
