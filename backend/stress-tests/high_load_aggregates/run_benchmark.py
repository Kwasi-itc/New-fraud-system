#!/usr/bin/env python3
"""Phase 0 baseline benchmark harness for high-load aggregate queries.

Runs strictly against an isolated disposable PostgreSQL database.
Uses psql/pgbench inside the disposable postgres:16 container (or local tools)
so it requires only Python 3 standard library.
"""

from __future__ import annotations

import argparse
import json
import math
import os
import re
import subprocess
import sys
import tempfile
import threading
import time
from dataclasses import dataclass, asdict
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

DISPOSABLE_MARKERS = ("disposable", "bench", "test", "tmp")
TENANT_SCHEMA = "tenant_00000000000040008000000000000001"
TABLE_NAME = "transactions"
FULL_TABLE = f"{TENANT_SCHEMA}.{TABLE_NAME}"

MERCHANTS = [
    ("m_hot_1k", 1_000),
    ("m_hot_100k", 100_000),
    ("m_hot_1m", 1_000_000),
]


@dataclass
class ExplainMetrics:
    scenario: str
    merchant_id: str
    index_mode: str
    node_types: list[str]
    execution_time_ms: float
    planning_time_ms: float
    heap_fetches: int
    shared_blks_hit: int
    shared_blks_read: int


@dataclass
class LatencySummary:
    scenario: str
    merchant_id: str
    index_mode: str
    concurrency: int
    duration_s: int
    total_queries: int
    tps: float
    p50_ms: float
    p95_ms: float
    p99_ms: float
    max_ms: float
    pg_cpu_pct_avg: float
    pg_cpu_pct_peak: float


@dataclass
class IngestBenchmark:
    label: str
    batches: int
    batch_size: int
    total_rows: int
    elapsed_s: float
    rows_per_sec: float
    pg_cpu_pct_avg: float


def validate_disposable_target(db_name: str, database_url: str | None) -> None:
    target = (database_url or db_name).lower()
    if not any(marker in target for marker in DISPOSABLE_MARKERS):
        raise SystemExit(
            f"REFUSING TO RUN: target '{target}' lacks an explicit disposable marker "
            f"({', '.join(DISPOSABLE_MARKERS)}). Never run against the application DB."
        )


def run_cmd(cmd: list[str], input_text: str | None = None, check: bool = True) -> subprocess.CompletedProcess[str]:
    res = subprocess.run(
        cmd,
        input=input_text,
        text=True,
        capture_output=True,
    )
    if check and res.returncode != 0:
        raise RuntimeError(f"Command {' '.join(cmd)} failed ({res.returncode}):\nSTDOUT:\n{res.stdout}\nSTDERR:\n{res.stderr}")
    return res


def docker_psql(container: str, db_user: str, db_name: str, sql: str) -> str:
    res = run_cmd(
        [
            "docker", "exec", "-i", container,
            "psql", "-U", db_user, "-d", db_name, "-v", "ON_ERROR_STOP=1", "-At",
        ],
        input_text=sql,
    )
    return res.stdout


def ensure_container(container: str, port: int, db_user: str, db_pass: str, db_name: str) -> None:
    inspect = subprocess.run(
        ["docker", "inspect", "-f", "{{.State.Running}}", container],
        text=True,
        capture_output=True,
    )
    if inspect.returncode == 0:
        run_cmd(["docker", "rm", "-f", container])

    run_cmd([
        "docker", "run", "-d",
        "--name", container,
        "--shm-size=1g",
        "-e", f"POSTGRES_USER={db_user}",
        "-e", f"POSTGRES_PASSWORD={db_pass}",
        "-e", f"POSTGRES_DB={db_name}",
        "-p", f"127.0.0.1:{port}:5432",
        "postgres:16",
        "-c", "shared_buffers=512MB",
        "-c", "work_mem=16MB",
        "-c", "max_connections=300",
    ])

    for _ in range(60):
        probe = subprocess.run(
            ["docker", "exec", container, "psql", "-U", db_user, "-d", db_name, "-c", "SELECT 1"],
            text=True,
            capture_output=True,
        )
        if probe.returncode == 0:
            time.sleep(0.5)
            probe2 = subprocess.run(
                ["docker", "exec", container, "psql", "-U", db_user, "-d", db_name, "-c", "SELECT 1"],
                text=True,
                capture_output=True,
            )
            if probe2.returncode == 0:
                return
        time.sleep(0.5)
    raise RuntimeError(f"Postgres container {container} did not become ready in time")


