"""Isolated real-service smoke pilot with shared PostgreSQL and ordered migrations."""
from __future__ import annotations

import argparse
import asyncio
import json
import os
import re
import subprocess
import uuid
from pathlib import Path

from production_replay.api_client import ServiceClients, ServiceConfig
from production_replay.benchmark_reporting import write_json_atomic
from production_replay.manifest import load_manifest
from production_replay.setup_environment import EnvironmentSetup
from .queue_campaign import async_main as run_experiment
from .prepare_inputs import prepare, synthetic_fields
from .queue_recovery import QueueCollector, QueueRecoveryMonitor, RecoveryGate, RecoveryPolicy
from production_replay.postgres_observation import PostgresObserver


def fixture(project: str, image_prefix: str | None = None) -> dict:
    backend = Path(__file__).resolve().parents[2]
    labels = {"system-tests.disposable": "true", "system-tests.project": project}
    env = {"DATABASE_URL": "postgres://system_tests:disposable-test@postgres:5432/system_tests?sslmode=disable&pool_max_conns=8",
           "SERVICE_AUTH_MODE": "disabled", "LOG_LEVEL": "warn", "GIN_MODE": "release",
           "DATA_MODEL_SERVICE_URL": "http://data-model:8080", "INGESTION_SERVICE_URL": "http://ingestion:8081",
           "DATABASE_MAX_CONNS": "8", "WORKER_DATABASE_MAX_CONNS": "4", "WORKER_MODE": "poll",
           "AGGREGATE_PUSHDOWN_AGGREGATES": "count,count_distinct,sum,avg,min,max"}
    services = {"postgres": {"image": "postgres:16", "labels": labels,
        "environment": {"POSTGRES_DB": "system_tests", "POSTGRES_USER": "system_tests", "POSTGRES_PASSWORD": "disposable-test"},
        "tmpfs": ["/var/lib/postgresql/data"], "mem_limit": "512m",
        "healthcheck": {"test": ["CMD", "pg_isready", "-U", "system_tests", "-d", "system_tests"],
                        "interval": "2s", "timeout": "2s", "retries": 30}}}
    previous = None
    for role, port in (("data-model", 8080), ("ingestion", 8081), ("decision-engine", 8082)):
        image = (image_prefix or project) + "-" + role
        common = {"image": image, "pull_policy": "never", "labels": labels, "environment": env, "mem_limit": "512m"}
        migration = role + "-migrate"
        dependencies = {"postgres": {"condition": "service_healthy"}}
        if previous:
            dependencies[previous] = {"condition": "service_completed_successfully"}
        services[migration] = {**common,
            "environment": {**env, "DATABASE_URL": env["DATABASE_URL"].replace("&pool_max_conns=8", "")},
            "entrypoint": ["/app/migrate"], "command": ["up"], "depends_on": dependencies, "restart": "no"}
        if image_prefix is None:
            services[migration]["build"] = (
                {"context": str(backend), "dockerfile": role + "-service/Dockerfile"}
                if role in ("data-model", "decision-engine")
                else {"context": str(backend / (role + "-service"))}
            )
        services[role] = {**common, "depends_on": {migration: {"condition": "service_completed_successfully"}},
            "ports": [f"127.0.0.1::{port}"]}
        services[role + "-worker"] = {**common, "entrypoint": ["/app/worker"],
            "depends_on": {migration: {"condition": "service_completed_successfully"}}}
        previous = migration
    return {"services": services}


