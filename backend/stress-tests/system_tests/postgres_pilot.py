"""Disposable PostgreSQL SQL-contract pilot. Never connects to an application database."""
from __future__ import annotations

import argparse
import asyncio
import json
import os
import subprocess
import time
import uuid
from pathlib import Path

from production_replay.benchmark_reporting import write_json_atomic
from production_replay.postgres_observation import PostgresObserver
from .queue_recovery import QueueCollector, queue_sql


def docker(*args: str, timeout: float = 30) -> str:
    result = subprocess.run(["docker", *args], capture_output=True, text=True, timeout=timeout, check=False)
    if result.returncode:
        raise RuntimeError("docker_command_failed")
    return result.stdout.strip()


def verify_container(identity: str, name: str) -> None:
    data = json.loads(docker("inspect", identity))[0]
    if (data["Id"] != identity or data["Name"] != "/" + name
            or data["Config"]["Labels"].get("system-tests.disposable") != "true"):
        raise ValueError("disposable_container_identity_mismatch")


def require(condition: bool, contract: str) -> None:
    # These are acceptance gates, so Python's -O flag must not disable them.
    if not condition:
        raise RuntimeError("postgres_contract_failed:" + contract)


async def pilot(identity: str, output: Path) -> None:
    def sql(statement: str) -> str:
        return docker("exec", identity, "psql", "-U", "system_tests", "-d", "system_tests",
                      "-X", "-qAt", "-v", "ON_ERROR_STOP=1", "-c", statement)

    # Minimal SQL contract fixture; this does not substitute for running service migrations.
    sql("""CREATE SCHEMA fixture;
        CREATE TYPE fixture.job_state AS ENUM ('available','running','retryable','scheduled','pending','completed');
        CREATE TABLE fixture.river_queue (name text PRIMARY KEY, paused_at timestamptz);
        CREATE TABLE fixture.river_job (
            id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY, queue text NOT NULL,
            state fixture.job_state NOT NULL, scheduled_at timestamptz NOT NULL);
        CREATE INDEX fixture_active_queue_idx ON fixture.river_job(queue,state,scheduled_at);
        INSERT INTO fixture.river_queue VALUES ('decisions',NULL),('other',NULL);
        INSERT INTO fixture.river_job(queue,state,scheduled_at)
            SELECT 'other','completed',now() FROM generate_series(1,20000);
        INSERT INTO fixture.river_job(queue,state,scheduled_at)
            SELECT 'decisions','available',now()-interval '30 seconds' FROM generate_series(1,1200);
        INSERT INTO fixture.river_job(queue,state,scheduled_at)
            VALUES ('decisions','scheduled',now()+interval '1 hour');
        ANALYZE fixture.river_job;""")
    command = ["docker", "exec", "-e", "PGOPTIONS=-c default_transaction_read_only=on -c statement_timeout=3000 -c lock_timeout=1000",
               identity, "psql", "-U", "system_tests", "-d", "system_tests"]
    db = PostgresObserver(command, dict(os.environ), 10)
    collector = QueueCollector(db, "system_tests", "fixture", ["decisions", "other"], 1000)
    capped = await collector()
    require(capped["status"] == "ok", "collection")
    require(capped["queues"]["decisions"]["counts_exact"] is False, "cap_detected")
    require(capped["queues"]["decisions"]["active_jobs"] == 1002, "capped_count")
    require(capped["queues"]["other"]["active_jobs"] == 0, "explicit_zero")
    require(capped["queues"]["decisions"]["oldest_due_seconds"] >= 29, "due_age")
    missing = await QueueCollector(db, "system_tests", "fixture", ["missing"], 1000)()
    require(missing["status"] == "error", "missing_registry")
    wrong = await QueueCollector(db, "wrong_database", "fixture", ["decisions"], 1000)()
    require(wrong["status"] == "error", "database_identity")
    write_attempt = await db.query("DELETE FROM fixture.river_job;")
    require(write_attempt["status"] == "error", "read_only")
    plan = json.loads(sql("EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) " + queue_sql("fixture", ["decisions", "other"], 1000)))
    write_json_atomic(output / "query-plan.json", plan)
    sql("DELETE FROM fixture.river_job WHERE queue='decisions' AND state='available';")
    drained = await collector()
    require(drained["queues"]["decisions"]["active_jobs"] == 1, "drain_count")
    require(drained["queues"]["decisions"]["counts_exact"] is True, "drain_exact")
    require(drained["queues"]["decisions"]["oldest_due_seconds"] == 0, "future_is_not_due")
    sql("UPDATE fixture.river_queue SET paused_at=now() WHERE name='decisions';")
    paused = await collector()
    require(paused["queues"]["decisions"]["paused"] is True, "paused")
    write_json_atomic(output / "observations.json", {"capped": capped, "drained": drained, "paused": paused})


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()
    args.output.mkdir(parents=True, exist_ok=False)
    name = "system-tests-pg-" + uuid.uuid4().hex[:12]
    identity = None
    state = {"status": "running", "container_name": name, "passed": False, "cleanup_verified": False,
             "scope": "postgres16_minimal_river_contract_fixture_not_service_migrations",
             "fixture_index_is_not_a_deployed_index_claim": True}
    write_json_atomic(args.output / "summary.json", state)
    try:
        print("Starting disposable PostgreSQL fixture", flush=True)
        identity = docker("run", "--detach", "--name", name, "--label", "system-tests.disposable=true",
            "--cpus", "1", "--memory", "512m", "--tmpfs", "/var/lib/postgresql/data",
            "-e", "POSTGRES_DB=system_tests", "-e", "POSTGRES_USER=system_tests",
            "-e", "POSTGRES_PASSWORD=disposable-test", "postgres:16", timeout=240)
        verify_container(identity, name)
        deadline = time.monotonic() + 60
        while True:
            try:
                docker("exec", identity, "pg_isready", "-U", "system_tests", "-d", "system_tests")
                break
            except RuntimeError:
                if time.monotonic() >= deadline:
                    raise TimeoutError("postgres_startup_timeout")
                time.sleep(1)
        print("Running SQL contracts and collecting EXPLAIN evidence", flush=True)
        asyncio.run(pilot(identity, args.output))
        state.update(status="completed", passed=True)
    except BaseException as exc:
        state.update(status="failed", error_type=type(exc).__name__)
        raise
    finally:
        if identity:
            try:
                verify_container(identity, name)
                docker("rm", "--force", identity)
                state["cleanup_verified"] = True
            except BaseException as exc:
                state.update(passed=False, cleanup_error_type=type(exc).__name__)
        write_json_atomic(args.output / "summary.json", state)
    if not state["passed"]:
        raise SystemExit(1)
    print("PostgreSQL pilot passed; disposable container removed", flush=True)


if __name__ == "__main__":
    main()