def seed_database(container: str, db_user: str, db_name: str) -> dict[str, Any]:
    print("Creating schema and seeding ~1.4M transactions + 5% in-place ModePatch updates...")
    t0 = time.monotonic()
    setup_sql = f"""
    DROP SCHEMA IF EXISTS {TENANT_SCHEMA} CASCADE;
    CREATE SCHEMA {TENANT_SCHEMA};
    CREATE TABLE {FULL_TABLE} (
        id UUID NOT NULL PRIMARY KEY,
        object_id TEXT NOT NULL,
        updated_at TIMESTAMPTZ NOT NULL,
        valid_from TIMESTAMPTZ NOT NULL DEFAULT NOW(),
        valid_until TIMESTAMPTZ NOT NULL DEFAULT 'INFINITY',
        merchant_id TEXT NOT NULL,
        created_at TIMESTAMPTZ NOT NULL,
        amount DOUBLE PRECISION NOT NULL,
        status TEXT NOT NULL
    );
    CREATE UNIQUE INDEX uniq_transactions_object_id ON {FULL_TABLE} (object_id);

    -- 1k hot merchant in 7d
    INSERT INTO {FULL_TABLE} (id, object_id, updated_at, merchant_id, created_at, amount, status)
    SELECT
        gen_random_uuid(),
        'tx_1k_' || g,
        NOW() - ((g % 10080) || ' minutes')::interval,
        'm_hot_1k',
        NOW() - ((g % 10080) || ' minutes')::interval,
        ((g % 500) + 10)::double precision,
        CASE WHEN g % 10 = 0 THEN 'declined' ELSE 'approved' END
    FROM generate_series(1, 1000) AS g;

    -- 100k hot merchant in 7d
    INSERT INTO {FULL_TABLE} (id, object_id, updated_at, merchant_id, created_at, amount, status)
    SELECT
        gen_random_uuid(),
        'tx_100k_' || g,
        NOW() - ((g % 604800) || ' seconds')::interval,
        'm_hot_100k',
        NOW() - ((g % 604800) || ' seconds')::interval,
        ((g % 500) + 10)::double precision,
        CASE WHEN g % 10 = 0 THEN 'declined' ELSE 'approved' END
    FROM generate_series(1, 100000) AS g;

    -- 1M hot merchant in 7d + 100k older (8d..30d) for 30d AVG coverage
    INSERT INTO {FULL_TABLE} (id, object_id, updated_at, merchant_id, created_at, amount, status)
    SELECT
        gen_random_uuid(),
        'tx_1m_' || g,
        NOW() - ((g % 604800) || ' seconds')::interval,
        'm_hot_1m',
        NOW() - ((g % 604800) || ' seconds')::interval,
        ((g % 500) + 10)::double precision,
        CASE WHEN g % 10 = 0 THEN 'declined' ELSE 'approved' END
    FROM generate_series(1, 1000000) AS g;

    INSERT INTO {FULL_TABLE} (id, object_id, updated_at, merchant_id, created_at, amount, status)
    SELECT
        gen_random_uuid(),
        'tx_1m_old_' || g,
        NOW() - INTERVAL '7 days' - ((g % 1987200) || ' seconds')::interval,
        'm_hot_1m',
        NOW() - INTERVAL '7 days' - ((g % 1987200) || ' seconds')::interval,
        ((g % 500) + 10)::double precision,
        CASE WHEN g % 10 = 0 THEN 'declined' ELSE 'approved' END
    FROM generate_series(1, 100000) AS g;

    -- 10,000 background merchants (20 rows each = 200,000 rows over 30d)
    INSERT INTO {FULL_TABLE} (id, object_id, updated_at, merchant_id, created_at, amount, status)
    SELECT
        gen_random_uuid(),
        'tx_bg_' || g,
        NOW() - ((g % 2592000) || ' seconds')::interval,
        'm_bg_' || ((g % 10000) + 1),
        NOW() - ((g % 2592000) || ' seconds')::interval,
        ((g % 250) + 5)::double precision,
        CASE WHEN g % 10 = 0 THEN 'declined' ELSE 'approved' END
    FROM generate_series(1, 200000) AS g;

    -- 5% in-place ModePatch updates to simulate production row churn
    UPDATE {FULL_TABLE}
    SET status = CASE WHEN status = 'approved' THEN 'settled' ELSE status END,
        updated_at = NOW()
    WHERE hashtext(object_id) % 20 = 0;

    ANALYZE {FULL_TABLE};
    """
    docker_psql(container, db_user, db_name, setup_sql)
    elapsed = time.monotonic() - t0

    stats_raw = docker_psql(
        container, db_user, db_name,
        f"""
        SELECT json_build_object(
            'total_rows', (SELECT COUNT(*) FROM {FULL_TABLE}),
            'table_size', pg_size_pretty(pg_relation_size('{FULL_TABLE}')),
            'n_tup_upd', (SELECT n_tup_upd FROM pg_stat_user_tables WHERE relname = '{TABLE_NAME}'),
            'n_tup_hot_upd', (SELECT n_tup_hot_upd FROM pg_stat_user_tables WHERE relname = '{TABLE_NAME}'),
            'n_dead_tup', (SELECT n_dead_tup FROM pg_stat_user_tables WHERE relname = '{TABLE_NAME}')
        )::text;
        """,
    ).strip()
    stats = json.loads(stats_raw)
    stats["seed_elapsed_s"] = round(elapsed, 2)
    print(f"Seeded {stats['total_rows']} rows ({stats['table_size']}) in {stats['seed_elapsed_s']}s")
    return stats