async def exercise(output: Path, endpoints: dict[str, str], project: str, worker_id: str, postgres_id: str) -> dict:
    source = output / "source"
    source.mkdir()
    manifest = {"version": 1, "timezone": "UTC",
        "reference_data": {"merchant_globs": ["unused.json"], "merchant_product_globs": ["unused.json"], "staff_csv": "unused.csv"},
        "transaction_streams": [{"id": "pilot", "adapter": "production_transaction_csv_v1", "globs": ["unused.csv"],
            "channel": "wallet", "direction": "incoming", "system_type": "wallet_transfer"}]}
    write_json_atomic(source / "manifest.json", manifest)
    async with ServiceClients(ServiceConfig(endpoints["data-model"], endpoints["ingestion"], endpoints["decision-engine"])) as clients:
        setup = await asyncio.wait_for(EnvironmentSetup(load_manifest(source / "manifest.json"), clients,
            None, "Isolated system test pilot", scenario_set="internal").run(publication_timeout_seconds=120), timeout=180)
    result = {"tenant_id": setup["tenant_id"], "scenarios": len(setup["scenarios"]), "paths": {}}
    for path in ("ingestion", "ingest-sync", "ingest-async"):
        cohort = source / (path + ".ndjson")
        with cohort.open("w", encoding="utf-8") as handle:
            for index in range(10):
                identity = f"pilot-{path}-{index}"
                handle.write(json.dumps({"object_id": identity, "transaction_id": identity,
                    "date": "2026-10-01T00:00:00Z", "amount": 10, "channel": "wallet", "direction": "incoming",
                    "system_type": "wallet_transfer", "stream_id": "pilot", "merchant_id": "pilot-merchant",
                    "account_ref": "pilot-account"}) + "\n")
        code = await run_experiment(["--input", str(cohort), "--output", str(output / path),
            "--tenant-id", setup["tenant_id"], "--path", path, "--stage", "smoke:2:5",
            "--expected-scenarios", str(len(setup["scenarios"])), "--deadline", "30", "--max-scheduling-lag", "1",
            "--completion-timeout", "60", "--data-model-url", endpoints["data-model"],
            "--ingestion-url", endpoints["ingestion"], "--decision-engine-url", endpoints["decision-engine"]])
        result["paths"][path] = {"exit_code": code, "passed": code == 0}
    result["passed"] = all(r["passed"] for r in result["paths"].values())
    if result["passed"]:
        cohort = source / "outage.ndjson"
        prepare(cohort, synthetic_fields("pilot-outage", 24), 24)
        command = ["docker", "exec", "-e", "PGOPTIONS=-c default_transaction_read_only=on -c statement_timeout=3000 -c lock_timeout=1000",
                   postgres_id, "psql", "-U", "system_tests", "-d", "system_tests"]
        database = PostgresObserver(command, dict(os.environ), 10)
        collector = QueueCollector(database, "system_tests", "public", ["async_decision_executions"])
        monitor = QueueRecoveryMonitor(collector,
            RecoveryGate(["async_decision_executions"], RecoveryPolicy(6, 6, 1, 5, 5, 5)),
            output / "outage-queues.ndjson", 0.5)

        async def outage_run():
            return await run_experiment(["--input", str(cohort), "--output", str(output / "worker-outage"),
                "--tenant-id", setup["tenant_id"], "--stage", "baseline:2:2", "--stage", "outage:2:4",
                "--stage", "recovery:2:6", "--expected-scenarios", str(len(setup["scenarios"])),
                "--deadline", "30", "--completion-timeout", "60", "--max-scheduling-lag", "1",
                "--data-model-url", endpoints["data-model"], "--ingestion-url", endpoints["ingestion"],
                "--decision-engine-url", endpoints["decision-engine"], "--worker-container-id", worker_id,
                "--worker-project", project, "--worker-outage-stage", "outage", "--worker-outage-seconds", "2"])
        code = await monitor.run_during(outage_run)
        result["queue_recovery"] = monitor.summary()
        result["paths"]["worker-outage"] = {"exit_code": code, "passed": code == 0 and monitor.summary()["passed"]}
        # Independent durable counts in this new, tiny synthetic tenant. Not a scale reconciler.
        tenant = str(uuid.UUID(setup["tenant_id"]))
        schema = "tenant_" + uuid.UUID(tenant).hex
        durable = await database.query(f"""SELECT json_build_object(
            'transactions', (SELECT count(*) FROM "{schema}".transactions),
            'audit', (SELECT count(*) FROM core_ingestion.ingestion_audit WHERE tenant_id='{tenant}'::uuid),
            'audit_objects', (SELECT count(DISTINCT object_id) FROM core_ingestion.ingestion_audit WHERE tenant_id='{tenant}'::uuid),
            'outbox', (SELECT count(*) FROM core_ingestion.outbox_events WHERE tenant_id='{tenant}'::uuid),
            'completed_async', (SELECT count(*) FROM core.async_decision_executions
                WHERE tenant_id='{tenant}'::uuid AND status='completed'));""")
        result["durable_fixture_counts"] = durable
        expected = {"transactions": 54, "audit": 54, "audit_objects": 54, "outbox": 54, "completed_async": 34}
        result["durable_fixture_counts_passed"] = durable.get("metrics") == expected
        result["passed"] = all(r["passed"] for r in result["paths"].values()) and result["durable_fixture_counts_passed"]
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--reuse-images-from", help="Prior system-tests-* project's locally built images")
    args = parser.parse_args()
    if args.reuse_images_from and not re.fullmatch(r"system-tests-[a-z0-9-]{1,60}", args.reuse_images_from):
        parser.error("image prefix must identify a prior system-tests-* project")
    run_fixture(args.output, args.reuse_images_from, exercise)


