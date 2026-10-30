"""Read-only dataset verification and plans, restricted to an owned disposable container."""
from __future__ import annotations

import json
import subprocess
import uuid
from pathlib import Path

from production_replay.benchmark_reporting import write_json_atomic
from .model import AGGREGATES, Experiment, history_oracle


def capture_history(container: str, project: str, fixtures: list[dict], exp: Experiment, output: Path) -> dict:
    from .cli import inspect_owned
    inspect_owned(container, project)
    if not exp.history_rows and not any(x in AGGREGATES for x in exp.families):
        return {"status": "not_applicable", "historical_rows": 0}
    def query(sql):
        raw = subprocess.check_output(["docker", "exec", "-e",
            "PGOPTIONS=-c default_transaction_read_only=on -c statement_timeout=30000 -c lock_timeout=1000",
            container, "psql", "-U", "system_tests", "-d", "system_tests", "-X", "-w", "-qAt",
            "-v", "ON_ERROR_STOP=1", "-c", sql], text=True, timeout=35)
        return json.loads(raw)
    directory = output / "query-plans"
    directory.mkdir(exist_ok=False)
    datasets = []
    for fixture in fixtures:
        tid = str(uuid.UUID(fixture["tenant_id"]))
        schema = "tenant_" + tid.replace("-", "")
        table = f'"{schema}"."history"'
        state = query(f"SELECT json_build_object('rows', count(*), 'bytes', pg_total_relation_size('{table}')) FROM {table}")
        if state["rows"] != exp.history_rows:
            raise ValueError("seeded_history_count_mismatch")
        state["indexes"] = query(f"SELECT coalesce(json_agg(row_to_json(x)), '[]') FROM "
                                 f"(SELECT indexname,indexdef FROM pg_indexes WHERE schemaname='{schema}' AND tablename='history') x")
        state["tenant_id"] = tid
        datasets.append(state)
        for kind in sorted(set(exp.families) & set(AGGREGATES)):
            expression = {"count": 'COUNT("sequence")', "count_distinct": 'COUNT(DISTINCT "destination")',
                          "sum": 'COALESCE(SUM("amount"),0)', "avg": 'AVG("amount")',
                          "min": 'MIN("amount")', "max": 'MAX("amount")'}[kind]
            sql = (f"SELECT {expression} FROM {table} WHERE merchant_id='m0' "
                   "AND timestamp >= '2026-01-15T11:55:00Z' AND timestamp < '2026-01-15T12:00:00Z'")
            plan = query("EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) " + sql)
            write_json_atomic(directory / f"{tid}-{kind}.json", {"sql": sql, "plan": plan,
                "scope": "representative_literal_bound_query_outside_load", "not_actual_request_trace": True})
    return {"status": "verified", "datasets": datasets,
            "cache_effect": "plans_execute_reads_before_qualification_and_warmup; not_cold_DB"}