def query_definitions() -> list[tuple[str, str]]:
    return [
        ("count_1h", f"SELECT COUNT(\"object_id\") FROM {FULL_TABLE} WHERE \"merchant_id\" = '{{merchant}}' AND \"created_at\" >= NOW() - INTERVAL '1 hour'"),
        ("sum_24h", f"SELECT COALESCE(SUM(\"amount\"), 0) FROM {FULL_TABLE} WHERE \"merchant_id\" = '{{merchant}}' AND \"created_at\" >= NOW() - INTERVAL '24 hours'"),
        ("sum_7d", f"SELECT COALESCE(SUM(\"amount\"), 0) FROM {FULL_TABLE} WHERE \"merchant_id\" = '{{merchant}}' AND \"created_at\" >= NOW() - INTERVAL '7 days'"),
        ("avg_30d", f"SELECT AVG(\"amount\") FROM {FULL_TABLE} WHERE \"merchant_id\" = '{{merchant}}' AND \"created_at\" >= NOW() - INTERVAL '30 days'"),
        ("sum_7d_filtered", f"SELECT COALESCE(SUM(\"amount\"), 0) FROM {FULL_TABLE} WHERE \"merchant_id\" = '{{merchant}}' AND \"status\" = 'approved' AND \"created_at\" >= NOW() - INTERVAL '7 days'"),
    ]


def collect_plan_nodes(plan: dict[str, Any], nodes: list[str], totals: dict[str, int]) -> None:
    nodes.append(plan.get("Node Type", "Unknown"))
    totals["heap_fetches"] += int(plan.get("Heap Fetches", 0) or 0)
    # Top-level plan node in Postgres EXPLAIN JSON already holds cumulative buffer counts across children
    for child in plan.get("Plans", []):
        collect_plan_nodes(child, nodes, totals)