def run_fixture(output: Path, image_prefix, exercise_fn, *, compose_factory=fixture,
                scope="synthetic_smoke_not_capacity_or_business_rule_qualification"):
    """Own the disposable stack lifecycle for a supplied workload."""
    if image_prefix and not re.fullmatch(r"system-tests-[a-z0-9-]{1,60}", image_prefix):
        raise ValueError("invalid image prefix")
    output = output.resolve()
    output.mkdir(parents=True, exist_ok=False)
    project = "system-tests-" + uuid.uuid4().hex[:12]
    compose = output / "compose.json"
    write_json_atomic(compose, compose_factory(project, image_prefix))
    base = ["docker", "compose", "--project-name", project, "--file", str(compose)]
    state = {"project": project, "status": "starting", "passed": False, "cleanup_verified": False,
             "scope": scope}
    write_json_atomic(output / "lifecycle.json", state)

    def command(*parts, timeout=60):
        if parts[0] == "up":
            with (output / "docker.log").open("a", encoding="utf-8") as log:
                completed = subprocess.run([*base, *parts], stdout=log, stderr=subprocess.STDOUT, timeout=timeout)
            if completed.returncode:
                raise RuntimeError("compose_operation_failed")
            return ""
        completed = subprocess.run([*base, *parts], capture_output=True, text=True, timeout=timeout)
        with (output / "docker.log").open("a", encoding="utf-8") as log:
            log.write(completed.stdout + completed.stderr)
        if completed.returncode:
            raise RuntimeError("compose_operation_failed")
        return completed.stdout.strip()

    try:
        print("Starting isolated application services; see docker.log", flush=True)
        command("up", *([] if image_prefix else ["--build"]), "--detach", timeout=900)
        endpoints = {}
        for role, port in (("data-model", 8080), ("ingestion", 8081), ("decision-engine", 8082)):
            address = command("port", role, str(port))
            endpoints[role] = "http://" + address
        print("Running workload against isolated local services", flush=True)
        worker_id = command("ps", "--quiet", "decision-engine-worker")
        postgres_id = command("ps", "--quiet", "postgres")
        evidence = asyncio.run(exercise_fn(output, endpoints, project, worker_id, postgres_id))
        state.update(status="completed", passed=evidence["passed"])
        write_json_atomic(output / "evidence.json", evidence)
    except BaseException as exc:
        state.update(status="failed", error_type=type(exc).__name__)
        raise
    finally:
        try:
            command("logs", "--no-color", timeout=30)
            # Validate all project containers before removing only this randomly named project.
            ids = command("ps", "--all", "--quiet").splitlines()
            for identity in ids:
                data = json.loads(subprocess.check_output(["docker", "inspect", identity], text=True, timeout=20))[0]
                labels = data["Config"]["Labels"]
                if (labels.get("system-tests.project") != project or labels.get("system-tests.disposable") != "true"
                        or labels.get("com.docker.compose.project") != project):
                    raise ValueError("cleanup_target_verification_failed")
            command("down", "--remove-orphans", "--volumes", timeout=60)
            state["cleanup_verified"] = not command("ps", "--all", "--quiet")
            state["passed"] &= state["cleanup_verified"]
        except BaseException as exc:
            state.update(passed=False, cleanup_error_type=type(exc).__name__)
        write_json_atomic(output / "lifecycle.json", state)
    if not state["passed"]:
        raise SystemExit(1)
    print("Local test passed; isolated containers removed", flush=True)


if __name__ == "__main__":
    main()