def run_explain(container: str, db_user: str, db_name: str, scenario: str, sql_tmpl: str, merchant_id: str, index_mode: str) -> ExplainMetrics:
    sql = sql_tmpl.format(merchant=merchant_id)
    out = docker_psql(
        container, db_user, db_name,
        f"EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) {sql};",
    )
    payload = json.loads(out)[0]
    plan = payload["Plan"]
    node_types: list[str] = []
    totals = {"heap_fetches": 0}
    collect_plan_nodes(plan, node_types, totals)
    return ExplainMetrics(
        scenario=scenario,
        merchant_id=merchant_id,
        index_mode=index_mode,
        node_types=node_types,
        execution_time_ms=round(float(payload.get("Execution Time", 0.0)), 3),
        planning_time_ms=round(float(payload.get("Planning Time", 0.0)), 3),
        heap_fetches=totals["heap_fetches"],
        shared_blks_hit=int(plan.get("Shared Hit Blocks", 0) or 0),
        shared_blks_read=int(plan.get("Shared Read Blocks", 0) or 0),
    )


class CpuSampler:
    def __init__(self, container: str) -> None:
        self.container = container
        self.samples: list[float] = []
        self._stop = threading.Event()
        self._thread = threading.Thread(target=self._run, daemon=True)

    def start(self) -> None:
        self._thread.start()

    def stop(self) -> tuple[float, float]:
        self._stop.set()
        self._thread.join(timeout=5)
        if not self.samples:
            return 0.0, 0.0
        return round(sum(self.samples) / len(self.samples), 1), round(max(self.samples), 1)

    def _run(self) -> None:
        while not self._stop.is_set():
            res = subprocess.run(
                ["docker", "stats", "--no-stream", "--format", "{{.CPUPerc}}", self.container],
                text=True,
                capture_output=True,
            )
            if res.returncode == 0:
                raw = res.stdout.strip().rstrip("%")
                try:
                    self.samples.append(float(raw))
                except ValueError:
                    pass
            self._stop.wait(1.0)


def percentile(sorted_vals: list[float], pct: float) -> float:
    if not sorted_vals:
        return 0.0
    idx = (len(sorted_vals) - 1) * (pct / 100.0)
    lo = math.floor(idx)
    hi = math.ceil(idx)
    if lo == hi:
        return sorted_vals[lo]
    return sorted_vals[lo] + (sorted_vals[hi] - sorted_vals[lo]) * (idx - lo)


def run_pgbench_query(
    container: str,
    db_user: str,
    db_name: str,
    scenario: str,
    sql_tmpl: str,
    merchant_id: str,
    index_mode: str,
    concurrency: int,
    duration_s: int,
) -> LatencySummary:
    sql = sql_tmpl.format(merchant=merchant_id) + ";"
    docker_psql(container, db_user, db_name, "SELECT 1;")  # ensure responsive
    script_path = "/tmp/bench_query.sql"
    subprocess.run(
        ["docker", "exec", "-i", container, "bash", "-c", f"rm -f /tmp/pgbench_log* && cat > {script_path}"],
        input=sql + "\n",
        text=True,
        check=True,
    )

    jobs = min(concurrency, 16)
    sampler = CpuSampler(container)
    sampler.start()
    res = run_cmd([
        "docker", "exec", container,
        "pgbench", "-U", db_user, "-d", db_name,
        "-n", "-f", script_path,
        "-c", str(concurrency),
        "-j", str(jobs),
        "-T", str(duration_s),
        "-l", "--log-prefix=/tmp/pgbench_log",
    ])
    cpu_avg, cpu_peak = sampler.stop()

    lat_raw = run_cmd([
        "docker", "exec", container,
        "bash", "-c", "cat /tmp/pgbench_log* | awk '{print $3 / 1000.0}'",
    ]).stdout
    latencies = sorted(float(line) for line in lat_raw.splitlines() if line.strip())

    tps_match = re.search(r"tps = ([0-9.]+) \(without initial connection time\)", res.stdout)
    tps = float(tps_match.group(1)) if tps_match else (len(latencies) / max(duration_s, 1))

    return LatencySummary(
        scenario=scenario,
        merchant_id=merchant_id,
        index_mode=index_mode,
        concurrency=concurrency,
        duration_s=duration_s,
        total_queries=len(latencies),
        tps=round(tps, 2),
        p50_ms=round(percentile(latencies, 50), 2),
        p95_ms=round(percentile(latencies, 95), 2),
        p99_ms=round(percentile(latencies, 99), 2),
        max_ms=round(latencies[-1], 2) if latencies else 0.0,
        pg_cpu_pct_avg=cpu_avg,
        pg_cpu_pct_peak=cpu_peak,
    )


def run_batch_ingest_benchmark(container: str, db_user: str, db_name: str, label: str, batches: int = 20, batch_size: int = 500) -> IngestBenchmark:
    sampler = CpuSampler(container)
    sampler.start()
    t0 = time.monotonic()
    for b in range(batches):
        prefix = f"bench_{label}_{int(time.time()*1000)}_{b}"
        sql = f"""
        WITH batch AS (
            SELECT
                gen_random_uuid() AS id,
                '{prefix}_' || g AS object_id,
                NOW() AS updated_at,
                'm_bg_' || ((g % 1000) + 1) AS merchant_id,
                NOW() AS created_at,
                ((g % 500) + 10)::double precision AS amount,
                'approved'::text AS status
            FROM generate_series(1, {batch_size}) AS g
        )
        INSERT INTO {FULL_TABLE} (id, object_id, updated_at, merchant_id, created_at, amount, status)
        SELECT id, object_id, updated_at, merchant_id, created_at, amount, status FROM batch
        ON CONFLICT (object_id) DO NOTHING;
        """
        docker_psql(container, db_user, db_name, sql)
    elapsed = time.monotonic() - t0
    cpu_avg, _ = sampler.stop()
    total_rows = batches * batch_size
    return IngestBenchmark(
        label=label,
        batches=batches,
        batch_size=batch_size,
        total_rows=total_rows,
        elapsed_s=round(elapsed, 3),
        rows_per_sec=round(total_rows / max(elapsed, 0.001), 1),
        pg_cpu_pct_avg=cpu_avg,
    )


def write_report(
    out_dir: Path,
    seed_stats: dict[str, Any],
    explains: list[ExplainMetrics],
    latencies: list[LatencySummary],
    ingests: list[IngestBenchmark],
    report_filename: str = "BASELINE_REPORT.md",
    json_filename: str = "baseline_results.json",
    title: str = "High-Load Aggregates Phase 0 Baseline Report",
) -> None:
    results = {
        "generated_at": datetime.now(timezone.utc).isoformat(),
        "seed_stats": seed_stats,
        "explains": [asdict(e) for e in explains],
        "latencies": [asdict(l) for l in latencies],
        "ingests": [asdict(i) for i in ingests],
    }
    (out_dir / json_filename).write_text(json.dumps(results, indent=2), encoding="utf-8")

    lines = [
        f"# {title}",
        "",
        f"- **Generated at**: `{results['generated_at']}`",
        f"- **PostgreSQL**: `postgres:16` (isolated disposable container, `shared_buffers=512MB`, `work_mem=16MB`, `max_connections=300`)",
        f"- **Dataset**: `{seed_stats['total_rows']}` rows (`{seed_stats['table_size']}`), seeded in `{seed_stats['seed_elapsed_s']}s`",
        f"- **In-place `ModePatch` churn**: `n_tup_upd={seed_stats['n_tup_upd']}`, `n_tup_hot_upd={seed_stats['n_tup_hot_upd']}`, `n_dead_tup={seed_stats['n_dead_tup']}`",
        "- **Target SLO Gate**: Aggregate `p99 <= 25 ms` at `200` concurrent evaluations on `m_hot_1m` (1M-row merchant, 7d window), `< 10%` `BatchIngest` regression.",
        "",
        "## 1. EXPLAIN (ANALYZE, BUFFERS) Plans",
        "",
        "| Index Mode | Merchant | Query | Plan Nodes | Exec (ms) | Heap Fetches | Shared Hit | Shared Read |",
        "| :--- | :--- | :--- | :--- | ---: | ---: | ---: | ---: |",
    ]
    for e in explains:
        plan_str = " → ".join(e.node_types[:4])
        lines.append(
            f"| `{e.index_mode}` | `{e.merchant_id}` | `{e.scenario}` | {plan_str} | {e.execution_time_ms} | {e.heap_fetches} | {e.shared_blks_hit} | {e.shared_blks_read} |"
        )

    lines.extend([
        "",
        "## 2. Concurrent Evaluation Latency (p50 / p95 / p99)",
        "",
        "| Index Mode | Merchant | Query | Concurrency | Queries | TPS | p50 (ms) | p95 (ms) | p99 (ms) | PG CPU avg/peak (%) |",
        "| :--- | :--- | :--- | ---: | ---: | ---: | ---: | ---: | ---: | :--- |",
    ])
    for l in latencies:
        lines.append(
            f"| `{l.index_mode}` | `{l.merchant_id}` | `{l.scenario}` | {l.concurrency} | {l.total_queries} | {l.tps} | {l.p50_ms} | {l.p95_ms} | {l.p99_ms} | {l.pg_cpu_pct_avg}% / {l.pg_cpu_pct_peak}% |"
        )

    lines.extend([
        "",
        "## 3. BatchIngest (500-row batches) Throughput",
        "",
        "| Label | Batches | Total Rows | Elapsed (s) | Throughput (rows/s) | PG CPU avg (%) |",
        "| :--- | ---: | ---: | ---: | ---: | ---: |",
    ])
    for ing in ingests:
        lines.append(
            f"| `{ing.label}` | {ing.batches} | {ing.total_rows} | {ing.elapsed_s} | {ing.rows_per_sec} | {ing.pg_cpu_pct_avg}% |"
        )

    (out_dir / report_filename).write_text("\n".join(lines) + "\n", encoding="utf-8")
    print(f"Wrote {out_dir / report_filename} and {json_filename}")


def main() -> int:
    parser = argparse.ArgumentParser(description="Phase 0 High-Load Aggregates Benchmark Harness")
    parser.add_argument("--container", default="high-load-aggregates-bench-db")
    parser.add_argument("--port", type=int, default=55439)
    parser.add_argument("--db-user", default="bench")
    parser.add_argument("--db-pass", default="bench")
    parser.add_argument("--db-name", default="high_load_aggregates_disposable")
    parser.add_argument("--manage-container", action="store_true", help="Start a fresh disposable postgres:16 container")
    parser.add_argument("--duration", type=int, default=6, help="pgbench duration in seconds per concurrency run")
    parser.add_argument("--phase1", action="store_true", help="Also benchmark Phase 1 covering index (INCLUDE + autovacuum) and write PHASE1_REPORT.md")
    args = parser.parse_args()

    validate_disposable_target(args.db_name, os.environ.get("DATABASE_URL"))

    if args.manage_container:
        ensure_container(args.container, args.port, args.db_user, args.db_pass, args.db_name)

    seed_stats = seed_database(args.container, args.db_user, args.db_name)
    queries = dict(query_definitions())

    explains: list[ExplainMetrics] = []
    latencies: list[LatencySummary] = []
    ingests: list[IngestBenchmark] = []

    # 1. Baseline: no aggregate index (only PK + unique object_id)
    print("\n--- Mode 1: no_agg_index (current unindexed draft / missing prep) ---")
    ingests.append(run_batch_ingest_benchmark(args.container, args.db_user, args.db_name, "no_agg_index"))
    for merchant_id, _ in MERCHANTS:
        for sc_name, sql_tmpl in query_definitions():
            explains.append(run_explain(args.container, args.db_user, args.db_name, sc_name, sql_tmpl, merchant_id, "no_agg_index"))

    # Run concurrency matrix on unindexed 100k merchant (1M sequential scan at c=200 would saturate for minutes; measure c=1 and c=50 on 100k + c=1 on 1M)
    for sc_name in ("sum_7d",):
        for conc in (1, 50):
            latencies.append(run_pgbench_query(args.container, args.db_user, args.db_name, sc_name, queries[sc_name], "m_hot_100k", "no_agg_index", conc, args.duration))
        latencies.append(run_pgbench_query(args.container, args.db_user, args.db_name, sc_name, queries[sc_name], "m_hot_1m", "no_agg_index", 1, args.duration))

    # 2. Current PublicationService best-case index: (merchant_id, created_at) without INCLUDE
    print("\n--- Mode 2: current_pub_index ((merchant_id, created_at), no INCLUDE) ---")
    docker_psql(
        args.container, args.db_user, args.db_name,
        f"CREATE INDEX idx_current_pub_merchant_created ON {FULL_TABLE} (merchant_id, created_at); ANALYZE {FULL_TABLE};",
    )
    ingests.append(run_batch_ingest_benchmark(args.container, args.db_user, args.db_name, "current_pub_index"))

    for merchant_id, _ in MERCHANTS:
        for sc_name, sql_tmpl in query_definitions():
            explains.append(run_explain(args.container, args.db_user, args.db_name, sc_name, sql_tmpl, merchant_id, "current_pub_index"))

    for merchant_id in ("m_hot_1k", "m_hot_100k", "m_hot_1m"):
        for sc_name in ("count_1h", "sum_24h", "sum_7d", "avg_30d", "sum_7d_filtered"):
            if merchant_id != "m_hot_1m" and sc_name not in ("sum_7d",):
                continue
            for conc in (1, 50, 200):
                print(f"Benchmarking current_pub_index | {merchant_id} | {sc_name} | c={conc}")
                latencies.append(run_pgbench_query(args.container, args.db_user, args.db_name, sc_name, queries[sc_name], merchant_id, "current_pub_index", conc, args.duration))

    out_dir = Path(__file__).resolve().parent
    if args.phase1:
        print("\n--- Mode 3: phase1_covering_index ((merchant_id, created_at) INCLUDE (amount, status) + VACUUM ANALYZE) ---")
        docker_psql(
            args.container, args.db_user, args.db_name,
            f"""
            ALTER TABLE {FULL_TABLE} SET (autovacuum_vacuum_scale_factor = 0.02, autovacuum_analyze_scale_factor = 0.01);
            DROP INDEX IF EXISTS idx_current_pub_merchant_created;
            CREATE INDEX CONCURRENTLY idx_phase1_covering_merchant_created ON {FULL_TABLE} (merchant_id, created_at) INCLUDE (amount, status);
            VACUUM ANALYZE {FULL_TABLE};
            """,
        )
        ingests.append(run_batch_ingest_benchmark(args.container, args.db_user, args.db_name, "phase1_covering_index"))
        for merchant_id, _ in MERCHANTS:
            for sc_name, sql_tmpl in query_definitions():
                explains.append(run_explain(args.container, args.db_user, args.db_name, sc_name, sql_tmpl, merchant_id, "phase1_covering_index"))
        for merchant_id in ("m_hot_1k", "m_hot_100k", "m_hot_1m"):
            for sc_name in ("count_1h", "sum_24h", "sum_7d", "avg_30d", "sum_7d_filtered"):
                if merchant_id != "m_hot_1m" and sc_name not in ("sum_7d",):
                    continue
                for conc in (1, 50, 200):
                    print(f"Benchmarking phase1_covering_index | {merchant_id} | {sc_name} | c={conc}")
                    latencies.append(run_pgbench_query(args.container, args.db_user, args.db_name, sc_name, queries[sc_name], merchant_id, "phase1_covering_index", conc, args.duration))
        write_report(
            out_dir, seed_stats, explains, latencies, ingests,
            report_filename="PHASE1_REPORT.md",
            json_filename="phase1_results.json",
            title="High-Load Aggregates Phase 1 Covering Index Gate Report",
        )
        return 0

    write_report(out_dir, seed_stats, explains, latencies, ingests)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
